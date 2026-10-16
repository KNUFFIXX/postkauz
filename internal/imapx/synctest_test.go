package imapx

// Belege zu A01 N1 (Go 1.27: Timer-Kanäle sind immer synchron, die
// Einstellung asynctimerchan gibt es nicht mehr) und zum Nachtrag zu
// ADR-007 (Watchdog mit Obergrenze je Kommando). Die Tests laufen in einer
// testing/synctest-Blase mit künstlicher Uhr: Zeitpunkte sind exakt und
// die Tests dauern Millisekunden, auch wenn sie Minuten „warten".
// Netz: net.Pipe innerhalb der Blase (synctest verlangt, dass alles, was
// blockiert, aus der Blase kommt).

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/provider"
)

// pipeClient baut einen Client um das eine Ende eines net.Pipe (ohne
// IMAP); das andere Ende spielt den Server.
func pipeClient(timeout, maxDur time.Duration) (*Client, net.Conn, net.Conn) {
	cli, srv := net.Pipe()
	return &Client{timeout: timeout, maxDur: maxDur, act: &activityConn{Conn: cli}}, cli, srv
}

// readAll liest bis zum Fehler (Watchdog schließt die Verbindung).
func readAll(r io.Reader) error {
	buf := make([]byte, 64)
	for {
		if _, err := r.Read(buf); err != nil {
			return err
		}
	}
}

func TestWatchdogTimersSynctest(t *testing.T) {
	// Schweigen: Der Server schickt nichts; der Watchdog schließt exakt
	// nach CommandTimeout.
	t.Run("silence", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, cli, srv := pipeClient(30*time.Second, 10*time.Minute)
			defer srv.Close()
			start := time.Now()
			err := c.guard(func() error { return readAll(c.act) }, func() { _ = cli.Close() })
			if !errors.Is(err, provider.ErrConnection) || !strings.Contains(err.Error(), "did not answer within 30s") {
				t.Fatalf("err = %v", err)
			}
			if d := time.Since(start); d != 30*time.Second {
				t.Fatalf("watchdog fired after %s, want exactly 30s", d)
			}
		})
	})

	// Rinnsal: alle 20 s ein Byte. Ohne Obergrenze liefe das Kommando
	// ewig (Lücke a der Gegenprüfung); mit Obergrenze endet es exakt nach
	// CommandMaxDuration.
	t.Run("trickle hits the total limit", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, cli, srv := pipeClient(30*time.Second, 10*time.Minute)
			writer := make(chan struct{})
			defer func() { <-writer }() // Schreiber endet am geschlossenen Pipe
			go func() {
				defer close(writer)
				defer srv.Close()
				for {
					time.Sleep(20 * time.Second)
					if _, err := srv.Write([]byte{'x'}); err != nil {
						return
					}
				}
			}()
			start := time.Now()
			err := c.guard(func() error { return readAll(c.act) }, func() { _ = cli.Close() })
			if !errors.Is(err, provider.ErrConnection) || !strings.Contains(err.Error(), "did not finish within 10m0s") {
				t.Fatalf("err = %v", err)
			}
			if d := time.Since(start); d != 10*time.Minute {
				t.Fatalf("watchdog fired after %s, want exactly 10m", d)
			}
		})
	})

	// Rückruf: Zeit in einem Rückruf des Aufrufers (Blob schreiben, parsen)
	// zählt nicht als Schweigen (Lücke b); liest der Rückruf danach wieder
	// vom Server, zählt Schweigen ab Beginn des Lesens.
	t.Run("callback time is not silence", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, cli, srv := pipeClient(30*time.Second, 10*time.Minute)
			go func() {
				_, _ = srv.Write([]byte("abc"))
				_ = readAll(srv) // danach stumm, bis der Client schließt
				_ = srv.Close()
			}()
			start := time.Now()
			var readAt time.Time
			err := c.guard(func() error {
				return c.callback(c.act, func(r io.Reader) error {
					buf := make([]byte, 3)
					if _, err := io.ReadFull(r, buf); err != nil {
						return err
					}
					time.Sleep(2 * time.Minute) // lokale Arbeit, liest nicht
					readAt = time.Now()
					return readAll(r)
				})
			}, func() { _ = cli.Close() })
			if !errors.Is(err, provider.ErrConnection) || !strings.Contains(err.Error(), "did not answer") {
				t.Fatalf("err = %v", err)
			}
			if d := readAt.Sub(start); d != 2*time.Minute {
				t.Fatalf("callback interrupted after %s", d)
			}
			if d := time.Since(readAt); d != 30*time.Second {
				t.Fatalf("silence detected %s after reading resumed, want 30s", d)
			}
		})
	})

	// Ein Kommando, das vor der Frist fertig ist, lässt keinen Timer und
	// keine Goroutine zurück (Stop per defer; synctest.Test scheitert,
	// wenn am Ende Goroutinen der Blase blockiert sind).
	t.Run("finished command stops the timer", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c, cli, srv := pipeClient(30*time.Second, 10*time.Minute)
			defer cli.Close()
			defer srv.Close()
			go func() { _, _ = srv.Write([]byte("ok")) }()
			closed := false
			err := c.guard(func() error {
				buf := make([]byte, 2)
				_, err := io.ReadFull(c.act, buf)
				return err
			}, func() { closed = true })
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Hour) // ein vergessener Timer würde jetzt zuschlagen
			if closed {
				t.Fatal("watchdog fired after the command had finished")
			}
		})
	})
}

