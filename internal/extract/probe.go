package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/procattr"
)

// Probe des PDF-Programms (A12 Teil 3 (b), Befund B6): Abnahme D17 fand im
// PATH zuerst das pdftotext von Git für Windows (Xpdf 4.06). Xpdf liest
// nicht von stdin; es gab seine Hilfe aus, und PDF-Anhänge scheiterten
// endgültig. PostKauz führt das Programm deshalb einmal mit einer kleinen,
// eingebetteten Test-PDF über stdin aus (dieselben Argumente wie beim
// Auszug, Zeitgrenze 10 s) und erwartet ProbeText. Gelingt das, gilt das
// Programm für den Prozess; scheitert es endgültig (Hilfe, Exit-Code, kein
// oder falscher Text), werden PDFs nicht failed, sondern skipped
// (ReasonPDFNoStdin), und die Probe läuft höchstens einmal je Stunde
// erneut; scheitert sie vorübergehend (Start, Signal, NTSTATUS,
// Zeitgrenze), fällt kein Urteil (Auszug wie bisher, nächste Probe später).
// Der Zustand gilt je Pfad für den ganzen Prozess (alle Extraktoren).

// Ergebnisse der Probe (doctor --json pdftotext.probe).
const (
	ProbeOK        = "ok"
	ProbeFailed    = "failed"
	ProbeTransient = "transient"
	ProbeNotRun    = "not_run"
)

// ProbeText ist der Text der eingebetteten Test-PDF.
const ProbeText = "PostKauz probe"

// ProbeTimeout ist die Zeitgrenze der Probe; ProbeRetryAfter der Abstand
// nach einem Fehlschlag.
const (
	ProbeTimeout    = 10 * time.Second
	ProbeRetryAfter = time.Hour
)

// Gründe übersprungener PDFs (extract_error, Status skipped).
const (
	// ReasonPDFToolMissing: [extract] pdftotext nennt eine Datei, die fehlt
	// (etwa nach einem Update von Poppler in einen neuen Ordner).
	ReasonPDFToolMissing = "pdftotext not found at the configured path"
	// ReasonPDFNoStdin: Die Probe ist endgültig gescheitert.
	ReasonPDFNoStdin = "pdftotext cannot read the PDF from stdin (wrong program? see doctor)"
)

// ToolReasons sind die Gründe, die eine gelungene Probe nachholt
// (RequeueSkipped).
var ToolReasons = []string{ReasonNoPDFTool, ReasonPDFToolMissing, ReasonPDFNoStdin}

// probePDF ist eine kleine PDF mit Helvetica und ProbeText (beim Start
// gebaut, damit die Verweise der xref-Tabelle stimmen).
var probePDF = buildProbePDF()

