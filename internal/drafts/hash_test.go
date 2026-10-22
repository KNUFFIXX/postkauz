package drafts

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

func baseHash() HashInput {
	return HashInput{IdentityID: 2, FromName: "Demo Büro", FromEmail: "demo@example.org",
		To:      []store.Addr{{Name: "Anna", Address: "anna@example.org"}, {Address: "bert@example.net"}},
		Cc:      []store.Addr{{Name: "Cora", Address: "cora@example.org"}, {Address: "dora@example.org"}},
		Bcc:     []store.Addr{{Address: "eva@example.org"}, {Address: "fritz@example.org"}},
		Subject: "Re: Termin", BodyText: "Hallo Anna,\npasst.", BodyHTML: "<p>Hallo Anna,<br>passt.</p>",
		AttachmentHashes: []string{strings.Repeat("a", 64), strings.Repeat("b", 64)},
		InReplyTo:        "orig@example.org", Kind: KindReply}
}

func reversed(in []store.Addr) []store.Addr {
	out := make([]store.Addr, 0, len(in))
	for i := len(in) - 1; i >= 0; i-- {
		out = append(out, in[i])
	}
	return out
}

// E7: Umsortieren (und Schreibweise der Adresse, Leerraum) ändert den Hash
// nicht, jede Feldänderung schon.
func TestPreviewHashOrderAndFields(t *testing.T) {
	base := PreviewHash(baseHash())
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(base) {
		t.Fatalf("hash = %q", base)
	}
	same := map[string]func(*HashInput){
		"to reordered":          func(h *HashInput) { h.To = reversed(h.To) },
		"cc reordered":          func(h *HashInput) { h.Cc = reversed(h.Cc) },
		"bcc reordered":         func(h *HashInput) { h.Bcc = reversed(h.Bcc) },
		"attachments reordered": func(h *HashInput) { h.AttachmentHashes = []string{h.AttachmentHashes[1], h.AttachmentHashes[0]} },
		"address case/space":    func(h *HashInput) { h.To[0].Address = " Anna@Example.ORG " },
		"name whitespace":       func(h *HashInput) { h.To[0].Name = "  Anna " },
		"from email case":       func(h *HashInput) { h.FromEmail = "Demo@Example.org" },
		"in-reply-to brackets":  func(h *HashInput) { h.InReplyTo = "<orig@example.org>" },
	}
	for name, f := range same {
		h := baseHash()
		f(&h)
		if got := PreviewHash(h); got != base {
			t.Errorf("%s changed the hash", name)
		}
	}
	diff := map[string]func(*HashInput){
		"identity":             func(h *HashInput) { h.IdentityID = 3 },
		"from name":            func(h *HashInput) { h.FromName = "Demo" },
		"from email":           func(h *HashInput) { h.FromEmail = "andere@example.org" },
		"to address":           func(h *HashInput) { h.To[1].Address = "berta@example.net" },
		"to name":              func(h *HashInput) { h.To[0].Name = "Anne" },
		"to added":             func(h *HashInput) { h.To = append(h.To, store.Addr{Address: "neu@example.org"}) },
		"to removed":           func(h *HashInput) { h.To = h.To[:1] },
		"cc address":           func(h *HashInput) { h.Cc[0].Address = "cara@example.org" },
		"cc removed":           func(h *HashInput) { h.Cc = nil },
		"bcc address":          func(h *HashInput) { h.Bcc[0].Address = "eve@example.org" },
		"bcc added":            func(h *HashInput) { h.Bcc = append(h.Bcc, store.Addr{Address: "x@example.org"}) },
		"to moved to cc":       func(h *HashInput) { h.Cc = append(h.Cc, h.To[1]); h.To = h.To[:1] },
		"cc moved to bcc":      func(h *HashInput) { h.Bcc = append(h.Bcc, h.Cc[1]); h.Cc = h.Cc[:1] },
		"subject":              func(h *HashInput) { h.Subject = "Re: Termin!" },
		"subject whitespace":   func(h *HashInput) { h.Subject = "Re:  Termin" },
		"body text":            func(h *HashInput) { h.BodyText += " " },
		"body html":            func(h *HashInput) { h.BodyHTML = strings.Replace(h.BodyHTML, "passt", "passt nicht", 1) },
		"attachment changed":   func(h *HashInput) { h.AttachmentHashes[0] = strings.Repeat("c", 64) },
		"attachment added":     func(h *HashInput) { h.AttachmentHashes = append(h.AttachmentHashes, strings.Repeat("d", 64)) },
		"attachment removed":   func(h *HashInput) { h.AttachmentHashes = h.AttachmentHashes[:1] },
		"in-reply-to":          func(h *HashInput) { h.InReplyTo = "other@example.org" },
		"kind":                 func(h *HashInput) { h.Kind = KindReplyAll },
		"text moved into html": func(h *HashInput) { h.BodyHTML, h.BodyText = h.BodyText, h.BodyHTML },
	}
	seen := map[string]string{}
	for name, f := range diff {
		h := baseHash()
		f(&h)
		got := PreviewHash(h)
		if got == base {
			t.Errorf("%s did not change the hash", name)
		}
		if other, ok := seen[got]; ok {
			t.Errorf("%s and %s give the same hash", name, other)
		}
		seen[got] = name
	}
}

