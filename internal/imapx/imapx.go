// Package imapx ist die dünne eigene Schicht über github.com/emersion/go-imap/v2
// (ADR-007). Nichts außerhalb dieses Pakets importiert go-imap (`make guard`
// prüft das und dass v2.0.0-beta.8 gepinnt ist); ein Fork oder Wechsel der
// Bibliothek ändert nur diese Schicht.
//
// Auflagen aus ADR-007, Stand S1.3:
//
//   - Watchdog: Jedes Kommando hat eine Deadline; bei Überschreitung wird die
//     Verbindung geschlossen (go-imap kennt keine Kommando-Timeouts) und
//     ErrConnection gemeldet. Session baut die Verbindung danach mit
//     exponentiellem Backoff und Jitter neu auf (session.go).
//   - TCP-Keepalive über net.Dialer.KeepAliveConfig.
//   - WordDecoder immer mit go-message/charset (sonst bleiben Betreffzeilen
//     in windows-1252 oder iso-8859-1 roh).
//   - Klartext und unbestätigte Zertifikate nur für Testserver auf localhost.
//
// Fehlerklassen: ErrConnection (wiederholbar), ErrAuth (nie wiederholen),
// ErrRejected (Server lehnt ein Kommando mit NO/BAD ab; nicht wiederholen),
// ErrProtocol (unlesbare Antwort; Verbindung verwerfen, nicht wiederholen).
//
// Seit S1.4/S1.5: EXAMINE/SELECT, UID SEARCH, FETCH (Metadaten, Flags mit
// CHANGEDSINCE, Rohmail gestreamt), STATUS, APPEND und STORE (mailbox.go)
// sowie IDLE mit eigenem Neustart (idle.go).
//
// Watchdog (ADR-007, Nachtrag 16.10.2026): Er misst Schweigen und hat eine
// Obergrenze. Er schlägt zu, wenn der Server CommandTimeout lang kein Byte
// geschickt hat (Zeit in einem Rückruf des Aufrufers, z. B. Blob schreiben
// und parsen in FetchRaw, zählt nicht als Schweigen) oder wenn ein Kommando
// insgesamt länger als CommandMaxDuration läuft (Standard 10 min; ein
// Server, der ständig wenige Bytes schickt, hält ein Kommando so nicht
// beliebig lange offen).
//
// Timer (A01 N1, Go 1.27: Timer-Kanäle sind immer synchron, asynctimerchan
// gibt es nicht mehr): guard setzt seinen Timer nur nach einem Empfang aus
// t.C zurück und stoppt ihn per defer; idle.go und sleepCtx nutzen je einen
// Timer einmal und stoppen ihn per defer. Keine Stelle leert t.C von Hand
// oder verlässt sich auf einen gepufferten Wert; Tests mit testing/synctest
// belegen die Zeitpunkte (synctest_test.go).
package imapx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-sasl"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// TLSMode wie accounts.tls.
type TLSMode string

const (
	TLSImplicit TLSMode = "tls"
	TLSStartTLS TLSMode = "starttls"
	TLSNone     TLSMode = "none"
)

// Standardwerte des Watchdogs.
const (
	DefaultDialTimeout    = 15 * time.Second
	DefaultCommandTimeout = 30 * time.Second
	// DefaultCommandMaxDuration ist die Obergrenze je Kommando
	// (ADR-007, Nachtrag 16.10.2026).
	DefaultCommandMaxDuration = 10 * time.Minute
	// DefaultMaxMessageBytes begrenzt eine Rohmail in FetchRaw (A02 A4,
	// [imap] max_message_bytes); so groß ist auch ein Blob höchstens.
	DefaultMaxMessageBytes int64 = 256 << 20
)