// pipeListener ist ein net.Listener über net.Pipe (für die Blase).
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

func (l *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	cli, srv := net.Pipe()
	select {
	case l.conns <- srv:
		return cli, nil
	case <-l.closed:
		return nil, net.ErrClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// pipeServer startet den go-imap-Testserver in der Blase.
func pipeServer(t *testing.T) (*pipeListener, *imapmemserver.User, func()) {
	u := imapmemserver.NewUser(testUser, testPass)
	if err := u.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		InsecureAuth: true,
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIdle: {}},
	})
	ln := newPipeListener()
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()
	return ln, u, func() { _ = srv.Close(); <-done }
}

func pipeConfig(ln *pipeListener) Config {
	return Config{Host: "127.0.0.1", Port: 143, TLS: TLSNone, InsecureTestServer: true, dialContext: ln.dial,
		CommandTimeout: 30 * time.Second}
}

// IDLE-Neustart (ADR-007: alle 5 bis 10 min selbst neu starten): Ohne
// Änderung kehrt Idle exakt nach maxWait zurück; eine neue Nachricht meldet
// es sofort. Danach ist die Verbindung normal benutzbar.
func TestIdleRestartSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ln, u, stop := pipeServer(t)
		defer stop()
		c, err := Dial(context.Background(), pipeConfig(ln))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		if err := c.Login(testUser, testPass); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		changed, err := c.Idle(context.Background(), "INBOX", DefaultIdleRestart)
		if err != nil || changed {
			t.Fatalf("idle: changed=%v err=%v", changed, err)
		}
		if d := time.Since(start); d != DefaultIdleRestart {
			t.Fatalf("idle returned after %s, want exactly %s", d, DefaultIdleRestart)
		}
		go func() {
			time.Sleep(90 * time.Second)
			_, _ = u.Append("INBOX", literal(testMail(1)), &imap.AppendOptions{})
		}()
		start = time.Now()
		changed, err = c.Idle(context.Background(), "INBOX", DefaultIdleRestart)
		if err != nil || !changed {
			t.Fatalf("idle with append: changed=%v err=%v", changed, err)
		}
		if d := time.Since(start); d != 90*time.Second {
			t.Fatalf("change reported after %s, want 90s", d)
		}
		if uids, err := c.UIDSearch(0, SearchOptions{}); err != nil || len(uids) != 1 {
			t.Fatalf("after idle: %v %v", uids, err)
		}
	})
}

// Backoff (sleepCtx: ein Timer je Wartezeit, Stop per defer): Ohne Jitter
// (Rand = 0,5) wartet die Session 1 s, 2 s, 4 s zwischen vier Versuchen,
// zusammen exakt 7 s; ein Abbruch während des Wartens endet sofort.
func TestSessionBackoffSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dials := 0
		cfg := Config{Host: "127.0.0.1", Port: 143, TLS: TLSNone, InsecureTestServer: true,
			dialContext: func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("connection refused")
			}}
		s := NewSession(cfg, nil, SessionOptions{MaxAttempts: 4, Rand: func() float64 { return 0.5 },
			Backoff: Backoff{Initial: time.Second, Max: 5 * time.Minute, Factor: 2, Jitter: 0.2}})
		start := time.Now()
		err := s.Do(context.Background(), func(*Client) error { return nil })
		if !errors.Is(err, provider.ErrConnection) || dials != 4 {
			t.Fatalf("err=%v dials=%d", err, dials)
		}
		if d := time.Since(start); d != 7*time.Second {
			t.Fatalf("backoff took %s, want exactly 7s", d)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()
		start = time.Now()
		err = s.Do(ctx, func(*Client) error { return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("cancel during backoff: %v", err)
		}
		if d := time.Since(start); d != 1500*time.Millisecond {
			t.Fatalf("cancel took %s", d)
		}
	})
}
