package ops

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// actionExec ist ein Executor über dem Katalog der Vertragsumgebung mit
// den Standardregeln.
func actionExec(t *testing.T, e *contractEnv) (*Executor, *audit.Memory) {
	t.Helper()
	reg, err := NewDefaultRegistry(e.deps)
	if err != nil {
		t.Fatal(err)
	}
	au := &audit.Memory{}
	rules := policy.DefaultRules()
	return &Executor{Registry: reg, Audit: au, Rules: &rules}, au
}

func execJSON(t *testing.T, x *Executor, key policy.Key, op, input string, dry bool) (Result, error) {
	t.Helper()
	return x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: op, Input: json.RawMessage(input), DryRun: &dry})
}

var (
	assistantKey = policy.Key{ID: "key:assi", Name: "assi", Scopes: policy.Roles["assistant"]}
	operatorKey  = policy.Key{ID: "key:op", Name: "op", Scopes: policy.Roles["operator"]}
)

// S3.1 Akzeptanz 4 und E5: Tags lokal, set_by wie im Audit; Tag-Namen
// dürfen ins Audit, Betreffs nicht; ungültige Namen sind ein
// Eingabefehler ohne den Namen.
func TestMessagesTag(t *testing.T) {
	e := newContractEnv(t)
	x, au := actionExec(t, e)
	res, err := execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1],"add":["Rechnung"," wichtig "]}`, false)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(TagOutput)
	if out.Added != 2 || len(out.Messages) != 1 || !slices.Equal(out.Messages[0].Tags, []string{"rechnung", "wichtig"}) || out.Keywords.Status != "off" {
		t.Fatalf("out = %+v", out)
	}
	var setBy string
	if err := e.st.DBForTest().QueryRow(`SELECT set_by FROM message_tags LIMIT 1`).Scan(&setBy); err != nil || setBy != "key:assi" {
		t.Fatalf("set_by = %q %v", setBy, err)
	}
	for _, ev := range au.Events() {
		b, _ := json.Marshal(ev)
		if strings.Contains(string(b), "Hallo") {
			t.Fatalf("subject in audit: %s", b)
		}
	}
	if p := string(au.Events()[0].ParamsRedacted); !strings.Contains(p, "rechnung") {
		t.Fatalf("tag names missing in audit params: %s", p)
	}
	_, err = execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1],"add":["mit leer"]}`, false)
	var ie *InputError
	if !errors.As(err, &ie) || strings.Contains(err.Error(), "mit leer") {
		t.Fatalf("invalid tag: %v", err)
	}
	// Eine fremde ID ist nicht gefunden.
	if _, err := execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1,999],"add":["x"]}`, false); !errors.Is(err, store.ErrMessageNotFound) {
		t.Fatalf("foreign id: %v", err)
	}
	res, err = execJSON(t, x, assistantKey, "tags.list", `{"account":"demo"}`, false)
	if err != nil || len(res.Output.(TagsListOutput).Tags) != 2 {
		t.Fatalf("tags.list = %+v %v", res.Output, err)
	}
}

// E1: mark wirkt sofort im Spiegel (pending) und reiht den Job ein; der
// Trockenlauf ändert nichts. V20: mark braucht mail:mark, den assistant
// nicht hat.
func TestMessagesMarkQueues(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	var pe *PolicyError
	if _, err := execJSON(t, x, assistantKey, "messages.mark", `{"account":"demo","ids":[1],"seen":false}`, false); !errors.As(err, &pe) ||
		pe.Decision.Reason != "missing_scope" {
		t.Fatalf("assistant may mark: %v", err)
	}
	res, err := execJSON(t, x, operatorKey, "messages.mark", `{"account":"demo","ids":[1],"seen":false}`, true)
	if err != nil || res.Output.(ActionOutput).ActionID != 0 {
		t.Fatalf("dry run = %+v %v", res.Output, err)
	}
	res, err = execJSON(t, x, operatorKey, "messages.mark", `{"account":"demo","ids":[1],"seen":false}`, false)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(ActionOutput)
	if out.Status != "queued" || out.ActionID == 0 || out.ServerCopies != 1 || !strings.Contains(out.NextStep, "action show") {
		t.Fatalf("out = %+v", out)
	}
	if n, _ := e.st.PendingFlagCopies(context.Background(), 1); n != 1 {
		t.Fatalf("pending copies = %d", n)
	}
	n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action' AND key_id = 'key:op' AND audit_id = ?`, res.AuditID)
	if n != 1 {
		t.Fatalf("job = %d", n)
	}
}

