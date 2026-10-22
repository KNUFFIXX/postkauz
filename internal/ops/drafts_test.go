package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Tests der Entwurfsoperationen (S2.3). Umgebung wie im Vertragstest
// (Konto demo, Nachricht 1 mit Anhängen, Identitäten 1 und 2, Entwurf 1 in
// draft, Entwurf 2 in pending_approval mit Freigabe 1), dazu ein Executor
// mit dem Audit in der Datenbank (wie die CLI).

type fakeChecker struct {
	res   DraftCheckResult
	calls int
	eml   []byte
	// during läuft, während der Hook prüft (zweiter Schreiber, W2).
	during func()
}

func (f *fakeChecker) CheckDraft(_ context.Context, in DraftCheckInput) (DraftCheckResult, error) {
	f.calls++
	f.eml = in.EML
	if f.during != nil {
		f.during()
	}
	return f.res, nil
}

type draftEnv struct {
	*contractEnv
	x      *Executor
	attach string
	check  *fakeChecker
}

var (
	humanCLI  = policy.Key{ID: "cli", Scopes: policy.Roles["admin"], Human: true}
	assistant = policy.Key{ID: "key:assistent", Role: "assistant", Scopes: policy.Roles["assistant"]}
)

func newDraftEnv(t *testing.T, withChecker bool) *draftEnv {
	t.Helper()
	c := newContractEnv(t)
	e := &draftEnv{contractEnv: c, attach: t.TempDir()}
	c.deps.Draft.AttachDir = e.attach
	if withChecker {
		e.check = &fakeChecker{res: DraftCheckResult{Result: drafts.HookPass}}
		c.deps.DraftCheck = e.check
	}
	reg, err := NewDefaultRegistry(c.deps)
	if err != nil {
		t.Fatal(err)
	}
	e.x = &Executor{Registry: reg, Audit: c.st.AuditWriter()}
	return e
}

func (e *draftEnv) do(t *testing.T, key policy.Key, op string, in any, dry bool) (any, error) {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	actor := audit.ActorAgent
	if key.IsHuman() {
		actor = audit.ActorHuman
	}
	res, err := e.x.Execute(context.Background(), Request{Key: key, Actor: actor, Operation: op, Input: b, DryRun: &dry})
	return res.Output, err
}

func (e *draftEnv) must(t *testing.T, key policy.Key, op string, in any) any {
	t.Helper()
	out, err := e.do(t, key, op, in, false)
	if err != nil {
		t.Fatalf("%s: %v", op, err)
	}
	return out
}

func wantDenied(t *testing.T, err error, reason string) {
	t.Helper()
	var de *DeniedError
	var pe *PolicyError
	switch {
	case errors.As(err, &de) && de.Reason == reason:
	case errors.As(err, &pe) && pe.Decision.Reason == reason:
	default:
		t.Fatalf("error = %v, want denied %s", err, reason)
	}
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("error %v is not ErrDenied", err)
	}
}

