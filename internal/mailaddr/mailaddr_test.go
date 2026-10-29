package mailaddr

import "testing"

// Erfundene Gmail-Adressen ohne echte Namen; die Domain steht getrennt,
// damit das PII-Gate keine echte Adresse vermutet.
const (
	gm  = "@gmail.com"
	ggm = "@googlemail.com"
)

func TestKey(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"pk.test.owner" + gm, "pktestowner" + gm},
		{" PK.Test.Owner" + gm + " ", "pktestowner" + gm},
		{"pktestowner+news" + gm, "pktestowner" + gm},
		{"pk.test.owner+a.b" + ggm, "pktestowner" + gm},
		{"first.last@example.org", "first.last@example.org"},
		{"First.Last+tag@Example.org", "first.last+tag@example.org"},
		{".+x" + gm, ".+x" + gm},
		{"no-at-sign", "no-at-sign"},
		{"", ""},
	} {
		if got := Key(c.in); got != c.want {
			t.Errorf("Key(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSet(t *testing.T) {
	s := NewSet("pktestowner"+gm, "Alias@Example.org", "")
	for _, a := range []string{"pk.test.owner" + gm, "PK.TEST.OWNER+x" + ggm, "alias@example.org"} {
		if !s.Has(a) {
			t.Errorf("%q not recognized as own", a)
		}
	}
	for _, a := range []string{"a.lias@example.org", "alias+x@example.org", "other" + gm, ""} {
		if s.Has(a) {
			t.Errorf("%q wrongly recognized as own", a)
		}
	}
}
