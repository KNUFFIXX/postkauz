package ops

import (
	"slices"
	"strings"
	"testing"
)

// A04 Teil 1 (Lastenheft Vorgang 4): drafts.create mit kind=forward
// speichert In-Reply-To und References wie eine Antwort, drafts.get zeigt
// sie samt Herkunft (derived_from_email), und die abgelegte Kopie trägt sie,
// damit Gmail die Weiterleitung im Verlauf des Originals lässt.
func TestA04ForwardDraftStaysInThread(t *testing.T) {
	e := newPushEnv(t)
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "forward", "in_reply_to": 1, "to": []string{"c@example.org"},
		"include_attachments": false}).(DraftsWriteOutput)
	if out.Preview.InReplyTo != "<a@example.org>" || out.Preview.Subject != "Fwd: Hallo" {
		t.Fatalf("preview = %+v", out.Preview)
	}
	if out.DerivedFromEmail == nil || !slices.Contains(out.DerivedFromEmail.Fields, "in_reply_to") ||
		!slices.Contains(out.DerivedFromEmail.Fields, "references") {
		t.Fatalf("derived = %+v", out.DerivedFromEmail)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if got.InReplyTo != "<a@example.org>" || !strings.Contains(got.References, "<a@example.org>") {
		t.Fatalf("get: in_reply_to %q references %q", got.InReplyTo, got.References)
	}
	e.push(t)
	pushed := string(e.app.raws[len(e.app.raws)-1])
	head, _, _ := strings.Cut(pushed, "\r\n\r\n")
	if !strings.Contains(head, "\r\nIn-Reply-To: <a@example.org>\r\n") || !strings.Contains(head, "\r\nReferences: ") {
		t.Fatalf("pushed header block:\n%s", head)
	}
}
