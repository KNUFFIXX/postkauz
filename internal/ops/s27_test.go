package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
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

// Entscheid 43 (S2.7): drafts.get und drafts.list zählen ältere Kopien
// desselben Entwurfs im Entwürfe-Ordner laut Spiegel; die neueste Kopie
// (zuletzt abgelegt oder vom Mailclient gespeichert) zählt nicht, Kopien
// anderer Entwürfe auch nicht.
func TestOlderDraftCopies(t *testing.T) {
	e := newPushEnv(t)
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Termin", "body_markdown": "Hallo"}).(DraftsWriteOutput)
	id := out.DraftID
	e.push(t)
	first := e.draft(t, id)
	e.mirror(t, []store.NewMessage{mirrored(first)}, nil, uint32(first.RemoteUID)+1)
	count := func() (DraftDetail, int) {
		t.Helper()
		g := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
		l := e.must(t, assistant, "drafts.list", map[string]any{"account": "demo"}).(DraftsListOutput)
		n := -1
		for _, s := range l.Drafts {
			if s.DraftID == id {
				n = s.OlderCopies
			} else if s.OlderCopies != 0 {
				t.Fatalf("draft %d has older copies: %+v", s.DraftID, s)
			}
		}
		return g, n
	}
	if g, n := count(); g.OlderCopies != 0 || n != 0 || len(g.Warnings) != 0 {
		t.Fatalf("one copy: %+v, list %d", g, n)
	}
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": "Termin neu"})
	e.settle(t) // Entscheid 43 (S2.6): 30 s Ruhe vor der neuen Ablage
	e.push(t)
	second := e.draft(t, id)
	// Vor dem nächsten Sync kennt der Spiegel nur die alte Kopie: Sie ist
	// älter als die abgelegte (remote_uid).
	if g, _ := count(); g.OlderCopies != 1 || len(g.OlderCopyUIDs) != 1 || g.OlderCopyUIDs[0] != first.RemoteUID {
		t.Fatalf("before the sync: %+v", g)
	}
	e.mirror(t, []store.NewMessage{mirrored(second)}, nil, uint32(second.RemoteUID)+1)
	g, n := count()
	if g.OlderCopies != 1 || n != 1 || g.OlderCopyUIDs[0] != first.RemoteUID ||
		!slices.Contains(g.Warnings, OlderCopiesWarning(id, 1)) {
		t.Fatalf("two copies: %+v, list %d", g, n)
	}
	// Ein Mailclient speichert eine bearbeitete Fassung (höhere UID): Beide
	// Kopien von PostKauz sind jetzt älter.
	client := store.NewMessage{Link: store.LinkFlags{UID: 500, Draft: true}, MessageID: second.MessageIDHeader, ContentHash: "client", Size: 120}
	e.mirror(t, []store.NewMessage{client}, nil, 501)
	if g, n := count(); g.OlderCopies != 2 || n != 2 {
		t.Fatalf("after the client saved a copy: %+v, list %d", g, n)
	}
	// Der Mensch löscht beide alten Kopien: keine Warnung mehr.
	e.mirror(t, nil, []uint32{uint32(first.RemoteUID), uint32(second.RemoteUID)}, 501)
	if g, n := count(); g.OlderCopies != 0 || n != 0 {
		t.Fatalf("after deleting: %+v, list %d", g, n)
	}
}
