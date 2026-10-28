package tags

import (
	"slices"
	"testing"
)

// V16: System-Keywords bleiben lokal, gleich wie geschrieben; gewöhnliche
// Namen (auch ähnliche) gehen auf den Server.
func TestReserved(t *testing.T) {
	for _, n := range []string{"junk", "Junk", "NonJunk", "notjunk", "$Junk", "$NotJunk", "$Forwarded", "$MDNSent", "$Phishing", "$label1", "$label5"} {
		if !Reserved(n) {
			t.Errorf("%s is not reserved", n)
		}
	}
	for _, n := range []string{"rechnung", "junk-mail", "forwarded", "label1", "spam"} {
		if Reserved(n) {
			t.Errorf("%s is reserved", n)
		}
	}
	sync, local := SplitReserved([]string{"junk", "rechnung", "notjunk"})
	if !slices.Equal(sync, []string{"rechnung"}) || !slices.Equal(local, []string{"junk", "notjunk"}) {
		t.Fatalf("split = %v / %v", sync, local)
	}
}