// Config beschreibt eine Verbindung.
type Config struct {
	Host string
	Port int
	TLS  TLSMode
	// InsecureTestServer erlaubt Klartext und unbestätigte Zertifikate; nur
	// für Hosts auf dem eigenen Rechner (wird hier erneut geprüft).
	InsecureTestServer bool
	DialTimeout        time.Duration
	// CommandTimeout: so lange darf der Server während eines Kommandos
	// schweigen (0 = 30 s).
	CommandTimeout time.Duration
	// CommandMaxDuration: so lange darf ein Kommando insgesamt dauern
	// (0 = 10 min). Kleiner als CommandTimeout wird auf CommandTimeout
	// angehoben.
	CommandMaxDuration time.Duration
	// MaxMessageBytes begrenzt eine Rohmail in FetchRaw (0 = 256 MiB).
	// Eine größere Nachricht wird nicht gelesen; der Rückruf bekommt einen
	// Reader, der sofort ErrMessageTooLarge meldet (Fehler je Nachricht,
	// der Abruf der übrigen läuft weiter).
	MaxMessageBytes int64

	// dialContext ersetzt den TCP-Aufbau (Tests mit testing/synctest und
	// net.Pipe); nil = net.Dialer mit Keepalive.
	dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Client ist eine angemeldete oder anmeldebereite Verbindung.
type Client struct {
	c       *imapclient.Client
	timeout time.Duration // Schweigen (CommandTimeout)
	maxDur  time.Duration // Obergrenze je Kommando (CommandMaxDuration)
	maxMsg  int64         // größte Rohmail in FetchRaw (MaxMessageBytes)
	stopCtx func() bool
	act     *activityConn
	// changed wird bei unaufgeforderten Serverdaten (EXISTS, EXPUNGE,
	// FETCH FLAGS) angestoßen; IDLE wartet darauf (idle.go).
	changed chan struct{}
	// sel ist der zuletzt per SELECT/EXAMINE gewählte Ordner (mailbox.go).
	sel *SelectInfo
}

// activityConn merkt sich den Zeitpunkt des letzten gelesenen Bytes. Der
// Watchdog misst daran, ob der Server schweigt; ein langer FETCH über eine
// langsame Leitung bricht deshalb nicht ab, solange Daten fließen.
type activityConn struct {
	net.Conn
	last atomic.Int64 // UnixNano
	// busy > 0: Ein Rückruf des Aufrufers läuft (FetchRaw); das Lesen ruht
	// dann aus eigenem Grund und zählt nicht als Schweigen des Servers.
	busy atomic.Int32
}

func (a *activityConn) Read(p []byte) (int, error) {
	n, err := a.Conn.Read(p)
	if n > 0 {
		a.touch()
	}
	return n, err
}

func (a *activityConn) touch() { a.last.Store(time.Now().UnixNano()) }

func (a *activityConn) isBusy() bool { return a != nil && a.busy.Load() > 0 }

// callback ruft f als Rückruf des Aufrufers mit dem Datenstrom r auf. Die
// Zeit in f zählt nicht als Schweigen des Servers (wohl aber zur
// Obergrenze des Kommandos), ausgenommen die Zeit, in der f aus r liest:
// Dann wartet f auf den Server, und Schweigen zählt ab Beginn des Lesens.
func (c *Client) callback(r io.Reader, f func(io.Reader) error) error {
	a := c.act
	if a == nil {
		return f(r)
	}
	a.busy.Add(1)
	defer func() {
		a.touch()
		a.busy.Add(-1)
	}()
	return f(serverReader{r: r, a: a})
}

// serverReader hebt während eines Lesevorgangs die Rückruf-Ausnahme auf.
type serverReader struct {
	r io.Reader
	a *activityConn
}

func (s serverReader) Read(p []byte) (int, error) {
	s.a.touch()
	s.a.busy.Add(-1)
	defer s.a.busy.Add(1)
	return s.r.Read(p)
}

func (a *activityConn) lastRead() time.Time {
	if a == nil {
		return time.Time{}
	}
	if v := a.last.Load(); v != 0 {
		return time.Unix(0, v)
	}
	return time.Time{}
}

// WordDecoder liefert den RFC-2047-Decoder mit Zeichensatz-Unterstützung
// (Auflage 3 aus ADR-007).
func WordDecoder() *mime.WordDecoder {
	return &mime.WordDecoder{CharsetReader: charset.Reader}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ErrRejected: Der Server hat ein Kommando mit NO oder BAD abgelehnt. Die
// Verbindung ist in Ordnung; ein Wiederholen ändert nichts.
var ErrRejected = errors.New("imapx: command rejected by server")

func connErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, provider.ErrConnection) || errors.Is(err, provider.ErrAuth) || errors.Is(err, ErrRejected) ||
		errors.Is(err, provider.ErrProtocol) {
		return err
	}
	// go-imap meldet unlesbare Antworten als Fehler des Decoders
	// („imapwire: …"); sie sind kein Netzproblem und kommen beim Wiederholen
	// genauso (z. B. GreenMail mit falscher Literal-Länge bei 8-Bit-Köpfen).
	if strings.Contains(err.Error(), "imapwire:") {
		return fmt.Errorf("%w: %v", provider.ErrProtocol, err)
	}
	return fmt.Errorf("%w: %v", provider.ErrConnection, err)
}

