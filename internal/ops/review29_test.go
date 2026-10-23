package ops

import (
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
)

// Gegenprüfung S2.9, B1: drafts.request_ready lehnt einen Entwurf ohne
// Empfänger ab (denied, no_recipients), bevor der Prüf-Hook läuft; anlegen
// darf man ihn, und nach dem Ergänzen eines Empfängers geht die Vorlage.
func TestReviewB1RequestReadyNeedsRecipient(t *testing.T) {
	e := newDraftEnv(t, true)
	out := e.must(t, assistant, "drafts.create", map[string]any{"account": "demo", "kind": "new", "subject": "Ohne Empfänger", "body_markdown": "Text"}).(DraftsWriteOutput)
	for _, dry := range []bool{true, false} {
		_, err := e.do(t, assistant, "drafts.request_ready", map[string]any{"draft_id": out.DraftID}, dry)
		wantDenied(t, err, drafts.ReasonNoRecipients)
	}
	if e.check.calls != 0 {
		t.Fatalf("check hook ran %d times for a draft without recipients", e.check.calls)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if got.Status != "draft" || got.ApprovalID != 0 {
		t.Fatalf("after refused request: %+v", got)
	}
	evs, _ := e.auditJSON(t)
	denied := 0
	for _, ev := range evs {
		if ev.Operation == "drafts.request_ready" && ev.Result == audit.ResultDenied && ev.Reason == drafts.ReasonNoRecipients {
			denied++
		}
	}
	if denied != 2 {
		t.Fatalf("%d audited no_recipients denials, want 2", denied)
	}
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "to": []string{"erika@example.org"}})
	if r := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": out.DraftID}).(DraftsRequestOutput); r.Status != "pending_approval" {
		t.Fatalf("request with recipient: %+v", r)
	}
}
