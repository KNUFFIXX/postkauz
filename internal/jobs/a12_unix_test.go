//go:build unix

package jobs

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
)

// A12 Teil 0 (c): Das Budget zählt Versuche; liegen im Block nur
// zurückgestellte Anhänge, endet der Lauf trotzdem nach dem Budget.
func TestA12ExtractBudgetCountsAttempts(t *testing.T) {
	f := pdfFixture(t, 25)
	ctx := context.Background()
	clk := &a11Clock{now: time.Now()}
	r := f.a11Runner(extract.Options{PDFToText: "/bin/sh", PDFArgs: []string{"-c", "sleep 0.05; kill -9 $$"}}, clk, nil)
	r.ExtractBudget = 150 * time.Millisecond
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	n, err := f.st.QueryIntForTest(`SELECT count(*) FROM meta WHERE key LIKE 'extract_retry/%'`)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 || n >= 25 {
		t.Fatalf("attempts in one run = %d, want at least one and fewer than 25 (budget)", n)
	}
}

// Die Warnung eines gescheiterten Auszugs nennt das Hilfsprogramm (Pfad),
// nie Dateiname oder Inhalt.
func TestA12ExtractWarningNamesTool(t *testing.T) {
	f := pdfFixture(t, 1)
	clk := &a11Clock{now: time.Now()}
	var log bytes.Buffer
	r := f.a11Runner(extract.Options{PDFToText: "/bin/sh", PDFArgs: []string{"-c", "echo kaputt >&2; exit 1"}}, clk, &log)
	if _, err := r.RunAccount(context.Background(), f.acct); err != nil {
		t.Fatal(err)
	}
	if l := log.String(); !strings.Contains(l, "tool=/bin/sh") || strings.Contains(l, "geheim-name") {
		t.Fatalf("log:\n%s", l)
	}
}