// cmdErr unterscheidet Ablehnungen (NO/BAD, ErrRejected) von
// Verbindungsfehlern. Nur Typ und Code des Servers werden weitergegeben,
// nicht sein Freitext.
func cmdErr(err error) error {
	var ie *imap.Error
	if errors.As(err, &ie) {
		code := string(ie.Code)
		if code == "" {
			code = "-"
		}
		return fmt.Errorf("%w (%s, code %s)", ErrRejected, ie.Type, code)
	}
	return connErr(err)
}

// Dial baut die Verbindung auf (TCP mit Keepalive, TLS bzw. STARTTLS) und
// wartet auf den Gruß des Servers, alles unter Deadline. ctx bricht auch
// spätere Kommandos ab (Verbindung wird geschlossen).
func Dial(ctx context.Context, cfg Config) (*Client, error) {
	return dial(ctx, ctx, cfg)
}

// dial trennt den Kontext des Verbindungsaufbaus (dialCtx) von dem der
// Lebensdauer (lifeCtx); Session hält Verbindungen über einzelne Aufrufe
// hinaus.
func dial(dialCtx, lifeCtx context.Context, cfg Config) (*Client, error) {
	ctx := dialCtx
	if cfg.DialTimeout <= 0 {
		cfg.DialTimeout = DefaultDialTimeout
	}
	if cfg.CommandTimeout <= 0 {
		cfg.CommandTimeout = DefaultCommandTimeout
	}
	if cfg.CommandMaxDuration <= 0 {
		cfg.CommandMaxDuration = DefaultCommandMaxDuration
	}
	if cfg.CommandMaxDuration < cfg.CommandTimeout {
		cfg.CommandMaxDuration = cfg.CommandTimeout
	}
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if (cfg.TLS == TLSNone || cfg.InsecureTestServer) && !isLoopback(cfg.Host) {
		return nil, fmt.Errorf("imapx: plain text or unverified TLS is only allowed for localhost test servers")
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dialer := &net.Dialer{
		Timeout:         cfg.DialTimeout,
		KeepAliveConfig: net.KeepAliveConfig{Enable: true, Idle: 60 * time.Second, Interval: 15 * time.Second, Count: 4},
	}
	dctx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	dialFn := dialer.DialContext
	if cfg.dialContext != nil {
		dialFn = cfg.dialContext
	}
	raw, err := dialFn(dctx, "tcp", addr)
	if err != nil {
		return nil, connErr(err)
	}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, NextProtos: []string{"imap"},
		InsecureSkipVerify: cfg.InsecureTestServer} //nolint:gosec // nur localhost-Testserver, oben geprüft
	cl := &Client{timeout: cfg.CommandTimeout, maxDur: cfg.CommandMaxDuration, maxMsg: cfg.MaxMessageBytes, changed: make(chan struct{}, 1)}
	opts := &imapclient.Options{WordDecoder: WordDecoder(), TLSConfig: tlsCfg,
		UnilateralDataHandler: cl.unilateralHandler()}

	var conn net.Conn = raw
	if cfg.TLS == TLSImplicit {
		tc := tls.Client(raw, tlsCfg)
		if err := tc.HandshakeContext(dctx); err != nil {
			_ = raw.Close()
			return nil, connErr(fmt.Errorf("TLS handshake: %w", err))
		}
		conn = tc
	}
	cl.act = &activityConn{Conn: conn}
	conn = cl.act

	err = cl.guard(func() error {
		var err error
		switch cfg.TLS {
		case TLSStartTLS:
			cl.c, err = imapclient.NewStartTLS(conn, opts)
			return err
		case TLSImplicit, TLSNone:
			cl.c = imapclient.New(conn, opts)
			return cl.c.WaitGreeting()
		default:
			return fmt.Errorf("imapx: unknown TLS mode %q", cfg.TLS)
		}
	}, func() { _ = conn.Close() })
	if err != nil {
		_ = conn.Close()
		return nil, connErr(err)
	}
	cl.stopCtx = context.AfterFunc(lifeCtx, func() { _ = cl.c.Close() })
	return cl, nil
}

