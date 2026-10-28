package ops

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// V18 der Nachbesserung T2: Eine zweite Anforderung binnen 60 s nach der
// letzten reiht nichts Neues ein (too_soon, mit retry_after), wenn der Job
// schon läuft oder erledigt ist; ein noch wartender Job nimmt sie wie
// bisher auf. Nach dem Mindestabstand entsteht wieder ein Job.
func TestSyncRequestMinimumGap(t *testing.T) {
	e := newSyncRequestEnv(t)
	ctx := context.Background()
	agent := policy.Key{ID: "key:agent", Name: "agent", Scopes: policy.Roles["assistant"]}
	req := func() SyncRequestOutput {
		t.Helper()
		res, err := e.x.Execute(ctx, Request{Key: agent, Operation: "sync.request", Input: json.RawMessage(`{"account":"demo"}`)})
		if err != nil {
			t.Fatal(err)
		}
		return res.Output.(SyncRequestOutput)
	}
	first := req()
	if first.Status != "queued" || first.Merged {
		t.Fatalf("first = %+v", first)
	}
	// Wartet noch: angeschlossen, kein zweiter Job.
	if o := req(); o.Status != "queued" || !o.Merged || o.JobID != first.JobID || e.syncJobs(t) != 1 {
		t.Fatalf("while queued = %+v", o)
	}
	// Läuft: nichts Neues (sonst ein Nachlauf).
	j, ok, err := e.st.ClaimSyncJob(ctx, e.acct, "test", time.Minute, false)
	if err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	o := req()
	if o.Status != "too_soon" || o.JobID != first.JobID || o.RetryAfter == "" || o.Note != SyncRequestTooSoonNote {
		t.Fatalf("while running = %+v", o)
	}
	// Erledigt: weiter nichts Neues binnen 60 s.
	if done, err := e.st.CompleteSyncJob(ctx, j, "test"); err != nil || !done {
		t.Fatalf("complete: %v %v", done, err)
	}
	if o := req(); o.Status != "too_soon" || e.syncJobs(t) != 1 {
		t.Fatalf("after done = %+v, jobs %d", o, e.syncJobs(t))
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM jobs WHERE type = 'sync' AND state = 'done'`); n != 1 {
		t.Fatalf("done jobs = %d", n)
	}
	// Nach dem Mindestabstand: ein neuer Job.
	r, err := e.st.RequestSyncJob(ctx, e.acct, nil, time.Now().Add(SyncRequestMinGap+time.Second), store.JobOrigin{KeyID: "key:agent"}, SyncRequestMinGap)
	if err != nil || r.TooSoon || !r.Created || r.ID == first.JobID {
		t.Fatalf("after the gap = %+v %v", r, err)
	}
}
