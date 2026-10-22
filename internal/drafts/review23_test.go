package drafts

import (
	"context"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// testMessageHash steht in den Dienst-Tests für den Hash der gebauten
// Nachricht (den baut erst internal/ops mit internal/compose).
const testMessageHash = "test-message-hash"

// Gegenprüfung S2.3, W1: Die Freigabe bindet den Hash der fertigen
// Nachricht. Weicht er beim Freigeben ab (oder fehlt er, Freigabe von vor
// Schema 12), fällt der Entwurf auf draft zurück, die Freigabe wird
// superseded, das Ergebnis ist approval_stale.
func TestApproveBindsMessageHash(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.create()
	r, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{Fingerprint: Fingerprint(d), MessageHash: "hash-a"})
	if err != nil || r.Approval == nil || r.Approval.MessageHash != "hash-a" {
		t.Fatalf("request: %+v %v", r.Approval, err)
	}
	if a, err := e.st.Approval(ctx, r.Approval.ID); err != nil || a.MessageHash != "hash-a" {
		t.Fatalf("stored approval: %+v %v", a, err)
	}
	res, err := e.svc.Approve(ctx, r.Approval.ID, "Mensch", "hash-b")
	if ReasonOf(err) != ReasonApprovalStale || res.To != StatusDraft {
		t.Fatalf("approve with another message: %+v %v", res, err)
	}
	got, _ := e.st.Draft(ctx, d.ID)
	a, _ := e.st.Approval(ctx, r.Approval.ID)
	if got.Status != string(StatusDraft) || a.Decision != DecisionSuperseded {
		t.Fatalf("after stale approve: draft %s, approval %s", got.Status, a.Decision)
	}
	// Neu vorgelegt und mit derselben Nachricht: ready.
	r, err = e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: "hash-b"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := e.svc.Approve(ctx, r.Approval.ID, "Mensch", "hash-b"); err != nil || res.To != StatusReady {
		t.Fatalf("approve: %+v %v", res, err)
	}
	// Eine Freigabe ohne Hash (vor Schema 12) ist nicht freizugeben.
	d2 := e.create()
	r, err = e.svc.RequestReady(ctx, d2.ID, "key:agent", HookPass, Checked{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, r.Approval.ID, "Mensch", ""); ReasonOf(err) != ReasonApprovalStale {
		t.Fatalf("approve without hash: %v", err)
	}
}

// Gegenprüfung S2.3, W2: Ändert ein zweiter Schreiber den Entwurf, während
// der Hook den alten Stand prüft, legt RequestReady nicht vor
// (draft_changed): keine Freigabe, kein Hook-Ergebnis, Zustand draft.
func TestRequestReadyRejectsChangedDraft(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.create()
	seen := Fingerprint(d) // so weit kam der Hook
	other := &Service{Store: e.st, Now: e.svc.Now}
	if _, err := other.Update(ctx, d.ID, content(canarySubject+" vom zweiten Schreiber"), ""); err != nil {
		t.Fatal(err)
	}
	for _, hook := range []string{HookPass, HookFail} {
		if _, err := e.svc.RequestReady(ctx, d.ID, "key:agent", hook, Checked{Fingerprint: seen, MessageHash: "h"}); ReasonOf(err) != ReasonDraftChanged {
			t.Fatalf("hook %s: %v, want draft_changed", hook, err)
		}
	}
	got, _ := e.st.Draft(ctx, d.ID)
	if got.Status != string(StatusDraft) || got.ApprovalID != 0 || got.HookResult != "" {
		t.Fatalf("draft after refused request: %+v", got)
	}
	// Mit dem aktuellen Stand klappt es.
	if r, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{Fingerprint: Fingerprint(got), MessageHash: "h"}); err != nil || r.To != StatusPendingApproval {
		t.Fatalf("request: %+v %v", r, err)
	}
}

// Gegenprüfung S2.3, W4: Update mit dem Fingerprint des gelesenen Stands
// überschreibt keine Änderung eines anderen Schreibers.
func TestUpdateRejectsStaleBase(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.create()
	base := Fingerprint(d)
	c := content(canarySubject)
	c.BodyText, c.BodyMarkdown = "Text von B", "Text von B"
	if _, err := e.svc.Update(ctx, d.ID, c, base); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(ctx, d.ID, content("Betreff von A"), base); ReasonOf(err) != ReasonDraftChanged {
		t.Fatalf("update on a stale base: %v", err)
	}
	got, _ := e.st.Draft(ctx, d.ID)
	if got.BodyText != "Text von B" || got.Subject != canarySubject {
		t.Fatalf("draft = %q %q", got.Subject, got.BodyText)
	}
	// Fingerprint: gleich für denselben Inhalt, anders bei jeder Eingabe des Baus.
	for name, change := range map[string]func(*store.Draft){
		"markdown": func(x *store.Draft) { x.BodyMarkdown += "!" },
		"options":  func(x *store.Draft) { x.ComposeOptions = `{"quote":false,"signature":true}` },
		"subject":  func(x *store.Draft) { x.Subject += "!" },
		"identity": func(x *store.Draft) { x.IdentityID++ },
	} {
		x := got
		change(&x)
		if Fingerprint(x) == Fingerprint(got) {
			t.Errorf("fingerprint ignores %s", name)
		}
	}
	x := got
	x.Status, x.HookResult = string(StatusReady), HookPass
	if Fingerprint(x) != Fingerprint(got) {
		t.Error("fingerprint depends on the status")
	}
}
