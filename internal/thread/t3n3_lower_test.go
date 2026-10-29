package thread

import "testing"

// Dritte Nachbesserung T3, Punkt 9a: NormalizeAddress schreibt nur ASCII
// klein; das Kelvin-Zeichen U+212A bleibt.
func TestT3N3NormalizeAddressASCIIOnly(t *testing.T) {
	if got := NormalizeAddress("Fremd <Owner@\u212Aanzlei.example.org>"); got != "owner@\u212Aanzlei.example.org" {
		t.Errorf("NormalizeAddress = %q", got)
	}
}