// E3: Ein Agent erreicht beim Papierkorb nie mehr als den Antrag; die
// Freigabe kann nur der Mensch; danach ist die Aktion eingereiht.
func TestMessagesTrashNeedsHuman(t *testing.T) {
	e := newContractEnv(t)
	x, au := actionExec(t, e)
	res, err := execJSON(t, x, operatorKey, "messages.trash", `{"account":"demo","ids":[1]}`, false)
	if err != nil || res.Decision.Outcome != policy.NeedsApproval {
		t.Fatalf("trash = %+v %v", res, err)
	}
	out := res.Output.(ActionOutput)
	if out.Status != "needs_approval" || out.ApprovalID == 0 || !out.HumanOnly || !strings.Contains(out.NextStep, "action approve") ||
		out.Target.Role != "trash" || out.ExpiresAt == "" {
		t.Fatalf("out = %+v", out)
	}
	if evs := au.Events(); evs[len(evs)-1].Result != audit.ResultNeedsApproval {
		t.Fatalf("end entry = %+v", evs[len(evs)-1])
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action'`); n != 0 {
		t.Fatal("job before approval")
	}
	id := `{"action_id":` + itoa(out.ActionID) + `}`
	if _, err := execJSON(t, x, policy.Key{ID: "key:adm", Scopes: policy.Roles["admin"]}, "actions.approve", id, false); !errors.Is(err, ErrDenied) ||
		!strings.Contains(err.Error(), "human_only") {
		t.Fatalf("agent approve = %v", err)
	}
	res, err = execJSON(t, x, humanKey, "actions.approve", id, false)
	if err != nil {
		t.Fatal(err)
	}
	d := res.Output.(ActionDecisionOutput)
	if d.State != store.ActionQueued || d.DecidedBy != "Testperson" {
		t.Fatalf("approve = %+v", d)
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action' AND key_id = 'cli'`); n != 1 {
		t.Fatalf("job after approval = %d", n)
	}
	if _, err := execJSON(t, x, humanKey, "actions.approve", id, false); !errors.Is(err, ErrDenied) {
		t.Fatalf("second approve = %v", err)
	}
	res, err = execJSON(t, x, assistantKey, "actions.get", id, false)
	if err != nil || res.Output.(ActionView).State != store.ActionQueued || len(res.Output.(ActionView).Targets) != 1 {
		t.Fatalf("get = %+v %v", res.Output, err)
	}
}

// Ohne Entwurfsdienst (Deps.Drafts nil) stellt trash den Antrag trotzdem
// und nennt die Frist (vorher nil-Zeiger, gefunden im Integrationstest).
func TestMessagesTrashWithoutDrafts(t *testing.T) {
	e := newContractEnv(t)
	e.deps.Drafts = nil
	x, _ := actionExec(t, e)
	res, err := execJSON(t, x, operatorKey, "messages.trash", `{"account":"demo","ids":[1]}`, false)
	if err != nil || res.Output.(ActionOutput).ExpiresAt == "" {
		t.Fatalf("trash = %+v %v", res.Output, err)
	}
	if _, err := execJSON(t, x, humanKey, "drafts.cleanup_copies", `{"draft_id":1}`, false); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("cleanup without drafts = %v", err)
	}
}