// auditJSON liefert alle Audit-Einträge der Datenbank als JSON.
func (e *draftEnv) auditJSON(t *testing.T) ([]audit.Event, string) {
	t.Helper()
	evs, err := e.st.AuditEvents(context.Background(), 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	// Als Text: json.Marshal schriebe ParamsRedacted ([]byte) als Base64,
	// und die Suche nach Inhalt ginge ins Leere.
	var b strings.Builder
	for _, ev := range evs {
		fmt.Fprintf(&b, "%s %s %s %s %s %s %s\n", ev.ActorType, ev.KeyID, ev.Operation, ev.ParamsRedacted, strings.Join(ev.Targets, ","),
			ev.Result, ev.Reason)
	}
	return evs, b.String()
}

// Akzeptanz S2.3: Lebenslauf mit Freigabe. Eine Änderung nach der Freigabe
// setzt den Entwurf auf draft und die Freigabe auf superseded; die alte
// Freigabe ist danach approval_stale. Jeder Übergang steht mit Akteur,
// Draft-ID, Zustand und Freigabe im Audit, nie mit Inhalt.
func TestDraftLifecycleWithApproval(t *testing.T) {
	e := newDraftEnv(t, false)
	const subject, body = "KanarienBetreff-S23", "KanarienText-S23"

	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": 1, "to": []string{"Zweit <zweit@example.net>"},
		"body_markdown": body}).(DraftsWriteOutput)
	if out.DraftID != 3 || out.Status != "draft" || out.Remote != "pending" || out.RemoteNote != RemoteNotePending || out.PushJobID == 0 ||
		!strings.HasPrefix(out.MessageID, "pk.") || !strings.HasSuffix(out.MessageID, "@example.org") || out.Account != "demo" || out.OriginalID != 1 {
		t.Fatalf("create = %+v", out)
	}
	p := out.Preview
	if p.Subject != "Re: Hallo" || p.InReplyTo != "<a@example.org>" || p.From != "Demo <demo@example.org>" ||
		strings.Join(p.To, ",") != "Zweit <zweit@example.net>" || !strings.Contains(p.Text, body) || !strings.Contains(p.Text, "Gruß") ||
		strings.Contains(p.Text, "> Text") {
		t.Fatalf("preview = %+v", p)
	}
	// S2.8: Das Zitat steht nicht im eigenen Text, sondern in der Hülle.
	if q := out.Original; out.Source != "draft" || q == nil || !q.Untrusted || q.Source != "email" || q.Part != "quote" ||
		q.OriginalID != 1 || !strings.Contains(q.Text, "> Text") || strings.Contains(q.Text, body) {
		t.Fatalf("original = %+v", out.Original)
	}
	id := out.DraftID

	// Vorlegen (Agent), freigeben (Mensch).
	req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput)
	if req.Status != "pending_approval" || req.PreviousStatus != "draft" || req.Hook != "pass" || req.ApprovalID == 0 || req.ExpiresAt == "" {
		t.Fatalf("request = %+v", req)
	}
	dec := e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID}).(DraftsDecideOutput)
	if dec.Status != "ready" || dec.Decision != "approved" || dec.DecidedBy != "Testperson" || dec.PreviousStatus != "pending_approval" {
		t.Fatalf("approve = %+v", dec)
	}
	// Agent ändert nach der Freigabe: draft, Freigabe superseded.
	upd := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": subject}).(DraftsWriteOutput)
	if upd.Status != "draft" || upd.PreviousStatus != "ready" || upd.ApprovalSuperseded != req.ApprovalID || !upd.Changed ||
		upd.Preview.Subject != subject || strings.Join(upd.Preview.To, ",") != "Zweit <zweit@example.net>" {
		t.Fatalf("update = %+v", upd)
	}
	ap := e.must(t, assistant, "approvals.get", map[string]any{"approval_id": req.ApprovalID}).(ApprovalOutput)
	if ap.Status != "superseded" || ap.DraftStatus != "draft" || ap.DecidedBy != "Testperson" {
		t.Fatalf("approval = %+v", ap)
	}
	// Die alte Freigabe gilt nicht mehr.
	_, err := e.do(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID}, false)
	wantDenied(t, err, "approval_stale")
	// Neue Vorlage, Änderung in pending_approval: wieder draft, superseded.
	req2 := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput)
	if req2.ApprovalID == req.ApprovalID {
		t.Fatal("no new approval")
	}
	upd = e.must(t, humanCLI, "drafts.update", map[string]any{"draft_id": id, "body_markdown": body + " 2"}).(DraftsWriteOutput)
	if upd.Status != "draft" || upd.ApprovalSuperseded != req2.ApprovalID {
		t.Fatalf("update in pending_approval = %+v", upd)
	}
	// Unveränderte Änderung: nichts passiert.
	upd = e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": subject}).(DraftsWriteOutput)
	if upd.Changed || upd.Status != "draft" {
		t.Fatalf("no-op update = %+v", upd)
	}
	// Ablehnen braucht eine offene Vorlage.
	req3 := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput)
	rej := e.must(t, humanCLI, "drafts.reject", map[string]any{"approval_id": req3.ApprovalID, "reason": "AblehnungsgrundKanarie"}).(DraftsDecideOutput)
	if rej.Status != "draft" || rej.Decision != "rejected" {
		t.Fatalf("reject = %+v", rej)
	}
	// Abschließen; danach ist jede Änderung draft_closed.
	cl := e.must(t, humanCLI, "drafts.abandon", map[string]any{"draft_id": id}).(DraftsCloseOutput)
	if cl.Status != "abandoned" || cl.PreviousStatus != "draft" {
		t.Fatalf("abandon = %+v", cl)
	}
	_, err = e.do(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": "x"}, false)
	wantDenied(t, err, "draft_closed")
	_, err = e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}, false)
	wantDenied(t, err, "draft_closed")

	// Audit: je Übergang ein Eintrag mit Akteur, Draft-ID, Zuständen;
	// Ablehnungen mit Grund; nirgends Betreff, Text, Adressen oder Grund
	// des Menschen.
	evs, all := e.auditJSON(t)
	for _, leak := range []string{subject, body, "zweit@example.net", "Zweit", "AblehnungsgrundKanarie", "Hallo"} {
		if strings.Contains(all, leak) {
			t.Fatalf("audit contains %q: %s", leak, all)
		}
	}
	var transitions []string
	for _, ev := range evs {
		if ev.Reason != drafts.ReasonTransition {
			continue
		}
		var pm map[string]any
		_ = json.Unmarshal(ev.ParamsRedacted, &pm)
		if pm["draft_id"] != float64(id) {
			continue
		}
		transitions = append(transitions, ev.Operation+":"+string(ev.ActorType)+":"+pm["from"].(string)+">"+pm["to"].(string))
		if ev.KeyID == "" || ev.Result != audit.ResultOK {
			t.Errorf("transition event %+v", ev)
		}
	}
	want := []string{"drafts.create:agent:new>draft", "drafts.request_ready:agent:draft>pending_approval",
		"drafts.approve:human:pending_approval>ready", "drafts.update:agent:ready>draft",
		"drafts.request_ready:agent:draft>pending_approval", "drafts.update:human:pending_approval>draft",
		"drafts.request_ready:agent:draft>pending_approval", "drafts.reject:human:pending_approval>draft", "drafts.abandon:human:draft>abandoned"}
	if strings.Join(transitions, " ") != strings.Join(want, " ") {
		t.Fatalf("transitions\n got %v\nwant %v", transitions, want)
	}
	var stale, closed bool
	for _, ev := range evs {
		stale = stale || (ev.Operation == "drafts.approve" && ev.Result == audit.ResultDenied && ev.Reason == "approval_stale")
		closed = closed || (ev.Operation == "drafts.update" && ev.Result == audit.ResultDenied && ev.Reason == "draft_closed")
	}
	if !stale || !closed {
		t.Fatalf("denials not audited (stale %v, closed %v): %s", stale, closed, all)
	}
}

