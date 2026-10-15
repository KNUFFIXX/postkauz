// Package imapx ist die dünne eigene Schicht über github.com/emersion/go-imap/v2
// (ADR-007). Nichts außerhalb dieses Pakets importiert go-imap; ein Fork
// oder Wechsel der Bibliothek ändert nur diese Schicht.
//
// Auflagen aus ADR-007, Stand S1.1 (Verbinden, Anmelden, Fähigkeiten, LIST):
//
//   - Watchdog: Jedes Kommando hat eine Deadline; bei Überschreitung wird die
//     Verbindung geschlossen (go-imap kennt keine Kommando-Timeouts). Der
//     Neuaufbau mit Backoff folgt mit dem Sync (S1.5).
//   - TCP-Keepalive über net.Dialer.KeepAliveConfig.
//   - WordDecoder immer mit go-message/charset (sonst bleiben Betreffzeilen
//     in windows-1252 oder iso-8859-1 roh).
//   - Klartext und unbestätigte Zertifikate nur für Testserver auf localhost.
//
// SELECT, FETCH, IDLE, APPEND und die Änderungserkennung folgen in S1.3–S1.5.
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

func connErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, provider.ErrConnection) || errors.Is(err, provider.ErrAuth) {
		return err
	}
	return fmt.Errorf("%w: %v", provider.ErrConnection, err)
}

// Dial baut die Verbindung auf (TCP mit Keepalive, TLS bzw. STARTTLS) und
// wartet auf den Gruß des Servers, alles unter Deadline. ctx bricht auch
// spätere Kommandos ab (Verbindung wird geschlossen).
func Dial(ctx context.Context, cfg Config) (*Client, error) {
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
	opts := &imapclient.Options{WordDecoder: WordDecoder(), TLSConfig: tlsCfg}

	var conn net.Conn = raw
	if cfg.TLS == TLSImplicit {
		tc := tls.Client(raw, tlsCfg)
		if err := tc.HandshakeContext(dctx); err != nil {
			_ = raw.Close()
			return nil, connErr(fmt.Errorf("TLS handshake: %w", err))
		}
		conn = tc
	}

	cl := &Client{timeout: cfg.CommandTimeout}
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
	cl.stopCtx = context.AfterFunc(ctx, func() { _ = cl.c.Close() })
	return cl, nil
}

// guard ist der Watchdog: f läuft mit Deadline; bei Überschreitung wird
// die Verbindung über closeFn geschlossen und ErrConnection gemeldet.
func (c *Client) guard(f func() error, closeFn func()) error {
	var timedOut atomic.Bool
	t := time.AfterFunc(c.timeout, func() { timedOut.Store(true); closeFn() })
	err := f()
	t.Stop()
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
		return nil, connErr(err)
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
	Path  string   // UTF-8 (IMAP-UTF-7 dekodiert die Bibliothek)
	Delim string   // Trennzeichen, "" wenn flach
	Attrs []string // z. B. \Sent, \Noselect
}

// List liefert alle Ordner (LIST "" "*"). Server mit SPECIAL-USE nennen die
// Rollen-Attribute dabei von sich aus (RFC 6154).
func (c *Client) List() ([]MailboxInfo, error) {
	var data []*imap.ListData
	err := c.run(func() error {
		var err error
		data, err = c.c.List("", "*", nil).Collect()
		return err
	})
	if err != nil {
		return nil, connErr(err)
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
		out = append(out, mi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
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
