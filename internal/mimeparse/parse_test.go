package mimeparse

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/corpus"
)

func parse(t *testing.T, raw string, opts Options) *Message {
	t.Helper()
	m, err := Parse(strings.NewReader(raw), opts)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestParseDateTolerant(t *testing.T) {
	cet := time.FixedZone("", 3600)
	cases := map[string]time.Time{
		"Mon, 02 Sep 2024 10:00:00 +0100":         time.Date(2024, 9, 2, 10, 0, 0, 0, cet),
		"Mon, 02 Sep 2024 10:00:00 0100":          time.Date(2024, 9, 2, 10, 0, 0, 0, cet), // Spike-Befund
		"Mon, 02 Sep 2024 10:00:00 0200":          time.Date(2024, 9, 2, 8, 0, 0, 0, time.UTC),
		"2 Sep 2024 10:00 0100 (CET)":             time.Date(2024, 9, 2, 10, 0, 0, 0, cet),
		"Mon, 2 Sep 2024 10:00:00 +0100 (CET)":    time.Date(2024, 9, 2, 10, 0, 0, 0, cet),
		"Mon, 02 Sep 2024 10:00:00":               time.Date(2024, 9, 2, 10, 0, 0, 0, time.UTC),
		"2024-09-02T10:00:00+01:00":               time.Date(2024, 9, 2, 10, 0, 0, 0, cet),
		"Mon Sep  2 10:00:00 2024":                time.Date(2024, 9, 2, 10, 0, 0, 0, time.UTC),
		"  Mon, 02 Sep 2024 10:00:00 GMT  ":       time.Date(2024, 9, 2, 10, 0, 0, 0, time.UTC),
		"Mon, 02 Sep 2024 10:00:00 \x00+0100\x00": time.Date(2024, 9, 2, 10, 0, 0, 0, cet),
	}
	for in, want := range cases {
		got, err := ParseDate(in)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("ParseDate(%q) = %v, %v; want %v", in, got, err, want.UTC())
		}
	}
	for _, bad := range []string{"", "irgendwann im Mai", "32.13.2024"} {
		if _, err := ParseDate(bad); err == nil {
			t.Errorf("ParseDate(%q) should fail", bad)
		}
	}
}

// Die acht Korpus-Fälle mit Zone ohne Vorzeichen (wie im Spike) werden
// korrekt gelesen, fehlendes/kaputtes Date fällt auf INTERNALDATE.
func TestDateFallbacksFromCorpus(t *testing.T) {
	c, err := corpus.Generate(corpus.Options{ZipBombBytes: -1})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i, e := range c.Manifest.Messages {
		switch e.Broken {
		case "tz_without_sign", "missing_date", "invalid_date":
		default:
			continue
		}
		internal, _ := time.Parse(time.RFC3339, e.InternalDate)
		m := parse(t, string(c.Raw[i]), Options{InternalDate: internal})
		if m.Date.Format(time.RFC3339) != e.Date || string(m.DateSource) != e.DateSource {
			t.Errorf("%s %s: %v (%s), want %s (%s)", e.File, e.Broken, m.Date, m.DateSource, e.Date, e.DateSource)
		}
		counts[e.Broken]++
	}
	if counts["tz_without_sign"] != 8 || counts["missing_date"] == 0 || counts["invalid_date"] == 0 {
		t.Fatalf("cases = %v", counts)
	}
}

func TestMissingDateWithoutInternalDate(t *testing.T) {
	m := parse(t, "Subject: x\r\n\r\nbody\r\n", Options{})
	if !m.Date.IsZero() || m.DateSource != DateNone {
		t.Fatalf("date = %v %s", m.Date, m.DateSource)
	}
}

func multipartWith(parts ...string) string {
	var b strings.Builder
	b.WriteString("From: a@example.invalid\r\nSubject: test\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"B\"\r\n\r\n")
	for _, p := range parts {
		b.WriteString("--B\r\n" + p + "\r\n")
	}
	b.WriteString("--B--\r\n")
	return b.String()
}

func b64Part(name, ctype string, data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var lines []string
	for len(enc) > 76 {
		lines = append(lines, enc[:76])
		enc = enc[76:]
	}
	lines = append(lines, enc)
	return "Content-Type: " + ctype + "\r\nContent-Disposition: attachment; filename=\"" + name +
		"\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + strings.Join(lines, "\r\n")
}

