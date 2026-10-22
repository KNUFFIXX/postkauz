package compose

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/emersion/go-message"
)

// dateLayout ist das Format der Date-Kopfzeile (RFC 5322).
const dateLayout = "Mon, 02 Jan 2006 15:04:05 -0700"

// header baut einen Kopf mit Feldern in der gegebenen Reihenfolge
// (go-message schreibt zuletzt hinzugefügte Felder zuerst).
func header(fields ...[2]string) message.Header {
	var h message.Header
	for i := len(fields) - 1; i >= 0; i-- {
		if fields[i][1] != "" {
			h.Add(fields[i][0], fields[i][1])
		}
	}
	return h
}

// boundaries leitet die MIME-Grenzen aus dem Inhalt der Nachricht ab
// (Gegenprüfung S2.3, W1): Derselbe Entwurf ergibt bei jedem Bau dieselben
// Bytes (bis auf Date), damit die Freigabe die fertige Nachricht binden
// kann (MessageHash). Zufällige Grenzen gingen nicht; Grenzen aus der
// Message-ID allein auch nicht, denn die kennt der Agent und könnte die
// Grenze in seinen Text schreiben (MIME-Einschleusung). Die Grenze hängt
// deshalb vom ganzen Inhalt ab (Text, HTML, Anhänge, Kopffelder); ein Text,
// der seine eigene Grenze enthält, ist ein Fixpunkt von SHA-256. Zur
// Sicherheit wird trotzdem geprüft, dass keine Grenze im Inhalt vorkommt.
func (b *builder) boundaries(atts []loadedAttachment) (alt, mixed string, err error) {
	m := &b.msg
	h := sha256.New()
	field := func(s string) {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	field("postkauz-boundary-v1")
	field(m.MessageID)
	field(m.Subject)
	field(addrList([]Address{m.From}) + "\n" + addrList(m.ReplyTo) + "\n" + addrList(m.To) + "\n" + addrList(m.Cc) + "\n" + addrList(m.Bcc))
	field(m.BodyText)
	field(m.BodyHTML)
	for _, a := range atts {
		field(a.info.Filename)
		field(a.info.ContentType)
		field(a.info.SHA256)
	}
	seed := h.Sum(nil)
	mk := func(label string) string {
		s := sha256.Sum256(append(append([]byte{}, seed...), label...))
		return "pk-" + hex.EncodeToString(s[:12])
	}
	alt, mixed = mk("alternative"), mk("mixed")
	for _, bd := range []string{alt, mixed} {
		if strings.Contains(m.BodyText, bd) || strings.Contains(m.BodyHTML, bd) {
			return "", "", fmt.Errorf("compose: boundary occurs in the content")
		}
		for _, a := range atts {
			if bytes.Contains(a.data, []byte(bd)) {
				return "", "", fmt.Errorf("compose: boundary occurs in the content")
			}
		}
	}
	return alt, mixed, nil
}

// MessageHash ist SHA-256 (hex) über eine gebaute Nachricht ohne die
// Date-Kopfzeile (Gegenprüfung S2.3, W1): Date setzt jeder Bau neu (E11:
// bei jedem Push), alles andere ist bei gleichem Entwurf gleich (Message-ID
// fest, Grenzen aus dem Inhalt). Damit bindet eine Freigabe die Bytes, die
// der Mensch im Mailclient sendet: Absender, Reply-To, Signatur, Zitat, das
// erzeugte HTML, die Anhänge und die Schalter quote/signature.
func MessageHash(raw []byte) string {
	head, body, ok := bytes.Cut(raw, []byte("\r\n\r\n"))
	h := sha256.New()
	h.Write([]byte("postkauz-message-v1\n"))
	lines := strings.SplitAfter(string(head), "\r\n")
	skip := false
	for _, l := range lines {
		folded := strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")
		if !folded {
			name, _, _ := strings.Cut(l, ":")
			skip = strings.EqualFold(strings.TrimSpace(name), "Date")
		}
		if !skip {
			h.Write([]byte(l))
		}
	}
	if ok {
		h.Write([]byte("\r\n\r\n"))
		h.Write(body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func addrList(l []Address) string {
	s := make([]string, len(l))
	for i, a := range l {
		if strings.Contains(a.Name, "=?") {
			// Wie beim Betreff (K4): ein Name, der wie ein encoded-word
			// aussieht, wird selbst kodiert, sonst dekodiert ihn der Client.
			s[i] = encodeWords(a.Name) + " <" + a.Address + ">"
			continue
		}
		s[i] = (&mail.Address{Name: a.Name, Address: a.Address}).String()
	}
	return strings.Join(s, ", ")
}

// headerWord kodiert einen Kopfzeilentext nach RFC 2047, wenn nötig
// (Nicht-ASCII). Enthält er wörtlich "=?", wird er immer kodiert
// (Gegenprüfung S2.3, K4): Ein Betreff wie "=?utf-8?q?=E2=80=AE?=" ginge
// sonst unkodiert durch, und der Mailclient des Empfängers dekodierte ihn
// zu Zeichen wie U+202E (Schreibrichtung umkehren), die der Mensch beim
// Prüfen nicht sah.
func headerWord(v string) string {
	if strings.Contains(v, "=?") {
		return encodeWords(v)
	}
	return mime.QEncoding.Encode("utf-8", v)
}

// encodeWords kodiert v vollständig als Folge von encoded-words (Base64,
// UTF-8), jedes höchstens 75 Zeichen lang, getrennt durch Leerzeichen (die
// ein Decoder zwischen encoded-words entfernt). Ein Wort endet nie mitten
// in einem Zeichen.
func encodeWords(v string) string {
	const prefix, suffix, max = "=?utf-8?b?", "?=", 75
	maxBytes := (max - len(prefix) - len(suffix)) / 4 * 3
	var words []string
	for len(v) > 0 {
		n := min(len(v), maxBytes)
		for n < len(v) && n > 0 && !utf8.RuneStart(v[n]) {
			n--
		}
		words = append(words, prefix+base64.StdEncoding.EncodeToString([]byte(v[:n]))+suffix)
		v = v[n:]
	}
	return strings.Join(words, " ")
}

func idList(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return "<" + strings.Join(ids, "> <") + ">"
}

func (b *builder) write(w io.Writer, atts []loadedAttachment) error {
	m := &b.msg
	alt, mixed, err := b.boundaries(atts)
	if err != nil {
		return err
	}
	topType := mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alt})
	if len(atts) > 0 {
		topType = mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": mixed})
	} else {
		mixed = ""
	}
	inReplyTo := ""
	if m.InReplyTo != "" {
		inReplyTo = "<" + m.InReplyTo + ">"
	}
	top := header(
		[2]string{"Date", m.Date.Format(dateLayout)},
		[2]string{"From", addrList([]Address{m.From})},
		[2]string{"Reply-To", addrList(m.ReplyTo)},
		[2]string{"To", addrList(m.To)},
		[2]string{"Cc", addrList(m.Cc)},
		[2]string{"Bcc", addrList(m.Bcc)},
		[2]string{"Subject", headerWord(m.Subject)},
		[2]string{"Message-ID", "<" + m.MessageID + ">"},
		[2]string{"In-Reply-To", inReplyTo},
		[2]string{"References", idList(m.References)},
		[2]string{"MIME-Version", "1.0"},
		[2]string{"Content-Type", topType},
	)
	mw, err := message.CreateWriter(w, top)
	if err != nil {
		return fmt.Errorf("compose: write header: %w", err)
	}
	altW := mw
	if mixed != "" {
		altW, err = mw.CreatePart(header([2]string{"Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alt})}))
		if err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
	}
	text := m.BodyText
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	for _, p := range []struct{ ct, body string }{
		{"text/plain; charset=utf-8", text},
		{"text/html; charset=utf-8", m.BodyHTML},
	} {
		pw, err := altW.CreatePart(header([2]string{"Content-Type", p.ct}, [2]string{"Content-Transfer-Encoding", "quoted-printable"}))
		if err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		if _, err := io.WriteString(pw, p.body); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		if err := pw.Close(); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
	}
	if mixed != "" {
		if err := altW.Close(); err != nil {
			return fmt.Errorf("compose: write part: %w", err)
		}
		for _, a := range atts {
			if err := writeAttachment(mw, a); err != nil {
				return err
			}
		}
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("compose: write message: %w", err)
	}
	return nil
}

func writeAttachment(mw *message.Writer, a loadedAttachment) error {
	var h message.Header
	ctParams, dispParams := map[string]string{}, map[string]string{}
	if a.info.Filename != "" {
		ctParams["name"] = a.info.Filename
		dispParams["filename"] = a.info.Filename
	}
	ct := a.info.ContentType
	if strings.HasPrefix(ct, "text/") && utf8.Valid(a.data) {
		ctParams["charset"] = "utf-8"
	}
	// Reihenfolge im Kopf: Content-Type, Content-Disposition, CTE.
	h.Set("Content-Transfer-Encoding", "base64")
	h.SetContentDisposition("attachment", dispParams)
	h.SetContentType(ct, ctParams)
	pw, err := mw.CreatePart(h)
	if err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	if _, err := pw.Write(a.data); err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	if err := pw.Close(); err != nil {
		return fmt.Errorf("compose: write attachment: %w", err)
	}
	return nil
}
