package magic

import (
	"archive/zip"
	"bytes"
	"go/build"
	"io"
	"strings"
	"testing"
)

func zipWith(names ...string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, n := range names {
		f, _ := w.Create(n)
		_, _ = f.Write([]byte(strings.Repeat("x", 100)))
	}
	_ = w.Close()
	return b.Bytes()
}

func odt() []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	_, _ = f.Write([]byte("application/vnd.oasis.opendocument.text"))
	g, _ := w.Create("content.xml")
	_, _ = g.Write([]byte("<x/>"))
	_ = w.Close()
	return b.Bytes()
}

func detect(b []byte) Type {
	var s Sniffer
	_, _ = io.Copy(&s, bytes.NewReader(b))
	return s.Detect()
}

func TestDetect(t *testing.T) {
	big := append(zipWith("[Content_Types].xml", "word/document.xml"), make([]byte, 0)...)
	// Großes XLSX: Namen nur im zentralen Verzeichnis am Ende auffindbar.
	var bx bytes.Buffer
	zw := zip.NewWriter(&bx)
	pad, _ := zw.CreateHeader(&zip.FileHeader{Name: "[Content_Types].xml", Method: zip.Store})
	_, _ = pad.Write(bytes.Repeat([]byte{'a'}, 200<<10))
	f, _ := zw.Create("xl/workbook.xml")
	_, _ = f.Write([]byte("<w/>"))
	_ = zw.Close()
	cases := map[string]struct {
		data []byte
		mime string
		exe  bool
	}{
		"pdf":       {[]byte("%PDF-1.7\n..."), PDF, false},
		"exe":       {append([]byte("MZ\x90\x00"), make([]byte, 100)...), EXE, true},
		"elf":       {[]byte("\x7fELF\x02\x01\x01"), ELF, true},
		"script":    {[]byte("#!/bin/sh\nrm -rf /\n"), Script, true},
		"png":       {[]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), "image/png", false},
		"jpeg":      {[]byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00"), "image/jpeg", false},
		"zip":       {zipWith("a.txt", "b.txt"), ZIP, false},
		"docx":      {big, DOCX, false},
		"xlsx tail": {bx.Bytes(), XLSX, false},
		"pptx":      {zipWith("[Content_Types].xml", "ppt/presentation.xml"), PPTX, false},
		"jar":       {zipWith("META-INF/MANIFEST.MF", "a/B.class"), JAR, true},
		"odt":       {odt(), "application/vnd.oasis.opendocument.text", false},
		"ole":       {[]byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1\x00\x00"), OLE, false},
		"rtf":       {[]byte(`{\rtf1\ansi Hallo}`), RTF, false},
		"gzip":      {[]byte("\x1f\x8b\x08\x00\x00"), GZIP, false},
		"text":      {[]byte("Notiz: Zählerstand 4711\r\n"), TextPlain, false},
		"latin1":    {[]byte("Z\xe4hlerstand\r\n"), TextPlain, false},
		"html":      {[]byte("<html><body>x</body></html>"), TextHTML, false},
		"mail":      {[]byte("From: a@example.org\r\nTo: b@example.org\r\nSubject: x\r\nDate: Mon\r\n\r\nText"), RFC822, false},
		"lnk":       {[]byte("L\x00\x00\x00\x01\x14\x02\x00\x00\x00"), Shortcut, true},
		"binary":    {[]byte{0x00, 0x01, 0x02, 0x03, 0xfe, 0xff, 0x10, 0x11}, OctetStream, false},
		"empty":     {nil, OctetStream, false},
	}
	for name, c := range cases {
		got := detect(c.data)
		if got.MIME != c.mime || got.Executable != c.exe {
			t.Errorf("%s: %+v, want %s exe=%v", name, got, c.mime, c.exe)
		}
	}
}

func TestCheck(t *testing.T) {
	exe := Type{MIME: EXE, Executable: true}
	pdf := Type{MIME: PDF}
	png := Type{MIME: "image/png"}
	docx := Type{MIME: DOCX}
	txt := Type{MIME: TextPlain, Text: true}
	unknown := Type{MIME: OctetStream}
	cases := []struct {
		name, declared, file string
		d                    Type
		mismatch, exe        bool
	}{
		{"pdf ok", "application/pdf", "Rechnung.pdf", pdf, false, false},
		{"exe as pdf", "application/pdf", "Rechnung.pdf", exe, true, true},
		{"exe generic with pdf name", "application/octet-stream", "Rechnung.pdf", exe, true, true},
		{"png as jpeg", "image/jpeg", "Foto.jpg", png, true, false},
		{"jpg alias", "image/jpg", "Foto.jpg", Type{MIME: "image/jpeg"}, false, false},
		{"docx generic", "application/octet-stream", "Vertrag.docx", docx, false, false},
		{"docx declared zip", "application/zip", "Vertrag.zip", docx, false, false},
		{"zip declared docx", DOCX, "Vertrag.docx", Type{MIME: ZIP, Archive: true}, true, false},
		{"text variants", "text/csv", "liste.csv", txt, false, false},
		{"eml", "message/rfc822", "Weiterleitung.eml", Type{MIME: RFC822, Text: true}, false, false},
		{"eml as text", "message/rfc822", "Weiterleitung.eml", txt, false, false},
		{"pdf with random bytes", "application/pdf", "scan.pdf", unknown, true, false},
		{"audio unverifiable", "audio/mpeg", "ansage.mp3", unknown, false, false},
		{"no name generic", "application/octet-stream", "", pdf, false, false},
		{"bat is executable", "text/plain", "start.bat", txt, false, true},
		{"js is executable", "application/javascript", "rechnung.js", txt, false, true},
		{"text claimed pdf", "application/pdf", "x.pdf", txt, true, false},
	}
	for _, c := range cases {
		r := Check(c.declared, c.file, c.d)
		if r.Mismatch != c.mismatch || r.Executable != c.exe {
			t.Errorf("%s: %+v, want mismatch=%v exe=%v", c.name, r, c.mismatch, c.exe)
		}
		if r.Mismatch && r.Reason == "" {
			t.Errorf("%s: mismatch without reason", c.name)
		}
	}
}

// Archive werden nie entpackt: Das Paket importiert keinen Entpacker.
func TestNoDecompressorImports(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if strings.HasPrefix(imp, "archive/") || strings.HasPrefix(imp, "compress/") {
			t.Errorf("magic imports %s", imp)
		}
	}
}
