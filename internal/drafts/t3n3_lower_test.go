package drafts

import (
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
)

// Dritte Nachbesserung T3, Punkt 9a: fromAddress schrieb per strings.ToLower
// klein (Unicode) und hob so die ASCII-Härtung von mailaddr auf:
// Kelvin-Zeichen U+212A wurde "k", ein fremder Absender galt als eigener.
func TestT3N3FromAddressKelvin(t *testing.T) {
	own := mailaddr.NewSet("owner@kanzlei.example.org")
	foreign := "Fremd <owner@Kanzlei.example.org>"
	if own.Has(fromAddress(foreign)) {
		t.Errorf("foreign sender %q counts as own after fromAddress = %q", foreign, fromAddress(foreign))
	}
}