func buildProbePDF() []byte {
	content := "BT /F1 12 Tf 72 720 Td (" + ProbeText + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// ProbePDFForTest liefert die eingebettete Test-PDF.
func ProbePDFForTest() []byte { return bytes.Clone(probePDF) }

// ProbeResult ist das Ergebnis einer Probe.
type ProbeResult struct {
	Path  string
	State string // ProbeOK, ProbeFailed, ProbeTransient, ProbeNotRun
	// Detail: erste Zeile der Ausgabe oder des Fehlers (nie Inhalt eines
	// Anhangs; die Test-PDF ist eingebettet).
	Detail string
	At     time.Time
	// Changed: Das Ergebnis weicht vom vorigen dieses Pfads ab (Log: eine
	// Warnung je Prozess und Ergebnis, nicht je Anhang).
	Changed bool
}

// probes hält das letzte Ergebnis je Pfad (Prozess).
var probes = struct {
	sync.Mutex
	m map[string]ProbeResult
}{m: map[string]ProbeResult{}}

// ResetProbesForTest löscht die gemerkten Ergebnisse.
func ResetProbesForTest() {
	probes.Lock()
	probes.m = map[string]ProbeResult{}
	probes.Unlock()
}

func (x *Extractor) now() time.Time {
	if x.opts.Now != nil {
		return x.opts.Now()
	}
	return time.Now()
}

// LastProbe liefert das gemerkte Ergebnis für das PDF-Programm dieses
// Extraktors (ok false: noch keins).
func (x *Extractor) LastProbe() (ProbeResult, bool) {
	tool := x.PDFTool()
	probes.Lock()
	defer probes.Unlock()
	r, ok := probes.m[tool]
	return r, ok && tool != ""
}

// Probe prüft das PDF-Programm, wenn fällig (nach einem Erfolg nie wieder
// im Prozess, nach einem Fehlschlag frühestens ProbeRetryAfter später).
// Ohne Programm oder mit fehlender Datei: ProbeNotRun.
func (x *Extractor) Probe(ctx context.Context) ProbeResult {
	tool := x.PDFTool()
	if tool == "" {
		return ProbeResult{State: ProbeNotRun, Detail: ReasonNoPDFTool}
	}
	if x.toolMissing(tool) {
		return ProbeResult{Path: tool, State: ProbeNotRun, Detail: ReasonPDFToolMissing}
	}
	now := x.now()
	probes.Lock()
	prev, had := probes.m[tool]
	probes.Unlock()
	if had && (prev.State == ProbeOK || now.Sub(prev.At) < ProbeRetryAfter) {
		prev.Changed = false
		return prev
	}
	r := x.runProbe(ctx, tool)
	r.At = now
	r.Changed = !had || prev.State != r.State
	probes.Lock()
	probes.m[tool] = r
	probes.Unlock()
	return r
}

// runProbe führt die Probe aus.
func (x *Extractor) runProbe(ctx context.Context, tool string) ProbeResult {
	y := &Extractor{opts: x.opts}
	y.opts.Timeout = ProbeTimeout
	res, err := y.run(ctx, tool, x.opts.PDFArgs, nil, bytes.NewReader(probePDF))
	out := ProbeResult{Path: tool}
	var re *RunError
	switch {
	case err == nil && strings.TrimSpace(res.Text) == ProbeText:
		out.State = ProbeOK
	case err == nil:
		out.State, out.Detail = ProbeFailed, "unexpected output: "+firstLine(res.Text)
		if strings.TrimSpace(res.Text) == "" {
			out.Detail = "no text"
		}
	case errors.As(err, &re) && re.Transient, errors.Is(err, ErrTimeout), ctx.Err() != nil:
		out.State, out.Detail = ProbeTransient, firstLine(err.Error())
	default:
		out.State, out.Detail = ProbeFailed, firstLine(err.Error())
	}
	return out
}

// probeFailed meldet, ob die letzte Probe dieses Pfads endgültig
// gescheitert ist (dann werden PDFs skipped).
func probeFailed(tool string) bool {
	probes.Lock()
	defer probes.Unlock()
	r, ok := probes.m[tool]
	return ok && r.State == ProbeFailed
}

// toolMissing: ein konfigurierter Pfad, dessen Datei fehlt.
func (x *Extractor) toolMissing(tool string) bool {
	if !x.opts.PDFConfigured {
		return false
	}
	_, err := os.Stat(tool)
	return err != nil
}

// Version liefert die erste Zeile von „<Programm> -v“ (Poppler und Xpdf
// schreiben sie nach stderr), Zeitgrenze 10 s; "" ohne Programm.
func (x *Extractor) Version(ctx context.Context) string {
	tool := x.PDFTool()
	if tool == "" || x.toolMissing(tool) {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "-v")
	cmd.Env = baseEnv()
	procattr.Prepare(cmd)
	out, _ := cmd.CombinedOutput()
	return firstLine(string(out))
}

// ConfigOptions übersetzt [extract] pdftotext in Options: "" = erstes
// pdftotext im PATH, "off" = keine PDFs, sonst der absolute Pfad (vom
// Lader geprüft).
func ConfigOptions(pdftotext string) Options {
	switch v := strings.TrimSpace(pdftotext); v {
	case "":
		return Options{}
	case "off":
		return Options{PDFToText: "-"}
	default:
		return Options{PDFToText: v, PDFConfigured: true}
	}
}

// SetProbeForTest merkt für tool ein Ergebnis (Tests mit Skripten als
// pdftotext, die die Test-PDF nicht lesen).
func SetProbeForTest(tool, state string, at time.Time) {
	probes.Lock()
	probes.m[tool] = ProbeResult{Path: tool, State: state, At: at}
	probes.Unlock()
}
