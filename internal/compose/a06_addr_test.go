package compose

import (
	"strings"
	"testing"
)

// A06 Teil 1: FormatAddress schreibt einen Text, den parseAddress (net/mail)
// wieder zur selben Adresse liest. Erwartete Ausnahmen: Name gleich der
// Adresse (auch in anderer Schreibweise) ergibt Name "", mehrfacher
// Leerraum wird ein Leerzeichen.
func TestA06FormatAddressRoundtrip(t *testing.T) {
	const addr = "h@example.com"
	cases := []struct{ name, want, text string }{
		{"", "", "h@example.com"},
		{"Hans Muster", "Hans Muster", "Hans Muster <h@example.com>"},
		{"Nachname, Vorname", "Nachname, Vorname", `"Nachname, Vorname" <h@example.com>`},
		{"Hans (Büro)", "Hans (Büro)", `"Hans (Büro)" <h@example.com>`},
		{`Hans "Hausmeister" Muster`, `Hans "Hausmeister" Muster`, `"Hans \"Hausmeister\" Muster" <h@example.com>`},
		{`Muster\Hans`, `Muster\Hans`, `"Muster\\Hans" <h@example.com>`},
		{"Team: Technik", "Team: Technik", `"Team: Technik" <h@example.com>`},
		{"Hausverwaltung; Büro", "Hausverwaltung; Büro", `"Hausverwaltung; Büro" <h@example.com>`},
		{"Dr. Hans Muster", "Dr. Hans Muster", `"Dr. Hans Muster" <h@example.com>`},
		{"Jürgen Müßig", "Jürgen Müßig", "Jürgen Müßig <h@example.com>"},
		{"Müller, Jürgen", "Müller, Jürgen", `"Müller, Jürgen" <h@example.com>`},
		{"Sonne 😀 Team", "Sonne 😀 Team", "Sonne 😀 Team <h@example.com>"},
		{"[extern] Hans <Büro>", "[extern] Hans <Büro>", `"[extern] Hans <Büro>" <h@example.com>`},
		{"x@example.org", "x@example.org", `"x@example.org" <h@example.com>`},
		{"=?utf-8?q?x?=", "=?utf-8?q?x?=", `"=?utf-8?q?x?=" <h@example.com>`},
		{"O'Brien", "O'Brien", "O'Brien <h@example.com>"},
		// Ausnahmen.
		{"h@example.com", "", "h@example.com"},
		{"H@Example.COM", "", "h@example.com"},
		{"Hans   \t Muster", "Hans Muster", "Hans Muster <h@example.com>"},
		{"  Hans  ", "Hans", "Hans <h@example.com>"},
		{"   ", "", "h@example.com"},
	}
	// Nachbesserung 2: lokaler Teil, der Anführungszeichen braucht.
	for _, c := range []struct{ name, address, text string }{
		{"", "a b@example.com", `"a b"@example.com`},
		{"Hans", "a b@example.com", `Hans <"a b"@example.com>`},
		{"", `a"b@example.com`, `"a\"b"@example.com`},
		{"", "a.b@example.com", "a.b@example.com"},
	} {
		text := FormatAddress(c.name, c.address)
		if text != c.text {
			t.Errorf("FormatAddress(%q, %q) = %q, want %q", c.name, c.address, text, c.text)
		}
		got, err := parseAddress("to[0]", text)
		if err != nil || got.Address != c.address || got.Name != c.name {
			t.Errorf("parse(format(%q, %q)) = %+v, %v", c.name, c.address, got, err)
		}
	}
	for _, c := range cases {
		text := FormatAddress(c.name, addr)
		if text != c.text {
			t.Errorf("FormatAddress(%q) = %q, want %q", c.name, text, c.text)
		}
		got, err := parseAddress("to[0]", text)
		if err != nil {
			t.Errorf("parse(format(%q)) = %q: %v", c.name, text, err)
			continue
		}
		if got.Name != c.want || got.Address != addr {
			t.Errorf("parse(format(%q)) = %+v, want name %q", c.name, got, c.want)
		}
		if strings.Contains(text, "=?utf-8?b?") {
			t.Errorf("FormatAddress(%q) = %q: encoded-word in a text for people", c.name, text)
		}
	}
}

// Der Name bleibt im gebauten Entwurf erhalten: To in der Nachricht
// trägt den Namen kodiert und in Anführungszeichen, wo nötig.
func TestA06FormatAddressBuildsMessage(t *testing.T) {
	in := baseInput(KindNew)
	in.To = []string{FormatAddress("Nachname, Vorname", "h@example.com"), FormatAddress("x@example.com", "x@example.com"),
		FormatAddress("Hans (Büro)", "b@example.com")}
	m := mustBuild(t, in)
	if len(m.To) != 3 || m.To[0].Name != "Nachname, Vorname" || m.To[1].Name != "" || m.To[1].Address != "x@example.com" ||
		m.To[2].Name != "Hans (Büro)" {
		t.Fatalf("to = %+v", m.To)
	}
	if raw := strings.ReplaceAll(string(m.Raw), "\r\n ", " "); !strings.Contains(raw, "To: \"Nachname, Vorname\" <h@example.com>, <x@example.com>, =?utf-8?") {
		t.Fatalf("To header:\n%s", m.Raw)
	}
}

// Nachbesserung 2: Name gleich der Adresse fällt schon beim Bau weg; das
// erste drafts.update ändert dann weder Empfänger noch preview_hash.
func TestA06NameEqualsAddressNormalizedOnBuild(t *testing.T) {
	in := baseInput(KindNew)
	in.To = []string{`"x@example.com" <x@example.com>`, `"X@Example.com" <x@example.com>`, `"Hans" <h@example.com>`}
	m := mustBuild(t, in)
	if len(m.To) != 2 || m.To[0].Name != "" || m.To[1].Name != "Hans" {
		t.Fatalf("to = %+v", m.To)
	}
	in = baseInput(KindReply)
	in.Original.From = []Address{{Name: "anna@example.org", Address: "anna@example.org"}}
	in.Original.ReplyTo = nil
	m = mustBuild(t, in)
	if len(m.To) != 1 || m.To[0].Name != "" || m.To[0].Address != "anna@example.org" {
		t.Fatalf("derived to = %+v", m.To)
	}
}
