package imapx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// blackholeProxy leitet TCP an backend weiter. Im Blackhole-Modus nimmt er
// Verbindungen an und hält sie offen, leitet aber nichts mehr weiter (wie
// eine stumm gewordene Gegenstelle oder ein Router, der Pakete verwirft).
type blackholeProxy struct {
	ln      net.Listener
	backend string
	hole    atomic.Bool
	mu      sync.Mutex
	conns   []net.Conn
}

func newBlackholeProxy(t *testing.T, backend string) *blackholeProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &blackholeProxy{ln: ln, backend: backend}
	go p.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		p.mu.Lock()
		for _, c := range p.conns {
			_ = c.Close()
		}
		p.mu.Unlock()
	})
	return p
}

func (p *blackholeProxy) track(c net.Conn) {
	p.mu.Lock()
	p.conns = append(p.conns, c)
	p.mu.Unlock()
}

func (p *blackholeProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.track(c)
		if p.hole.Load() {
			go func() { _, _ = io.Copy(io.Discard, c) }() // annehmen, schweigen
			continue
		}
		b, err := net.Dial("tcp", p.backend)
		if err != nil {
			_ = c.Close()
			continue
		}
		p.track(b)
		go p.pipe(c, b)
		go p.pipe(b, c)
	}
}

func (p *blackholeProxy) pipe(dst, src net.Conn) {
	buf := make([]byte, 4096)
	for {
		n, err := src.Read(buf)
		if n > 0 && !p.hole.Load() {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func loginFunc(c *Client) error { return c.Login(testUser, testPass) }

// Akzeptanz S1.3: Ein Server, der mitten in der Sitzung verstummt
// (Blackhole), wird innerhalb der konfigurierten Deadline erkannt.
func TestSessionDetectsSilentServerWithinDeadline(t *testing.T) {
	p := newBlackholeProxy(t, fakeServer(t, nil))
	cfg := cfgFor(t, p.ln.Addr().String())
	cfg.CommandTimeout = 300 * time.Millisecond
	s := NewSession(cfg, loginFunc, SessionOptions{MaxAttempts: 1})
	defer s.Close()
	ctx := context.Background()
	if err := s.Do(ctx, func(c *Client) error { _, err := c.List(); return err }); err != nil {
		t.Fatal(err)
	}
	p.hole.Store(true)
	start := time.Now()
	err := s.Do(ctx, func(c *Client) error { _, err := c.List(); return err })
	elapsed := time.Since(start)
	if !errors.Is(err, provider.ErrConnection) || !strings.Contains(err.Error(), "watchdog") {
		t.Fatalf("err = %v", err)
	}
	if elapsed < 300*time.Millisecond || elapsed > 1500*time.Millisecond {
		t.Fatalf("detected after %s, deadline 300ms", elapsed)
	}
}

// Nach dem Ausfall baut die Session die Verbindung mit Backoff neu auf.
func TestSessionReconnectsWithBackoff(t *testing.T) {
	p := newBlackholeProxy(t, fakeServer(t, nil))
	cfg := cfgFor(t, p.ln.Addr().String())
	cfg.CommandTimeout = 200 * time.Millisecond
	var delays []time.Duration
	s := NewSession(cfg, loginFunc, SessionOptions{
		MaxAttempts: 4,
		Backoff:     Backoff{Initial: 50 * time.Millisecond, Max: time.Second, Factor: 2, Jitter: 0.2},
		Rand:        func() float64 { return 0.5 }, // kein Jitter im Test
		Sleep: func(_ context.Context, d time.Duration) error {
			delays = append(delays, d)
			if len(delays) == 2 {
				p.hole.Store(false) // Server kommt nach dem zweiten Warten zurück
			}
			return nil
		},
	})
	defer s.Close()
	ctx := context.Background()
	list := func(c *Client) error { _, err := c.List(); return err }
	if err := s.Do(ctx, list); err != nil {
		t.Fatal(err)
	}
	p.hole.Store(true)
	if err := s.Do(ctx, list); err != nil {
		t.Fatalf("after reconnect: %v", err)
	}
	// Versuch 1 auf alter Verbindung, Versuch 2 neue Verbindung ins Leere,
	// Versuch 3 erfolgreich: 3 Verbindungsaufbauten insgesamt.
	if fmt.Sprint(delays) != "[50ms 100ms]" || s.Dials() != 3 {
		t.Fatalf("delays = %v, dials = %d", delays, s.Dials())
	}
}

func TestSessionDoesNotRetryAuthOrRejection(t *testing.T) {
	addr := fakeServer(t, nil)
	slept := 0
	opts := SessionOptions{Sleep: func(context.Context, time.Duration) error { slept++; return nil }}
	s := NewSession(cfgFor(t, addr), func(c *Client) error { return c.Login(testUser, "wrong") }, opts)
	if err := s.Do(context.Background(), func(*Client) error { return nil }); !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("err = %v", err)
	}
	if s.Dials() != 1 || slept != 0 {
		t.Fatalf("auth failure retried: dials=%d slept=%d", s.Dials(), slept)
	}

	s = NewSession(cfgFor(t, addr), loginFunc, opts)
	defer s.Close()
	err := s.Do(context.Background(), func(c *Client) error { return fmt.Errorf("%w (NO)", ErrRejected) })
	if !errors.Is(err, ErrRejected) || s.Dials() != 1 || slept != 0 {
		t.Fatalf("rejection retried: err=%v dials=%d slept=%d", err, s.Dials(), slept)
	}
	// Verbindung bleibt nach einer Ablehnung bestehen.
	if err := s.Do(context.Background(), func(c *Client) error { return c.Noop() }); err != nil || s.Dials() != 1 {
		t.Fatalf("noop: %v dials=%d", err, s.Dials())
	}
}

// Ein abgebrochener Kontext beendet auch ein hängendes Kommando sofort.
func TestSessionContextCancelAbortsHangingCommand(t *testing.T) {
	p := newBlackholeProxy(t, fakeServer(t, nil))
	cfg := cfgFor(t, p.ln.Addr().String())
	cfg.CommandTimeout = 10 * time.Second
	s := NewSession(cfg, loginFunc, SessionOptions{})
	defer s.Close()
	if err := s.Do(context.Background(), func(c *Client) error { return c.Noop() }); err != nil {
		t.Fatal(err)
	}
	p.hole.Store(true)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := s.Do(ctx, func(c *Client) error { return c.Noop() })
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
}

func TestSessionClosed(t *testing.T) {
	s := NewSession(cfgFor(t, fakeServer(t, nil)), loginFunc, SessionOptions{})
	_ = s.Close()
	if err := s.Do(context.Background(), func(*Client) error { return nil }); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("err = %v", err)
	}
}

func TestBackoffDelay(t *testing.T) {
	b := Backoff{Initial: time.Second, Max: 10 * time.Second, Factor: 2, Jitter: 0.2}
	mid := func() float64 { return 0.5 }
	for attempt, want := range map[int]time.Duration{1: time.Second, 2: 2 * time.Second, 3: 4 * time.Second, 4: 8 * time.Second, 5: 10 * time.Second, 50: 10 * time.Second} {
		if got := b.Delay(attempt, mid); got != want {
			t.Errorf("attempt %d: %s, want %s", attempt, got, want)
		}
	}
	lo, hi := b.Delay(1, func() float64 { return 0 }), b.Delay(1, func() float64 { return 0.999999 })
	if lo < 800*time.Millisecond || hi > 1200*time.Millisecond || lo >= hi {
		t.Errorf("jitter range %s..%s", lo, hi)
	}
	if d := (Backoff{}).Delay(1, mid); d != DefaultBackoff.Initial {
		t.Errorf("default = %s", d)
	}
}
