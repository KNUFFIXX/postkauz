package mailaddr

import (
	"strings"
	"testing"
)

// Zweite Nachbesserung T3, Punkt 9 (B3 der Nachprüfung): Fremde Adressen
// gelten nie als eigene. Key schreibt nur ASCII klein: strings.ToLower
// machte aus dem Kelvin-Zeichen U+212A ein ASCII-k und aus U+0130 (großes
// I mit Punkt) ein ASCII-i, eine fremde Adresse bekäme so den Schlüssel
// einer eigenen. Adressen ohne echte Namen; Gmail-Domains aus den
// Konstanten gm und ggm (PII-Gate).
func TestT3N2ForeignNeverOwn(t *testing.T) {
	own := NewSet("pktestowner"+gm, "pk.test@example.net")
	for _, a := range []string{
		"", " ", "pktestowner", gm, "pktestowner" + gm + ".evil.example.org", "pktestowner@evil.example.org",
		"pktestowner" + gm + "@evil.example.org", "pktestowner@gmai1.com.invalid", "pktestowner@gmail.co.invalid",
		"pktest.owner@googlemail.de.invalid", "pktestowner@g.mail.com.invalid",
		"pktеstowner" + gm,      // kyrillisches e
		"pktestowner@gmail。com", // ideographischer Punkt
		"pktest@example.net", "p.k.test@example.net", "pk.test+x@example.net",
		"\"pktestowner\"" + gm, // Quoted-Local-Part: anderer Schlüssel (nur falsch negativ)
		"+pktestowner" + gm, "." + gm,
	} {
		if own.Has(a) {
			t.Errorf("foreign address %q counts as own (key %q)", a, Key(a))
		}
	}
	for _, a := range []string{"PK.Test.Owner" + gm, "pktestowner+news" + ggm, " PKTESTOWNER" + strings.ToUpper(gm) + " ", "PK.TEST@EXAMPLE.NET"} {
		if !own.Has(a) {
			t.Errorf("own spelling %q not recognised", a)
		}
	}
	// Kelvin-Zeichen U+212A und U+0130: strings.ToLower macht daraus ASCII.
	own2 := NewSet("kunde@example.org", "info@example.org")
	for _, a := range []string{"Kunde@example.org", "İnfo@example.org", "KUNDE@EXAMPLE.ORG"} {
		if own2.Has(a) {
			t.Errorf("address %q with a non-ASCII capital counts as own (key %q)", a, Key(a))
		}
	}
	if got := Key("Kunde+x" + gm); got != "Kunde"+gm {
		t.Errorf("Key keeps non-ASCII letters: got %q", got)
	}
}

// Lower ist die exakte Schreibweise ohne Rücksicht auf ASCII-Groß- und
// Kleinschreibung (Wahl der Identität vor dem Vergleich nach Key).
func TestT3N2Lower(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{" PK.Test.Owner" + gm + " ", "pk.test.owner" + gm},
		{"Info@Example.ORG", "info@example.org"},
		{"Kunde@Example.org", "Kunde@example.org"},
		{"İnfo@example.org", "İnfo@example.org"},
		{"", ""},
	} {
		if got := Lower(c.in); got != c.want {
			t.Errorf("Lower(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