// Gründe, aus denen der Watchdog zuschlägt.
const (
	watchSilence = 1 // Server hat CommandTimeout lang geschwiegen
	watchTotal   = 2 // Kommando lief länger als CommandMaxDuration
)

// watchdogDue liefert, wie lange der Watchdog bis zur nächsten Prüfung
// warten darf, oder (0, Grund), wenn er jetzt zuschlagen muss.
func (c *Client) watchdogDue(start, now time.Time) (time.Duration, int) {
	totalLeft := c.maxDur - now.Sub(start)
	if c.maxDur > 0 && totalLeft <= 0 {
		return 0, watchTotal
	}
	if c.maxDur <= 0 {
		totalLeft = c.timeout
	}
	if c.act.isBusy() {
		// Rückruf läuft: kein Schweigen; später wieder prüfen.
		return min(c.timeout, totalLeft), 0
	}
	last := start
	if la := c.act.lastRead(); la.After(last) {
		last = la
	}
	silentLeft := c.timeout - now.Sub(last)
	if silentLeft <= 0 {
		return 0, watchSilence
	}
	return min(silentLeft, totalLeft), 0
}

// guard ist der Watchdog: f läuft, bis es fertig ist. Schweigt der Server
// länger als CommandTimeout (kein gelesenes Byte seit Start bzw. seit dem
// letzten Byte; Rückrufe ausgenommen) oder dauert f insgesamt länger als
// CommandMaxDuration, wird die Verbindung über closeFn geschlossen und
// ErrConnection gemeldet.
//
// Timer-Modell (Go 1.27, synchrone Timer-Kanäle): Der Timer wird nur nach
// einem Empfang aus t.C per Reset neu gestellt und per defer gestoppt; es
// gibt kein Leeren des Kanals und keinen gepufferten Wert, auf den sich
// der Code verlässt (Belege: synctest_test.go).
func (c *Client) guard(f func() error, closeFn func()) error {
	start := time.Now()
	var reason atomic.Int32
	done := make(chan struct{})
	first, _ := c.watchdogDue(start, start)
	// Der Watchdog läuft unter crash.Go (A02 A5). Endet er durch einen
	// Panic, schließt er vorher die Verbindung (f hinge sonst ohne Aufsicht)
	// und guard meldet den *crash.PanicError.
	wd := crash.Go("IMAP watchdog", func() error {
		ok := false
		defer func() {
			if !ok {
				closeFn()
			}
		}()
		t := time.NewTimer(first)
		defer t.Stop()
		for {
			select {
			case <-done:
				ok = true
				return nil
			case <-t.C:
				wait, why := c.watchdogDue(start, time.Now())
				if why == 0 {
					t.Reset(wait)
					continue
				}
				reason.Store(int32(why))
				ok = true
				closeFn()
				return nil
			}
		}
	})
	err := f()
	close(done)
	if werr := <-wd; werr != nil {
		return werr
	}
	switch reason.Load() {
	case watchSilence:
		return fmt.Errorf("%w: server did not answer within %s (watchdog closed the connection)", provider.ErrConnection, c.timeout)
	case watchTotal:
		return fmt.Errorf("%w: command did not finish within %s (watchdog closed the connection)", provider.ErrConnection, c.maxDur)
	}
	return err
}

