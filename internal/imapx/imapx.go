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
// sowie IDLE mit eigenem Neustart (idle.go). Der Watchdog misst Schweigen,
// nicht Gesamtdauer: Er schlägt zu, wenn seit der Deadline kein Byte kam.
package imapx

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
	CommandTimeout     time.Duration
}

// Client ist eine angemeldete oder anmeldebereite Verbindung.
type Client struct {
	c       *imapclient.Client
	timeout time.Duration
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
}

func (a *activityConn) Read(p []byte) (int, error) {
	n, err := a.Conn.Read(p)
	if n > 0 {
		a.last.Store(time.Now().UnixNano())
	}
	return n, err
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
	raw, err := dialer.DialContext(dctx, "tcp", addr)
	if err != nil {
		return nil, connErr(err)
	}
	tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12, NextProtos: []string{"imap"},
		InsecureSkipVerify: cfg.InsecureTestServer} //nolint:gosec // nur localhost-Testserver, oben geprüft
	cl := &Client{timeout: cfg.CommandTimeout, changed: make(chan struct{}, 1)}
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

// guard ist der Watchdog: f läuft, bis es fertig ist; schweigt der Server
// länger als die Deadline (kein gelesenes Byte seit Start bzw. seit dem
// letzten Byte), wird die Verbindung über closeFn geschlossen und
// ErrConnection gemeldet.
func (c *Client) guard(f func() error, closeFn func()) error {
	start := time.Now()
	var timedOut atomic.Bool
	done := make(chan struct{})
	go func() {
		t := time.NewTimer(c.timeout)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				last := start
				if la := c.act.lastRead(); la.After(last) {
					last = la
				}
				if rem := c.timeout - time.Since(last); rem > 0 {
					t.Reset(rem)
					continue
				}
				timedOut.Store(true)
				closeFn()
				return
			}
		}
	}()
	err := f()
	close(done)
	if timedOut.Load() {
		return fmt.Errorf("%w: server did not answer within %s (watchdog closed the connection)", provider.ErrConnection, c.timeout)
	}
	return err
}

func (c *Client) run(f func() error) error {
	return c.guard(f, func() { _ = c.c.Close() })
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
