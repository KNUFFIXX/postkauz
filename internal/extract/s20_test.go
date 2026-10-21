package extract

// Tests zu A02, Teil A (S2.0): Panics im Extraktor (A5).

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

const panicCanary = "KANARIE-ANHANGTEXT-5e1f"

// A02 A5 (Frage 15): Ein Panic im Extraktor (im Prozess) lässt nur diesen
// Anhang scheitern, mit bereinigtem Grund ohne Inhalt; der Prozess lebt,
// der nächste Anhang wird extrahiert.
func TestExtractorPanicFailsOnlyThisAttachment(t *testing.T) {
	testPanic = func(kind string) {
		if kind == string(KindHTML) {
			panic("attachment text: " + panicCanary)
		}
	}
	defer func() { testPanic = nil }()
	x := New(Options{})
	res := x.Extract(context.Background(), KindHTML, strings.NewReader("<p>"+panicCanary+"</p>"), 20)
	if res.Status != StatusFailed || res.Error != "internal error in extractor: panic value withheld (string)" ||
		strings.Contains(res.Error+res.Text, panicCanary) {
		t.Fatalf("panicking extractor: %+v", res)
	}
	if res := x.Extract(context.Background(), KindText, strings.NewReader("weiter"), 6); res.Status != StatusOK || res.Text != "weiter" {
		t.Fatalf("next attachment: %+v", res)
	}
}

// A02 A5: Im Kindprozess (DOCX) endet ein Panic nicht mit der
// Standardausgabe von Go („panic: <Wert>" plus Stack mit Argumenten),
// deren erste Zeile der Elternprozess als Grund speichern würde, sondern
// mit dem bereinigten Bericht und Exit 2.
func TestChildPanicIsCleaned(t *testing.T) {
	testPanic = func(string) { panic("document text: " + panicCanary) }
	defer func() { testPanic = nil }()
	var out, errOut bytes.Buffer
	code := childMain(string(KindDOCX), strings.NewReader("PK"), &out, &errOut)
	if code != 2 || strings.Contains(out.String()+errOut.String(), panicCanary) ||
		firstLine(errOut.String()) != "internal error in extractor: panic value withheld (string)" ||
		!strings.Contains(errOut.String(), "internal/extract.TestChildPanicIsCleaned") {
		t.Fatalf("child: %d\nstdout %q\nstderr %q", code, out.String(), errOut.String())
	}
}