// state ist der Zustand ohne das Audit (das Audit wächst mit jedem Aufruf).
func (e *draftEnv) state(t *testing.T) string {
	t.Helper()
	var keep []string
	for _, l := range strings.Split(e.fingerprint(t), "\n") {
		if !strings.HasPrefix(l, "audit_events") && !strings.HasPrefix(l, "sqlite_sequence") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// Akzeptanz S2.3 (E2): Freigeben, Ablehnen, Verwerfen, für gesendet
// Erklären und Identitäten sind nur für den Menschen; mit Schlüssel jeder
// Rolle denied human_only, der Zustand bleibt.
func TestDraftHumanOnly(t *testing.T) {
	e := newDraftEnv(t, false)
	calls := map[string]any{
		"drafts.approve": map[string]any{"approval_id": 1}, "drafts.reject": map[string]any{"approval_id": 1},
		"drafts.abandon": map[string]any{"draft_id": 1}, "drafts.mark_sent": map[string]any{"draft_id": 2},
		"identities.add": map[string]any{"account": "demo", "email": "x@example.org"}, "identities.list": map[string]any{},
		"identities.set_default": map[string]any{"id": 2}, "identities.remove": map[string]any{"id": 2},
	}
	before := e.state(t)
	for role := range policy.Roles {
		for op, in := range calls {
			key := policy.Key{ID: "key:" + role, Role: role, Scopes: policy.Roles[role], Human: true} // Human zählt mit Schlüssel nie
			_, err := e.do(t, key, op, in, false)
			wantDenied(t, err, "human_only")
		}
	}
	if e.state(t) != before {
		t.Fatal("state changed")
	}
	d, err := e.st.Draft(context.Background(), 2)
	if err != nil || d.Status != "pending_approval" {
		t.Fatalf("draft 2 = %s, %v", d.Status, err)
	}
}

// Trockenlauf: create liefert die Vorschau (Kopf und die ersten 500
// Zeichen), nichts wird gespeichert, kein Job, kein Blob; request_ready
// prüft nur.
func TestDraftDryRun(t *testing.T) {
	e := newDraftEnv(t, false)
	if err := os.WriteFile(filepath.Join(e.attach, "notiz.txt"), []byte("Anhang"), 0o600); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("Wort ", 300)
	before := e.state(t)
	out, err := e.do(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"}, "subject": "Vorschau",
		"body_markdown": long, "attachments": []map[string]any{{"file": "notiz.txt"}}}, true)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(DraftsWriteOutput)
	if !o.DryRun || o.DraftID != 0 || o.MessageID != "" || o.PushJobID != 0 || o.Preview.Subject != "Vorschau" || !o.Preview.TextTruncated ||
		len([]rune(o.Preview.Text)) != PreviewChars || len(o.Attachments) != 1 || o.Attachments[0].Filename != "notiz.txt" {
		t.Fatalf("dry run = %+v", o)
	}
	rq, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}, true)
	if err != nil {
		t.Fatal(err)
	}
	if r := rq.(DraftsRequestOutput); !r.DryRun || r.Status != "pending_approval" || r.ApprovalID != 0 {
		t.Fatalf("request dry run = %+v", r)
	}
	if _, err := e.do(t, humanCLI, "drafts.approve", map[string]any{"approval_id": 1}, true); err != nil {
		t.Fatal(err)
	}
	if e.state(t) != before {
		t.Fatal("dry run changed state")
	}
	// Prüft wirklich: ein Trockenlauf auf einen abgeschlossenen Entwurf scheitert.
	e.must(t, humanCLI, "drafts.mark_sent", map[string]any{"draft_id": 1})
	_, err = e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}, true)
	wantDenied(t, err, "draft_closed")
}

