package mailsync

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// newRequestEnv: Fake-Server mit INBOX und Archiv, Runner mit Job sync.
func newRequestEnv(t *testing.T) *pushEnv {
	t.Helper()
	e := newPushEnv(t, "Archiv")
	e.runner.Requests = ops.NewSyncJobs(ops.Deps{Drafts: e.svc})
	return e
}

func (e *pushEnv) syncJobState(t *testing.T, id int64) string {
	t.Helper()
	s, _, ok, err := e.st.SyncJobState(context.Background(), id)
	if err != nil || !ok {
		t.Fatalf("job %d: %v %v", id, ok, err)
	}
	return s
}

func (e *pushEnv) mirrored(t *testing.T) int {
	t.Helper()
	n, err := e.st.CountPresentMessages(context.Background(), e.acct)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// watch startet sync --watch (Intervall 1 h, Jobs alle 50 ms) und zählt
// die Läufe; stop beendet ihn und liefert seinen Fehler.
func (e *pushEnv) watch(t *testing.T) (rounds *atomic.Int32, requests chan string, stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	rounds, requests = &atomic.Int32{}, make(chan string, 8)
	done := make(chan error, 1)
	go func() {
		_, err := e.runner.Sync(ctx, e.account(t), provider.PasswordCredential(testPass), ops.SyncOptions{Watch: true,
			Interval: time.Hour, JobsInterval: 50 * time.Millisecond}, func(p ops.Progress) {
			switch p.Event {
			case "round":
				rounds.Add(1)
			case "sync_request":
				requests <- p.Message
			}
		})
		done <- err
	}()
	waitFor(t, 5*time.Second, func() bool { return rounds.Load() == 1 })
	return rounds, requests, func() error { cancel(); return <-done }
}

// T2: Ein laufendes sync --watch führt einen angeforderten Sync binnen
// --jobs-interval aus (mit Ordner nur diesen) und schließt den Job ab.
func TestWatchRunsRequestedSync(t *testing.T) {
	e := newRequestEnv(t)
	rounds, requests, stop := e.watch(t)
	// Archiv, nicht INBOX: Eine IDLE-Meldung der INBOX löste sonst selbst
	// einen Lauf aus, der den Job miterledigt.
	if _, err := e.u.Append("Archiv", lit(mail(1)), &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	id, _, err := e.st.EnqueueSyncJob(context.Background(), e.acct, []string{"Archiv"}, time.Now(), store.JobOrigin{KeyID: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool { return e.syncJobState(t, id) == "done" })
	t.Logf("requested sync done after %s", time.Since(start).Round(time.Millisecond))
	if got := <-requests; got != "running" {
		t.Fatalf("progress = %q", got)
	}
	if n := e.mirrored(t); n != 1 || rounds.Load() != 2 {
		t.Fatalf("mirrored %d, rounds %d", n, rounds.Load())
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
}

// T2: Ist der Schlüssel, der den Sync anforderte, inzwischen widerrufen,
// läuft nichts: Job failed mit Grund, kein zusätzlicher Lauf.
func TestWatchRefusesRequestOfRevokedKey(t *testing.T) {
	e := newRequestEnv(t)
	ctx := context.Background()
	if _, err := e.st.CreateAPIKey(ctx, store.APIKey{Name: "agent", KeyHash: ops.HashKey("agent"), Role: "assistant",
		Scopes: []string{"mail:sync"}, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	k, _ := e.st.APIKeyByName(ctx, "agent")
	if _, err := e.st.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	rounds, requests, stop := e.watch(t)
	id, _, err := e.st.EnqueueSyncJob(ctx, e.acct, nil, time.Now(), store.JobOrigin{KeyID: "key:agent"})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool { return e.syncJobState(t, id) == "failed" })
	if got := <-requests; got != "refused (key_revoked)" {
		t.Fatalf("progress = %q", got)
	}
	time.Sleep(150 * time.Millisecond)
	if rounds.Load() != 1 {
		t.Fatalf("rounds = %d: a refused request ran", rounds.Load())
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}
	if _, last, _, _ := e.st.SyncJobState(ctx, id); last != "origin_denied: key_revoked" {
		t.Fatalf("last error = %q", last)
	}
}

// T2: Ein einmaliger postkauz sync erledigt einen offenen Job sync, den er
// abdeckt (alle Ordner bzw. die angeforderten); einen für einen Ordner,
// den er nicht spiegelt, lässt er offen.
func TestOneOffSyncSettlesRequests(t *testing.T) {
	e := newRequestEnv(t)
	ctx := context.Background()
	archiv, _, err := e.st.EnqueueSyncJob(ctx, e.acct, []string{"Archiv"}, time.Now(), store.JobOrigin{KeyID: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sync(t, ops.SyncOptions{Mailboxes: []string{"INBOX"}}); err != nil {
		t.Fatal(err)
	}
	if s := e.syncJobState(t, archiv); s != "queued" {
		t.Fatalf("job for Archiv after an INBOX-only sync: %s", s)
	}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if s := e.syncJobState(t, archiv); s != "done" {
		t.Fatalf("job after a full sync: %s", s)
	}
	// Eine Anforderung nach dem Beginn eines Laufs bleibt offen.
	later, _, err := e.st.EnqueueSyncJob(ctx, e.acct, nil, time.Now().Add(time.Hour), store.JobOrigin{KeyID: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if s := e.syncJobState(t, later); s != "queued" {
		t.Fatalf("job requested after the run began: %s", s)
	}
}
