package search

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFoldGermanVariants(t *testing.T) {
	same := [][]string{
		{"prüfen", "pruefen", "prufen", "PRÜFEN", "Pruefen"},
		{"Erdgeschoß", "Erdgeschoss", "ERDGESCHOSS", "Erdgeschoẞ"},
		{"Zählerstände", "Zaehlerstaende", "zahlerstande"},
		{"Müller", "Mueller", "muller"},
		{"Übergabe", "Uebergabe", "ubergabe"},
		{"Zoë", "Zoe", "zo"},
		{"ﬁnal", "final"},        // Ligatur
		{"Ｒｅｃｈｎｕｎｇ", "rechnung"}, // Vollbreite
		{"café", "cafe"},
	}
	for _, group := range same {
		want := Fold(group[0])
		for _, s := range group[1:] {
			if got := Fold(s); got != want {
				t.Errorf("Fold(%q) = %q, Fold(%q) = %q", s, got, group[0], want)
			}
		}
	}
	// „ue" bleibt nach q und wenn das u zwischen Vokalen steht.
	for in, want := range map[string]string{"Steuer": "steuer", "Quelle": "quelle", "neue": "neue", "Bauer": "bauer",
		"Grundsteuer": "grundsteuer", "Frequenz": "frequenz", "Straße": "strasse", "a\x00b": "a b"} {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
	if got := FoldText("  Erdgeschoß\n\n links\t"); got != "erdgeschoss links" {
		t.Errorf("FoldText = %q", got)
	}
}

func TestStemmerGerman(t *testing.T) {
	st := NewStemmer()
	for _, pair := range [][2]string{{"Rechnung", "Rechnungen"}, {"prüfen", "pruefen"}, {"Haus", "Häuser"},
		{"Mahnung", "Mahnungen"}, {"Erdgeschoß", "Erdgeschosses"}} {
		a, b := st.StemText(pair[0]), st.StemText(pair[1])
		if a != b || a == "" {
			t.Errorf("stem(%q) = %q, stem(%q) = %q; want equal", pair[0], a, pair[1], b)
		}
	}
	if got := st.StemText("Die Rechnungen, bitte prüfen!"); got != "die rechnung bitt pruf" {
		t.Errorf("StemText = %q", got)
	}
	long := strings.Repeat("x", 80)
	if got := st.StemText("kurz " + long); got != "kurz" {
		t.Errorf("long token kept: %q", got)
	}
}

func TestBuildDoc(t *testing.T) {
	d := NewStemmer().Build(Source{Subject: "Rechnungen für das Erdgeschoß", From: "Bärbel Beispiel <baerbel@beispiel.example.org>",
		To: json.RawMessage(`[{"name":"Postfach Test","address":"postfach@example.org"}]`), Body: "Bitte prüfen.",
		Attachments: []string{"Zählerstände.pdf", "Text der Anlage"}})
	if d.Word[ColSubject] != "rechnung fur das erdgeschoss" || d.Tri[ColSubject] != "rechnungen fuer das erdgeschoss" {
		t.Errorf("subject = %q / %q", d.Word[ColSubject], d.Tri[ColSubject])
	}
	if !strings.Contains(d.Tri[ColSender], "barbel@beispiel.example.org") || !strings.Contains(d.Word[ColRecipients], "postfach") {
		t.Errorf("sender/recipients = %q / %q", d.Tri[ColSender], d.Word[ColRecipients])
	}
	if d.Tri[ColAttachments] != "zahlerstande.pdf text der anlage" {
		t.Errorf("attachments = %q", d.Tri[ColAttachments])
	}
	big := NewStemmer().Build(Source{Body: strings.Repeat("ä", MaxBodyBytes)})
	if len(big.Tri[ColBody]) > MaxBodyBytes {
		t.Errorf("body not truncated: %d", len(big.Tri[ColBody]))
	}
}

func TestParseQueryLanguage(t *testing.T) {
	q, err := Parse(`Rechnung -mahnung "Erdgeschoss links" from:anna to:"Postfach Test" subject:Zähler has:attachment `+
		`is:unread -in:Archiv thread:7 tag:wichtig after:2026-01-01 before:31.12.2026 ab -"x y"`, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	type tw struct {
		text, field, mode, word, tri string
		neg                          bool
	}
	want := []tw{
		{"Rechnung", "", "stem", `"rechnung"`, `"rechnung"`, false},
		{"mahnung", "", "stem", `"mahnung"`, `"mahnung"`, true},
		{"Erdgeschoss links", "", "phrase", `"erdgeschoss link"`, `"erdgeschoss links"`, false},
		{"anna", "from", "stem", `{sender} : "anna"`, `{sender} : "anna"`, false},
		{"Postfach Test", "to", "phrase", `{recipients} : "postfach test"`, `{recipients} : "postfach test"`, false},
		{"Zähler", "subject", "stem", `{subject} : "zahl"`, `{subject} : "zahler"`, false},
		{"ab", "", "prefix", `"ab"*`, "", false},
		{"x y", "", "phrase", `"x y"`, `"x y"`, true},
	}
	if len(q.Terms) != len(want) {
		t.Fatalf("terms = %+v", q.Terms)
	}
	for i, w := range want {
		g := q.Terms[i]
		if g.Text != w.text || g.Field != w.field || g.Mode != w.mode || g.Word != w.word || g.Tri != w.tri || g.Negated != w.neg {
			t.Errorf("term %d = %+v, want %+v", i, g, w)
		}
	}
	kinds := ""
	for _, f := range q.Filters {
		kinds += f.Kind + "=" + f.Value
		if f.Negated {
			kinds += "(not)"
		}
		kinds += " "
	}
	if kinds != "has_attachment= is=unread mailbox=Archiv(not) thread=7 tag=wichtig after=2026-01-01 before=2026-12-31 " {
		t.Errorf("filters = %q", kinds)
	}
	if q.Order() != "relevance" || len(q.Positive()) != 6 || len(q.Negative()) != 2 {
		t.Errorf("order %s, positive %d, negative %d", q.Order(), len(q.Positive()), len(q.Negative()))
	}
}

func TestParseKeepsInputOutOfFTSSyntax(t *testing.T) {
	// FTS5-Syntax in der Eingabe bleibt Suchtext (in Anführungszeichen).
	q, err := Parse(`a" OR subject:* NEAR(x) {body}:y "un"closed`, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	for _, tm := range q.Terms {
		for _, expr := range []string{tm.Word, tm.Tri} {
			if expr == "" {
				continue
			}
			body := strings.TrimPrefix(strings.TrimPrefix(expr, "{subject} : "), "{sender} : ")
			body = strings.TrimSuffix(body, "*")
			if !strings.HasPrefix(body, `"`) || !strings.HasSuffix(body, `"`) ||
				strings.Contains(strings.ReplaceAll(body[1:len(body)-1], `""`, ""), `"`) {
				t.Errorf("unquoted FTS expression %q from %q", expr, tm.Text)
			}
		}
	}
	if _, err := Parse(`subject:*`, time.UTC); err == nil {
		// nur Satzzeichen: ohne Wirkung, die Anfrage ist leer
		t.Error("query of punctuation only accepted")
	}
}

func TestParseErrorsAndShortTerms(t *testing.T) {
	var qe *QueryError
	for _, in := range []string{"", "   ", "has:pdf", "is:wichtig", "thread:abc", "after:gestern", "from:", "-",
		strings.Repeat("a ", 600), strings.Repeat("w ", 17)} {
		if _, err := Parse(in, time.UTC); !errors.As(err, &qe) {
			t.Errorf("Parse(%q) = %v, want a query error", in, err)
		}
	}
	q, err := Parse("has:attachment", time.UTC)
	if err != nil || q.Order() != "date" || len(q.Terms) != 0 {
		t.Fatalf("filter-only = %+v %v", q, err)
	}
	// unbekannter Operator bleibt Text, „Re:" ebenso
	q, err = Parse("foo:bar Re:", time.UTC)
	if err != nil || len(q.Terms) != 2 || q.Terms[0].Mode != "phrase" || q.Terms[1].Mode != "prefix" {
		t.Fatalf("unknown operator = %+v %v", q.Terms, err)
	}
	q, _ = Parse("üb", time.UTC)
	if q.Terms[0].Word != `"ub"*` || q.Terms[0].Tri != "" {
		t.Errorf("short term = %+v", q.Terms[0])
	}
	q, _ = Parse("+++ Vollstreck --", time.UTC)
	if len(q.Terms) != 2 || q.Terms[0].Mode != "substring" || q.Terms[1].Tri != `"vollstreck"` || len(q.Ignored) != 1 {
		t.Errorf("terms = %+v", q.Terms)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	q, _ := Parse("Rechnung", time.UTC)
	fp := q.Fingerprint("account:1")
	c := Cursor{Fingerprint: fp, Now: 1760000000000, Score: 12.345678901234567, ID: 42}
	got, err := DecodeCursor(c.Encode(), fp)
	if err != nil || got != c {
		t.Fatalf("cursor = %+v %v", got, err)
	}
	q2, _ := Parse("Rechnungen", time.UTC)
	if _, err := DecodeCursor(c.Encode(), q2.Fingerprint("account:1")); err == nil {
		t.Error("cursor of another query accepted")
	}
	if _, err := DecodeCursor(c.Encode(), q.Fingerprint("")); err == nil {
		t.Error("cursor of another account scope accepted")
	}
	if _, err := DecodeCursor("!!", fp); err == nil {
		t.Error("garbage cursor accepted")
	}
}

// Teilwortsuche (Trigram): Die Regel ue→u hängt vom Zusammenhang ab und
// gilt dort nicht; Teilwörter mitten aus „Steuer" oder „Quelle" treffen,
// ü und ue finden sich weiter gegenseitig.
func TestTrigramSubstringsWithUE(t *testing.T) {
	st := NewStemmer()
	cases := []struct {
		text, query string
		want        bool
	}{
		{"Steuerbescheid 2025", "uer", true},
		{"Steuerbescheid 2025", "steuerb", true},
		{"Quelle: Hausverwaltung", "uelle", true},
		{"Bauernhof", "auer", true},
		{"neue Zähler", "eue", true},
		{"Prüfung der Abrechnung", "pruef", true},
		{"Prüfung der Abrechnung", "prüf", true},
		{"Pruefung der Abrechnung", "prüf", true},
		{"Pruefung der Abrechnung", "uefung", true},
		{"Grüße", "gruesse", true},
		{"Mädchen", "maed", true},
		{"Steuer", "stur", false},
	}
	for _, c := range cases {
		d := st.Build(Source{Subject: c.text})
		q, err := Parse(c.query, time.UTC)
		if err != nil || len(q.Terms) != 1 || q.Terms[0].Tri == "" {
			t.Fatalf("%q: %+v %v", c.query, q, err)
		}
		needle := strings.Trim(q.Terms[0].Tri, `"`)
		if got := strings.Contains(d.Tri[ColSubject], needle); got != c.want {
			t.Errorf("%q in %q (index %q, query %q) = %v, want %v", c.query, c.text, d.Tri[ColSubject], needle, got, c.want)
		}
	}
}
