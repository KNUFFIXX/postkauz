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
		"Re: Termin\u200b am Montag":       "Termin am Montag",     // unsichtbare Zeichen fallen weg
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
	sig := "Max Muster\nMusterstraße 1\n00000 Beispielstadt\nTelefon 0000 111111\nwww.example.org"
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

// Abnahme D5 (23.10.2026, Gmail im Browser): Gmail sendet den unveränderten
// Entwurf mit neuer Message-ID, bricht Zeilen neu um und schickt das Zitat
// ohne „>". StripQuotes lässt Einleitung und Zitat dann stehen; ohne die
// zweite Lesart (CutAtQuote am Zitat des Entwurfs) lag die Ähnlichkeit bei
// 0,58.
const (
	d5Own   = "Hallo,\n\ndas ist Runde 1 der Abnahme D5 (Webmail). Diesen Entwurf hat PostKauz angelegt. Bitte unverändert senden.\n\nViele Grüße"
	d5Intro = "Am 23.10.2026 18:51 schrieb Anna Beispiel <anna@example.org>:"
)

// d5Draft ist ein Antwort-Entwurf mit eigenem Text und dem Zitat, wie
// compose es in body_text baut.
func d5Draft(own, intro, quote string) store.Draft {
	d := fbDraft(own)
	d.BodyText = own + "\n\n" + intro + "\n> " + strings.ReplaceAll(quote, "\n", "\n> ") + "\n"
	return d
}

func TestFallbackGmailQuoteWithoutMarker(t *testing.T) {
	d := d5Draft(d5Own, d5Intro, "Test")
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
	gmail := func(own, intro, quote string) string {
		return strings.Replace(own, "PostKauz angelegt.", "PostKauz\nangelegt.", 1) + "\n\n" + intro + "\n\n" + quote + "\n"
	}
	sent := gmail(d5Own, d5Intro, "Test")
	if full := fd.Score(Words(SentOwnText(sentTextOf(sent)))); full >= FallbackMinJaccard {
		t.Fatalf("test is too weak: the full text already scores %.2f", full)
	}
	m := fbSent("Re: Termin am Montag", d.UpdatedAt.Add(3*time.Minute))
	if s, ok := fd.Match(m, sentTextOf(sent)); !ok || s != 1 {
		t.Fatalf("unchanged draft sent by Gmail not matched (score %.3f)", s)
	}
	// Einleitung über zwei Zeilen umbrochen.
	wrapped := gmail(d5Own, "Am 23.10.2026 18:51 schrieb Anna Beispiel\n<anna@example.org>:", "Test")
	if s, ok := fd.Match(m, sentTextOf(wrapped)); !ok {
		t.Fatalf("wrapped intro not matched (score %.3f)", s)
	}
	for _, c := range []struct{ name, text string }{
		{"text changed before the quote", gmail(strings.Replace(d5Own, "das ist Runde 1 der Abnahme D5 (Webmail). Diesen Entwurf hat PostKauz angelegt.",
			"ich habe alles umgeschrieben, weil der Termin leider doch nicht passt und wir neu planen müssen.", 1), d5Intro, "Test")},
		{"draft text only inside the quote of another reply", "Nein, das machen wir anders.\n\n" + d5Intro + "\n\n" + d5Own},
		// Gegenprüfung N1, W1: eine Zeile, die nur wie eine Einleitung
		// aussieht, und ein Nachsatz unter dem Zitat schneiden nicht.
		{"own text after a look-alike intro", d5Own + "\n\nAm Freitag schrieb mir der Hausmeister:\nDie Heizung im Keller ist wieder kaputt, bitte schnell melden."},
		{"other intro of the same sender", d5Own + "\n\nAnna Beispiel schrieb am Montag dazu:\nBitte noch die Rechnung von September schicken, die fehlt uns."},
		{"postscript under the quote", gmail(d5Own, d5Intro, "Test\n\nPS: Bitte bring morgen auch die Schlüssel für den Keller und den Dachboden mit.")},
	} {
		if s, ok := fd.Match(m, sentTextOf(c.text)); ok {
			t.Errorf("%s: matched (score %.3f)", c.name, s)
		}
	}
	// Ohne Zitat im Entwurf gibt es keine zweite Lesart.
	if _, ok := CutAtQuote(sent, NewFallbackDraft(fbDraft(d5Own), d5Own, "").Quote); ok {
		t.Fatal("cut without a quote in the draft")
	}
}

// Gegenprüfung N1, W2: Eine eigene [draft] quote_line, die der Filter für
// Zitat-Einleitungen nicht kennt, hilft trotzdem, weil die zweite Lesart an
// der Einleitung des Entwurfs selbst schneidet.
func TestFallbackCustomQuoteLine(t *testing.T) {
	intro := "Anna Beispiel <anna@example.org> meinte dazu:"
	d := d5Draft(d5Own, intro, "Passt dir Montag?\nGruß Anna")
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
	sent := d5Own + "\n\n" + intro + "\n\nPasst dir Montag?\nGruß Anna\n"
	if s, ok := fd.Match(fbSent("Re: Termin am Montag", d.UpdatedAt), sentTextOf(sent)); !ok {
		t.Fatalf("custom quote line not matched (score %.3f)", s)
	}
}

