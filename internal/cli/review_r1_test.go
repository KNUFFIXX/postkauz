package cli

import (
	"slices"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Gegenprüfung S2.6–S2.8, K8: draft show --eml nennt alle Warnungen des
// Entwurfs auf stderr (Anhangtyp, ältere Kopien), nicht nur die älteren
// Kopien; jede nur einmal.
func TestDraftShowEMLNotesAllWarnings(t *testing.T) {
	attach := "attachment Rechnung.pdf: the content is not a PDF (declared application/pdf)"
	older := ops.OlderCopiesWarning(1, 2)
	got := draftShowNotes(ops.DraftDetail{EML: "Date: x\r\n\r\nx", EMLNote: "byte for byte the copy (uid 4)",
		Warnings: []string{attach, older}, OlderCopies: 2})
	want := []string{"eml: byte for byte the copy (uid 4)", attach, older}
	if !slices.Equal(got, want) {
		t.Fatalf("notes = %q, want %q", got, want)
	}
	if n := draftShowNotes(ops.DraftDetail{Warnings: []string{attach}}); n != nil {
		t.Fatalf("notes without --eml = %q", n)
	}
}