// A14: Auch mit dry_run_default bekommt der Agent needs_approval, ohne
// dass ein Antrag entsteht.
func TestMessagesTrashDryRunDefault(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	key := operatorKey
	key.DryRunDefault = true
	res, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "messages.trash",
		Input: json.RawMessage(`{"account":"demo","ids":[1]}`)})
	if err != nil || res.Decision.Outcome != policy.NeedsApproval || !res.Decision.DryRun || res.Output.(ActionOutput).ActionID != 0 {
		t.Fatalf("res = %+v %v", res, err)
	}
	if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`); n != 1 {
		t.Fatalf("actions = %d (only the seeded one)", n)
	}
}

// Ziele nur aus dem Spiegel: Mehrere mögliche Quellkopien ohne from sind
// eine Ablehnung (nicht raten); mit from geht es.
func TestMessagesMoveSource(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	ctx := context.Background()
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	var archiv int64
	for _, b := range boxes {
		if b.Path == "Archiv" {
			archiv = b.ID
		}
	}
	// Nachricht 1 liegt zusätzlich im Archiv; Ziel Entwürfe-Ordner per Rolle
	// (seit V8 nur für den Menschen).
	if err := e.st.ExecForTest(`INSERT INTO message_mailboxes(message_id, mailbox_id, uid) VALUES (1, ?, 7)`, archiv); err != nil {
		t.Fatal(err)
	}
	_, err := execJSON(t, x, humanKey, "messages.move", `{"account":"demo","ids":[1],"role":"drafts"}`, false)
	if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "ambiguous_source") {
		t.Fatalf("ambiguous = %v", err)
	}
	res, err := execJSON(t, x, humanKey, "messages.move", `{"account":"demo","ids":[1],"role":"drafts","from":"Archiv"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.Action(ctx, res.Output.(ActionOutput).ActionID)
	if len(a.Targets) != 1 || a.Targets[0].MailboxID != archiv || a.Targets[0].UID != 7 {
		t.Fatalf("targets = %+v", a.Targets)
	}
	// Schon im Ziel: übersprungen bzw. nichts zu tun.
	_, err = execJSON(t, x, operatorKey, "messages.move", `{"account":"demo","ids":[1],"role":"archive","from":"Archiv"}`, false)
	if !errors.Is(err, ErrDenied) || !strings.Contains(err.Error(), "nothing_to_do") {
		t.Fatalf("already there = %v", err)
	}
}

// A14: drafts.cleanup_copies trifft nur Kopien, die der Spiegel unter
// Ordner, UID und UIDVALIDITY mit dem eigenen Hash führt.
func TestDraftsCleanupCopies(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	res, err := execJSON(t, x, humanKey, "drafts.cleanup_copies", `{"draft_id":1}`, false)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(CleanupOutput)
	if out.Copies != 1 || out.Skipped != 1 || out.ActionID == 0 {
		t.Fatalf("out = %+v", out)
	}
	a, _ := e.st.Action(context.Background(), out.ActionID)
	if a.State != store.ActionQueued || a.Kind != store.ActionTrash || len(a.Targets) != 1 || a.Targets[0].UID != 9 {
		t.Fatalf("action = %+v", a)
	}
	// Anderer Inhalt unter derselben UID: nicht angefasst.
	if err := e.st.ExecForTest(`UPDATE messages SET content_hash = 'fremd' WHERE message_id_header = 'pk.alt@example.org'`); err != nil {
		t.Fatal(err)
	}
	res, err = execJSON(t, x, humanKey, "drafts.cleanup_copies", `{"draft_id":1}`, false)
	if err != nil || res.Output.(CleanupOutput).Copies != 0 {
		t.Fatalf("changed copy = %+v %v", res.Output, err)
	}
}

// T2 Punkt 5 (Lastenheft Vorgang 5): Der Schlüssel der Nacht-Triage
// (Rolle triage) kann lesen, suchen, Entwürfe anlegen und vorlegen, taggen
// und markieren, sonst nichts: kein Verschieben, kein Papierkorb, kein
// Sync, keine Freigabe, keine Verwaltung.
func TestTriageRoleCanDoNothingElse(t *testing.T) {
	key := policy.Key{ID: "key:nacht", Name: "nacht", Scopes: policy.Roles["triage"]}
	var allowed []string
	for _, op := range Catalog(Deps{}) {
		if d := policy.Check(key, op.Requirement(), false); d.Outcome != policy.Deny {
			allowed = append(allowed, op.Name)
		}
	}
	sort.Strings(allowed)
	want := []string{"accounts.list", "approvals.get", "attachments.list", "attachments.text", "actions.get", "actions.list",
		"drafts.create", "drafts.get", "drafts.list", "drafts.request_ready", "drafts.style", "drafts.update",
		"mailboxes.list", "mailboxes.overview", "messages.get", "messages.search", "messages.tag",
		"policy.get", "tags.list", "threads.get"}
	sort.Strings(want)
	if !slices.Equal(allowed, want) {
		t.Fatalf("triage may run\n%v\nwant\n%v", allowed, want)
	}
	for _, s := range []policy.Scope{policy.ScopeMailMark, policy.ScopeMailMove, policy.ScopeMailTrash, policy.ScopeMailSync, policy.ScopeAdmin, policy.ScopeMailExport} {
		if key.Has(s) {
			t.Fatalf("triage has %s", s)
		}
	}
}
