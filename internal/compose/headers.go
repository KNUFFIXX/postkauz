package compose

import (
	"encoding/hex"
	"fmt"
	"io"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"github.com/KNUFFIXX/postkauz/internal/mailaddr"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// MaxReferences ist die Höchstzahl der Einträge in References (E11).
const MaxReferences = 20

var (
	replyPrefixRe   = regexp.MustCompile(`(?i)^\s*(re|aw|antw)\s*(\[\d+\]|\(\d+\))?\s*:`)
	forwardPrefixRe = regexp.MustCompile(`(?i)^\s*(fwd|fw|wg)\s*(\[\d+\]|\(\d+\))?\s*:`)
)

// headerText bereinigt einen Kopfzeilenwert: unsichtbare Zeichen weg,
// Steuerzeichen (auch Zeilenumbrüche) werden Leerzeichen, außen kein
// Leerraum.
func headerText(s string) string {
	s = sanitize.Text(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// ReplySubject leitet den Betreff einer Antwort ab (E11): Ein vorhandenes
// Antwortpräfix bleibt, sonst kommt prefix davor (leer =
// DefaultReplyPrefix).
func ReplySubject(orig, prefix string) string {
	return prefixed(orig, prefix, DefaultReplyPrefix, replyPrefixRe)
}

// ForwardSubject leitet den Betreff einer Weiterleitung ab (E11): Ein
// vorhandenes Weiterleitungspräfix bleibt, sonst kommt prefix davor (leer =
// DefaultForwardPrefix).
func ForwardSubject(orig, prefix string) string {
	return prefixed(orig, prefix, DefaultForwardPrefix, forwardPrefixRe)
}

func prefixed(orig, prefix, def string, has *regexp.Regexp) string {
	s := headerText(orig)
	if has.MatchString(s) {
		return s
	}
	if prefix == "" {
		prefix = def
	}
	return strings.TrimSpace(headerText(prefix) + " " + s)
}

// References liefert In-Reply-To und References einer Antwort auf o (E11):
// In-Reply-To = Message-ID von o; References = References von o (fehlen
// sie, dann In-Reply-To von o) + Message-ID von o, ohne Dubletten und
// ungültige Einträge, bei mehr als MaxReferences der erste und die letzten
// MaxReferences-1. Ohne Message-ID bleibt inReplyTo leer.
func References(o Original) (inReplyTo string, refs []string) {
	base := o.References
	if len(base) == 0 {
		base = o.InReplyTo
	}
	seen := map[string]bool{}
	add := func(id string) {
		id = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(id), "<"), ">")
		if !validID(id) || seen[id] {
			return
		}
		seen[id] = true
		refs = append(refs, id)
	}
	for _, id := range base {
		add(id)
	}
	mid := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(o.MessageID), "<"), ">")
	if validID(mid) {
		inReplyTo = mid
		// Steht die eigene ID schon weiter vorn, gehört sie ans Ende.
		if seen[mid] {
			for i, r := range refs {
				if r == mid {
					refs = append(refs[:i], refs[i+1:]...)
					break
				}
			}
			delete(seen, mid)
		}
		add(mid)
	}
	if len(refs) > MaxReferences {
		refs = append([]string{refs[0]}, refs[len(refs)-(MaxReferences-1):]...)
	}
	return inReplyTo, refs
}

// validID: eine Message-ID ohne Klammern, ohne Leerraum und Steuerzeichen
// (sonst ließe sie sich nicht sicher in eine Kopfzeile schreiben).
func validID(id string) bool {
	if id == "" || len(id) > 998 || !utf8.ValidString(id) {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return r <= ' ' || r == '<' || r == '>' || r == 0x7f || unicode.IsSpace(r) || unicode.IsControl(r) || sanitize.Invisible(r)
	})
}

// NewMessageID erzeugt eine Message-ID nach E11 (ohne <>):
// pk.<20 Hex aus r>@<domain>. domain wird klein und als ASCII (Punycode)
// geschrieben; ungültig oder leer = "localhost.invalid".
func NewMessageID(r io.Reader, domain string) (string, error) {
	var buf [10]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return "", fmt.Errorf("compose: random source: %w", err)
	}
	return "pk." + hex.EncodeToString(buf[:]) + "@" + idDomain(domain), nil
}

