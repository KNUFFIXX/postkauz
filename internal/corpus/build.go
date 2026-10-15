package corpus

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Bausteine zum Schreiben von MIME-Nachrichten. Alles mit CRLF.

const crlf = "\r\n"

// adjust passt Text an einen Zeichensatz an (nicht darstellbare Zeichen
// werden ersetzt). Das Ergebnis ist die Erwartung im Manifest.
func adjust(s, cs string) string {
	switch cs {
	case "iso-8859-1":
		s = strings.NewReplacer("€", "EUR", "„", `"`, "“", `"`, "”", `"`, "–", "-", "…", "...").Replace(s)
		s = stripNonBMPish(s)
	case "windows-1252":
		s = stripNonBMPish(s)
	}
	return s
}

// stripNonBMPish entfernt Emoji und andere Zeichen außerhalb der
// westeuropäischen Zeichensätze (für die Generator-Texte genügt das).
func stripNonBMPish(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= 0x2000 && r != '€' && r != '„' && r != '“' && r != '”' && r != '–' && r != '…' {
			// mit dem Leerzeichen davor entfernen („Text 🔧" → „Text")
			if n := len(out); n > 0 && out[n-1] == ' ' {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

// encodeCharset liefert die Bytes von s im Zeichensatz cs (s muss vorher
// mit adjust angepasst sein).
func encodeCharset(s, cs string) []byte {
	switch cs {
	case "utf-8", "us-ascii":
		return []byte(s)
	case "iso-8859-1":
		b, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(s))
		if err != nil {
			panic(fmt.Sprintf("corpus: %q not encodable in %s: %v", s, cs, err))
		}
		return b
	case "windows-1252":
		b, err := charmap.Windows1252.NewEncoder().Bytes([]byte(s))
		if err != nil {
			panic(fmt.Sprintf("corpus: %q not encodable in %s: %v", s, cs, err))
		}
		return b
	}
	panic("corpus: unknown charset " + cs)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// encodeWord kodiert s als RFC-2047-Wort(e) in cs mit B oder Q.
func encodeWord(s, cs, enc string) string {
	if isASCII(s) {
		return s
	}
	raw := string(encodeCharset(s, cs))
	if enc == "Q" {
		return mime.QEncoding.Encode(cs, raw)
	}
	return mime.BEncoding.Encode(cs, raw)
}

// fold faltet eine Kopfzeile an Leerzeichen auf höchstens 78 Zeichen.
func fold(line string) string {
	if len(line) <= 78 {
		return line
	}
	var out strings.Builder
	cur := 0
	words := strings.Split(line, " ")
	for i, w := range words {
		if i > 0 {
			if cur+1+len(w) > 78 {
				out.WriteString(crlf + " ")
				cur = 1
			} else {
				out.WriteString(" ")
				cur++
			}
		}
		out.WriteString(w)
		cur += len(w)
	}
	return out.String()
}

type header struct{ name, value string }

func writeHeaders(b *bytes.Buffer, hs []header) {
	for _, h := range hs {
		b.WriteString(fold(h.name + ": " + h.value))
		b.WriteString(crlf)
	}
}

// formatAddr schreibt „Name <adresse>" mit kodiertem Namen.
func formatAddr(name, addr, cs, enc string) string {
	if name == "" {
		return "<" + addr + ">"
	}
	if isASCII(name) {
		if strings.ContainsAny(name, `()<>[]:;@\,."`) {
			return `"` + strings.ReplaceAll(name, `"`, `\"`) + `" <` + addr + ">"
		}
		return name + " <" + addr + ">"
	}
	return encodeWord(name, cs, enc) + " <" + addr + ">"
}

// formatDate schreibt ein RFC-5322-Datum.
func formatDate(t time.Time) string { return t.Format("Mon, 02 Jan 2006 15:04:05 -0700") }

// ---- Entitäten -------------------------------------------------------------

func base64Lines(data []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(data)
	var b bytes.Buffer
	for len(enc) > 76 {
		b.WriteString(enc[:76])
		b.WriteString(crlf)
		enc = enc[76:]
	}
	b.WriteString(enc)
	b.WriteString(crlf)
	return b.Bytes()
}

func qp(data []byte) []byte {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	_, _ = w.Write(data)
	_ = w.Close()
	out := b.Bytes()
	if !bytes.HasSuffix(out, []byte(crlf)) {
		out = append(out, crlf...)
	}
	return out
}

// eightBit normalisiert Zeilenenden auf CRLF.
func eightBit(data []byte) []byte {
	s := strings.ReplaceAll(string(data), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", crlf)
	if !strings.HasSuffix(s, crlf) {
		s += crlf
	}
	return []byte(s)
}

// textEntity ist ein text/plain- oder text/html-Teil in cs mit cte
// ("quoted-printable", "base64" oder "8bit").
func textEntity(text, sub, cs, cte string) []byte {
	data := encodeCharset(text, cs)
	var body []byte
	switch cte {
	case "base64":
		body = base64Lines(data)
	case "8bit":
		body = eightBit(data)
	default:
		cte = "quoted-printable"
		body = qp(data)
	}
	var b bytes.Buffer
	writeHeaders(&b, []header{
		{"Content-Type", fmt.Sprintf("text/%s; charset=%q", sub, cs)},
		{"Content-Transfer-Encoding", cte},
	})
	b.WriteString(crlf)
	b.Write(body)
	return b.Bytes()
}

// multipartEntity fasst Teile zusammen.
func multipartEntity(sub, boundary string, parts [][]byte, extraParams string) []byte {
	var b bytes.Buffer
	writeHeaders(&b, []header{{"Content-Type", fmt.Sprintf("multipart/%s; boundary=%q%s", sub, boundary, extraParams)}})
	b.WriteString(crlf)
	b.WriteString("This is a multi-part message in MIME format." + crlf)
	for _, p := range parts {
		b.WriteString(crlf + "--" + boundary + crlf)
		b.Write(p)
	}
	b.WriteString(crlf + "--" + boundary + "--" + crlf)
	return b.Bytes()
}

// Namensstile für Anhänge.
const (
	nameRFC2231      = "rfc2231"      // filename*=utf-8''…
	nameRFC2231Cont  = "rfc2231-cont" // filename*0*=…; filename*1*=…
	nameRFC2047      = "rfc2047"      // filename="=?utf-8?B?…?=" (Outlook-Stil)
	nameASCII        = "ascii"        // filename="…"
	nameNone         = "none"         // kein Dateiname
	nameContentTypeO = "ct-only"      // nur name= im Content-Type
)

const attrChars = "!#$&+-.^_`|~"

func pctEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte(attrChars, c) >= 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// splitPct teilt einen prozentkodierten String, ohne %XX zu zerreißen.
func splitPct(s string, n int) []string {
	var out []string
	for len(s) > n {
		cut := n
		if i := strings.LastIndexByte(s[:cut], '%'); i >= cut-2 {
			cut = i
		}
		out = append(out, s[:cut])
		s = s[cut:]
	}
	return append(out, s)
}

// attachmentEntity schreibt einen Anhang (base64).
func attachmentEntity(name, ctype, style, disp string, data []byte, contentID string) []byte {
	ct := ctype
	cd := disp
	switch style {
	case nameRFC2231:
		ct += "; name*=utf-8''" + pctEncode(name)
		cd += "; filename*=utf-8''" + pctEncode(name)
	case nameRFC2231Cont:
		segs := splitPct(pctEncode(name), 40)
		for i, s := range segs {
			if i == 0 {
				cd += fmt.Sprintf("; filename*%d*=utf-8''%s", i, s)
			} else {
				cd += fmt.Sprintf("; filename*%d*=%s", i, s)
			}
		}
	case nameRFC2047:
		w := mime.BEncoding.Encode("utf-8", name)
		ct += fmt.Sprintf("; name=%q", w)
		cd += fmt.Sprintf("; filename=%q", w)
	case nameASCII:
		ct += fmt.Sprintf("; name=%q", name)
		cd += fmt.Sprintf("; filename=%q", name)
	case nameContentTypeO:
		ct += fmt.Sprintf("; name=%q", mime.BEncoding.Encode("utf-8", name))
	case nameNone:
	}
	hs := []header{{"Content-Type", ct}, {"Content-Transfer-Encoding", "base64"}}
	if disp != "" {
		hs = append(hs, header{"Content-Disposition", cd})
	}
	if contentID != "" {
		hs = append(hs, header{"Content-ID", "<" + contentID + ">"})
	}
	var b bytes.Buffer
	writeHeaders(&b, hs)
	b.WriteString(crlf)
	b.Write(base64Lines(data))
	return b.Bytes()
}

// rfc822Entity hängt eine Nachricht als message/rfc822 an.
func rfc822Entity(name string, raw []byte) []byte {
	var b bytes.Buffer
	writeHeaders(&b, []header{
		{"Content-Type", "message/rfc822"},
		{"Content-Disposition", fmt.Sprintf("attachment; filename=%q", name)},
	})
	b.WriteString(crlf)
	b.Write(raw)
	return b.Bytes()
}
