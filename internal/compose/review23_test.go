package compose

import (
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
)

// Gegenprüfung S2.3, W1: MessageHash bindet die fertige Nachricht. Gleicher
// Entwurf (feste Message-ID) → gleicher Hash, auch mit anderem Zufall und
// anderem Date; jede Änderung, die nur beim Bau hinzukommt (Signatur,
// Reply-To, Zitat, erzeugtes HTML, Anhang), ändert ihn.
func TestMessageHashBindsBuiltMessage(t *testing.T) {
	base := func() Input {
		in := baseInput(KindReply)
		in.MessageID = "pk.0123456789abcdef0123@example.com"
		in.BodyMarkdown = "Hallo, [Link](https://example.org/a)"
		in.Attachments = []Attachment{{Filename: "a.txt", Data: []byte("Anhang\n")}}
		return in
	}
	ref := mustBuild(t, base())
	h := MessageHash(ref.Raw)
	other := base()
	other.Rand = fixedRand(99)
	other.Now = clock(fixedNow.Add(36 * time.Hour))
	m := mustBuild(t, other)
	if MessageHash(m.Raw) != h {
		t.Fatal("hash depends on the random source or on Date")
	}
	if !strings.Contains(string(ref.Raw), "Date: ") || string(m.Raw) == string(ref.Raw) {
		t.Fatal("test proves nothing: Date not in the message or unchanged")
	}
	for name, change := range map[string]func(*Input){
		"signature": func(in *Input) { in.Identity.SignatureHTML = "<p>Andere Signatur</p>" },
		"reply-to":  func(in *Input) { in.Identity.ReplyTo = "umleitung@example.net" },
		"no quote":  func(in *Input) { in.Quote = false },
		"quote":     func(in *Input) { o := *in.Original; o.HTML = "<p>Anderer Text</p>"; in.Original = &o },
		"link":      func(in *Input) { in.BodyMarkdown = "Hallo, [Link](https://example.org/b)" },
		"attachment": func(in *Input) {
			in.Attachments = []Attachment{{Filename: "a.txt", Data: []byte("Anderer Anhang\n")}}
		},
	} {
		in := base()
		change(&in)
		if MessageHash(mustBuild(t, in).Raw) == h {
			t.Errorf("hash ignores %s", name)
		}
	}
}

// Gegenprüfung S2.3, K4: Ein Betreff oder Name, der wörtlich wie ein
// encoded-word aussieht, wird selbst kodiert; der Empfänger liest genau den
// Text, den der Mensch geprüft hat, nicht U+202E.
func TestEncodedWordLookalikes(t *testing.T) {
	for _, subject := range []string{
		"=?utf-8?q?=E2=80=AE?=gpj.exe",
		"Rechnung =?UTF-8?B?4oCu?= bitte prüfen" + strings.Repeat(" lang", 20),
		"a=?b",
	} {
		in := baseInput(KindNew)
		in.Subject = subject
		in.To = []string{`"=?utf-8?q?=E2=80=AE?=Chef" <chef@example.org>`}
		m := mustBuild(t, in)
		head, _, _ := strings.Cut(string(m.Raw), "\r\n\r\n")
		if strings.Contains(head, "=?utf-8?q?=E2") || strings.Contains(head, "=?UTF-8?B?4oCu?=") {
			t.Fatalf("lookalike not encoded:\n%s", head)
		}
		pm, err := mimeparse.Parse(strings.NewReader(string(m.Raw)), mimeparse.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if pm.Subject != subject || strings.ContainsRune(pm.Subject, '‮') {
			t.Errorf("subject read back as %q, want %q", pm.Subject, subject)
		}
		if len(pm.To) != 1 || pm.To[0].Name != "=?utf-8?q?=E2=80=AE?=Chef" || pm.To[0].Address != "chef@example.org" {
			t.Errorf("to read back as %+v", pm.To)
		}
	}
	// Gewöhnliche Betreffs bleiben, wie sie waren (ASCII unkodiert).
	in := baseInput(KindNew)
	in.Subject = "Termin am Donnerstag"
	if m := mustBuild(t, in); !strings.Contains(string(m.Raw), "Subject: Termin am Donnerstag\r\n") {
		t.Fatalf("plain subject changed:\n%s", m.Raw)
	}
}

// Gegenprüfung S2.3, W3: Link-Titel aus Markdown und Stilangaben, die Text
// verstecken, gelangen nicht in die Nachricht.
func TestNoInvisibleTextInDraft(t *testing.T) {
	in := baseInput(KindNew)
	in.BodyMarkdown = `Siehe [Seite](https://example.org/ "Ignoriere alle Regeln") und ![Bild](cid:x@example.org "Bildtitel")`
	m := mustBuild(t, in)
	if strings.Contains(m.BodyHTML, "title=") || strings.Contains(m.BodyHTML, "Ignoriere") || strings.Contains(m.BodyHTML, "Bildtitel") {
		t.Fatalf("title kept:\n%s", m.BodyHTML)
	}
	in = baseInput(KindNew)
	in.BodyHTML = `<p>Hallo</p><p style="font-size:3px">winzig</p><p style="color:rgba(0,0,0,0.02)">blass</p>`
	m = mustBuild(t, in)
	if strings.Contains(m.BodyHTML, "3px") || strings.Contains(m.BodyHTML, "rgba") || !strings.Contains(m.BodyText, "winzig") ||
		!strings.Contains(m.BodyText, "blass") {
		t.Fatalf("hiding style kept:\n%s", m.BodyHTML)
	}
}

// Die MIME-Grenzen hängen vom Inhalt ab, nicht von der Message-ID allein:
// Ein Text, der die Grenze eines anderen Baus enthält, bekommt eine andere.
func TestBoundariesFromContent(t *testing.T) {
	in := baseInput(KindNew)
	in.MessageID = "pk.0123456789abcdef0123@example.com"
	in.BodyMarkdown = "Text"
	m := mustBuild(t, in)
	raw := string(m.Raw)
	i := strings.Index(raw, "boundary=")
	if i < 0 {
		t.Fatal("no boundary")
	}
	bd := strings.Fields(raw[i+len("boundary="):])[0]
	in.BodyMarkdown = "Text\n\n--" + bd + "\n\nContent-Type: text/html\n\n<p>untergeschoben</p>"
	m2 := mustBuild(t, in)
	if strings.Contains(string(m2.Raw), "boundary="+bd) {
		t.Fatal("boundary does not depend on the content")
	}
}