func idDomain(d string) string {
	d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
	if a, err := idna.Lookup.ToASCII(d); err == nil {
		d = a
	}
	if d == "" || !validID(d) || strings.ContainsAny(d, "@\"()[]\\,;:") {
		return "localhost.invalid"
	}
	return d
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return ""
}

// normAddr ist der Vergleichsschlüssel einer Adresse.
// normAddr: nur ASCII klein (mailaddr.Lower, dritte Nachbesserung T3,
// Punkt 9a), wie der Vergleich mit den eigenen Adressen.
func normAddr(s string) string { return mailaddr.Lower(s) }

// ReplyRecipients leitet die Empfänger einer Antwort auf o ab (E11): To =
// Reply-To von o, sonst From. Mit all („Allen antworten") Cc = alle übrigen
// To/Cc von o. Eigene Adressen (own) und Dubletten fehlen in beiden Listen.
//
// Ist der Absender eine eigene Adresse (Nachricht aus Gesendet oder an sich
// selbst), geht die Antwort wie in Gmail und Thunderbird an die
// ursprünglichen Empfänger: To = To von o, mit all Cc = Cc von o. Eigene
// Adressen fallen dabei nur weg, solange mindestens ein Empfänger bleibt
// (ist To danach leer, rückt Cc nach To); bleibt keiner, ist To die
// ursprüngliche (eigene) Adresse: Eine Mail an sich selbst wird an sich
// selbst beantwortet, nie mit leeren Empfängern (Gegenprüfung S2.9, B1).
func ReplyRecipients(o Original, own []string, all bool) (to, cc []Address) {
	// Eigene Adressen nach mailaddr.Key (Gmail: Punkte, „+“, googlemail.com;
	// Nachbesserung T3, Punkt 15), Dubletten weiter nach normAddr.
	ownSet := mailaddr.NewSet(own...)
	sender := o.ReplyTo
	if len(sender) == 0 {
		sender = o.From
	}
	allOwn := len(sender) > 0
	for _, a := range sender {
		if !ownSet.Has(a.Address) {
			allOwn = false
		}
	}
	seen := map[string]bool{}
	pick := func(list []Address, dropOwn bool) []Address {
		var out []Address
		for _, a := range list {
			n := normAddr(a.Address)
			if n == "" || (dropOwn && ownSet.Has(n)) || seen[n] {
				continue
			}
			seen[n] = true
			out = append(out, a)
		}
		return out
	}
	if !allOwn {
		to = pick(sender, true)
		if all {
			cc = pick(append(append([]Address{}, o.To...), o.Cc...), true)
		}
		return to, cc
	}
	to = pick(o.To, true)
	if all {
		cc = pick(o.Cc, true)
	}
	if len(to) == 0 && len(cc) > 0 {
		to, cc = cc, nil
	}
	if len(to) == 0 {
		// Nur eigene Adressen: an die ursprüngliche Adresse (To von o,
		// ohne To der Absender selbst).
		clear(seen)
		if to = pick(o.To, false); len(to) == 0 {
			to = pick(sender, false)
		}
	}
	return to, cc
}

// parseAddress prüft eine Adresse nach RFC 5322 (net/mail) und bereinigt
// den Namen.
func parseAddress(field, s string) (Address, error) {
	a, err := mail.ParseAddress(s)
	if err != nil {
		return Address{}, deny(ReasonInvalidAddress, field, "not an RFC 5322 address")
	}
	return Address{Name: headerText(a.Name), Address: a.Address}, nil
}