func (c *Client) run(f func() error) error {
	err := c.guard(f, func() { _ = c.c.Close() })
	if err != nil && errors.Is(err, net.ErrClosed) {
		// Wettlauf in go-imap (gemessen 22.10.2026, 13 von 4.000 Läufen):
		// Findet der Lese-Goroutine eine unlesbare Antwort, schließt sie die
		// Verbindung; schreibt das Kommando gerade, endet es mit „use of
		// closed network connection" statt mit dem Decoder-Fehler. Close
		// wartet auf die Lese-Goroutine und liefert deren Fehler: Ist es ein
		// Decoder-Fehler („imapwire:"), gilt er (ErrProtocol, kein
		// Wiederholen). Die Verbindung ist ohnehin schon zu.
		if cerr := c.c.Close(); cerr != nil && strings.Contains(cerr.Error(), "imapwire:") {
			return cerr
		}
	}
	return err
}

// authErr unterscheidet abgelehnte Anmeldung (ErrAuth, nicht wiederholen)
// von Verbindungsfehlern.
func authErr(err error) error {
	if err == nil {
		return nil
	}
	var ie *imap.Error
	if errors.As(err, &ie) && (ie.Type == imap.StatusResponseTypeNo || ie.Type == imap.StatusResponseTypeBad) {
		// Der Servertext kann den Nutzernamen enthalten, nie das Passwort;
		// trotzdem nur den Code und eine feste Meldung weitergeben.
		code := string(ie.Code)
		if code == "" {
			code = string(ie.Type)
		}
		return fmt.Errorf("%w (server: %s)", provider.ErrAuth, code)
	}
	return connErr(err)
}

// Login meldet sich mit Nutzername und Passwort an (IMAP LOGIN).
func (c *Client) Login(user string, password secrets.Secret) error {
	return authErr(c.run(func() error { return c.c.Login(user, password.Reveal()).Wait() }))
}

// Authenticate meldet sich per SASL an (z. B. XOAUTH2/OAUTHBEARER, siehe sasl.go).
func (c *Client) Authenticate(sc sasl.Client) error {
	return authErr(c.run(func() error { return c.c.Authenticate(sc) }))
}

// Caps liefert die Fähigkeiten (nach dem Login neu abgefragt), sortiert.
func (c *Client) Caps() ([]string, error) {
	var set imap.CapSet
	err := c.run(func() error {
		var err error
		set, err = c.c.Capability().Wait()
		return err
	})
	if err != nil {
		return nil, cmdErr(err)
	}
	out := make([]string, 0, len(set))
	for cp := range set {
		out = append(out, string(cp))
	}
	sort.Strings(out)
	return out, nil
}

// MailboxInfo ist ein Ordner aus LIST.
type MailboxInfo struct {
	Path   string         // UTF-8 (IMAP-UTF-7 dekodiert die Bibliothek)
	Delim  string         // Trennzeichen, "" wenn flach
	Attrs  []string       // z. B. \Sent, \Noselect
	Status *MailboxStatus // nur mit ListOptions.Status und LIST-STATUS
}

// MailboxStatus ist der Zustand eines Ordners aus STATUS bzw. LIST-STATUS.
type MailboxStatus struct {
	Messages      uint32
	Unseen        uint32
	UIDNext       uint32
	UIDValidity   uint32
	HighestModSeq uint64 // 0 = unbekannt (ohne CONDSTORE)
}

