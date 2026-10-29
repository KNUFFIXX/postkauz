package compose

import (
	"slices"
	"testing"
)

// Nachbesserung T3, Punkt 15: „Allen antworten" erkennt eigene
// Gmail-Adressen unabhängig von Punkten, Zusatz ab „+" und googlemail.com;
// andere Anbieter unverändert. Erfundene Adressen, Domain getrennt
// (PII-Gate).
func TestT3NReplyAllDropsOwnGmailSpellings(t *testing.T) {
	const gm, ggm = "@gmail.com", "@googlemail.com"
	own := []string{"pktestowner" + gm}
	o := Original{From: []Address{{Address: "erika@example.org"}},
		To: []Address{{Address: "PK.Test.Owner" + gm}, {Address: "dritte@example.net"}},
		Cc: []Address{{Address: "pk.test.owner+list" + ggm}, {Address: "vierte@example.net"}, {Address: "pk.testowner@example.net"}}}
	to, cc := ReplyRecipients(o, own, true)
	if got := addrs(to); !slices.Equal(got, []string{"erika@example.org"}) {
		t.Errorf("to = %v", got)
	}
	if got := addrs(cc); !slices.Equal(got, []string{"dritte@example.net", "vierte@example.net", "pk.testowner@example.net"}) {
		t.Errorf("cc = %v (own gmail spellings must be dropped, other domains kept)", got)
	}
	// Eigene Mail (Absender in der Schreibweise mit Punkten) an Dritte:
	// Antwort an die ursprünglichen Empfänger.
	self := Original{From: []Address{{Address: "pk.test.owner" + gm}}, To: []Address{{Address: "dritte@example.net"}}}
	if to, _ := ReplyRecipients(self, own, false); !slices.Equal(addrs(to), []string{"dritte@example.net"}) {
		t.Errorf("reply to own dotted mail: to = %v", addrs(to))
	}
}
