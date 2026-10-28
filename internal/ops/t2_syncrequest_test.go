package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

func newSyncRequestEnv(t *testing.T) *mirrorEnv {
	t.Helper()
	e := newMirrorEnv(t)
	if err := e.x.Registry.Register(SyncRequest(Deps{Accounts: e.st, Drafts: &drafts.Service{Store: e.st}})); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *mirrorEnv) syncJobs(t *testing.T) int {
	t.Helper()
	n, err := e.st.QueryIntForTest(`SELECT count(*) FROM jobs WHERE type = 'sync'`)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// T2 (sync.request, MCP mail_sync_now): Der Trockenlauf reiht nichts ein;
// sonst entsteht genau ein offener Job sync je Konto mit der Herkunft des
// Aufrufs, eine zweite Anforderung kommt hinzu (Ordner vereinigt, ohne
// Ordner = alle). Ohne Konto gilt das einzige; mail:sync ist Pflicht.
func TestSyncRequestQueuesOneJob(t *testing.T) {
	e := newSyncRequestEnv(t)
	ctx := context.Background()
	yes := true
	res, err := e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{"mailbox":"INBOX"}`), DryRun: &yes})
	if err != nil {
		t.Fatal(err)
	}
	if o := res.Output.(SyncRequestOutput); o.Status != "would_queue" || o.JobID != 0 || !o.DryRun || o.Account != "demo" || e.syncJobs(t) != 0 {
		t.Fatalf("dry run: %+v, jobs %d", o, e.syncJobs(t))
	}

	res, err = e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{"mailbox":"INBOX"}`)})
	if err != nil {
		t.Fatal(err)
	}
	first := res.Output.(SyncRequestOutput)
	if first.Status != "queued" || first.JobID == 0 || first.Merged || first.Note != SyncRequestNote || e.syncJobs(t) != 1 {
		t.Fatalf("queued: %+v", first)
	}
	j, ok, err := e.st.ClaimSyncJob(ctx, e.acct, "test", time.Minute, false)
	if err != nil || !ok || len(j.Mailboxes) != 1 || j.Mailboxes[0] != "INBOX" || j.Origin.KeyID != "cli" || j.Origin.AuditID != res.AuditID {
		t.Fatalf("job = %+v %v %v", j, ok, err)
	}
	if err := e.st.ReleaseJob(ctx, j.ID, "test", time.Now()); err != nil {
		t.Fatal(err)
	}

	// Zweite Anforderung ohne Ordner: derselbe Job, jetzt alle Ordner.
	res, err = e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{"account":"demo"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if o := res.Output.(SyncRequestOutput); o.JobID != first.JobID || !o.Merged || e.syncJobs(t) != 1 {
		t.Fatalf("second: %+v", o)
	}
	if j, ok, err = e.st.ClaimSyncJob(ctx, e.acct, "test", time.Minute, false); err != nil || !ok || len(j.Mailboxes) != 0 || j.Origin.KeyID != "cli" {
		t.Fatalf("merged job = %+v %v %v", j, ok, err)
	}

	// Ohne mail:sync: verweigert, nichts eingereiht.
	if _, err := e.x.Execute(ctx, Request{Key: policy.Key{ID: "key:leser", Scopes: policy.Roles["reader"]}, Operation: "sync.request",
		Input: json.RawMessage(`{}`)}); !errors.Is(err, ErrDenied) {
		t.Fatalf("reader: %v", err)
	}
	// Zwei Konten, kein Konto genannt: Eingabefehler.
	in := demoInput()
	in["name"] = "zweites"
	if _, err := e.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	var ie *InputError
	if _, err := e.x.Execute(ctx, Request{Key: adminKey, Operation: "sync.request", Input: json.RawMessage(`{}`)}); !errors.As(err, &ie) {
		t.Fatalf("two accounts: %v", err)
	}
}

// T2: Vor jeder Ausführung prüft SyncJobs.Denied die Herkunft frisch aus
// api_keys (wie DraftPusher.originDenied): widerrufen, abgelaufen, gelöscht
// oder ohne mail:sync → Grund; der Mensch (cli) darf immer. Refuse beendet
// den Job endgültig und schreibt das Systemereignis sync.job.
func TestSyncJobsChecksOrigin(t *testing.T) {
	e := newSyncRequestEnv(t)
	ctx := context.Background()
	sj := NewSyncJobs(Deps{Drafts: &drafts.Service{Store: e.st}})
	now := time.Now()
	sj.Now = func() time.Time { return now }
	mk := func(name, role string, scopes []string, expires time.Time) {
		t.Helper()
		if _, err := e.st.CreateAPIKey(ctx, store.APIKey{Name: name, KeyHash: HashKey(name), Role: role, Scopes: scopes,
			ExpiresAt: expires, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	mk("agent", "assistant", []string{"mail:read", "mail:sync"}, time.Time{})
	mk("old", "assistant", []string{"mail:read", "mail:sync"}, now.Add(-time.Hour))
	mk("reader", "reader", []string{"mail:read"}, time.Time{})
	mk("gone", "assistant", []string{"mail:sync"}, time.Time{})
	k, _ := e.st.APIKeyByName(ctx, "gone")
	if _, err := e.st.RevokeAPIKey(ctx, k.ID, now); err != nil {
		t.Fatal(err)
	}
	for keyID, want := range map[string]string{"cli": "", "system": "", "key:agent": "", "key:old": "key_expired",
		"key:reader": "missing_scope", "key:gone": "key_revoked", "key:unbekannt": "key_invalid"} {
		got, err := sj.Denied(ctx, store.SyncJob{Origin: store.JobOrigin{KeyID: keyID}})
		if err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", keyID, got, err, want)
		}
	}

	id, _, err := e.st.EnqueueSyncJob(ctx, e.acct, nil, now, store.JobOrigin{KeyID: "key:gone"})
	if err != nil {
		t.Fatal(err)
	}
	j, ok, err := sj.Claim(ctx, e.acct, true)
	if err != nil || !ok || j.ID != id {
		t.Fatalf("claim: %+v %v %v", j, ok, err)
	}
	a, _ := e.st.AccountByName(ctx, "demo")
	if err := sj.Refuse(ctx, a, j, "key_revoked"); err != nil {
		t.Fatal(err)
	}
	if state, last, _, _ := e.st.SyncJobState(ctx, id); state != "failed" || last != "origin_denied: key_revoked" {
		t.Fatalf("job state = %s %q", state, last)
	}
	ev, ok, err := e.st.LastAuditEvent(ctx, "sync.job", "account:demo")
	if err != nil || !ok || ev.Result != "denied" || ev.Reason != "key_revoked" || ev.ActorType != "system" {
		t.Fatalf("audit = %+v %v %v", ev, ok, err)
	}
	// Ein neuer Aufruf reiht wieder ein (der gescheiterte ist nicht offen).
	if id2, created, err := e.st.EnqueueSyncJob(ctx, e.acct, nil, now, store.JobOrigin{KeyID: "cli"}); err != nil || !created || id2 == id {
		t.Fatalf("enqueue after failure: %d %v %v", id2, created, err)
	}
}
