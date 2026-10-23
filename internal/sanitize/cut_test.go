package sanitize

import "testing"

// StripQuotes bleibt vorsichtig: Ohne „>" danach bleibt eine Einleitung
// wie „Am … schrieb …:" im Text (eigener Text danach ginge im Spiegel sonst
// verloren). Gmail im Browser sendet das Zitat eines Entwurfs so (Abnahme
// D5, 23.10.2026); den Fall löst der Rückfall-Abgleich in internal/drafts
// mit dem Zitat des Entwurfs selbst, nicht dieser Filter.
func TestStripQuotesKeepsAttributionWithoutMarker(t *testing.T) {
	in := "Danke.\n\nAm 03.03.2026 schrieb Anna <anna@example.org>:\n\nAlt"
	if got := StripQuotes(in); got != in {
		t.Fatalf("StripQuotes changed the text: %q", got)
	}
}