func (b *builder) addresses() error {
	in := b.in
	// Absender: Identität, sonst Kontoadresse.
	fromAddr, fromName := in.Identity.Email, in.Identity.Name
	if strings.TrimSpace(fromAddr) == "" {
		fromAddr, fromName = in.AccountAddress, ""
		b.note(NoteNoIdentity)
	}
	if strings.TrimSpace(fromAddr) == "" {
		return deny(ReasonInvalidInput, "from", "neither identity nor account address")
	}
	from, err := parseAddress("from", fromAddr)
	if err != nil {
		return err
	}
	if fromName != "" {
		from.Name = headerText(fromName)
	}
	b.msg.From = from
	if strings.TrimSpace(in.Identity.ReplyTo) != "" {
		rt, err := parseAddress("reply_to", in.Identity.ReplyTo)
		if err != nil {
			return err
		}
		b.msg.ReplyTo = []Address{rt}
	}

	// Genannte Empfänger.
	user := map[string][]Address{}
	for _, f := range []struct {
		name string
		list []string
	}{{"to", in.To}, {"cc", in.Cc}, {"bcc", in.Bcc}} {
		if len(f.list) > b.lim.MaxRecipients {
			return deny(ReasonTooManyRecipients, f.name, "%d recipients, at most %d", len(f.list), b.lim.MaxRecipients)
		}
		for i, s := range f.list {
			a, err := parseAddress(fmt.Sprintf("%s[%d]", f.name, i), s)
			if err != nil {
				return err
			}
			user[f.name] = append(user[f.name], a)
		}
	}

	// Abgeleitete Empfänger (Antworten).
	var to, cc []Address
	if (in.Kind == KindReply || in.Kind == KindReplyAll) && !in.ExplicitRecipients {
		own := append([]string{in.AccountAddress, in.Identity.Email, from.Address}, in.OwnAddresses...)
		dto, dcc := ReplyRecipients(*in.Original, own, in.Kind == KindReplyAll)
		to, cc = b.valid(dto), b.valid(dcc)
		if len(to) > 0 {
			b.derive(DerivedTo)
		}
		if len(cc) > 0 {
			b.derive(DerivedCc)
		}
	}
	seen := map[string]bool{}
	merge := func(lists ...[]Address) []Address {
		var out []Address
		for _, l := range lists {
			for _, a := range l {
				n := normAddr(a.Address)
				if seen[n] {
					continue
				}
				seen[n] = true
				out = append(out, a)
			}
		}
		return out
	}
	b.msg.To = merge(to, user["to"])
	b.msg.Cc = merge(cc, user["cc"])
	b.msg.Bcc = merge(user["bcc"])
	if n := len(b.msg.To); n > b.lim.MaxRecipients {
		return deny(ReasonTooManyRecipients, "to", "%d recipients, at most %d", n, b.lim.MaxRecipients)
	}
	if n := len(b.msg.Cc); n > b.lim.MaxRecipients {
		return deny(ReasonTooManyRecipients, "cc", "%d recipients, at most %d", n, b.lim.MaxRecipients)
	}
	return nil
}

// valid behält abgeleitete Adressen, die als Kopfzeile taugen.
func (b *builder) valid(list []Address) []Address {
	var out []Address
	for _, a := range list {
		p, err := mail.ParseAddress((&mail.Address{Address: a.Address}).String())
		if err != nil || p.Address == "" {
			b.note(NoteRecipientDropped)
			continue
		}
		out = append(out, Address{Name: headerText(a.Name), Address: p.Address})
	}
	return out
}

func (b *builder) subject() error {
	in := b.in
	if s := headerText(in.Subject); s != "" || in.Kind == KindNew {
		if utf8.RuneCountInString(s) > b.lim.MaxSubjectChars {
			return deny(ReasonTooLarge, "subject", "subject longer than %d characters", b.lim.MaxSubjectChars)
		}
		b.msg.Subject = s
		return nil
	}
	var s string
	if in.Kind == KindForward {
		s = ForwardSubject(in.Original.Subject, in.Identity.ForwardPrefix)
	} else {
		s = ReplySubject(in.Original.Subject, in.Identity.ReplyPrefix)
	}
	if utf8.RuneCountInString(s) > b.lim.MaxSubjectChars {
		s = strings.TrimSpace(string([]rune(s)[:b.lim.MaxSubjectChars]))
		b.note(NoteSubjectTruncated)
	}
	b.msg.Subject = s
	b.derive(DerivedSubject)
	return nil
}

func (b *builder) ids() error {
	in := b.in
	b.msg.Date = in.Now()
	if id := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(in.MessageID), "<"), ">"); id != "" {
		if !validID(id) || !strings.Contains(id, "@") {
			return deny(ReasonInvalidInput, "message_id", "malformed message id")
		}
		b.msg.MessageID = id
	} else {
		id, err := NewMessageID(in.Rand, domainOf(b.msg.From.Address))
		if err != nil {
			return err
		}
		b.msg.MessageID = id
	}
	if in.Kind == KindReply || in.Kind == KindReplyAll {
		b.msg.InReplyTo, b.msg.References = References(*in.Original)
		if b.msg.InReplyTo != "" {
			b.derive(DerivedInReplyTo)
		}
		if len(b.msg.References) > 0 {
			b.derive(DerivedReferences)
		}
	}
	return nil
}
