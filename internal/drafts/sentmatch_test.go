package drafts

import (
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.6, E5: Regeln des Rückfall-Abgleichs ohne Datenbank.

func TestNormalizeSubject(t *testing.T) {
	for in, want := range map[string]string{
		"Termin am Montag":                 "Termin am Montag",
		"Re: Termin am Montag":             "Termin am Montag",
		"RE:Termin   am\tMontag ":          "Termin am Montag",
		"AW: Re: aw:  Termin am Montag":    "Termin am Montag",
		"Antw: Termin am Montag":           "Termin am Montag",
		"Fwd: WG: FW: Termin am Montag":    "Termin am Montag",
		"wg : Termin am Montag":            "Termin am Montag",
		"Rechnung: Re: bleibt":             "Rechnung: Re: bleibt", // nur Präfixe am Anfang
		"Reise: Hotel":                     "Reise: Hotel",         // kein Präfix, nur ähnlich
		"Re: Termin​ am Montag":            "Termin am Montag",     // unsichtbare Zeichen fallen weg
		"Termin am Montag (Re: Rückfrage)": "Termin am Montag (Re: Rückfrage)",
	} {
		if got := NormalizeSubject(in); got != want {
			t.Errorf("NormalizeSubject(%q) = %q, want %q", in, got, want)
		}
	}
	// Groß- und Kleinschreibung des Betreffs selbst bleibt (E5 nennt nur
	// Präfixe und Leerraum).
	if NormalizeSubject("Re: termin") == NormalizeSubject("Termin") {
		t.Error("subject case must not be folded")
	}
}

func TestRecipientKey(t *testing.T) {
	a := RecipientKey([]store.Addr{{Name: "Anna", Address: "Anna@Example.org"}}, []store.Addr{{Address: "bob@example.org "}})
	b := RecipientKey([]store.Addr{{Address: "bob@example.org"}, {Name: "A. Muster", Address: "anna@example.org"}}, nil)
	if a != b || a != "anna@example.org\nbob@example.org" {
		t.Fatalf("keys %q / %q", a, b)
	}
	// To und Cc zählen zusammen; Dubletten nur einmal.
	if RecipientKey([]store.Addr{{Address: "anna@example.org"}}, []store.Addr{{Address: "ANNA@example.org"}}) != "anna@example.org" {
		t.Fatal("duplicate across To and Cc")
	}
	if RecipientKey([]store.Addr{{Address: "anna@example.org"}}) == a {
		t.Fatal("different sets give the same key")
	}
}

func TestFallbackWindowBounds(t *testing.T) {
	up := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		sent time.Time
		want bool
	}{
		{up, true},
		{up.Add(-time.Hour), true},
		{up.Add(-time.Hour - time.Millisecond), false},
		{up.Add(14 * 24 * time.Hour), true},
		{up.Add(14*24*time.Hour + time.Millisecond), false},
		{up.Add(3 * 24 * time.Hour), true},
	} {
		if got := InFallbackWindow(up, c.sent); got != c.want {
			t.Errorf("sent %s: %v, want %v", c.sent.Sub(up), got, c.want)
		}
	}
}

// words10 sind zehn verschiedene Wörter.
const words10 = "eins zwei drei vier fünf sechs sieben acht neun zehn"

func TestJaccardThreshold(t *testing.T) {
	base := Words(words10)
	if len(base) != 10 {
		t.Fatalf("words = %d", len(base))
	}
	for _, c := range []struct {
		name  string
		other string
		score float64
		match bool
	}{
		{"same", words10, 1, true},
		{"same words, other order and case", "ZEHN neun acht sieben sechs fünf vier drei zwei eins eins", 1, true},
		{"one word more: 10/11", words10 + " elf", 10.0 / 11, true},
		{"one word less: 9/10 (exactly 0.9)", strings.TrimSuffix(words10, " zehn"), 0.9, true},
		{"two words more: 10/12", words10 + " elf zwölf", 10.0 / 12, false},
		{"one replaced: 9/11", strings.Replace(words10, "zehn", "elf", 1), 9.0 / 11, false},
		{"nothing in common", "ganz anderer text", 0, false},
	} {
		got := Jaccard(base, Words(c.other))
		if diff := got - c.score; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("%s: score %v, want %v", c.name, got, c.score)
		}
		if (got >= FallbackMinJaccard) != c.match {
			t.Errorf("%s: match %v, want %v", c.name, got >= FallbackMinJaccard, c.match)
		}
	}
	if Jaccard(Words(""), Words("")) != 0 {
		t.Error("two empty texts must not match")
	}
}

func TestSentOwnTextCutsForward(t *testing.T) {
	text := "Bitte ansehen.\n\n---------- Forwarded message ---------\nFrom: a@example.org\n\nAlter Text"
	if got := SentOwnText(text); strings.Contains(got, "Alter") || !strings.Contains(got, "Bitte ansehen.") {
		t.Fatalf("SentOwnText = %q", got)
	}
	tb := "Siehe unten\n-------- Weitergeleitete Nachricht --------\nBetreff: x"
	if got := SentOwnText(tb); strings.Contains(got, "Betreff") {
		t.Fatalf("SentOwnText = %q", got)
	}
}

