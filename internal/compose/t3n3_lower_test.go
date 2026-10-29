package compose

import "testing"

// Dritte Nachbesserung T3, Punkt 9a: normAddr schreibt nur ASCII klein; eine
// Adresse mit Kelvin-Zeichen U+212A ist nicht die mit „k“.
func TestT3N3NormAddrASCIIOnly(t *testing.T) {
	if got := normAddr(" Owner@\u212Aanzlei.example.org "); got != "owner@\u212Aanzlei.example.org" {
		t.Errorf("normAddr = %q", got)
	}
}
