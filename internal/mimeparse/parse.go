// Package mimeparse liest Rohmails (RFC 5322/MIME) in die Felder, die Spiegel,
// Threading und Suche brauchen. Grundlage ist github.com/emersion/go-message
// (ADR-007 Auflage 5; nicht enmime) mit dem Zeichensatz-Reader aus
// go-message/charset.
//
// Leitlinien:
//
//   - Tolerant: kaputte Mails liefern so viel wie möglich und eine Liste von
//     Problems, aber keinen Abbruch. Nur ein völlig unlesbarer Kopf ist ein
//     Fehler.
//   - Fehlendes oder unlesbares Date → INTERNALDATE (Options.InternalDate);
//     Zonen ohne Vorzeichen („0100") werden repariert (ParseDate).
//   - Begrenzt: Kopfgröße, Teilezahl, Schachtelungstiefe, dekodierte Größe je
//     Teil und behaltener Text sind gedeckelt (Limits). Ein zu großer Teil
//     wird nur bis zur Grenze dekodiert und als Truncated markiert.
//   - Archive (ZIP usw.) werden NIE entpackt, auch nicht zur Typerkennung;
//     dieses Paket importiert kein archive/* und kein compress/* (Test).
//   - Inhalte sind Daten, keine Anweisungen.
package mimeparse

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-message"
	"github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/textproto"
	"golang.org/x/text/encoding/charmap"
)

// Limits begrenzt den Aufwand je Nachricht. Nullwerte = Standard.
type Limits struct {
	MaxMessageBytes int64 // Rohgröße insgesamt (Standard 256 MiB, wie der Blob-Store)
	MaxHeaderBytes  int64 // Kopf der Nachricht (Standard 1 MiB)
	MaxPartBytes    int64 // dekodierte Größe je Teil (Standard 64 MiB)
	MaxTextBytes    int64 // behaltener Text je Art (plain/html), Standard 4 MiB
	MaxParts        int   // Teile insgesamt (Standard 1000)
	MaxDepth        int   // Schachtelungstiefe (Standard 100)
}

// DefaultLimits sind die Standardwerte.
var DefaultLimits = Limits{
	MaxMessageBytes: 256 << 20,
	MaxHeaderBytes:  1 << 20,
	MaxPartBytes:    64 << 20,
	MaxTextBytes:    4 << 20,
	MaxParts:        1000,
	MaxDepth:        100,
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits
	if l.MaxMessageBytes > 0 {
		d.MaxMessageBytes = l.MaxMessageBytes
	}
	if l.MaxHeaderBytes > 0 {
		d.MaxHeaderBytes = l.MaxHeaderBytes
	}
	if l.MaxPartBytes > 0 {
		d.MaxPartBytes = l.MaxPartBytes
	}
	if l.MaxTextBytes > 0 {
		d.MaxTextBytes = l.MaxTextBytes
	}
	if l.MaxParts > 0 {
		d.MaxParts = l.MaxParts
	}
	if l.MaxDepth > 0 {
		d.MaxDepth = l.MaxDepth
	}
	return d
}

// Options steuert Parse.
type Options struct {
	// InternalDate ist das Serverdatum (IMAP INTERNALDATE); Ersatz für ein
	// fehlendes oder unlesbares Date.
	InternalDate time.Time
	Limits       Limits
	// OnAttachment wird je Anhang (auch Inline-Teile) mit dem dekodierten
	// Inhalt aufgerufen, höchstens MaxPartBytes+1 Bytes (mehr heißt
	// Truncated). a trägt die Metadaten ohne Größe und Hash; body gilt nur
	// während des Aufrufs, Ungelesenes liest Parse selbst (für Größe und
	// Hash). Ein Fehler wird als Problem notiert, Parse läuft weiter.
	// Gebraucht für den Blob-Store und die Typprüfung (S1.6).
	OnAttachment func(a Attachment, body io.Reader) error
}

