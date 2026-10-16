package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const helperEnv = "POSTKAUZ_EXTRACT_TEST_HELPER"

// TestHelperProcess ist kein Test, sondern ein Fake-Extraktor, den die
// Tests als Kindprozess starten (Muster aus os/exec).
func TestHelperProcess(t *testing.T) {
	switch os.Getenv(helperEnv) {
	case "":
		return
	case "hang":
		if f := os.Getenv("POSTKAUZ_EXTRACT_TEST_PIDFILE"); f != "" {
			_ = os.WriteFile(f, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
		time.Sleep(time.Hour)
	case "flood":
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for {
			if _, err := os.Stdout.Write(chunk); err != nil {
				os.Exit(3)
			}
		}
	case "env":
		fmt.Print(strings.Join(os.Environ(), "\n"))
	}
	os.Exit(0)
}

func helper(mode string, o Options) *Extractor {
	o.PDFToText = os.Args[0]
	o.PDFArgs = []string{"-test.run=^TestHelperProcess$"}
	o.Env = append(o.Env, helperEnv+"="+mode)
	return New(o)
}

// Akzeptanz S1.6: Ein hängender Extraktor wird nach der Zeitgrenze
// beendet; der Aufruf kehrt zurück, der Prozess lebt nicht weiter.
func TestTimeoutKillsHangingExtractor(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	pidFile := filepath.Join(t.TempDir(), "pid")
	x := helper("hang", Options{Timeout: 500 * time.Millisecond, Env: []string{"POSTKAUZ_EXTRACT_TEST_PIDFILE=" + pidFile}})
	start := time.Now()
	res := x.Extract(context.Background(), KindPDF, strings.NewReader("%PDF-1.4"), 8)
	took := time.Since(start)
	if res.Status != StatusFailed || !strings.Contains(res.Error, "timed out") {
		t.Fatalf("result = %+v", res)
	}
	if took < 500*time.Millisecond || took > 5*time.Second {
		t.Fatalf("returned after %s, want about 0.5s", took)
	}
	if runtime.GOOS == "windows" {
		return
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("helper did not start: %v", err)
	}
	pid, _ := strconv.Atoi(string(b))
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("extractor process %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOutputLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	x := helper("flood", Options{MaxOutput: 1000, Timeout: 10 * time.Second})
	res := x.Extract(context.Background(), KindPDF, strings.NewReader("%PDF-1.4"), 8)
	if res.Status != StatusOK || !res.Truncated || len(res.Text) != 1000 {
		t.Fatalf("status %s, truncated %v, len %d, err %q", res.Status, res.Truncated, len(res.Text), res.Error)
	}
}

// Kindprozesse sehen keine Geheimnisse aus der Umgebung.
func TestChildEnvironmentHasNoSecrets(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", "kanarienvogel-4711")
	t.Setenv("POSTKAUZ_KEY", "kanarienvogel-4712")
	res := helper("env", Options{}).Extract(context.Background(), KindPDF, strings.NewReader("x"), 1)
	if res.Status != StatusOK || strings.Contains(res.Text, "kanarienvogel") || !strings.Contains(res.Text, "GOMEMLIMIT=") {
		t.Fatalf("child environment = %q (%s %s)", res.Text, res.Status, res.Error)
	}
}

func docx(document string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("[Content_Types].xml")
	_, _ = f.Write([]byte(`<Types/>`))
	f, _ = w.Create("word/document.xml")
	_, _ = f.Write([]byte(document))
	_ = w.Close()
	return b.Bytes()
}

func self() Options { return Options{Self: []string{os.Args[0], "-test.run=^$"}} }

func TestDOCXInSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	d := docx(`<?xml version="1.0"?><w:document xmlns:w="urn:w"><w:body><w:p><w:r><w:t>Übergabe</w:t></w:r><w:r><w:tab/><w:t>Schlüssel</w:t></w:r></w:p><w:p><w:r><w:t>Zweiter Absatz</w:t></w:r></w:p></w:body></w:document>`)
	res := New(self()).Extract(context.Background(), KindDOCX, bytes.NewReader(d), int64(len(d)))
	if res.Status != StatusOK || res.Text != "Übergabe\tSchlüssel\nZweiter Absatz\n" {
		t.Fatalf("result = %+v", res)
	}
	// Vereinfachtes DOCX ohne <w:t>: aller Zeichentext.
	d = docx(`<w:document>Nur Text</w:document>`)
	if res := New(self()).Extract(context.Background(), KindDOCX, bytes.NewReader(d), int64(len(d))); res.Text != "Nur Text\n" {
		t.Fatalf("plain docx = %+v", res)
	}
}

func TestDOCXZipBombAndGarbage(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	bomb := docx("<w:document>" + strings.Repeat(" ", 40<<20) + "</w:document>")
	res := New(self()).Extract(context.Background(), KindDOCX, bytes.NewReader(bomb), int64(len(bomb)))
	if res.Status != StatusFailed || !strings.Contains(res.Error, "zip bomb") {
		t.Fatalf("bomb = %+v", res)
	}
	res = New(self()).Extract(context.Background(), KindDOCX, strings.NewReader("kein zip"), 8)
	if res.Status != StatusFailed || !strings.Contains(res.Error, "not a valid DOCX") {
		t.Fatalf("garbage = %+v", res)
	}
}

func TestPDF(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	x := New(Options{})
	if x.PDFTool() == "" {
		t.Skip("pdftotext not installed; PDF extraction is skipped in that case (tested below)")
	}
	pdf := minimalPDF("Extraktionsprobe PDF 4711")
	res := x.Extract(context.Background(), KindPDF, bytes.NewReader(pdf), int64(len(pdf)))
	if res.Status != StatusOK || !strings.Contains(res.Text, "Extraktionsprobe PDF 4711") {
		t.Fatalf("pdf = %+v", res)
	}
	res = x.Extract(context.Background(), KindPDF, strings.NewReader("%PDF-kaputt"), 11)
	if res.Status != StatusFailed {
		t.Fatalf("broken pdf = %+v", res)
	}
}

func TestSkipped(t *testing.T) {
	x := New(Options{PDFToText: "-", MaxInput: 100})
	if res := x.Extract(context.Background(), KindPDF, strings.NewReader("%PDF"), 4); res.Status != StatusSkipped || !strings.Contains(res.Error, "pdftotext") {
		t.Fatalf("no tool = %+v", res)
	}
	if res := x.Extract(context.Background(), KindText, strings.NewReader("x"), 101); res.Status != StatusSkipped {
		t.Fatalf("too large = %+v", res)
	}
	if res := x.Extract(context.Background(), KindNone, strings.NewReader("x"), 1); res.Status != StatusSkipped {
		t.Fatalf("none = %+v", res)
	}
}

func TestInProcessKinds(t *testing.T) {
	x := New(Options{})
	cases := []struct {
		kind   Kind
		in     string
		want   string
		hidden bool
	}{
		{KindText, "Z\xe4hlerstand 4711", "Zählerstand 4711", false},
		{KindText, "\xff\xfeA\x00\xe4\x00", "Aä", false},
		{KindText, "a‮b", "ab", false},
		{KindHTML, `<p>Aushang</p><p style="display:none">geheim</p>`, "Aushang", true},
		{KindMail, "From: Anna <a@example.org>\r\nSubject: Weitergeleitet\r\nDate: Mon, 2 Mar 2026 09:00:00 +0100\r\n\r\nInhalt der Mail\r\n",
			"Subject: Weitergeleitet\nFrom: Anna <a@example.org>\nDate: 2026-03-02T08:00:00Z\n\nInhalt der Mail", false},
	}
	for _, c := range cases {
		res := x.Extract(context.Background(), c.kind, strings.NewReader(c.in), int64(len(c.in)))
		if res.Status != StatusOK || strings.TrimSpace(res.Text) != c.want || res.Hidden != c.hidden {
			t.Errorf("%s %q: %+v", c.kind, c.in, res)
		}
	}
}

func TestKindOf(t *testing.T) {
	for mime, want := range map[string]Kind{"application/pdf": KindPDF, "text/plain": KindText, "text/html": KindHTML,
		"message/rfc822": KindMail, "image/png": KindNone, "application/zip": KindNone,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": KindDOCX} {
		if got := KindOf(mime); got != want {
			t.Errorf("KindOf(%s) = %q, want %q", mime, got, want)
		}
	}
}

// minimalPDF: gültiges PDF mit einer Zeile (wie im Korpus).
func minimalPDF(text string) []byte {
	content := "BT /F1 12 Tf 72 770 Td (" + text + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, x)
	return b.Bytes()
}