// ListOptions steuert LIST. Der Aufrufer setzt nur, was der Server laut
// CAPABILITY kann (SPECIAL-USE bzw. LIST-STATUS/IMAP4rev2, CONDSTORE).
type ListOptions struct {
	SpecialUse    bool // RETURN (SPECIAL-USE): Rollen-Attribute anfordern
	Status        bool // RETURN (STATUS (MESSAGES UNSEEN UIDNEXT UIDVALIDITY))
	HighestModSeq bool // zusätzlich HIGHESTMODSEQ im STATUS (CONDSTORE)
}

// List liefert alle Ordner (LIST "" "*"). Manche Server nennen
// SPECIAL-USE-Attribute nur mit ListOptions.SpecialUse (Stalwart).
func (c *Client) List() ([]MailboxInfo, error) { return c.ListWith(ListOptions{}) }

// ListWith liefert alle Ordner mit den gewünschten Zusatzangaben.
func (c *Client) ListWith(o ListOptions) ([]MailboxInfo, error) {
	var opts *imap.ListOptions
	if o.SpecialUse || o.Status {
		opts = &imap.ListOptions{ReturnSpecialUse: o.SpecialUse}
		if o.Status {
			opts.ReturnStatus = &imap.StatusOptions{NumMessages: true, NumUnseen: true, UIDNext: true,
				UIDValidity: true, HighestModSeq: o.HighestModSeq}
		}
	}
	var data []*imap.ListData
	err := c.run(func() error {
		var err error
		data, err = c.c.List("", "*", opts).Collect()
		return err
	})
	if err != nil {
		return nil, cmdErr(err)
	}
	out := make([]MailboxInfo, 0, len(data))
	for _, d := range data {
		mi := MailboxInfo{Path: d.Mailbox}
		if d.Delim != 0 {
			mi.Delim = string(d.Delim)
		}
		for _, a := range d.Attrs {
			mi.Attrs = append(mi.Attrs, string(a))
		}
		if st := d.Status; st != nil {
			ms := &MailboxStatus{UIDNext: uint32(st.UIDNext), UIDValidity: st.UIDValidity, HighestModSeq: st.HighestModSeq}
			if st.NumMessages != nil {
				ms.Messages = *st.NumMessages
			}
			if st.NumUnseen != nil {
				ms.Unseen = *st.NumUnseen
			}
			mi.Status = ms
		}
		out = append(out, mi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Create legt einen Ordner an; ein schon vorhandener Ordner ist kein Fehler.
// Gebraucht zum Befüllen der Testserver (Korpus) und später für Entwürfe.
func (c *Client) Create(path string) error {
	err := c.run(func() error { return c.c.Create(path, nil).Wait() })
	var ie *imap.Error
	if !errors.As(err, &ie) {
		return connErr(err)
	}
	if ie.Code == imap.ResponseCodeAlreadyExists {
		return nil
	}
	// Manche Server (GreenMail) antworten ohne ALREADYEXISTS: nachsehen.
	var found []*imap.ListData
	if lerr := c.run(func() error {
		var err error
		found, err = c.c.List("", path, nil).Collect()
		return err
	}); lerr == nil && len(found) == 1 && found[0].Mailbox == path {
		return nil
	}
	return cmdErr(err)
}

// Noop prüft, ob die Verbindung lebt (mit Watchdog).
func (c *Client) Noop() error {
	return cmdErr(c.run(func() error { return c.c.Noop().Wait() }))
}

// closeNow schließt die Verbindung sofort, ohne LOGOUT (Watchdog, Abbruch).
func (c *Client) closeNow() {
	if c.stopCtx != nil {
		c.stopCtx()
	}
	_ = c.c.Close()
}

// Close meldet sich ab (mit Deadline) und schließt die Verbindung.
func (c *Client) Close() error {
	if c.stopCtx != nil {
		c.stopCtx()
	}
	_ = c.run(func() error { return c.c.Logout().Wait() })
	_ = c.c.Close() // nach LOGOUT meist schon geschlossen
	return nil
}