// Address ist eine Absender- oder Empfängeradresse.
type Address struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// Attachment ist ein Teil, der nicht als Body-Text gilt (Anhang, Inline-Bild,
// weitergeleitete Nachricht).
type Attachment struct {
	PartID      string `json:"part_id"`      // IMAP-Teilnummer, z. B. "2" oder "1.3"
	Filename    string `json:"filename"`     // dekodiert (RFC 2231/2047), ggf. leer
	ContentType string `json:"content_type"` // wie deklariert (Magic Bytes: S1.6)
	Inline      bool   `json:"inline"`       // Content-Disposition inline bzw. ohne
	ContentID   string `json:"content_id,omitempty"`
	Size        int64  `json:"size"`   // dekodierte Bytes (bis zur Grenze)
	SHA256      string `json:"sha256"` // über die dekodierten Bytes; leer bei Truncated
	Truncated   bool   `json:"truncated,omitempty"`
}

// Message ist das Ergebnis von Parse.
type Message struct {
	MessageID  string // ohne spitze Klammern
	InReplyTo  []string
	References []string

	Subject string
	From    []Address
	Sender  []Address
	ReplyTo []Address
	To      []Address
	Cc      []Address

	Date       time.Time // UTC
	DateSource DateSource
	DateRaw    string // Date-Kopfzeile wie empfangen

	ListID          string
	ListUnsubscribe string
	AuthResults     []string

	Text        string // erster text/plain (plus weitere Inline-Textteile)
	HTML        string // erster text/html, roh (Bereinigung in S1.6)
	TextCharset string // deklarierter Zeichensatz des Textteils

	Attachments []Attachment

	Size     int64    // gelesene Rohbytes
	Parts    int      // Anzahl MIME-Teile
	Problems []string // Abweichungen, die toleriert wurden
}

// HasAttachments: mindestens ein Anhang, der nicht inline ist.
func (m *Message) HasAttachments() bool {
	for _, a := range m.Attachments {
		if !a.Inline {
			return true
		}
	}
	return false
}

func (m *Message) problem(format string, args ...any) {
	if len(m.Problems) < 100 {
		m.Problems = append(m.Problems, fmt.Sprintf(format, args...))
	}
}

// ErrUnreadable: Der Kopf ist so kaputt, dass nichts Sinnvolles übrig bleibt.
var ErrUnreadable = errors.New("mimeparse: unreadable message")

var wordDecoder = &mime.WordDecoder{CharsetReader: charset.Reader}

func init() {
	// go-message/charset setzt das beim Import bereits; hier ausdrücklich,
	// damit die Abhängigkeit nicht still entfällt (ADR-007 Auflage 3).
	message.CharsetReader = charset.Reader
}

// countingReader zählt Rohbytes und bricht bei MaxMessageBytes ab.
type countingReader struct {
	r   io.Reader
	n   int64
	max int64
	hit bool
}

func (c *countingReader) Read(p []byte) (int, error) {
	if c.n >= c.max {
		c.hit = true
		return 0, io.EOF
	}
	if rem := c.max - c.n; int64(len(p)) > rem {
		p = p[:rem]
	}
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// Parse liest eine Rohmail.
func Parse(r io.Reader, opts Options) (*Message, error) {
	lim := opts.Limits.withDefaults()
	cr := &countingReader{r: r, max: lim.MaxMessageBytes}
	br := bufio.NewReaderSize(cr, 64<<10)
	m := &Message{}

	rawHeader, err := readHeaderBlock(br, lim.MaxHeaderBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	clean, dropped := sanitizeHeader(rawHeader)
	if dropped > 0 {
		m.problem("dropped %d malformed header line(s)", dropped)
	}
	th, err := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(clean)))
	if err != nil && th.Len() == 0 {
		return nil, fmt.Errorf("%w: %v", ErrUnreadable, err)
	}
	if err != nil {
		m.problem("header: %v", err)
	}
	h := message.Header{Header: th}
	parseEnvelope(m, &h, opts.InternalDate)

	p := &walker{m: m, lim: lim, onAttachment: opts.OnAttachment}
	root, err := message.New(h, br)
	if err != nil && root != nil {
		m.problem("body: %v", err)
	}
	if root != nil {
		p.walk(root, 0, "")
	}
	// Rest lesen, damit Size stimmt (ohne Inhalt zu behalten).
	_, _ = io.Copy(io.Discard, br)
	m.Size = cr.n
	if cr.hit {
		m.problem("message exceeds %d bytes; rest ignored", lim.MaxMessageBytes)
	}
	return m, nil
}

