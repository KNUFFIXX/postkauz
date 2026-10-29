package store

import (
	"encoding/json"
	"slices"
	"testing"
)

// Nachbesserung T3, Punkt 15: Beim Thread-Rückfall fallen die eigenen
// Adressen aus den Beteiligten, bei Gmail auch in anderer Schreibweise
// (Punkte, Zusatz ab „+", googlemail.com); andere Anbieter unverändert.
// Erfundene Adressen, Domain getrennt (PII-Gate).
func TestT3NParticipantsWithoutOwnGmailSpellings(t *testing.T) {
	const gm, ggm = "@gmail.com", "@googlemail.com"
	self := []string{"pktestowner" + gm, "demo@example.org"}
	to, _ := json.Marshal([]map[string]string{{"address": "PK.Test.Owner" + gm}, {"address": "anna@example.org"}})
	cc, _ := json.Marshal([]map[string]string{{"address": "pktestowner+x" + ggm}, {"address": "de.mo@example.org"}})
	got := participants("Bob <bob@example.org>", to, cc, self)
	if want := []string{"bob@example.org", "anna@example.org", "de.mo@example.org"}; !slices.Equal(got, want) {
		t.Fatalf("participants = %v, want %v", got, want)
	}
}
