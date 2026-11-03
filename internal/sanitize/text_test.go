package sanitize

import "testing"

// A05 Teil 1: CutAtOriginal schneidet nach derselben Regel wie StripQuotes
// (Trennlinie, Unterstrich-Linie, Kopfblock), nicht nach einer
// Weiterleitung, und lässt „>"-Zeilen stehen.
func TestCutAtOriginal(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"separator", "Text\n> zitiert\n-----Original Message-----\nFrom: a@example.com\nalt", "Text\n> zitiert"},
		{"header block", "Text\n\nVon: A <a@example.com>\nGesendet: Montag\nAn: b@example.org\nBetreff: X\nalt", "Text\n"},
		{"underscores", "Text\n____________________\nalt", "Text"},
		{"nothing to cut", "Text\nVon: A\nnur eine Zeile", "Text\nVon: A\nnur eine Zeile"},
		{"after a forward", "Text\n---------- Forwarded message ----------\n-----Original Message-----\nalt",
			"Text\n---------- Forwarded message ----------\n-----Original Message-----\nalt"},
	} {
		if got := CutAtOriginal(c.in); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		// Gleiche Regel wie StripQuotes: Was CutAtOriginal abschneidet,
		// fehlt auch dort.
		if c.want != c.in && StripQuotes(c.in) != StripQuotes(c.want) {
			t.Errorf("%s: StripQuotes cuts elsewhere: %q vs %q", c.name, StripQuotes(c.in), StripQuotes(c.want))
		}
	}
}
