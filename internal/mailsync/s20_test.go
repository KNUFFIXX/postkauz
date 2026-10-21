package mailsync

// Tests zu A02, Teil A (S2.0): voller UID-Abgleich im Dauerbetrieb (A1).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
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

// A02 A4 (Frage 6d): Eine Rohmail über [imap] max_message_bytes ist ein
// Fehler dieser Nachricht; der Sync spiegelt die übrigen und endet ohne
// Fehler (Testserver im Prozess, Grenze klein).
func TestSyncSkipsMessageOverMaxMessageBytes(t *testing.T) {
	addr, u := memServer(t, false)
	_, _ = u.Append("INBOX", lit(mail(1)), &imap.AppendOptions{})
	big := append(mail(2), bytes.Repeat([]byte("x"), 8192)...)
	_, _ = u.Append("INBOX", lit(big), &imap.AppendOptions{})
	_, _ = u.Append("INBOX", lit(mail(3)), &imap.AppendOptions{})
	e := newEnv(t, addr)
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	prov, err := imapprov.Connect(context.Background(), imapprov.Config{Host: host, Port: p, TLS: "none", Username: testUser,
		InsecureTestServer: true, CommandTimeout: 5 * time.Second, MaxMessageBytes: 4096}, testPass)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = prov.Close() })
	rep, err := e.engine(t, prov).SyncAll(context.Background(), Options{})
	if err != nil || rep.Mirrored != 2 || rep.Errors != 1 || len(rep.Mailboxes) != 1 {
		t.Fatalf("sync = %+v %v", rep, err)
	}
	if pr := rep.Mailboxes[0].Problems; len(pr) != 1 || !strings.Contains(pr[0], "uid 2:") || !strings.Contains(pr[0], "max_message_bytes") {
		t.Fatalf("problems = %v", pr)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE subject IN ('Nachricht 1', 'Nachricht 3')`); n != 2 {
		t.Fatalf("mirrored = %d", n)
	}
}

// recordRemote merkt sich, für welche UIDs Köpfe und Rohmails angefordert
// wurden (FetchRaw = UID FETCH BODY.PEEK[]).
type recordRemote struct {
	Remote
	mu      sync.Mutex
	headers []provider.UID
	raw     []provider.UID
}

func (r *recordRemote) FetchHeaders(ctx context.Context, mb string, uv uint32, uids []provider.UID) ([]provider.Header, error) {
	r.mu.Lock()
	r.headers = append(r.headers, uids...)
	r.mu.Unlock()
	return r.Remote.FetchHeaders(ctx, mb, uv, uids)
}

func (r *recordRemote) FetchRaw(ctx context.Context, mb string, uv uint32, uids []provider.UID, fn func(provider.UID, io.Reader) error) error {
	r.mu.Lock()
	r.raw = append(r.raw, uids...)
	r.mu.Unlock()
	return r.Remote.FetchRaw(ctx, mb, uv, uids, fn)
}

// reset liefert die bisher angeforderten UIDs und leert die Listen.
func (r *recordRemote) reset() (headers, raw []provider.UID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	headers, raw = r.headers, r.raw
	r.headers, r.raw = nil, nil
	return headers, raw
}

// Gegenprüfung 21.10.2026, Befund 2 (A4): Eine Nachricht über
// max_message_bytes wird gar nicht erst angefordert (Größe aus RFC822.SIZE
// der Köpfe), zählt einmal als Fehler, wird gemerkt und in folgenden Läufen
// nicht erneut angefordert, auch nicht beim Löschabgleich, der sie als
// „lokal fehlend" sähe. sync --full prüft sie erneut; mit höherer Grenze
// wird sie gespiegelt und die Liste leer.
func TestSyncDoesNotFetchMessageOverLimit(t *testing.T) {
	addr, u := memServer(t, false)
	_, _ = u.Append("INBOX", lit(mail(1)), &imap.AppendOptions{})
	big := append(mail(2), bytes.Repeat([]byte("x"), 8192)...)
	_, _ = u.Append("INBOX", lit(big), &imap.AppendOptions{})
	_, _ = u.Append("INBOX", lit(mail(3)), &imap.AppendOptions{})
	e := newEnv(t, addr)
	rr := &recordRemote{Remote: connect(t, addr, 0)}
	eng := e.engine(t, rr)
	eng.MaxMessageBytes = 4096
	const bigUID = 2

	rep, err := eng.SyncAll(context.Background(), Options{})
	if err != nil || rep.Mirrored != 2 || rep.Errors != 1 {
		t.Fatalf("first sync = %+v %v", rep, err)
	}
	if pr := rep.Mailboxes[0].Problems; len(pr) != 1 || !strings.Contains(pr[0], "uid 2:") || !strings.Contains(pr[0], "max_message_bytes") {
		t.Fatalf("problems = %v", pr)
	}
	if _, raw := rr.reset(); slices.Contains(raw, bigUID) || len(raw) != 2 {
		t.Fatalf("FetchRaw for %v; the large message must not be fetched", raw)
	}
	if n := e.count(t, `SELECT count(*) FROM skipped_messages WHERE uid = 2 AND reason = 'too_large' AND size > 8192`); n != 1 {
		t.Fatalf("skipped_messages = %d", n)
	}

	// Folgeläufe, auch mit Löschabgleich (vollständige UID-Liste): nichts
	// wird erneut angefordert, kein Fehler.
	for _, o := range []Options{{}, {CheckDeleted: true}} {
		rep, err = eng.SyncAll(context.Background(), o)
		if err != nil || rep.Errors != 0 || rep.Mirrored != 2 {
			t.Fatalf("next sync %+v = %+v %v", o, rep, err)
		}
		if headers, raw := rr.reset(); len(headers) != 0 || len(raw) != 0 {
			t.Fatalf("next sync %+v requested headers %v, raw %v", o, headers, raw)
		}
	}

	// sync --full prüft erneut: wieder ein Fehler, wieder kein Inhalt.
	rep, err = eng.SyncAll(context.Background(), Options{Full: true})
	if err != nil || rep.Errors != 1 {
		t.Fatalf("full sync = %+v %v", rep, err)
	}
	if headers, raw := rr.reset(); !slices.Contains(headers, bigUID) || slices.Contains(raw, bigUID) {
		t.Fatalf("full sync: headers %v, raw %v", headers, raw)
	}

	// Höhere Grenze: sync --full spiegelt sie, die Liste ist leer.
	eng.MaxMessageBytes = 0
	rep, err = eng.SyncAll(context.Background(), Options{Full: true})
	if err != nil || rep.Errors != 0 || rep.Mirrored != 3 {
		t.Fatalf("full sync with default limit = %+v %v", rep, err)
	}
	if n := e.count(t, `SELECT count(*) FROM skipped_messages`); n != 0 {
		t.Fatalf("skipped_messages after mirroring = %d", n)
	}

	// Löscht der Server eine gemerkte Nachricht, verschwindet der Eintrag.
	eng.MaxMessageBytes = 4096
	_, _ = u.Append("INBOX", lit(append(mail(4), bytes.Repeat([]byte("y"), 8192)...)), &imap.AppendOptions{})
	if rep, err = eng.SyncAll(context.Background(), Options{}); err != nil || rep.Errors != 1 {
		t.Fatalf("sync with a new large message = %+v %v", rep, err)
	}
	out := imapSession(t, addr)
	uid := uidOf(t, out, "INBOX", "Nachricht 4")
	if err := out.Store(imap.UIDSetNum(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.UIDExpunge(imap.UIDSetNum(uid)).Close(); err != nil {
		t.Fatal(err)
	}
	if rep, err = eng.SyncAll(context.Background(), Options{}); err != nil || rep.Errors != 0 || rep.Removed != 0 {
		t.Fatalf("sync after deleting the large message = %+v %v", rep, err)
	}
	if n := e.count(t, `SELECT count(*) FROM skipped_messages`); n != 0 {
		t.Fatalf("skipped_messages after server delete = %d", n)
	}
}

// A02 A5 (Frage 15): Ein Panic im IDLE-Watcher (eigene Goroutine) beendet
// den Lauf mit einem Fehler (ErrInternal, ohne Panic-Wert), nicht den
// Prozess; der bereinigte Bericht steht im Log. Vorher endete das Programm
// mit der Standardausgabe von Go (Panic-Wert, Stack mit Argumenten).
func TestWatchIdlePanicEndsRunWithError(t *testing.T) {
	const canary = "KANARIE-MAILTEXT-idle-3a9c"
	var logBuf bytes.Buffer
	crash.SetLogger(slog.New(slog.NewJSONHandler(&logBuf, nil)))
	defer crash.SetLogger(nil)
	synctest.Test(t, func(t *testing.T) {
		r := &clockRemote{start: time.Now()}
		r.idle = func(context.Context, int, chan<- struct{}) error {
			time.Sleep(time.Minute)
			panic("IDLE saw " + canary)
		}
		e := &Engine{Store: nopStore{}, Remote: r}
		err := e.Watch(context.Background(), WatchOptions{Interval: 5 * time.Minute})
		var pe *crash.PanicError
		if !errors.Is(err, crash.ErrInternal) || !errors.As(err, &pe) || pe.Name != "IDLE watcher" || strings.Contains(err.Error(), canary) {
			t.Fatalf("Watch = %v", err)
		}
		if d := time.Since(r.start); d != time.Minute {
			t.Fatalf("Watch ended after %s, want 1m (right after the panic)", d)
		}
	})
	if log := logBuf.String(); strings.Contains(log, canary) || !strings.Contains(log, `"task":"IDLE watcher"`) ||
		!strings.Contains(log, "internal/mailsync.(*Engine).idleLoop") {
		t.Fatalf("log:\n%s", log)
	}
}

// A02 A5: Ein Panic beim Spiegeln eines Ordners (hier im Abruf der
// Rohmails) beendet den Lauf mit Fehler, statt als Ordnerfehler
// weiterzulaufen; der Panic-Wert erscheint nirgends.
func TestSyncPanicInMailboxEndsRun(t *testing.T) {
	const canary = "KANARIE-MAILTEXT-window-8d2b"
	addr, u := memServer(t, false)
	_, _ = u.Append("INBOX", lit(mail(1)), &imap.AppendOptions{})
	_ = u.Create("Archiv", nil)
	_, _ = u.Append("Archiv", lit(mail(2)), &imap.AppendOptions{})
	e := newEnv(t, addr)
	pr := &panicRemote{Remote: connect(t, addr, 0), canary: canary}
	rep, err := e.engine(t, pr).SyncAll(context.Background(), Options{})
	if !errors.Is(err, crash.ErrInternal) || strings.Contains(err.Error(), canary) || !strings.Contains(err.Error(), "internal error in sync mailbox") {
		t.Fatalf("SyncAll = %+v %v", rep, err)
	}
	if len(rep.Mailboxes) != 1 || rep.Mailboxes[0].Path != "INBOX" || strings.Contains(strings.Join(rep.Mailboxes[0].Problems, " "), canary) {
		t.Fatalf("report = %+v (the run must stop after the first mailbox)", rep)
	}
}

// panicRemote löst beim Abruf der Rohmails einen Panic mit Inhalt aus.
type panicRemote struct {
	Remote
	canary string
}

func (p *panicRemote) FetchRaw(context.Context, string, uint32, []provider.UID, func(provider.UID, io.Reader) error) error {
	panic("raw message " + p.canary)
}