// readHeaderBlock liest bis zur ersten Leerzeile (CRLF oder LF) bzw. bis EOF.
func readHeaderBlock(br *bufio.Reader, max int64) ([]byte, error) {
	var buf bytes.Buffer
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// überlange Kopfzeile: weiterlesen, Grenze gilt trotzdem
			buf.Write(line)
			if int64(buf.Len()) > max {
				return nil, fmt.Errorf("header exceeds %d bytes", max)
			}
			continue
		}
		buf.Write(line)
		if int64(buf.Len()) > max {
			return nil, fmt.Errorf("header exceeds %d bytes", max)
		}
		trimmed := bytes.TrimRight(line, "\r\n")
		if len(trimmed) == 0 && len(line) > 0 {
			return buf.Bytes(), nil // Leerzeile: Kopf zu Ende
		}
		if err == io.EOF {
			if buf.Len() == 0 {
				return nil, errors.New("empty message")
			}
			return buf.Bytes(), nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// sanitizeHeader entfernt NUL-Bytes, Zeilen ohne Doppelpunkt bzw. mit
// ungültigem Feldnamen und Fortsetzungszeilen ohne Vorgänger. Ergebnis mit
// CRLF und abschließender Leerzeile.
func sanitizeHeader(raw []byte) ([]byte, int) {
	raw = bytes.ReplaceAll(raw, []byte{0}, nil)
	lines := bytes.Split(raw, []byte("\n"))
	var out bytes.Buffer
	dropped := 0
	inField := false
	for _, l := range lines {
		l = bytes.TrimRight(l, "\r")
		if len(l) == 0 {
			continue
		}
		if l[0] == ' ' || l[0] == '\t' {
			if inField {
				out.Write(l)
				out.WriteString("\r\n")
			} else {
				dropped++
			}
			continue
		}
		i := bytes.IndexByte(l, ':')
		if i <= 0 || !validKey(bytes.TrimRight(l[:i], " \t")) {
			dropped++
			inField = false
			continue
		}
		out.Write(l)
		out.WriteString("\r\n")
		inField = true
	}
	out.WriteString("\r\n")
	return out.Bytes(), dropped
}

func validKey(k []byte) bool {
	if len(k) == 0 {
		return false
	}
	for _, c := range k {
		if c < 33 || c > 126 || c == ':' {
			return false
		}
	}
	return true
}

// fixUTF8 macht aus ungültigem UTF-8 (roh in Latin-1/Windows-1252
// geschriebene Kopfzeilen oder falsch deklarierte Texte) gültigen Text.
// Windows-1252 ist eine Obermenge der druckbaren Latin-1-Zeichen.
func fixUTF8(s string) (string, bool) {
	if utf8.ValidString(s) {
		return s, false
	}
	out, err := charmap.Windows1252.NewDecoder().String(s)
	if err != nil {
		return strings.ToValidUTF8(s, "�"), true
	}
	return out, true
}

// decodeText dekodiert RFC-2047-Wörter (mit Zeichensätzen) und repariert
// rohes 8-Bit.
func (m *Message) decodeText(field, v string) string {
	if strings.Contains(v, "=?") {
		if d, err := wordDecoder.DecodeHeader(v); err == nil {
			v = d
		} else {
			m.problem("%s: %v", field, err)
		}
	}
	v, fixed := fixUTF8(v)
	if fixed {
		m.problem("%s: raw 8-bit text, read as windows-1252", field)
	}
	return v
}

var msgIDRe = regexp.MustCompile(`<([^<>\s]+)>`)

// msgIDs liest Message-IDs tolerant (Spike: auch bei kaputter Syntax).
func msgIDs(v string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range msgIDRe.FindAllStringSubmatch(v, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func (m *Message) messageID(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if ids := msgIDs(v); len(ids) > 0 {
		return ids[0]
	}
	// ohne spitze Klammern: erstes Wort übernehmen
	m.problem("message-id without angle brackets")
	if f := strings.Fields(v); len(f) > 0 {
		return strings.Trim(f[0], "<>")
	}
	return ""
}

var addrRe = regexp.MustCompile(`[^\s<>@,;:"()\[\]]+@[^\s<>@,;:"()\[\]]+`)

func (m *Message) addresses(field, v string) []Address {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	p := mail.AddressParser{WordDecoder: wordDecoder}
	list, err := p.ParseList(v)
	if err == nil {
		out := make([]Address, 0, len(list))
		for _, a := range list {
			name, _ := fixUTF8(a.Name)
			out = append(out, Address{Name: name, Address: a.Address})
		}
		return out
	}
	// Rückfall: Adressen per Muster, Name = Text vor „<".
	m.problem("%s: %v", field, err)
	var out []Address
	for _, part := range strings.Split(v, ",") {
		addr := addrRe.FindString(part)
		if addr == "" {
			continue
		}
		name := ""
		if i := strings.Index(part, "<"); i > 0 {
			name = strings.Trim(strings.TrimSpace(part[:i]), `"`)
		}
		out = append(out, Address{Name: m.decodeText(field, name), Address: addr})
	}
	return out
}

func parseEnvelope(m *Message, h *message.Header, internal time.Time) {
	m.MessageID = m.messageID(h.Get("Message-Id"))
	m.InReplyTo = msgIDs(h.Get("In-Reply-To"))
	m.References = msgIDs(strings.Join(h.Values("References"), " "))
	m.Subject = m.decodeText("subject", h.Get("Subject"))
	m.From = m.addresses("from", h.Get("From"))
	m.Sender = m.addresses("sender", h.Get("Sender"))
	m.ReplyTo = m.addresses("reply-to", h.Get("Reply-To"))
	m.To = m.addresses("to", strings.Join(h.Values("To"), ", "))
	m.Cc = m.addresses("cc", strings.Join(h.Values("Cc"), ", "))
	m.DateRaw = h.Get("Date")
	var err error
	m.Date, m.DateSource, err = resolveDate(m.DateRaw, internal)
	if err != nil {
		if m.DateRaw == "" {
			m.problem("date: missing, using %s", m.DateSource)
		} else {
			m.problem("date: unparsable, using %s", m.DateSource)
		}
	}
	m.ListID = m.decodeText("list-id", h.Get("List-Id"))
	m.ListUnsubscribe = h.Get("List-Unsubscribe")
	for _, v := range h.Values("Authentication-Results") {
		m.AuthResults = append(m.AuthResults, v)
	}
}

// walker läuft durch den MIME-Baum.
type walker struct {
	m            *Message
	lim          Limits
	stopped      bool
	onAttachment func(Attachment, io.Reader) error
}

func partID(parent string, i int) string {
	if parent == "" {
		return fmt.Sprint(i)
	}
	return fmt.Sprintf("%s.%d", parent, i)
}

func (w *walker) walk(e *message.Entity, depth int, id string) {
	if w.stopped {
		return
	}
	w.m.Parts++
	if w.m.Parts > w.lim.MaxParts {
		w.m.problem("more than %d MIME parts; rest ignored", w.lim.MaxParts)
		w.stopped = true
		return
	}
	mediaType, params, err := e.Header.ContentType()
	if err != nil {
		if e.Header.Get("Content-Type") != "" {
			w.m.problem("part %s: content-type: %v", nonEmpty(id), err)
		}
		mediaType, params = "text/plain", map[string]string{}
	}
	mediaType = strings.ToLower(mediaType)

	if strings.HasPrefix(mediaType, "multipart/") {
		if depth >= w.lim.MaxDepth {
			w.m.problem("MIME nesting deeper than %d; rest ignored", w.lim.MaxDepth)
			return
		}
		mr := e.MultipartReader()
		if mr == nil || params["boundary"] == "" {
			w.m.problem("part %s: multipart without boundary, read as text", nonEmpty(id))
			w.text(e, "text/plain", params, id)
			return
		}
		for i := 1; ; i++ {
			child, err := mr.NextPart()
			if err == io.EOF {
				return
			}
			if err != nil && child == nil {
				w.m.problem("part %s: %v", partID(id, i), err)
				return
			}
			if err != nil {
				w.m.problem("part %s: %v", partID(id, i), err)
			}
			w.walk(child, depth+1, partID(id, i))
			if w.stopped {
				return
			}
		}
	}

	disp, dparams, _ := e.Header.ContentDisposition()
	disp = strings.ToLower(disp)
	filename := w.filename(dparams, params)
	isText := mediaType == "text/plain" || mediaType == "text/html"
	if isText && disp != "attachment" && filename == "" {
		w.text(e, mediaType, params, id)
		return
	}
	w.attachment(e, mediaType, disp, filename, nonEmpty(id))
}

func nonEmpty(id string) string {
	if id == "" {
		return "1" // einteilige Nachricht: Teil 1 (IMAP BODY[1])
	}
	return id
}

func (w *walker) filename(dparams, ctparams map[string]string) string {
	name := dparams["filename"]
	if name == "" {
		name = ctparams["name"]
	}
	if name == "" {
		return ""
	}
	return strings.TrimSpace(w.m.decodeText("filename", name))
}

// text liest einen Textteil bis MaxTextBytes.
func (w *walker) text(e *message.Entity, mediaType string, params map[string]string, id string) {
	budget := w.lim.MaxTextBytes
	lr := &io.LimitedReader{R: e.Body, N: budget + 1}
	b, err := io.ReadAll(lr)
	if err != nil {
		w.m.problem("part %s: %v", nonEmpty(id), err)
	}
	if int64(len(b)) > budget {
		b = b[:budget]
		w.m.problem("part %s: text longer than %d bytes, truncated", nonEmpty(id), budget)
	}
	s := strings.ReplaceAll(string(b), "\x00", "")
	if fixed, bad := fixUTF8(s); bad {
		s = fixed
		w.m.problem("part %s: invalid UTF-8 (declared %q), read as windows-1252", nonEmpty(id), params["charset"])
	}
	switch mediaType {
	case "text/html":
		if w.m.HTML == "" {
			w.m.HTML = s
		}
	default:
		if w.m.Text == "" {
			w.m.Text = s
			w.m.TextCharset = strings.ToLower(params["charset"])
		} else if int64(len(w.m.Text)+len(s)) <= budget {
			w.m.Text += "\n" + s
		}
	}
}

// attachment hasht die dekodierten Bytes bis MaxPartBytes. Mehr wird nicht
// dekodiert (Truncated); Archive werden nie entpackt.
func (w *walker) attachment(e *message.Entity, mediaType, disp, filename, id string) {
	a := Attachment{
		PartID:      id,
		Filename:    filename,
		ContentType: mediaType,
		Inline:      disp != "attachment",
		ContentID:   strings.Trim(strings.TrimSpace(e.Header.Get("Content-Id")), "<>"),
	}
	h := sha256.New()
	cnt := &countWriter{}
	body := io.TeeReader(io.LimitReader(e.Body, w.lim.MaxPartBytes+1), io.MultiWriter(h, cnt))
	if w.onAttachment != nil {
		if err := w.onAttachment(a, body); err != nil {
			w.m.problem("part %s: %v", id, err)
		}
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		w.m.problem("part %s: %v", id, err)
	}
	n := cnt.n
	if n > w.lim.MaxPartBytes {
		a.Truncated = true
		a.Size = w.lim.MaxPartBytes
		w.m.problem("part %s: larger than %d bytes, not decoded further", id, w.lim.MaxPartBytes)
	} else {
		a.Size = n
		a.SHA256 = hex.EncodeToString(h.Sum(nil))
	}
	w.m.Attachments = append(w.m.Attachments, a)
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// HeaderMessageID liest die Message-ID aus rohen Kopfzeilen (z. B. dem
// Ergebnis von IMAP BODY.PEEK[HEADER.FIELDS (MESSAGE-ID)]) mit denselben
// toleranten Regeln wie Parse. Der Sync gleicht damit vor dem Laden des
// Inhalts ab; beide Wege müssen dieselbe ID liefern. "" = keine.
func HeaderMessageID(raw []byte) string {
	clean, _ := sanitizeHeader(raw)
	th, _ := textproto.ReadHeader(bufio.NewReader(bytes.NewReader(clean)))
	m := &Message{}
	return m.messageID(th.Get("Message-Id"))
}
