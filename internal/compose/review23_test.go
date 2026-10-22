package compose

import (
	"strings"
	"testing"
	"time"
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
