package ops

import (
	"slices"
	"testing"
)

// Nachbesserung T3, Punkt 15: account set --aliases erkennt Dubletten und
// die Kontoadresse bei Gmail unabhängig von Punkten, Zusatz ab „+" und
// googlemail.com; andere Anbieter unverändert. Erfundene Adressen, Domain
// getrennt (PII-Gate).
func TestT3NAliasesGmailDuplicates(t *testing.T) {
	const gm, ggm = "@gmail.com", "@googlemail.com"
	got, err := normalizeAliases([]string{"PK.Test.Owner" + gm, "pktestowner+news" + ggm, "zweit.konto" + gm, "zweitkonto" + gm,
		"a.b@example.org", "ab@example.org"}, "pktestowner"+gm, "pktestowner"+gm)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.b@example.org", "ab@example.org", "zweit.konto" + gm}; !slices.Equal(got, want) {
		t.Fatalf("aliases = %v, want %v", got, want)
	}
}
