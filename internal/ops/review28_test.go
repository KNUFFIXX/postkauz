package ops

import (
	"strings"
	"testing"
)

// Gegenprüfung S2.8: Nachbesserungen an den Entwurfsausgaben.

// K1: Ist der Zeiger auf die Ausgangsnachricht leer (Spalte mit ON DELETE
// SET NULL), bleibt das gespeicherte Zitat trotzdem in der Hülle und läuft
// nicht als eigener Text mit source "draft".
func TestReviewK1QuoteStaysWrappedWithoutOriginalPointer(t *testing.T) {
	e := newDraftEnv(t, false)
	const own = "EigenerText-K1"
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "reply", "in_reply_to": 1, "body_markdown": own}).(DraftsWriteOutput)
	if err := e.st.ExecForTest(`UPDATE drafts SET in_reply_to_msg = NULL WHERE id = ?`, out.DraftID); err != nil {
		t.Fatal(err)
	}
	d := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if d.OriginalID != 0 || strings.Contains(d.BodyText, "> ") || d.Original == nil || d.Original.Part != "quote" ||
		d.Original.Source != "email" || !strings.Contains(d.Original.Text, "\n> ") {
		t.Fatalf("without pointer: body_text %q, original %+v", d.BodyText, d.Original)
	}
}
