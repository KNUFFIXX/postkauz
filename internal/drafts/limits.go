package drafts

import (
	"net/mail"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Grenzen (A02, „Vorgaben, die mehrere Schritte betreffen"). Gezählt wird in
// Zeichen (Unicode-Codepunkten), nicht in Bytes.
const (
	// MaxBodyChars gilt je für body_markdown, body_text und body_html.
	MaxBodyChars = 200000
	// MaxSubjectChars: Betreff (gefaltet wird erst beim Nachrichtenbau).
	MaxSubjectChars = 500
	// MaxRecipientsPerField: je To, Cc und Bcc.
	MaxRecipientsPerField = 50
	// MaxNameChars: Anzeigename einer Adresse (Schutz vor Riesenköpfen).
	MaxNameChars = 200
)

// Content ist der veränderliche Inhalt eines Entwurfs, wie ihn
// drafts.create/drafts.update (S2.3) nach dem Nachrichtenbau (S2.2)
// liefern: Empfänger, Betreff, Textformen, Kopfzeilen, Anhänge.
type Content struct {
	To, Cc, Bcc  []store.Addr
	Subject      string
	BodyMarkdown string
	BodyText     string
	BodyHTML     string
	// HeadersJSON: JSON-Objekt mit Kopfzeilen (z. B. In-Reply-To,
	// References); "" = {}.
	HeadersJSON string
	Attachments []store.DraftAttachment
}

// Validate prüft die Grenzen und Adressen. Eine Verletzung ist eine
// Ablehnung (DeniedError) mit Grund too_large, too_many_recipients,
// invalid_address oder invalid_header; die Meldung nennt Feld und Zahl,
// nie den Inhalt.
func Validate(c Content) error {
	for _, f := range []struct {
		name string
		v    string
	}{{"body_markdown", c.BodyMarkdown}, {"body_text", c.BodyText}, {"body_html", c.BodyHTML}} {
		if n := utf8.RuneCountInString(f.v); n > MaxBodyChars {
			return denied(ReasonTooLarge, "%s has %d characters (at most %d)", f.name, n, MaxBodyChars)
		}
	}
	if n := utf8.RuneCountInString(c.Subject); n > MaxSubjectChars {
		return denied(ReasonTooLarge, "subject has %d characters (at most %d)", n, MaxSubjectChars)
	}
	if hasLineBreakOrControl(c.Subject) {
		return denied(ReasonInvalidHeader, "subject contains a line break or control character")
	}
	for _, f := range []struct {
		name string
		list []store.Addr
	}{{"to", c.To}, {"cc", c.Cc}, {"bcc", c.Bcc}} {
		if len(f.list) > MaxRecipientsPerField {
			return denied(ReasonTooManyRecipients, "%s has %d recipients (at most %d)", f.name, len(f.list), MaxRecipientsPerField)
		}
		for i, a := range f.list {
			if err := ValidateAddr(a); err != nil {
				return denied(ReasonFor(err), "%s[%d]: %s", f.name, i, err.(*DeniedError).Msg)
			}
		}
	}
	if c.HeadersJSON != "" && !isJSONObject(c.HeadersJSON) {
		return denied(ReasonInvalidHeader, "headers must be a JSON object")
	}
	return nil
}

// ReasonFor ist ReasonOf mit Rückfall invalid_address.
func ReasonFor(err error) string {
	if r := ReasonOf(err); r != "" {
		return r
	}
	return ReasonInvalidAddress
}

// ValidateAddr prüft eine Adresse nach RFC 5322 (net/mail.ParseAddress):
// Die Adresse allein muss sich unverändert lesen lassen, der Name darf
// keine Zeilenumbrüche oder Steuerzeichen enthalten.
func ValidateAddr(a store.Addr) error {
	addr := strings.TrimSpace(a.Address)
	p, err := mail.ParseAddress(addr)
	if err != nil || p.Name != "" || p.Address != addr || !strings.Contains(addr, "@") {
		return denied(ReasonInvalidAddress, "not a valid e-mail address")
	}
	if hasLineBreakOrControl(a.Name) {
		return denied(ReasonInvalidHeader, "name contains a line break or control character")
	}
	if n := utf8.RuneCountInString(a.Name); n > MaxNameChars {
		return denied(ReasonTooLarge, "name has %d characters (at most %d)", n, MaxNameChars)
	}
	return nil
}

func hasLineBreakOrControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) || r == ' ' || r == ' ' }) >= 0
}