// Akzeptanz S1.2: Eine Zip-Bombe wird nicht entpackt. Der Anhang erscheint
// mit Größe und Hash der GEPACKTEN Bytes.
func TestZipBombIsNotExtracted(t *testing.T) {
	bomb := corpus.ZipBomb(256 << 20) // entpackt 256 MiB, gepackt rund 250 KiB
	raw := multipartWith("Content-Type: text/plain\r\n\r\nsiehe Anhang", b64Part("bombe.zip", "application/zip", bomb))
	start := time.Now()
	m := parse(t, raw, Options{})
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("parse took %s", d)
	}
	if len(m.Attachments) != 1 {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	a := m.Attachments[0]
	sum := sha256.Sum256(bomb)
	if a.Size != int64(len(bomb)) || a.SHA256 != hex.EncodeToString(sum[:]) || a.Filename != "bombe.zip" {
		t.Fatalf("attachment = %+v, packed size %d", a, len(bomb))
	}
}

// Dieses Paket darf keine Entpacker importieren (Archive werden nie
// entpackt, auch nicht zur Typerkennung).
func TestNoDecompressorImports(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range af.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			if strings.HasPrefix(p, "archive/") || strings.HasPrefix(p, "compress/") || strings.Contains(p, "klauspost/compress") {
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}

// Akzeptanz S1.2: Zu große Teile werden nur bis zur Grenze dekodiert; der
// nächste Teil wird trotzdem korrekt gelesen.
func TestOversizedPartIsTruncated(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 10_000)
	small := []byte("klein")
	raw := multipartWith(b64Part("gross.bin", "application/octet-stream", big), b64Part("klein.txt", "text/plain", small))
	m := parse(t, raw, Options{Limits: Limits{MaxPartBytes: 4096}})
	if len(m.Attachments) != 2 {
		t.Fatalf("attachments = %+v", m.Attachments)
	}
	if a := m.Attachments[0]; !a.Truncated || a.Size != 4096 || a.SHA256 != "" {
		t.Errorf("big = %+v", a)
	}
	sum := sha256.Sum256(small)
	if a := m.Attachments[1]; a.Truncated || a.SHA256 != hex.EncodeToString(sum[:]) || a.Filename != "klein.txt" {
		t.Errorf("small = %+v", a)
	}
	if !hasProblem(m, "not decoded further") {
		t.Errorf("problems = %v", m.Problems)
	}
}

// OnAttachment bekommt den dekodierten Inhalt jedes Anhangs, höchstens
// MaxPartBytes+1 Bytes; Ungelesenes liest Parse für Größe und Hash selbst.
func TestOnAttachmentGetsDecodedContent(t *testing.T) {
	big := bytes.Repeat([]byte("A"), 10_000)
	small := []byte("klein")
	raw := multipartWith(b64Part("gross.bin", "application/octet-stream", big), b64Part("klein.txt", "text/plain", small))
	got := map[string][]byte{}
	m := parse(t, raw, Options{Limits: Limits{MaxPartBytes: 4096}, OnAttachment: func(a Attachment, r io.Reader) error {
		if a.Filename == "klein.txt" {
			b, err := io.ReadAll(r)
			got[a.PartID] = b
			return err
		}
		b := make([]byte, 10) // liest nur den Anfang
		_, err := io.ReadFull(r, b)
		got[a.PartID] = b
		return err
	}})
	if !bytes.Equal(got["2"], small) || !bytes.Equal(got["1"], big[:10]) {
		t.Fatalf("got = %q", got)
	}
	if a := m.Attachments[0]; !a.Truncated || a.Size != 4096 {
		t.Errorf("big = %+v", a)
	}
	sum := sha256.Sum256(small)
	if a := m.Attachments[1]; a.SHA256 != hex.EncodeToString(sum[:]) || a.Size != int64(len(small)) {
		t.Errorf("small = %+v", a)
	}
}

func TestTextLimit(t *testing.T) {
	raw := "Subject: lang\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.Repeat("ä", 5000)
	m := parse(t, raw, Options{Limits: Limits{MaxTextBytes: 1000}})
	if len(m.Text) > 1000 || !hasProblem(m, "truncated") {
		t.Fatalf("len=%d problems=%v", len(m.Text), m.Problems)
	}
}

