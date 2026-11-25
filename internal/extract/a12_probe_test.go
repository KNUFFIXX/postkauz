package extract

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// Die eingebettete Test-PDF liefert mit Poppler (wenn installiert) genau
// ProbeText (A12 Teil 3 (b)).
func TestA12ProbePDFWithPoppler(t *testing.T) {
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		t.Skip("pdftotext not installed")
	}
	cmd := exec.Command(p, "-q", "-enc", "UTF-8", "-eol", "unix", "-", "-")
	cmd.Stdin = bytes.NewReader(ProbePDFForTest())
	out, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(out)) != ProbeText {
		t.Fatalf("pdftotext = %q, %v", out, err)
	}
}