// QuoteOf nimmt den letzten Block mit „>" und die Zeile davor; „>"-Zeilen
// im eigenen Text davor zählen nicht.
func TestQuoteOf(t *testing.T) {
	q := QuoteOf("Zu deiner Frage:\n> Wann?\nMontag.\n\nAm 01.10.2026 schrieb Anna <anna@example.org>:\n> Wann?\n> Gruß\n")
	if q.Line == nil || len(q.Line) != len(Words("Am 01.10.2026 schrieb Anna <anna@example.org>:")) {
		t.Fatalf("line = %v", q.Line)
	}
	if _, ok := q.Words["montag"]; ok {
		t.Fatal("own text counted as quote")
	}
	if q := QuoteOf("Nur eigener Text.\n\nGruß"); q.Line != nil {
		t.Fatal("quote without > lines")
	}
	if q := QuoteOf("> nur Zitat"); q.Line != nil {
		t.Fatal("quote without intro line")
	}
}

// A05 Teil 1: Antwort auf eine Mail mit Verlauf im Outlook-Stil. StripQuotes
// schneidet den gesendeten Text am Outlook-Kopfblock ab; das Zitat des
// Entwurfs wird vor dem Vergleich genauso gekürzt (symmetrische Kürzung).
const (
	a05Body = "Hallo,\n\nanbei die Unterlagen zur Wohnung im zweiten Stock. Bitte prüfen Sie die Abrechnung bis Ende der Woche\nund geben Sie uns kurz Bescheid, ob alles passt.\n\nMit freundlichen Grüßen\nHausverwaltung Beispiel"
	// a05History: langer Verlauf mit Wörtern, die im eigenen Text und im
	// zitierten Text nicht vorkommen.
	a05History = "Sehr geehrte Damen und Herren,\n\nwir beantragen eine Ratenzahlung für die Nachforderung aus der Heizkostenabrechnung 2025.\nDer Heizungsmonteur kommt laut Terminplan erst im Dezember, das Thermostat im Badezimmer klemmt weiterhin.\nZusätzlich fehlt seit Juli die Beleuchtung im Treppenhaus, der Hausmeister wurde mehrfach informiert.\nFreundliche Grüße\nFamilie Muster"
)

// a05Forms sind alle Formen aus originalRe und der Kopfblock ohne Trennlinie,
// deutsch und englisch.
var a05Forms = []struct{ name, head string }{
	{"ursprüngliche nachricht", "-----Ursprüngliche Nachricht-----\nVon: Familie Muster <muster@example.com>\nGesendet: Montag, 2. November 2026 10:00\nAn: info@example.org\nBetreff: Heizung"},
	{"original message", "-----Original Message-----\nFrom: Familie Muster <muster@example.com>\nSent: Monday, November 2, 2026 10:00 AM\nTo: info@example.org\nSubject: Heizung"},
	{"originalnachricht", "-----Originalnachricht-----\nVon: Familie Muster <muster@example.com>\nDatum: 02.11.2026\nBetreff: Heizung"},
	{"message d'origine", "-----Message d'origine-----\nDe: Familie Muster <muster@example.com>\nEnvoyé: lundi 2 novembre 2026 10:00\nObjet: Heizung"},
	{"underscore line", "________________________________\nVon: Familie Muster <muster@example.com>\nGesendet: Montag, 2. November 2026 10:00\nAn: info@example.org\nBetreff: Heizung"},
	{"header block de", "Von: Familie Muster <muster@example.com>\nGesendet: Montag, 2. November 2026 10:00\nAn: info@example.org\nBetreff: Heizung"},
	{"header block en", "From: Familie Muster <muster@example.com>\nSent: Monday, November 2, 2026 10:00 AM\nTo: info@example.org\nSubject: Heizung"},
}

// gmailSent baut den Text, den Gmail im Browser sendet: eigener Text (neu
// umbrochen), Einleitung, Leerzeile, Zitat ohne „>".
func gmailSent(own, intro, quote string) string {
	return strings.Replace(own, "PostKauz angelegt.", "PostKauz\nangelegt.", 1) + "\n\n" + intro + "\n\n" + quote + "\n"
}

