package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A12 Teil 2 (a): Die Policy fragt mit der Zahl der tatsächlichen Ziele.

// policyCheck ist CleanupCheck wie im Betrieb: Policy mit den Regeln der
// Umgebung (bulk_confirm_threshold 50), Rolle assistant.
func (e *cuEnv) policyCheck() {
	e.svc.CleanupCheck = func(_ context.Context, keyID string, copies int) (string, error) {
		k := policy.Key{ID: keyID, Scopes: policy.Roles["assistant"]}
		if dec := policy.CheckInput(k, policy.Facts{Action: policy.ActionTrash, Targets: copies}, e.rules); !dec.Allowed() {
			return dec.Reason, nil
		}
		return "", nil
	}
}

// 60 Änderungen nacheinander, jede abgelegt und aufgeräumt: jede
// Aufräum-Aktion hat genau ein Ziel, auch nach der 50.
func TestA12SixtyUpdatesEachCleaned(t *testing.T) {
	e := newCuEnv(t)
	e.policyCheck()
	d := e.newDraft("Probe")
	e.push(d, "v0", true)
	for i := 1; i <= 60; i++ {
		d = e.update(d, "Probe")
		e.push(d, "v"+itoa(int64(i)), true)
		acts := e.actionsOf(d.ID)
		if len(acts) != i || len(acts[0].Targets) != 1 {
			t.Fatalf("update %d: actions %d, targets of the newest %d", i, len(acts), len(acts[0].Targets))
		}
		e.run()
	}
	if e.draftCopies() != 1 {
		t.Fatalf("copies left = %d", e.draftCopies())
	}
	if n := e.count(`SELECT count(*) FROM audit_events WHERE operation = 'drafts.cleanup_queued' AND result = 'denied'`); n != 0 {
		t.Fatalf("denied cleanups = %d", n)
	}
}

// Entwurf mit 60 Ablagen und einer liegenden Kopie: supersede und discard
// zählen 1 Ziel (erlaubt); kommt zwischen Policy und Transaktion eine Kopie
// hinzu, lehnt der Aufruf ab und der Entwurf bleibt offen.
func TestA12PolicyTargetsCountsRealCopies(t *testing.T) {
	e := newCuEnv(t)
	e.policyCheck()
	ctx := context.Background()
	d := e.newDraft("Probe")
	e.push(d, "v0", true)
	for i := 1; i <= 60; i++ {
		d = e.update(d, "Probe")
		e.push(d, "v"+itoa(int64(i)), true)
		e.run()
	}
	if got := e.draft(d.ID); len(got.RemotePrevious) != 60 {
		t.Fatalf("remote_previous = %d", len(got.RemotePrevious))
	}
	for _, op := range []string{OpSupersede, OpDiscard} {
		n, err := e.svc.PolicyTargets(ctx, d.ID, op)
		if err != nil || n != 1 {
			t.Fatalf("%s: targets %d %v", op, n, err)
		}
		k := policy.Key{ID: cuKey, Scopes: policy.Roles["assistant"]}
		if dec := policy.CheckInput(k, policy.Facts{Action: policy.ActionTrash, Targets: n}, e.rules); !dec.Allowed() {
			t.Fatalf("%s: policy = %+v", op, dec)
		}
	}
	// Die Policy hat 0 Ziele geprüft, in der Transaktion ist es eins.
	_, err := e.svc.Discard(ctx, DiscardRequest{ID: d.ID, KeyID: cuKey, PolicyChecked: true, PolicyTargets: 0})
	if ReasonOf(err) != ReasonDraftChanged || !strings.Contains(err.Error(), "try again") {
		t.Fatalf("discard with fewer checked targets = %v", err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("status = %s", got.Status)
	}
	res, err := e.svc.Discard(ctx, DiscardRequest{ID: d.ID, KeyID: cuKey, PolicyChecked: true, PolicyTargets: 1})
	if err != nil || res.Copies != 1 {
		t.Fatalf("discard = %+v %v", res, err)
	}
}

// 55 frühere, noch liegende eigene Kopien: die Policy verlangt
// confirm_count; der Push legt keine Aktion an, das Audit nennt die Zahl.
func TestA12FiftyFiveCopiesNeedConfirm(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	e.svc.PreviousCopies = PreviousCopiesKeep
	d := e.newDraft("Probe")
	e.push(d, "v0", true)
	for i := 1; i <= 55; i++ {
		d = e.update(d, "Probe")
		e.push(d, "v"+itoa(int64(i)), true)
	}
	e.svc.PreviousCopies = ""
	e.policyCheck()
	n, err := e.svc.PolicyTargets(ctx, d.ID, OpDiscard)
	if err != nil || n != 56 {
		t.Fatalf("discard targets = %d %v", n, err)
	}
	k := policy.Key{ID: cuKey, Scopes: policy.Roles["assistant"]}
	if dec := policy.CheckInput(k, policy.Facts{Action: policy.ActionTrash, Targets: n}, e.rules); dec.Reason != "confirm_count_required" {
		t.Fatalf("policy = %+v", dec)
	}
	d = e.update(d, "Probe")
	e.push(d, "v56", true)
	if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
		t.Fatalf("actions = %d", n)
	}
	r, ok := e.lastAudit(OpCleanupQueued)
	var p map[string]any
	_ = json.Unmarshal(r.ParamsRedacted, &p)
	if !ok || r.Result != "denied" || r.Reason != "confirm_count_required" || p["copies"] != float64(56) {
		t.Fatalf("audit = %+v %v", r, p)
	}
}

// remote_note nennt frühere Kopien nach ihrem Stand (A12 Teil 2 (b)).
func TestA12PreviousCopyStates(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Probe")
	e.push(d, "zwei", true)
	state := func() PreviousCopy {
		t.Helper()
		l, err := e.svc.PreviousCopyStates(ctx, e.draft(d.ID))
		if err != nil || len(l) != 1 || l[0].UID != u1 {
			t.Fatalf("states = %+v %v", l, err)
		}
		return l[0]
	}
	acts := e.actionsOf(d.ID)
	if s := state(); s.State != PrevQueued || s.ActionID != acts[0].ID {
		t.Fatalf("queued: %+v", s)
	}
	e.exec(`UPDATE actions SET state = 'failed'`)
	e.exec(`UPDATE action_targets SET result = 'stale'`)
	if s := state(); s.State != PrevPending {
		t.Fatalf("pending: %+v", s)
	}
	e.exec(`UPDATE meta SET value = '{"n":3,"first":"2026-11-01T00:00:00Z"}' WHERE key LIKE 'draft_copy_cleanup/%'`)
	if s := state(); s.State != PrevGivenUp {
		t.Fatalf("given up: %+v", s)
	}
	e.exec(`UPDATE messages SET content_hash = 'anders' WHERE id = (SELECT message_id FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?)`,
		e.m.boxes["drafts"], u1)
	if s := state(); s.State != PrevChanged {
		t.Fatalf("changed: %+v", s)
	}
	_ = store.ActionDone
}
