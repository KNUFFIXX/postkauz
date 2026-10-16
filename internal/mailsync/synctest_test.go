package mailsync

// Belege zu A01 N1 für internal/mailsync (Ticker des Dauerbetriebs, Timer
// der IDLE-Wiederholung) mit testing/synctest: künstliche Uhr, exakte
// Zeitpunkte. Remote und Store sind Attrappen ohne Netz und ohne SQLite
// (synctest verlangt, dass alles Blockierende aus der Blase kommt).

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// clockRemote zählt Läufe (ListMailboxes eröffnet jeden Lauf) mit ihrer
// Startzeit; slow verlängert einzelne Läufe, idle steuert IDLE.
type clockRemote struct {
	mu     sync.Mutex
	start  time.Time
	rounds []time.Duration
	slow   map[int]time.Duration // Lauf-Nr. (0-basiert) → Dauer
	idle   func(ctx context.Context, n int, notify chan<- struct{}) error
	idles  int
}

func (r *clockRemote) Capabilities() provider.Capabilities {
	return provider.Capabilities{Idle: r.idle != nil}
}

func (r *clockRemote) ListMailboxes(context.Context) ([]provider.Mailbox, error) {
	r.mu.Lock()
	n := len(r.rounds)
	r.rounds = append(r.rounds, time.Since(r.start))
	d := r.slow[n]
	r.mu.Unlock()
	if d > 0 {
		time.Sleep(d)
	}
	return nil, nil
}

func (r *clockRemote) Changes(context.Context, string, provider.SyncState, provider.ChangesOptions) (provider.ChangeSet, error) {
	return provider.ChangeSet{}, nil
}

func (r *clockRemote) FetchHeaders(context.Context, string, uint32, []provider.UID) ([]provider.Header, error) {
	return nil, nil
}

func (r *clockRemote) FetchRaw(context.Context, string, uint32, []provider.UID, func(provider.UID, io.Reader) error) error {
	return nil
}

func (r *clockRemote) Idle(ctx context.Context, _ string, notify chan<- struct{}) error {
	r.mu.Lock()
	n := r.idles
	r.idles++
	r.mu.Unlock()
	return r.idle(ctx, n, notify)
}

func (r *clockRemote) times() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.rounds)
}

// nopStore ist ein Store ohne Ordner und Nachrichten.
type nopStore struct{}

func (nopStore) SyncMailboxes(context.Context, int64, []store.MailboxSpec) ([]string, []string, error) {
	return nil, nil, nil
}
func (nopStore) Mailboxes(context.Context, int64) ([]store.Mailbox, error) { return nil, nil }
func (nopStore) MailboxUIDs(context.Context, int64) ([]uint32, error)      { return nil, nil }
func (nopStore) LinkCount(context.Context, int64) (int, error)             { return 0, nil }
func (nopStore) CountPresentMessages(context.Context, int64) (int, error)  { return 0, nil }
func (nopStore) FindLinkCandidate(context.Context, int64, int64, string, int64) (int64, bool, error) {
	return 0, false, nil
}
func (nopStore) FindByContentHash(context.Context, int64, int64, string) (int64, bool, error) {
	return 0, false, nil
}
func (nopStore) ApplyBatch(context.Context, store.MirrorBatch) (store.BatchResult, error) {
	return store.BatchResult{}, nil
}

func durations(ds ...string) []time.Duration {
	out := make([]time.Duration, len(ds))
	for i, d := range ds {
		out[i], _ = time.ParseDuration(d)
	}
	return out
}

// Intervall per Ticker: Läufe exakt bei 0, 5, 10, 15 min. Ein Lauf, der
// länger als das Intervall dauert (hier 7 min ab 5 min), verschiebt den
// nächsten Lauf auf sein Ende (12 min: der fällige Tick wird beim nächsten
// Empfang sofort geliefert, wie vor Go 1.23); weitere verpasste Ticks
// verfallen, danach geht es im alten Raster weiter (15 min).
func TestWatchTickerSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &clockRemote{start: time.Now(), slow: map[int]time.Duration{1: 7 * time.Minute}}
		e := &Engine{Store: nopStore{}, Remote: r}
		ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
		defer cancel()
		if err := e.Watch(ctx, WatchOptions{Interval: 5 * time.Minute}); err != nil {
			t.Fatal(err)
		}
		want := durations("0s", "5m", "12m", "15m")
		if got := r.times(); !slices.Equal(got, want) {
			t.Fatalf("rounds at %v, want %v", got, want)
		}
	})
}

// IDLE: Eine Meldung löst sofort einen Lauf aus; nach einem
// Verbindungsfehler wartet die Schleife exakt IdleRetry (30 s), baut neu auf
// und synct einmal nach (Änderungen aus der Pause). Ein Anmeldefehler der
// IDLE-Verbindung beendet Watch, und Watch kehrt erst zurück, wenn die
// IDLE-Schleife beendet ist (synctest.Test schlägt sonst wegen blockierter
// Goroutinen fehl).
func TestWatchIdleRetrySynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &clockRemote{start: time.Now()}
		r.idle = func(ctx context.Context, n int, notify chan<- struct{}) error {
			switch n {
			case 0:
				time.Sleep(90 * time.Second)
				notify <- struct{}{}
				time.Sleep(30 * time.Second)
				return provider.ErrConnection
			case 1:
				time.Sleep(time.Minute)
				return provider.ErrAuth
			}
			<-ctx.Done()
			return nil
		}
		e := &Engine{Store: nopStore{}, Remote: r}
		err := e.Watch(context.Background(), WatchOptions{Interval: 5 * time.Minute})
		if !errors.Is(err, provider.ErrAuth) {
			t.Fatalf("Watch = %v, want ErrAuth", err)
		}
		// 0: Start; 1m30: IDLE-Meldung; 2m30: Wiederaufbau nach 30 s Pause.
		want := durations("0s", "1m30s", "2m30s")
		if got := r.times(); !slices.Equal(got, want) {
			t.Fatalf("rounds at %v, want %v", got, want)
		}
		if d := time.Since(r.start); d != 3*time.Minute+30*time.Second {
			t.Fatalf("Watch ended after %s, want 3m30s", d)
		}
	})
}

// Ein Anmeldefehler in einem Lauf beendet Watch; die IDLE-Schleife (die
// eine eigene Verbindung hält) endet mit, statt weiterzulaufen.
func TestWatchStopsIdleLoopOnRoundAuthError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		idleDone := make(chan struct{})
		r := &authRemote{clockRemote: clockRemote{start: time.Now()}}
		r.idle = func(ctx context.Context, _ int, _ chan<- struct{}) error {
			defer close(idleDone)
			<-ctx.Done()
			return nil
		}
		e := &Engine{Store: nopStore{}, Remote: r}
		if err := e.Watch(context.Background(), WatchOptions{Interval: 5 * time.Minute}); !errors.Is(err, provider.ErrAuth) {
			t.Fatalf("Watch = %v", err)
		}
		select {
		case <-idleDone:
		default:
			t.Fatal("IDLE loop still running after Watch returned")
		}
	})
}

// authRemote scheitert im zweiten Lauf mit einem Anmeldefehler.
type authRemote struct{ clockRemote }

func (a *authRemote) ListMailboxes(ctx context.Context) ([]provider.Mailbox, error) {
	_, _ = a.clockRemote.ListMailboxes(ctx)
	if len(a.times()) == 2 {
		return nil, provider.ErrAuth
	}
	return nil, nil
}