// Anhänge (E10, Entscheid 39) und Ablehnungsgründe des Nachrichtenbaus.
func TestDraftAttachmentsAndReasons(t *testing.T) {
	e := newDraftEnv(t, false)
	// Text, der sich als PDF ausgibt: erlaubt, mit Warnung.
	if err := os.WriteFile(filepath.Join(e.attach, "rechnung.pdf"), []byte("nur Text"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := e.must(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "to": []string{"a@example.org"},
		"attachments": []map[string]any{{"file": "rechnung.pdf"}, {"attachment_id": 1, "filename": "Notiz-neu.txt"}}}).(DraftsWriteOutput)
	if len(out.Attachments) != 2 || len(out.Warnings) != 1 || !strings.HasPrefix(out.Warnings[0], "attachments[0]") ||
		out.Attachments[0].ContentType != "text/plain" || out.Attachments[1].Filename != "Notiz-neu.txt" || e.blobs.puts != 1 {
		t.Fatalf("attachments = %+v warnings %v puts %d", out.Attachments, out.Warnings, e.blobs.puts)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if len(got.Warnings) != 1 || len(got.Attachments) != 2 {
		t.Fatalf("get = %+v", got)
	}
	// Eine Änderung ohne attachments behält die Anhänge (aus dem Blob-Store).
	upd := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "subject": "mit Anhang"}).(DraftsWriteOutput)
	if len(upd.Attachments) != 2 || upd.Attachments[0].SHA256 != out.Attachments[0].SHA256 {
		t.Fatalf("update attachments = %+v", upd.Attachments)
	}
	// Weiterleitung übernimmt die Anhänge der Ausgangsnachricht (E10);
	// einer ohne gespeicherten Inhalt fehlt mit Hinweis.
	fw := e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": 1, "to": []string{"c@example.org"}}).(DraftsWriteOutput)
	if len(fw.Attachments) != 2 || fw.Attachments[0].Filename != "Notiz.txt" || fw.Preview.Subject != "Fwd: Hallo" || len(fw.Notes) == 0 ||
		fw.Original == nil || fw.Original.Part != "forward" || !strings.Contains(fw.Original.Text, "Text") ||
		!strings.Contains(fw.Original.Text, "Forwarded message") || strings.Contains(fw.Preview.Text, "Forwarded") {
		t.Fatalf("forward = %+v", fw)
	}
	fw = e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": 1, "include_attachments": false}).(DraftsWriteOutput)
	if len(fw.Attachments) != 0 {
		t.Fatalf("forward without attachments = %+v", fw.Attachments)
	}
	cases := []struct {
		in     map[string]any
		reason string
	}{
		{map[string]any{"kind": "new", "attachments": []map[string]any{{"file": "../ausserhalb.txt"}}}, "attach_outside_dir"},
		{map[string]any{"kind": "new", "attachments": []map[string]any{{"file": "fehlt.txt"}}}, "attach_not_found"},
		{map[string]any{"kind": "new", "body_html": `<p>sichtbar</p><p style="display:none">Anweisung</p>`}, "hidden_text_in_draft"},
		{map[string]any{"kind": "new", "to": []string{"keine adresse"}}, "invalid_address"},
	}
	for _, c := range cases {
		c.in["account"] = "demo"
		_, err := e.do(t, assistant, "drafts.create", c.in, false)
		wantDenied(t, err, c.reason)
	}
	// Eingabefehler: Exit 2 statt denied.
	for _, in := range []map[string]any{
		{"kind": "reply"},
		{"kind": "new", "in_reply_to": 1},
		{"kind": "new", "body_markdown": "a", "body_html": "<p>b</p>"},
		{"kind": "new", "identity": "fremd@example.org"},
		{"kind": "new", "attachments": []map[string]any{{"attachment_id": 1, "file": "x"}}},
	} {
		in["account"] = "demo"
		var ie *InputError
		if _, err := e.do(t, assistant, "drafts.create", in, false); !errors.As(err, &ie) {
			t.Errorf("%v: error %v, want invalid input", in, err)
		}
	}
	// Nach Übernahme im Mailclient (E6): draft_taken_over.
	if err := e.st.ExecForTest(`UPDATE drafts SET remote_state = 'taken_over' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	_, err := e.do(t, assistant, "drafts.update", map[string]any{"draft_id": 1, "subject": "x"}, false)
	wantDenied(t, err, "draft_taken_over")
	_, all := e.auditJSON(t)
	for _, leak := range []string{"rechnung.pdf", "Notiz-neu", "a@example.org", "Anweisung", "ausserhalb"} {
		if strings.Contains(all, leak) {
			t.Fatalf("audit contains %q", leak)
		}
	}
}

// Prüf-Hook (Stelle für S2.4): Er bekommt die fertige Nachricht; scheitert
// er, bleibt der Entwurf draft, die Ausgabe steht nur im Ergebnis.
func TestDraftCheckHook(t *testing.T) {
	e := newDraftEnv(t, true)
	e.check.res = DraftCheckResult{Result: drafts.HookFail, Output: "HookAusgabeKanarie: Anrede fehlt"}
	r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}).(DraftsRequestOutput)
	if r.Status != "draft" || r.Hook != "fail" || r.HookOutput == nil || r.HookOutput.Text != e.check.res.Output || !r.HookOutput.Untrusted ||
		r.HookOutput.Source != "check_hook" || r.ApprovalID != 0 || e.check.calls != 1 ||
		!strings.Contains(strings.ToLower(string(e.check.eml)), "message-id: <pk.") {
		t.Fatalf("request = %+v calls %d", r, e.check.calls)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": 1}).(DraftDetail)
	if got.HookResult != "fail" || got.Status != "draft" {
		t.Fatalf("get = %+v", got)
	}
	evs, all := e.auditJSON(t)
	if strings.Contains(all, "HookAusgabeKanarie") {
		t.Fatal("hook output in the audit")
	}
	found := false
	for _, ev := range evs {
		found = found || (ev.Reason == drafts.ReasonTransition && strings.Contains(string(ev.ParamsRedacted), `"hook":"fail"`))
	}
	if !found {
		t.Fatalf("hook result not audited: %s", all)
	}
	// Kein Hook für einen schon vorgelegten Entwurf.
	_, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 2}, false)
	wantDenied(t, err, "invalid_transition")
	if e.check.calls != 1 {
		t.Fatal("hook ran for an invalid transition")
	}
	// Bestanden: Vorlage.
	e.check.res = DraftCheckResult{Result: drafts.HookPass}
	if r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": 1}).(DraftsRequestOutput); r.Status != "pending_approval" {
		t.Fatalf("request = %+v", r)
	}
}

// Liste, Ablauf beim Lesen und Freigabe-Auskunft.
func TestDraftListAndExpiry(t *testing.T) {
	e := newDraftEnv(t, false)
	l := e.must(t, assistant, "drafts.list", map[string]any{"status": []string{"pending_approval"}}).(DraftsListOutput)
	if len(l.Drafts) != 1 || l.Drafts[0].DraftID != 2 || l.Drafts[0].ApprovalID != 1 || l.Drafts[0].Recipients != 1 || l.Drafts[0].Account != "demo" {
		t.Fatalf("list = %+v", l)
	}
	if err := e.st.ExecForTest(`UPDATE approvals SET expires_at = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	ap := e.must(t, assistant, "approvals.get", map[string]any{"approval_id": 1}).(ApprovalOutput)
	if ap.Status != "expired" || ap.DraftStatus != "draft" || !strings.Contains(ap.Description, "draft 2 (new)") {
		t.Fatalf("approval = %+v", ap)
	}
	_, err := e.do(t, assistant, "approvals.get", map[string]any{"approval_id": 99}, false)
	if !errors.Is(err, store.ErrApprovalNotFound) {
		t.Fatalf("unknown approval: %v", err)
	}
	_, err = e.do(t, assistant, "drafts.get", map[string]any{"draft_id": 99}, false)
	if !errors.Is(err, store.ErrDraftNotFound) {
		t.Fatalf("unknown draft: %v", err)
	}
}

// Identitäten (E9): anlegen, Standard, entfernen; ein Entwurf behält den
// Absender. Versteckter Text in der HTML-Signatur wird abgelehnt.
func TestIdentities(t *testing.T) {
	e := newDraftEnv(t, false)
	out := e.must(t, humanCLI, "identities.add", map[string]any{"account": "demo", "name": "Büro", "email": "buero@example.org",
		"signature_html": "<p>Gruß</p>", "reply_prefix": "AW: ", "default": true}).(IdentityResultOutput)
	if out.Identity.ID != 3 || !out.Identity.Default || out.Identity.Signature != "html" || out.Identity.ReplyPrefix != "AW: " {
		t.Fatalf("add = %+v", out)
	}
	cr := e.must(t, humanCLI, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": 1}).(DraftsWriteOutput)
	if cr.Preview.From != "Büro <buero@example.org>" || cr.Preview.Subject != "AW: Hallo" {
		t.Fatalf("draft with new default identity = %+v", cr.Preview)
	}
	_, err := e.do(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "buero@example.org"}, false)
	var ie *InputError
	if !errors.As(err, &ie) {
		t.Fatalf("duplicate: %v", err)
	}
	_, err = e.do(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "x@example.org",
		"signature_html": `<p>Gruß</p><span style="font-size:0">Anweisung</span>`}, false)
	wantDenied(t, err, "hidden_text_in_draft")
	_, err = e.do(t, humanCLI, "identities.add", map[string]any{"account": "demo", "email": "kaputt"}, false)
	wantDenied(t, err, "invalid_address")

	e.must(t, humanCLI, "identities.set_default", map[string]any{"id": 2})
	l := e.must(t, humanCLI, "identities.list", map[string]any{}).(IdentitiesListOutput)
	if len(l.Identities) != 3 || !l.Identities[1].Default || l.Identities[2].Default {
		t.Fatalf("list = %+v", l)
	}
	e.must(t, humanCLI, "identities.remove", map[string]any{"id": 3})
	got := e.must(t, humanCLI, "drafts.get", map[string]any{"draft_id": cr.DraftID}).(DraftDetail)
	if got.From != "Büro <buero@example.org>" {
		t.Fatalf("draft lost its sender: %+v", got.From)
	}
	// Änderung nach Entfernen der Identität: Absender bleibt, Hinweis.
	upd := e.must(t, humanCLI, "drafts.update", map[string]any{"draft_id": cr.DraftID, "subject": "neu"}).(DraftsWriteOutput)
	if upd.Preview.From != "Büro <buero@example.org>" || strings.Contains(upd.Preview.Text, "Gruß") {
		t.Fatalf("update after removal = %+v", upd)
	}
	if _, err := e.do(t, humanCLI, "identities.remove", map[string]any{"id": 3}, false); !errors.Is(err, store.ErrIdentityNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}

// Entscheid 40: policy.get nennt für einen Schlüssel die Operationen nur
// für Menschen als eigene Liste; für den Menschen ist sie leer.
func TestPolicyGetHumanOnly(t *testing.T) {
	e := newDraftEnv(t, false)
	out := e.must(t, policy.Key{ID: "key:leser", Role: "reader", Scopes: policy.Roles["reader"]}, "policy.get", map[string]any{}).(PolicyOutput)
	var names []string
	for _, op := range out.HumanOnly {
		names = append(names, op.Name)
	}
	for _, want := range []string{"drafts.approve", "drafts.reject", "drafts.abandon", "drafts.mark_sent", "identities.add", "accounts.set"} {
		if !strings.Contains(strings.Join(names, " "), want) {
			t.Errorf("human_only lacks %s: %v", want, names)
		}
	}
	for _, op := range out.Operations {
		if strings.Contains(strings.Join(names, " "), op.Name) {
			t.Errorf("%s in operations and human_only", op.Name)
		}
	}
	out = e.must(t, humanCLI, "policy.get", map[string]any{}).(PolicyOutput)
	if len(out.HumanOnly) != 0 {
		t.Fatalf("human: human_only = %v", out.HumanOnly)
	}
}