func TestPartAndDepthLimits(t *testing.T) {
	var parts []string
	for range 50 {
		parts = append(parts, "Content-Type: text/plain\r\n\r\nx")
	}
	m := parse(t, multipartWith(parts...), Options{Limits: Limits{MaxParts: 10}})
	if m.Parts != 11 || !hasProblem(m, "MIME parts") {
		t.Errorf("parts=%d problems=%v", m.Parts, m.Problems)
	}

	inner := "Content-Type: text/plain\r\n\r\ninnen\r\n"
	for i := range 30 {
		b := "b" + strconv.Itoa(i)
		inner = "Content-Type: multipart/mixed; boundary=\"" + b + "\"\r\n\r\n--" + b + "\r\n" + inner + "\r\n--" + b + "--\r\n"
	}
	m = parse(t, "Subject: tief\r\n"+inner, Options{Limits: Limits{MaxDepth: 10}})
	if !hasProblem(m, "nesting") || m.Text != "" {
		t.Errorf("text=%q problems=%v", m.Text, m.Problems)
	}
	m = parse(t, "Subject: tief\r\n"+inner, Options{})
	if !strings.Contains(m.Text, "innen") {
		t.Errorf("30 levels must parse with default limits: %q %v", m.Text, m.Problems)
	}
}

func TestMessageAndHeaderLimits(t *testing.T) {
	raw := "Subject: gross\r\n\r\n" + strings.Repeat("x", 10_000)
	m := parse(t, raw, Options{Limits: Limits{MaxMessageBytes: 1000}})
	if m.Size != 1000 || !hasProblem(m, "exceeds") {
		t.Errorf("size=%d problems=%v", m.Size, m.Problems)
	}
	huge := "Subject: x\r\nX-Gross: " + strings.Repeat("y", 5000) + "\r\n\r\nbody"
	if _, err := Parse(strings.NewReader(huge), Options{Limits: Limits{MaxHeaderBytes: 1000}}); !errors.Is(err, ErrUnreadable) {
		t.Errorf("huge header: %v", err)
	}
}

func TestBrokenHeadersTolerated(t *testing.T) {
	raw := " fortsetzung ohne feld\r\nFrom: =?iso-8859-1?Q?J=FCrgen?= <j@example.invalid>\r\n" +
		"Keine Doppelpunkt-Zeile\r\nSubject: Gr\xfc\xdfe roh\x00\r\nMessage-ID: ohne-klammern@example.invalid\r\n" +
		"References: <a@example.invalid> kaputt <b@example.invalid> <a@example.invalid>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n\r\nText mit \xe4 falsch deklariert\x00\r\n"
	m := parse(t, raw, Options{})
	if m.Subject != "Grüße roh" {
		t.Errorf("subject = %q", m.Subject)
	}
	if len(m.From) != 1 || m.From[0].Name != "Jürgen" {
		t.Errorf("from = %+v", m.From)
	}
	if m.MessageID != "ohne-klammern@example.invalid" {
		t.Errorf("message-id = %q", m.MessageID)
	}
	if strings.Join(m.References, ",") != "a@example.invalid,b@example.invalid" {
		t.Errorf("references = %v", m.References)
	}
	if !strings.Contains(m.Text, "mit ä falsch") || strings.Contains(m.Text, "\x00") {
		t.Errorf("text = %q", m.Text)
	}
	if !hasProblem(m, "malformed header") {
		t.Errorf("problems = %v", m.Problems)
	}
}

func TestUnknownCharsetKeepsText(t *testing.T) {
	raw := "Subject: =?x-unbekannt?Q?abc?=\r\nContent-Type: text/plain; charset=x-unbekannt\r\n\r\nHallo\r\n"
	m := parse(t, raw, Options{})
	if !strings.Contains(m.Text, "Hallo") || len(m.Problems) == 0 {
		t.Errorf("text=%q problems=%v", m.Text, m.Problems)
	}
}

func TestAddressFallback(t *testing.T) {
	raw := "From: Kaputt <<a@example.invalid>, \"Zweite\" <b@example.invalid>\r\nSubject: x\r\n\r\n"
	m := parse(t, raw, Options{})
	if len(m.From) != 2 || m.From[0].Address != "a@example.invalid" || m.From[1].Address != "b@example.invalid" {
		t.Errorf("from = %+v", m.From)
	}
}

func TestEmptyMessageIsUnreadable(t *testing.T) {
	if _, err := Parse(strings.NewReader(""), Options{}); !errors.Is(err, ErrUnreadable) {
		t.Fatalf("err = %v", err)
	}
}

func hasProblem(m *Message, sub string) bool {
	for _, p := range m.Problems {
		if strings.Contains(p, sub) {
			return true
		}
	}
	return false
}
