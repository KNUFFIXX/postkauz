package imapx

import (
	"context"
	"errors"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// Backoff ist exponentiell mit Obergrenze und Jitter (docs/architektur.md,
// „Reconnect mit Backoff").
type Backoff struct {
	Initial time.Duration // erste Wartezeit (Standard 1 s)
	Max     time.Duration // Obergrenze (Standard 5 min)
	Factor  float64       // Wachstum je Versuch (Standard 2)
	Jitter  float64       // Anteil Zufall, 0..1 (Standard 0,2 = ±20 %)
}

// DefaultBackoff sind die Standardwerte.
var DefaultBackoff = Backoff{Initial: time.Second, Max: 5 * time.Minute, Factor: 2, Jitter: 0.2}

func (b Backoff) withDefaults() Backoff {
	d := DefaultBackoff
	if b.Initial > 0 {
		d.Initial = b.Initial
	}
	if b.Max > 0 {
		d.Max = b.Max
	}
	if b.Factor >= 1 {
		d.Factor = b.Factor
	}
	if b.Jitter >= 0 && b.Jitter <= 1 && (b != Backoff{}) {
		d.Jitter = b.Jitter
	}
	return d
}

// Delay ist die Wartezeit vor dem Versuch attempt (1 = erster Neuversuch).
// rnd liefert Werte in [0,1); nil = math/rand.
func (b Backoff) Delay(attempt int, rnd func() float64) time.Duration {
	b = b.withDefaults()
	if rnd == nil {
		rnd = rand.Float64
	}
	d := float64(b.Initial)
	for i := 1; i < attempt && d < float64(b.Max); i++ {
		d *= b.Factor
	}
	if d > float64(b.Max) {
		d = float64(b.Max)
	}
	d *= 1 + b.Jitter*(2*rnd()-1)
	return time.Duration(d)
}

// LoginFunc meldet eine frische Verbindung an (LOGIN oder SASL).
type LoginFunc func(*Client) error

// SessionOptions steuert Session.
type SessionOptions struct {
	Backoff Backoff
	// MaxAttempts begrenzt die Versuche je Do (Standard 5). Danach meldet Do
	// den letzten Verbindungsfehler; der nächste Do beginnt neu.
	MaxAttempts int
	// Rand und Sleep ersetzen Zufall und Warten (Tests).
	Rand  func() float64
	Sleep func(ctx context.Context, d time.Duration) error
}

// Session hält eine angemeldete Verbindung und baut sie bei
// Verbindungsfehlern (auch Watchdog-Abbrüchen) mit Backoff neu auf.
// Anmeldefehler (ErrAuth) und Ablehnungen (ErrRejected) werden nie
// wiederholt. Aufrufe von Do laufen nacheinander.
type Session struct {
	cfg   Config
	login LoginFunc
	opts  SessionOptions

	mu     sync.Mutex
	c      *Client
	closed bool

	dials atomic.Int64
}

// NewSession legt eine Session an; verbunden wird beim ersten Do.
func NewSession(cfg Config, login LoginFunc, opts SessionOptions) *Session {
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 5
	}
	if opts.Sleep == nil {
		opts.Sleep = sleepCtx
	}
	opts.Backoff = opts.Backoff.withDefaults()
	return &Session{cfg: cfg, login: login, opts: opts}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Dials zählt die Verbindungsaufbauten (für Tests und doctor).
func (s *Session) Dials() int64 { return s.dials.Load() }

// ErrSessionClosed: Do nach Close.
var ErrSessionClosed = errors.New("imapx: session closed")

// connect liefert die bestehende oder eine neue, angemeldete Verbindung.
func (s *Session) connect(ctx context.Context) (*Client, error) {
	if s.c != nil {
		return s.c, nil
	}
	s.dials.Add(1)
	c, err := dial(ctx, context.Background(), s.cfg)
	if err != nil {
		return nil, err
	}
	if s.login != nil {
		if err := s.login(c); err != nil {
			c.closeNow()
			return nil, err
		}
	}
	s.c = c
	return c, nil
}

func retryable(err error) bool {
	return errors.Is(err, provider.ErrConnection) && !errors.Is(err, provider.ErrAuth)
}

// Do führt f mit einer angemeldeten Verbindung aus. Bei einem
// Verbindungsfehler wird die Verbindung geschlossen und f nach Backoff auf
// einer neuen Verbindung wiederholt; f muss deshalb wiederholbar sein:
// lesende Kommandos oder ein Schreibbefehl, dessen Aufrufer Idempotenz
// zusichert (APPEND mit Message-ID, der vor dem erneuten Versuch die Kopie
// sucht; internal/provider/imap, Append). Alles andere geht über DoOnce.
// Endet ctx, wird die laufende Verbindung geschlossen.
func (s *Session) Do(ctx context.Context, f func(*Client) error) error {
	return s.do(ctx, f, true)
}

// DoOnce führt f höchstens einmal aus (T1a Punkt 5): für Schreibbefehle,
// die beim Wiederholen doppelt wirken könnten (APPEND ohne Message-ID,
// STORE, MOVE). Nur der Verbindungsaufbau vor f wird mit Backoff
// wiederholt; endet f mit einem Verbindungsfehler, kommt er zurück, und
// der Aufrufer prüft beim nächsten Lauf, was angekommen ist.
func (s *Session) DoOnce(ctx context.Context, f func(*Client) error) error {
	return s.do(ctx, f, false)
}

func (s *Session) do(ctx context.Context, f func(*Client) error, repeat bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last error
	for attempt := 1; attempt <= s.opts.MaxAttempts; attempt++ {
		if s.closed {
			return ErrSessionClosed
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		c, err := s.connect(ctx)
		ran := false
		if err == nil {
			stop := context.AfterFunc(ctx, c.closeNow)
			ran = true
			err = f(c)
			if !stop() && ctx.Err() != nil {
				s.drop()
				return ctx.Err()
			}
		}
		if err == nil {
			return nil
		}
		if !retryable(err) {
			if errors.Is(err, provider.ErrAuth) || errors.Is(err, provider.ErrProtocol) {
				s.drop() // nach einer unlesbaren Antwort ist der Datenstrom aus dem Takt
			}
			return err
		}
		last = err
		s.drop()
		if ran && !repeat {
			return err
		}
		if attempt == s.opts.MaxAttempts {
			break
		}
		if err := s.opts.Sleep(ctx, s.opts.Backoff.Delay(attempt, s.opts.Rand)); err != nil {
			return err
		}
	}
	return last
}

// drop schließt die aktuelle Verbindung ohne LOGOUT.
func (s *Session) drop() {
	if s.c != nil {
		s.c.closeNow()
		s.c = nil
	}
}

// Close meldet sich ab und schließt die Session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.c != nil {
		_ = s.c.Close()
		s.c = nil
	}
	return nil
}
