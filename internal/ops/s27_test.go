package ops

import (
	"strings"
	"testing"
)

// A02 S2.7 (ohne Server): drafts.get mit eml liefert genau die Bytes, die
// der Push dem Server übergeben hat (Rückgabewert des Push-Pfads über den
// Fake-Appender), auch wenn die Uhr des Aufrufs eine andere ist; nach einer
// Änderung sagt eml_matches_copy false und eml_note warum.
func TestDraftsGetEMLIsPushedBytes(t *testing.T) {
	e := newPushEnv(t)
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"Zoë <z@example.org>"},
		"subject": "Grüße – Termin", "body_markdown": "Hallo **Zoë**,\n\nbis morgen."}).(DraftsWriteOutput)
	before := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID, "eml": true}).(DraftDetail)
	if before.EML == "" || before.EMLMatchesCopy || !strings.Contains(before.EMLNote, "not in the Drafts folder yet") {
		t.Fatalf("before the push: matches %v, note %q", before.EMLMatchesCopy, before.EMLNote)
	}
	e.push(t)
	pushed := e.app.raws[len(e.app.raws)-1]
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID, "eml": true}).(DraftDetail)
	if got.EML != string(pushed) {
		t.Fatalf("eml differs from the pushed bytes\n--- eml ---\n%s\n--- pushed ---\n%s", got.EML, pushed)
	}
	if !got.EMLMatchesCopy || got.EMLDate != "Thu, 22 Oct 2026 14:03:05 +0200" || !strings.Contains(got.EMLNote, "byte for byte the copy (uid ") {
		t.Fatalf("after the push: %+v", got)
	}
	// Ohne eml kein Nachrichtentext in der Ausgabe.
	if plain := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail); plain.EML != "" || plain.EMLNote != "" {
		t.Fatalf("eml without asking: %+v", plain)
	}
	// Änderung: die Kopie ist älter.
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "subject": "Grüße – neuer Termin"})
	changed := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID, "eml": true}).(DraftDetail)
	if changed.EMLMatchesCopy || changed.EML == string(pushed) || !strings.Contains(changed.EMLNote, "changed after the copy") {
		t.Fatalf("after a change: %+v", changed)
	}
}