// Grenzen aus A02 („Vorgaben"): genau an der Grenze erlaubt, darüber
// denied mit Grund; Meldungen ohne Inhalt.
func TestValidateLimits(t *testing.T) {
	ok := Content{To: []store.Addr{{Name: "Anna", Address: "anna@example.org"}}, Subject: strings.Repeat("ü", MaxSubjectChars),
		BodyMarkdown: strings.Repeat("ä", MaxBodyChars), BodyText: strings.Repeat("x", MaxBodyChars), BodyHTML: strings.Repeat("y", MaxBodyChars)}
	for i := 0; i < MaxRecipientsPerField-1; i++ {
		ok.Cc = append(ok.Cc, store.Addr{Address: "cc@example.org"})
		ok.Bcc = append(ok.Bcc, store.Addr{Address: "bcc@example.org"})
	}
	ok.Cc = append(ok.Cc, store.Addr{Address: "cc@example.org"})
	ok.Bcc = append(ok.Bcc, store.Addr{Address: "bcc@example.org"})
	if err := Validate(ok); err != nil {
		t.Fatalf("at the limits: %v", err)
	}
	many := func() []store.Addr {
		out := make([]store.Addr, MaxRecipientsPerField+1)
		for i := range out {
			out[i] = store.Addr{Address: "x@example.org"}
		}
		return out
	}
	cases := map[string]struct {
		c      Content
		reason string
	}{
		"markdown too long":  {Content{BodyMarkdown: strings.Repeat("a", MaxBodyChars+1)}, ReasonTooLarge},
		"text too long":      {Content{BodyText: strings.Repeat("a", MaxBodyChars+1)}, ReasonTooLarge},
		"html too long":      {Content{BodyHTML: strings.Repeat("a", MaxBodyChars+1)}, ReasonTooLarge},
		"subject too long":   {Content{Subject: strings.Repeat("ß", MaxSubjectChars+1)}, ReasonTooLarge},
		"subject line break": {Content{Subject: "Hallo\r\nBcc: x@example.org"}, ReasonInvalidHeader},
		"to too many":        {Content{To: many()}, ReasonTooManyRecipients},
		"cc too many":        {Content{Cc: many()}, ReasonTooManyRecipients},
		"bcc too many":       {Content{Bcc: many()}, ReasonTooManyRecipients},
		"address no at":      {Content{To: []store.Addr{{Address: "anna"}}}, ReasonInvalidAddress},
		"address no domain":  {Content{To: []store.Addr{{Address: "anna@"}}}, ReasonInvalidAddress},
		"address with name":  {Content{Cc: []store.Addr{{Address: "Anna <anna@example.org>"}}}, ReasonInvalidAddress},
		"address two":        {Content{Bcc: []store.Addr{{Address: "a@example.org, b@example.org"}}}, ReasonInvalidAddress},
		"address header inj": {Content{To: []store.Addr{{Address: "a@example.org\r\nBcc: b@example.org"}}}, ReasonInvalidAddress},
		"name line break":    {Content{To: []store.Addr{{Name: "Anna\nBcc: b@example.org", Address: "a@example.org"}}}, ReasonInvalidHeader},
		"name too long":      {Content{To: []store.Addr{{Name: strings.Repeat("n", MaxNameChars+1), Address: "a@example.org"}}}, ReasonTooLarge},
		"headers not object": {Content{HeadersJSON: `["In-Reply-To"]`}, ReasonInvalidHeader},
	}
	for name, c := range cases {
		err := Validate(c.c)
		if !errors.Is(err, ErrDenied) || ReasonOf(err) != c.reason {
			t.Errorf("%s: err = %v, want denied %s", name, err, c.reason)
			continue
		}
		for _, leak := range []string{"Bcc:", "anna", "Hallo"} {
			if strings.Contains(err.Error(), leak) {
				t.Errorf("%s: error message contains content %q: %v", name, leak, err)
			}
		}
	}
}

// E11: pk.<20 Hex aus dem Zufall>@<Domain>, injizierbarer Zufall.
func TestNewMessageID(t *testing.T) {
	fixed := bytes.NewReader([]byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef, 0x10, 0x32})
	id, err := NewMessageID(fixed, "Demo@Example.ORG")
	if err != nil || id != "pk.0123456789abcdef1032@example.org" {
		t.Fatalf("id = %q, %v", id, err)
	}
	id, err = NewMessageID(bytes.NewReader(make([]byte, 10)), "a@bücher.example.org")
	if err != nil || id != "pk.00000000000000000000@xn--bcher-kva.example.org" {
		t.Fatalf("idn: %q, %v", id, err)
	}
	if id, err := NewMessageID(nil, "a@example.org"); err != nil || !regexp.MustCompile(`^pk\.[0-9a-f]{20}@example\.org$`).MatchString(id) {
		t.Fatalf("crypto/rand: %q, %v", id, err)
	}
	if _, err := NewMessageID(bytes.NewReader([]byte{1, 2}), "a@example.org"); err == nil {
		t.Error("short random source must fail")
	}
	for _, bad := range []string{"", "ohne-domain", "a@"} {
		if _, err := NewMessageID(nil, bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestDecidedBy(t *testing.T) {
	orig := currentUser
	t.Cleanup(func() { currentUser = orig })
	currentUser = func() (string, error) { return "betreiber", nil }
	if got := DecidedBy("  Demo Operator  "); got != "Demo Operator" {
		t.Errorf("configured: %q", got)
	}
	if got := DecidedBy(""); got != "betreiber" {
		t.Errorf("os user: %q", got)
	}
	currentUser = func() (string, error) { return "", errors.New("no user") }
	if got := DecidedBy(""); got != "human" {
		t.Errorf("fallback: %q", got)
	}
}
