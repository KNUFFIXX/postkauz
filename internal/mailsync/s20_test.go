package mailsync

// Tests zu A02, Teil A (S2.0): voller UID-Abgleich im Dauerbetrieb (A1).

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// brokenMail hat eine Kopfzeile ohne Doppelpunkt: Der go-imap-Testserver
// liefert dafür einen leeren BODY[] (memSkip), der Sync zählt sie als
// Fehler und spiegelt sie nicht. Lokal fehlt dann eine Nachricht, die der
// Server führt; genau dann verdeckt der Zahlenvergleich eine Löschung.
var brokenMail = []byte("Message-ID: <broken@example.org>\r\nFrom: a@example.org\r\nThis line has no colon\r\n" +
	"Subject: kaputt\r\n\r\nText\r\n")

// A02 A1 (Frage 6a): Löschung und neue Nachricht in derselben Runde,
// während lokal eine Nachricht fehlt (Fehler beim Spiegeln). Der Server
// meldet so viele Nachrichten wie „lokal bekannt + neu"; der einmalige
// Sync gleicht deshalb keine UID-Liste ab und übersieht die Löschung
// (unverändert). sync --watch erkennt sie spätestens beim vollen Abgleich
// nach FullCheckInterval.
func TestWatchFullCheckFindsDeletionHiddenByCount(t *testing.T) {
	addr, u := memServer(t, false)
	for i := 1; i <= 10; i++ {
		_, _ = u.Append("INBOX", lit(mail(i)), &imap.AppendOptions{})
	}
	_, _ = u.Append("INBOX", lit(brokenMail), &imap.AppendOptions{})
	e := newEnv(t, addr)
	eng := e.engine(t, connect(t, addr, 0))
	if rep, err := eng.SyncAll(context.Background(), Options{}); err != nil || rep.Mirrored != 10 || rep.Errors != 1 {
		t.Fatalf("initial = %+v %v", rep, err)
	}

	deleted := func() int {
		return e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 5' AND deleted_remote_at IS NOT NULL`)
	}
	change := func(del, add int) {
		out := imapSession(t, addr)
		uid := uidOf(t, out, "INBOX", "Nachricht "+strconv.Itoa(del))
		if err := out.Store(imap.UIDSetNum(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
			t.Fatal(err)
		}
		if err := out.UIDExpunge(imap.UIDSetNum(uid)).Close(); err != nil {
			t.Fatal(err)
		}
		_, _ = u.Append("INBOX", lit(mail(add)), &imap.AppendOptions{})
	}

	// Einmaliger Sync: Lücke wie bisher (der Zahlenvergleich passt).
	change(5, 11)
	rep, err := eng.SyncAll(context.Background(), Options{})
	if err != nil || rep.New != 1 || rep.Removed != 0 || deleted() != 0 {
		t.Fatalf("one-shot sync after delete+new = %+v %v (deleted %d); the count check is expected to miss it", rep, err, deleted())
	}

	// Dauerbetrieb: erster Lauf (voller Abgleich), dann dieselbe Lage noch
	// einmal erzeugen; der nächste volle Abgleich erkennt beide Löschungen.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rounds atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- eng.Watch(ctx, WatchOptions{Interval: 100 * time.Millisecond, FullCheckInterval: 600 * time.Millisecond,
			OnRound: func(Report, error) { rounds.Add(1) }})
	}()
	waitFor(t, 5*time.Second, func() bool { return deleted() == 1 })
	waitFor(t, 5*time.Second, func() bool { return rounds.Load() >= 2 })
	change(6, 12)
	waitFor(t, 10*time.Second, func() bool {
		return e.count(t, `SELECT count(*) FROM messages WHERE subject = 'Nachricht 6' AND deleted_remote_at IS NOT NULL`) == 1
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE subject IN ('Nachricht 11', 'Nachricht 12')`); n != 2 {
		t.Fatalf("new messages = %d", n)
	}
}

// checkRemote merkt sich je Aufruf von Changes, ob CheckDeleted gesetzt war.
type checkRemote struct {
	clockRemote
	mu     sync.Mutex
	checks []time.Duration // Zeitpunkte (seit start) mit CheckDeleted
	calls  int
}

func (r *checkRemote) Changes(_ context.Context, _ string, _ provider.SyncState, o provider.ChangesOptions) (provider.ChangeSet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if o.CheckDeleted {
		r.checks = append(r.checks, time.Since(r.start))
	}
	return provider.ChangeSet{}, nil
}

// oneBoxStore ist nopStore mit einem Ordner.
type oneBoxStore struct{ nopStore }

func (oneBoxStore) Mailboxes(context.Context, int64) ([]store.Mailbox, error) {
	return []store.Mailbox{{ID: 1, Path: "INBOX", Phase: store.PhaseReady, UIDValidity: 1, UIDNext: 1}}, nil
}

// A02 A1, Zeitpunkte mit künstlicher Uhr: Intervall 5 min, voller Abgleich
// stündlich (Standard). Der erste Lauf gleicht voll ab, danach der erste
// Lauf ab einer Stunde nach dem letzten vollen Abgleich; IDLE-Läufe nie.
func TestWatchFullCheckIntervalSynctest(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := &checkRemote{clockRemote: clockRemote{start: time.Now()}}
		r.idle = func(ctx context.Context, n int, notify chan<- struct{}) error {
			if n == 0 {
				time.Sleep(62 * time.Minute) // IDLE-Meldung kurz nach einem fälligen Abgleich
				notify <- struct{}{}
			}
			<-ctx.Done()
			return nil
		}
		e := &Engine{Store: oneBoxStore{}, Remote: r}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour+time.Minute)
		defer cancel()
		if err := e.Watch(ctx, WatchOptions{Interval: 5 * time.Minute}); err != nil {
			t.Fatal(err)
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		want := durations("0s", "1h", "2h")
		if !slices.Equal(r.checks, want) {
			t.Fatalf("full checks at %v, want %v (of %d Changes calls)", r.checks, want, r.calls)
		}
		// 25 Läufe über alle Ordner (0 bis 120 min) plus ein IDLE-Lauf.
		if r.calls != 26 {
			t.Fatalf("Changes calls = %d, want 26", r.calls)
		}
	})
}