// (1) Unverändert gesendet, Zitat ohne „>": Rückfall trifft mit 1,0, für
// jede Form des Verlaufs.
func TestFallbackOutlookHistory(t *testing.T) {
	for _, f := range a05Forms {
		quote := a05Body + "\n\n" + f.head + "\n\n" + a05History
		d := d5Draft(d5Own, d5Intro, quote)
		fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
		sent := sentTextOf(gmailSent(d5Own, d5Intro, quote))
		if strings.Contains(sent, "Ratenzahlung") {
			t.Fatalf("%s: StripQuotes did not cut at the history: %q", f.name, sent)
		}
		if full := fd.Score(Words(SentOwnText(sent))); full >= FallbackMinJaccard {
			t.Fatalf("%s: test is too weak: the full text already scores %.2f", f.name, full)
		}
		m := fbSent("Re: Termin am Montag", d.UpdatedAt.Add(3*time.Minute))
		if s, ok := fd.Match(m, sent); !ok || s != 1 {
			t.Errorf("%s: unchanged draft not matched (score %.3f)", f.name, s)
		}
	}
}

// Innere Zitate: Der Entwurf zitiert eine Mail, die selbst ein Zitat mit
// „>" trägt (body_text: „> > …"). Das Mailprogramm sendet das äußere Zitat
// ohne „>", das innere mit „>" (StripQuotes entfernt es dann) oder ganz
// ohne „>"; beides trifft.
func TestFallbackNestedQuote(t *testing.T) {
	inner := "> " + strings.ReplaceAll(a05History, "\n", "\n> ")
	quote := a05Body + "\n\nAm 01.11.2026 schrieb Familie Muster <muster@example.com>:\n" + inner
	d := d5Draft(d5Own, d5Intro, quote)
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
	m := fbSent("Re: Termin am Montag", d.UpdatedAt.Add(3*time.Minute))
	for name, q := range map[string]string{"inner quote kept": quote, "no marker at all": a05Body + "\n\nAm 01.11.2026 schrieb Familie Muster <muster@example.com>:\n" + a05History} {
		if s, ok := fd.Match(m, sentTextOf(gmailSent(d5Own, d5Intro, q))); !ok {
			t.Errorf("%s: not matched (score %.3f)", name, s)
		}
	}
}

// (2) Langes Zitat mit Outlook-Verlauf und ein Nachsatz unter dem
// zitierten Text aus Wörtern des Verlaufs: kein Schnitt (Jaccard, nicht
// „Enthaltensein"; die Gegenprüfung rechnete mit Enthaltensein 0,94).
func TestFallbackOutlookHistoryPostscript(t *testing.T) {
	for _, f := range a05Forms[:2] {
		quote := a05Body + "\n\n" + f.head + "\n\n" + a05History
		d := d5Draft(d5Own, d5Intro, quote)
		fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
		ps := "PS: Die Ratenzahlung für die Nachforderung aus der Heizkostenabrechnung klären wir mit dem Hausmeister, Thermostat und Beleuchtung im Treppenhaus folgen im Dezember."
		sent := sentTextOf(gmailSent(d5Own, d5Intro, a05Body+"\n\n"+ps+"\n\n"+f.head+"\n\n"+a05History))
		if _, ok := CutAtQuote(SentOwnText(sent), fd.Quote); ok {
			t.Errorf("%s: postscript under the quote was cut away", f.name)
		}
		if s, ok := fd.Match(fbSent("Re: Termin am Montag", d.UpdatedAt), sent); ok {
			t.Errorf("%s: matched (score %.3f)", f.name, s)
		}
		// Enthaltensein hätte geschnitten: Das gekürzte Zitat steht ganz im Rest.
		rest := Words(sent[strings.Index(sent, "Am 23.10.2026"):])
		in := 0
		for w := range fd.Quote.Words {
			if _, ok := rest[w]; ok {
				in++
			}
		}
		if float64(in)/float64(len(fd.Quote.Words)) < FallbackMinJaccard {
			t.Fatalf("%s: test is too weak: containment would not cut either", f.name)
		}
	}
}

// (3) Eine Zeile wie die Einleitung des Entwurfs, danach mehrere eigene
// Sätze statt des Zitats: kein Schnitt, auch mit Outlook-Verlauf im Zitat.
func TestFallbackOutlookHistoryLookAlikeIntro(t *testing.T) {
	quote := a05Body + "\n\n" + a05Forms[0].head + "\n\n" + a05History
	d := d5Draft(d5Own, d5Intro, quote)
	fd := NewFallbackDraft(d, compose.OwnText(d.BodyMarkdown, ""), "")
	own := "Die Abrechnung habe ich gelesen. Leider stimmen die Zählerstände nicht.\nBitte schicken Sie mir die Ablesebelege vom Januar. Danach melde ich mich wieder.\nDer Termin am Montag fällt aus."
	for _, sent := range []string{
		d5Own + "\n\n" + d5Intro + "\n\n" + own,
		d5Own + "\n\nAm Freitag schrieb mir der Hausmeister:\n" + own,
	} {
		if _, ok := CutAtQuote(SentOwnText(sentTextOf(sent)), fd.Quote); ok {
			t.Errorf("cut at a look-alike intro: %q", sent)
		}
		if s, ok := fd.Match(fbSent("Re: Termin am Montag", d.UpdatedAt), sentTextOf(sent)); ok {
			t.Errorf("matched (score %.3f): %q", s, sent)
		}
	}
}
