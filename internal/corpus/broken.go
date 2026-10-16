package corpus

import (
	"archive/zip"
	"bytes"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"
)

// ---- erfundene Anhangsinhalte (klein, harmlos) -------------------------------

func randBytes(r *rand.Rand, min, max int) []byte {
	b := make([]byte, min+r.IntN(max-min))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func fakePDF(r *rand.Rand) []byte {
	b := []byte("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n1 0 obj << /Type /Catalog >> endobj\n")
	b = append(b, randBytes(r, 200, 2000)...)
	return append(b, "\n%%EOF\n"...)
}

func fakeJPG(r *rand.Rand) []byte {
	b := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00\x01\x01\x00\x00\x01\x00\x01\x00\x00")
	b = append(b, randBytes(r, 300, 3000)...)
	return append(b, 0xff, 0xd9)
}

func fakePNG(r *rand.Rand) []byte {
	b := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	return append(b, randBytes(r, 100, 800)...)
}

func fakeTXT(r *rand.Rand) []byte {
	return []byte("Notiz zur Begehung\r\nZählerstand Wasser: " + fmt.Sprint(10000+r.IntN(90000)) + "\r\n")
}

func zipOf(files map[string]string, order []string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, _ := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate,
			Modified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
		_, _ = w.Write([]byte(files[name]))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func fakeDOCX(r *rand.Rand) []byte {
	return zipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types/>`,
		"word/document.xml":   "<w:document>" + sentences[r.IntN(len(sentences))] + "</w:document>",
	}, []string{"[Content_Types].xml", "word/document.xml"})
}

// fakeXLSX: wie fakeDOCX (gleicher Zufallszug), aber mit Arbeitsmappe,
// damit die Typprüfung (Magic Bytes, S1.6) eine XLSX-Datei erkennt.
func fakeXLSX(r *rand.Rand) []byte {
	return zipOf(map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types/>`,
		"xl/workbook.xml":     "<workbook>" + sentences[r.IntN(len(sentences))] + "</workbook>",
	}, []string{"[Content_Types].xml", "xl/workbook.xml"})
}

func fakeZIP(r *rand.Rand) []byte {
	return zipOf(map[string]string{
		"beleg1.txt": sentences[r.IntN(len(sentences))],
		"beleg2.txt": sentences[r.IntN(len(sentences))],
	}, []string{"beleg1.txt", "beleg2.txt"})
}

// ---- absichtlich kaputte Nachrichten -----------------------------------------

func replaceHeader(raw []byte, name, line string) []byte {
	lines := strings.Split(string(raw), crlf)
	for i, l := range lines {
		if l == "" {
			break
		}
		if strings.HasPrefix(l, name+":") {
			// Fortsetzungszeilen mit entfernen
			j := i + 1
			for j < len(lines) && (strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t")) {
				j++
			}
			repl := []string{}
			if line != "" {
				repl = []string{line}
			}
			lines = append(append(lines[:i:i], repl...), lines[j:]...)
			break
		}
	}
	return []byte(strings.Join(lines, crlf))
}