// fbDraft ist ein Entwurf für die Abgleichstests.
func fbDraft(markdown string) store.Draft {
	return store.Draft{ID: 7, Subject: "Re: Termin am Montag", UpdatedAt: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC),
		To: []store.Addr{{Name: "Anna", Address: "anna@example.org"}}, Cc: []store.Addr{{Address: "bob@example.org"}},
		BodyMarkdown: markdown}
}

const fbBody = "Hallo Anna,\n\nder Termin am Montag um 10 Uhr passt mir gut. Ich bringe die Unterlagen mit.\n\nViele Grüße"

func fbSent(subject string, date time.Time) store.SentMessage {
	return store.SentMessage{ID: 99, Subject: subject, Date: date, InSent: true,
		To: []store.Addr{{Address: "bob@example.org"}}, Cc: []store.Addr{{Name: "Anna M.", Address: "ANNA@example.org"}}}
}

func TestFallbackMatch(t *testing.T) {
	d := fbDraft(fbBody)
	sig := "Max Muster\nMusterstraße 1\nTelefon 0000"
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), sig)
	at := d.UpdatedAt.Add(5 * time.Minute)
	// So sieht der zitatfreie Text der gesendeten Nachricht aus: eigener
	// Text, Signatur (Zitat und Einleitung hat StripQuotes beim Spiegeln
	// entfernt).
	sentPlain := fbBody + "\n\n" + sig

	for _, c := range []struct {
		name string
		m    store.SentMessage
		text string
		want bool
	}{
		{"same content, other prefix and recipient order", fbSent("AW: Termin am Montag", at), sentPlain, true},
		{"one word added", fbSent("Re: Termin am Montag", at), strings.Replace(sentPlain, "gut.", "gut, danke.", 1), true},
		{"text strongly changed", fbSent("Re: Termin am Montag", at), "Leider kann ich am Montag nicht. Geht Dienstag?\n\n" + sig, false},
		{"other subject", fbSent("Re: Termin am Dienstag", at), sentPlain, false},
		{"recipient missing", func() store.SentMessage { m := fbSent("Re: Termin am Montag", at); m.Cc = nil; return m }(), sentPlain, false},
		{"recipient added", func() store.SentMessage {
			m := fbSent("Re: Termin am Montag", at)
			m.Cc = append(m.Cc, store.Addr{Address: "carl@example.org"})
			return m
		}(), sentPlain, false},
		{"sent too early", fbSent("Re: Termin am Montag", d.UpdatedAt.Add(-2*time.Hour)), sentPlain, false},
		{"sent too late", fbSent("Re: Termin am Montag", d.UpdatedAt.Add(15*24*time.Hour)), sentPlain, false},
		{"forwarded part does not count", fbSent("Re: Termin am Montag", at),
			sentPlain + "\n\n---------- Forwarded message ---------\nFrom: x@example.org\nviele weitere wörter die nicht zählen dürfen", true},
	} {
		score, ok := fd.Match(c.m, c.text)
		if ok != c.want {
			t.Errorf("%s: match %v (score %.3f), want %v", c.name, ok, score, c.want)
		}
	}
}

// Zitat und Signatur fallen heraus: Ohne Entfernen läge die Ähnlichkeit
// deutlich unter 0,9.
func TestFallbackIgnoresQuoteAndSignature(t *testing.T) {
	short := "Passt, danke."
	d := fbDraft(short)
	sig := "Max Muster\nMusterstraße 1\n41460 Neustadt\nTelefon 0000 111111\nwww.example.org"
	fd := NewFallbackDraft(d, compose.OwnText(short, ""), sig)
	m := fbSent("Re: Termin am Montag", d.UpdatedAt)
	if _, ok := fd.Match(m, short+"\n\n"+sig); !ok {
		t.Fatal("signature words count against the draft")
	}
	if s := Jaccard(Words(short), Words(short+" "+sig)); s >= FallbackMinJaccard {
		t.Fatalf("test is too weak: without removal the score is %.2f", s)
	}
	// Ein Zitat, das der Agent selbst in den Entwurf schrieb, zählt nicht.
	quoted := short + "\n\n> Alte Frage mit vielen Wörtern, die im Zitat stehen und nicht zählen"
	fq := NewFallbackDraft(fbDraft(quoted), compose.OwnText(quoted, ""), "")
	if _, ok := fq.Match(m, short); !ok {
		t.Fatal("quote in the draft counts")
	}
	// Ohne eigenen Text oder ohne Empfänger kein Rückfall.
	if e := NewFallbackDraft(fbDraft(""), "", ""); len(e.Words) != 0 {
		t.Fatal("empty draft has words")
	}
}

// Die gesendete Nachricht im Format, das compose baut (Zitat mit „>",
// englische Einleitungszeile), ergibt nach StripQuotes den eigenen Text.
func TestFallbackWithComposedReply(t *testing.T) {
	d := fbDraft(fbBody)
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
	sent := fbBody + "\n\nOn Wed, 21 Oct 2026 09:15 +0200, Anna <anna@example.org> wrote:\n> Passt dir Montag?\n> Gruß Anna"
	if _, ok := fd.Match(fbSent("Re: Termin am Montag", d.UpdatedAt), sentTextOf(sent)); !ok {
		t.Fatal("composed reply not matched")
	}
}

func sentTextOf(plain string) string { return sentText(store.Body{TextPlain: plain}) }
