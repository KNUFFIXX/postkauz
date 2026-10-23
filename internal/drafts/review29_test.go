package drafts

import (
	"context"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung S2.9, B1: Ein Entwurf ohne jeden Empfänger darf entstehen
// (der Mensch ergänzt), aber nicht zur Freigabe vorgelegt werden.
func TestReviewB1RequestReadyWithoutRecipients(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	c := content(canarySubject)
	c.To = nil
	r, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: c, CreatedBy: "key:agent"})
	if err != nil {
		t.Fatalf("create without recipients: %v", err)
	}
	d := r.Draft
	res, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{Fingerprint: Fingerprint(d), MessageHash: testMessageHash})
	if ReasonOf(err) != ReasonNoRecipients || res.Approval != nil {
		t.Fatalf("request without recipients: %+v %v", res, err)
	}
	got, err := e.st.Draft(ctx, d.ID)
	if err != nil || got.Status != string(StatusDraft) || got.ApprovalID != 0 || got.HookResult != "" {
		t.Fatalf("after refused request: %+v %v", got, err)
	}
	// Ein Bcc genügt.
	if err := CheckRecipients(func() store.Draft { x := got; x.Bcc = content("").To[:1]; return x }()); err != nil {
		t.Fatalf("bcc only: %v", err)
	}
}
