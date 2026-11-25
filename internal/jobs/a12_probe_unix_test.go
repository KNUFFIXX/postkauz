//go:build unix

package jobs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
)

// A12 Teil 3 (a), (b): PDF-Programm aus der Konfiguration und Probe.

// script legt ein ausführbares Skript an (Inhalt nach #!/bin/sh).
func script(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// goodPDF liest stdin und gibt den Text der Test-PDF aus (wie Poppler).
const goodPDF = `cat >/dev/null; printf 'PostKauz probe\n\n\f'`

// xpdfLike gibt seine Hilfe aus und endet mit Exit 99 (wie Xpdf 4.06).
const xpdfLike = `echo "pdftotext version 4.06 [www.xpdfreader.com]" >&2; echo "Usage: pdftotext [options] <PDF-file> [<text-file>]" >&2; exit 99`

func (f *fixture) runner12(o extract.Options, clk *a11Clock) *Runner {
	o.Now = clk.Now
	return &Runner{Store: f.st, Blobs: f.blobs, Extractor: extract.New(o), Batch: 7, ExtractBudget: NoExtractBudget, Now: clk.Now}
}

// Ein Programm wie Xpdf: Probe scheitert endgültig, PDFs skipped mit
// eigenem Grund, nie failed; danach das richtige Programm (ohne Neustart):
// nach einer Stunde läuft die Probe neu, die PDFs werden pending → ok.
func TestA12ProbeWrongProgramThenRight(t *testing.T) {
	extract.ResetProbesForTest()
	f := pdfFixture(t, 2)
	ctx := context.Background()
	clk := &a11Clock{now: time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)}
	tool := filepath.Join(t.TempDir(), "pdftotext")
	script(t, tool, xpdfLike)
	r := f.runner12(extract.ConfigOptions(tool), clk)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	st, e := f.status(t)
	if st != "skipped" || e != extract.ReasonPDFNoStdin {
		t.Fatalf("with the wrong program: %s %q", st, e)
	}
	if n, _ := f.st.QueryIntForTest(`SELECT count(*) FROM attachments WHERE extract_status = 'failed'`); n != 0 {
		t.Fatalf("failed = %d", n)
	}
	// Das richtige Programm unter demselben Pfad (Poppler nachinstalliert).
	script(t, tool, goodPDF)
	clk.now = clk.now.Add(30 * time.Minute)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.status(t); st != "skipped" {
		t.Fatalf("probe repeated within the hour: %s", st)
	}
	clk.now = clk.now.Add(31 * time.Minute)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	if st, e := f.status(t); st != "ok" {
		t.Fatalf("after the hour: %s %q", st, e)
	}
}

// Konfigurierte Datei fehlt: skipped mit eigenem Grund; ist sie da, wird
// nachgeholt.
func TestA12ConfiguredToolMissing(t *testing.T) {
	extract.ResetProbesForTest()
	f := pdfFixture(t, 1)
	ctx := context.Background()
	clk := &a11Clock{now: time.Now()}
	tool := filepath.Join(t.TempDir(), "poppler", "pdftotext")
	r := f.runner12(extract.ConfigOptions(tool), clk)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	if st, e := f.status(t); st != "skipped" || e != extract.ReasonPDFToolMissing {
		t.Fatalf("missing: %s %q", st, e)
	}
	if err := os.MkdirAll(filepath.Dir(tool), 0o755); err != nil {
		t.Fatal(err)
	}
	script(t, tool, goodPDF)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	if st, e := f.status(t); st != "ok" {
		t.Fatalf("after the file is there: %s %q", st, e)
	}
}

// Der konfigurierte Pfad gilt, auch wenn ein anderes pdftotext im PATH
// steht.
func TestA12ConfiguredPathWins(t *testing.T) {
	extract.ResetProbesForTest()
	dir := t.TempDir()
	script(t, filepath.Join(dir, "pdftotext"), xpdfLike)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	tool := filepath.Join(t.TempDir(), "pdftotext")
	script(t, tool, goodPDF)
	x := extract.New(extract.ConfigOptions(tool))
	if x.PDFTool() != tool {
		t.Fatalf("tool = %s", x.PDFTool())
	}
	if pr := x.Probe(context.Background()); pr.State != extract.ProbeOK {
		t.Fatalf("probe = %+v", pr)
	}
	if off := extract.New(extract.ConfigOptions("off")); off.PDFTool() != "" {
		t.Fatalf("off: %s", off.PDFTool())
	}
}

// Probe scheitert vorübergehend (Start): kein Urteil, der Auszug läuft wie
// bisher (pending mit Zähler, nicht skipped).
func TestA12ProbeTransientNoVerdict(t *testing.T) {
	extract.ResetProbesForTest()
	f := pdfFixture(t, 1)
	clk := &a11Clock{now: time.Now()}
	r := f.runner12(extract.Options{PDFToText: t.TempDir()}, clk) // ein Ordner startet nicht
	if _, err := r.RunAccount(context.Background(), f.acct); err != nil {
		t.Fatal(err)
	}
	pr, ok := r.Extractor.LastProbe()
	if st, _ := f.status(t); st != "pending" || !ok || pr.State != extract.ProbeTransient {
		t.Fatalf("status %s, probe %+v", st, pr)
	}
}