func insertHeader(raw []byte, before, line string) []byte {
	i := bytes.Index(raw, []byte(crlf+before+":"))
	if i < 0 {
		panic("corpus: header not found: " + before)
	}
	return append(append(clone(raw[:i+2]), line+crlf...), raw[i+2:]...)
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// broken erzeugt eine kaputte Nachricht und das, was der Parser daraus
// trotzdem lesen soll.
func (g *gen) broken(p plan) ([]byte, Entry) {
	s := g.normalSpec(people[g.pick(len(people)-1)], me, p.date)
	s.charset, s.enc, s.html, s.attachments, s.related, s.cc = "utf-8", "B", false, nil, false, nil
	if s.subject == longSubject {
		s.subject = subjects[0]
	}
	var raw []byte
	var e Entry
	switch p.broken {
	case "tz_without_sign":
		// Zone ohne Vorzeichen; sechsmal 0100 (wie im Spike), zweimal 0200.
		zone, off := "0100", 3600
		if p.tzVariant%4 == 3 {
			zone, off = "0200", 7200
		}
		d := p.date.In(time.FixedZone("", off))
		raw, e = g.render(s)
		raw = replaceHeader(raw, "Date", "Date: "+d.Format("Mon, 02 Jan 2006 15:04:05")+" "+zone)
		e.Date = d.UTC().Format(time.RFC3339)
	case "missing_date":
		raw, e = g.render(s)
		raw = replaceHeader(raw, "Date", "")
		e.Date, e.DateSource = e.InternalDate, "internal"
	case "invalid_date":
		raw, e = g.render(s)
		raw = replaceHeader(raw, "Date", "Date: irgendwann im Mai")
		e.Date, e.DateSource = e.InternalDate, "internal"
	case "invalid_message_id":
		raw, e = g.render(s)
		id := fmt.Sprintf("kaputt-ohne-klammern-%05d", g.idx)
		raw = replaceHeader(raw, "Message-ID", "Message-ID: "+id)
		e.MessageID = id
	case "missing_message_id":
		raw, e = g.render(s)
		raw = replaceHeader(raw, "Message-ID", "")
		e.MessageID = ""
	case "raw_8bit_header":
		// Betreff roh (ohne RFC 2047), halb in windows-1252, halb in UTF-8.
		cs := "utf-8"
		if g.chance(0.5) {
			cs = "windows-1252"
		}
		subj := adjust("Roh: "+s.subject, cs)
		raw, e = g.render(s)
		raw = replaceHeader(raw, "Subject", "Subject: "+string(encodeCharset(subj, cs)))
		e.Subject, e.HeaderEncoding = subj, "raw"
	case "deep_nesting":
		raw, e = g.render(s)
		head, body, _ := bytes.Cut(raw, []byte("MIME-Version: 1.0"+crlf))
		inner := body
		for range 40 {
			inner = multipartEntity("mixed", g.boundary(), [][]byte{inner}, "")
		}
		raw = append(append(clone(head), "MIME-Version: 1.0"+crlf...), inner...)
	case "overlong_line":
		marker := "Überlange Zeile ohne Umbruch"
		long := strings.Repeat(marker+" ", 1500) // rund 45 KB in einer Zeile
		head, _, _ := bytes.Cut(g.mustRender(&e, s), []byte("MIME-Version: 1.0"+crlf))
		raw = append(clone(head), "MIME-Version: 1.0"+crlf+
			"Content-Type: text/plain; charset=\"utf-8\""+crlf+
			"Content-Transfer-Encoding: 8bit"+crlf+crlf+
			e.BodyContains+crlf+long+crlf...)
	case "lf_only":
		raw, e = g.render(s)
		raw = bytes.ReplaceAll(raw, []byte(crlf), []byte("\n"))
	case "nul_bytes":
		raw, e = g.render(s)
		raw = insertHeader(raw, "Date", "X-Kaputt: mit\x00NUL")
		raw = append(raw, "\x00\x00 NUL am Ende\r\n"...)
		e.Unchecked = []string{"body_contains"}
	case "truncated":
		// Mit Anhang, mitten im Anhang abgeschnitten: schließende Grenze fehlt.
		s.attachments = []int{0}
		raw, e = g.render(s)
		raw = raw[:len(raw)-len(raw)/4]
		e.Unchecked = []string{"attachments"}
	case "header_no_colon":
		raw, e = g.render(s)
		raw = insertHeader(raw, "Subject", "Diese Zeile hat keinen Doppelpunkt")
	default:
		panic("corpus: unknown broken kind " + p.broken)
	}
	e.Broken = p.broken
	return raw, e
}

func (g *gen) mustRender(e *Entry, s spec) []byte {
	raw, entry := g.render(s)
	*e = entry
	return raw
}
