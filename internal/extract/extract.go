// Package extract gewinnt Text aus Anhängen (S1.6, docs/sicherheit.md
// Abschnitt 7): PDF über pdftotext (wenn installiert, sonst übersprungen),
// DOCX über die Standardbibliothek (archive/zip, encoding/xml), Text und
// HTML direkt, weitergeleitete Mails über den eigenen Parser.
//
// Was fremde Binärformate auspackt (PDF, DOCX), läuft in einem
// **Subprozess** mit Zeitgrenze (Standard 30 s, danach wird er beendet),
// Eingabegrenze (Standard 32 MiB), Ausgabegrenze (Standard 1 MiB Text) und
// weichem Speicherlimit (GOMEMLIMIT im eigenen Kindprozess). Der
// Kindprozess für DOCX ist das eigene Programm (os.Executable) mit der
// Umgebungsvariable POSTKAUZ_INTERNAL_EXTRACT; das init dieses Pakets
// erkennt das und arbeitet dann nur als Extraktor. Kindprozesse bekommen
// eine knappe Umgebung ohne Geheimnisse (keine POSTKAUZ_PASSWORD_*).
//
// Text, HTML und Mails werden im Prozess gelesen (kein Entpacken; der
// HTML-Teil läuft durch internal/sanitize, versteckter Text wird entfernt
// und gemeldet).
package extract

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/magic"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
	"github.com/KNUFFIXX/postkauz/internal/procattr"
	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// Kind ist die Art der Extraktion.
type Kind string

// Arten.
const (
	KindNone Kind = ""
	KindPDF  Kind = "pdf"
	KindDOCX Kind = "docx"
	KindText Kind = "text"
	KindHTML Kind = "html"
	KindMail Kind = "mail"
)

// KindOf ordnet einem erkannten Typ (Magic Bytes) die Extraktion zu.
func KindOf(mime string) Kind {
	switch mime {
	case magic.PDF:
		return KindPDF
	case magic.DOCX:
		return KindDOCX
	case magic.TextHTML:
		return KindHTML
	case magic.RFC822:
		return KindMail
	case magic.TextPlain, "text/xml":
		return KindText
	}
	return KindNone
}

// Status einer Extraktion (attachments.extract_status).
const (
	StatusOK      = "ok"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
)

// Options steuert den Extraktor. Nullwerte = Standard.
type Options struct {
	Timeout   time.Duration // je Anhang (30 s)
	MaxInput  int64         // größter Anhang (32 MiB); größere werden übersprungen
	MaxOutput int           // Text (1 MiB); mehr wird abgeschnitten
	// PDFToText ist das Programm für PDF ("" = pdftotext im PATH suchen,
	// "-" = PDF nicht extrahieren); PDFArgs seine Argumente
	// (Standard: -q -enc UTF-8 -eol unix - -; Eingabe über stdin).
	PDFToText string
	PDFArgs   []string
	// PDFConfigured: PDFToText kommt aus [extract] pdftotext (A12 Teil 3
	// (a)); fehlt die Datei, werden PDFs skipped (ReasonPDFToolMissing)
	// statt am Start zu scheitern.
	PDFConfigured bool
	// Now ist die Uhr der Probe (Tests); nil = time.Now.
	Now func() time.Time
	// Self ist das Kommando für den eigenen Kindprozess (DOCX); leer =
	// os.Executable().
	Self []string
	// MemoryLimit ist GOMEMLIMIT des eigenen Kindprozesses (256 MiB).
	MemoryLimit int64
	// Env ergänzt die knappe Umgebung der Kindprozesse (Tests).
	Env []string
}

// Defaults.
const (
	DefaultTimeout     = 30 * time.Second
	DefaultMaxInput    = 32 << 20
	DefaultMaxOutput   = 1 << 20
	DefaultMemoryLimit = 256 << 20
)

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.MaxInput <= 0 {
		o.MaxInput = DefaultMaxInput
	}
	if o.MaxOutput <= 0 {
		o.MaxOutput = DefaultMaxOutput
	}
	if o.MemoryLimit <= 0 {
		o.MemoryLimit = DefaultMemoryLimit
	}
	if o.PDFArgs == nil {
		o.PDFArgs = []string{"-q", "-enc", "UTF-8", "-eol", "unix", "-", "-"}
	}
	return o
}

// Result ist das Ergebnis einer Extraktion. Error nennt den Grund bei
// failed/skipped, nie Inhalt.
type Result struct {
	Status    string
	Text      string
	Error     string
	Truncated bool
	// Hidden: Der Anhang (HTML, Mail, DOCX mit <w:vanish/>) enthielt
	// versteckten Text, der entfernt wurde.
	Hidden bool
	// ErrKind: Art des Fehlers bei failed (A11 Teil 3): start, signal,
	// ntstatus (vorübergehend: das Hilfsprogramm lief nicht), exit,
	// timeout, internal (endgültig). Transient: vorübergehend; der
	// extract-Job versucht es später erneut.
	ErrKind   string
	Transient bool
}

// Arten eines Fehlers (Result.ErrKind, A11 Teil 3).
const (
	ErrKindStart    = "start"    // der Start scheiterte (fork/exec, exec:)
	ErrKindSignal   = "signal"   // durch ein Signal beendet (Unix)
	ErrKindNTStatus = "ntstatus" // Windows: Exit-Code ab 0xC0000000 (NTSTATUS, etwa 0xC0000142)
	ErrKindExit     = "exit"     // gewöhnlicher Exit-Code mit Meldung
	ErrKindTimeout  = "timeout"
	ErrKindInternal = "internal" // Fehler im Parser oder in der Bereinigung
	// ErrKindIO: Das Programm lief, aber Ein- oder Ausgabe scheiterte danach
	// (mehr als 2 KB auf stderr bei Exit 0, Rohre nach dem Ende nicht
	// geschlossen: exec.ErrWaitDelay); endgültig (A12 Teil 0 (c)).
	ErrKindIO = "io"
)

// RunError ist ein Fehler des Kindprozesses mit seiner Art (A11 Teil 3:
// vorher ging der Fehlertyp in run verloren).
type RunError struct {
	Kind      string
	Transient bool
	Msg       string
	Err       error
}

func (e *RunError) Error() string { return "extractor failed: " + e.Msg }
func (e *RunError) Unwrap() error { return e.Err }

// classifyRun ordnet den Fehler von cmd.Wait nach gelungenem Start ein
// (A12 Teil 0 (c): vorher galt jeder Fehler ohne *exec.ExitError als
// gescheiterter Start): vorübergehend nur bei Signal oder NTSTATUS (das
// Programm lief nicht zu Ende); ein Fehler ohne Exit-Code nach dem Start
// (errOutputFull auf stderr, exec.ErrWaitDelay) ist endgültig (ErrKindIO).
// Ein gescheiterter Start (cmd.Start) ist ErrKindStart und wird in run
// eingeordnet.
func classifyRun(err error) (kind string, transient bool) {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return ErrKindIO, false
	}
	code := ee.ExitCode()
	switch {
	case code == -1:
		return ErrKindSignal, true // Unix: durch ein Signal beendet (Signaled)
	case uint32(code) >= 0xC0000000:
		return ErrKindNTStatus, true // Windows: „exit status 0xc0000142“
	}
	return ErrKindExit, false
}

// IsTransientReason meldet, ob ein gespeicherter extract_error einen
// vorübergehenden Grund nennt (A11 Teil 3 (b)): beginnt mit „extractor
// failed: exit status 0xc“ oder enthält „fork/exec“, „exec:“ oder
// „signal:“ (dieselben Muster wie store.RequeueTransientFailed).
func IsTransientReason(s string) bool {
	if strings.HasPrefix(strings.ToLower(s), "extractor failed: exit status 0xc") {
		return true
	}
	for _, c := range []string{"fork/exec", "exec:", "signal:"} {
		if strings.Contains(s, c) {
			return true
		}
	}
	return false
}

// ReasonNoPDFTool ist der Grund übersprungener PDFs ohne pdftotext; ist
// das Werkzeug später da, reiht der extract-Job sie wieder ein.
const ReasonNoPDFTool = "pdftotext is not installed"

// Extractor führt Extraktionen aus.
type Extractor struct {
	opts Options
}

// New legt einen Extraktor an.
func New(o Options) *Extractor { return &Extractor{opts: o.withDefaults()} }

// ErrTimeout: Der Kindprozess hat die Zeitgrenze überschritten.
var ErrTimeout = errors.New("extract: timed out")

// PDFTool liefert den Pfad zu pdftotext oder "" (nicht installiert bzw.
// abgeschaltet).
func (x *Extractor) PDFTool() string {
	switch x.opts.PDFToText {
	case "-":
		return ""
	case "":
		p, err := exec.LookPath("pdftotext")
		if err != nil {
			return ""
		}
		return p
	}
	return x.opts.PDFToText
}

// ToolFor nennt das Hilfsprogramm für Anhänge der Art kind (A12 Teil 0
// (c): Warnung eines gescheiterten Auszugs): pdftotext bei PDF, das eigene
// Programm bei DOCX, sonst "" (im Prozess).
func (x *Extractor) ToolFor(kind Kind) string {
	switch kind {
	case KindPDF:
		return x.PDFTool()
	case KindDOCX:
		if len(x.opts.Self) > 0 {
			return x.opts.Self[0]
		}
		exe, _ := os.Executable()
		return exe
	}
	return ""
}

// Extract gewinnt Text aus einem Anhang der Art kind. size ist seine
// Größe (für die Eingabegrenze vorab).
func (x *Extractor) Extract(ctx context.Context, kind Kind, r io.Reader, size int64) Result {
	if kind == KindNone {
		return Result{Status: StatusSkipped, Error: "no text extractor for this type"}
	}
	if size > x.opts.MaxInput {
		return Result{Status: StatusSkipped, Error: fmt.Sprintf("larger than %d bytes", x.opts.MaxInput)}
	}
	in := io.LimitReader(r, x.opts.MaxInput)
	var res Result
	var err error
	switch kind {
	case KindPDF:
		tool := x.PDFTool()
		switch {
		case tool == "":
			return Result{Status: StatusSkipped, Error: ReasonNoPDFTool}
		case x.toolMissing(tool):
			return Result{Status: StatusSkipped, Error: ReasonPDFToolMissing}
		case probeFailed(tool):
			// A12 Teil 3 (b): die Probe ist endgültig gescheitert (falsches
			// Programm); nicht failed, damit eine gelungene Probe nachholt.
			return Result{Status: StatusSkipped, Error: ReasonPDFNoStdin}
		}
		res, err = x.run(ctx, tool, x.opts.PDFArgs, nil, in)
	case KindDOCX:
		self := x.opts.Self
		if len(self) == 0 {
			exe, e := os.Executable()
			if e != nil {
				return Result{Status: StatusFailed, Error: "cannot find own program for the extractor"}
			}
			self = []string{exe}
		}
		res, err = x.run(ctx, self[0], self[1:], []string{childEnv + "=" + string(KindDOCX)}, in)
		if rest, ok := strings.CutPrefix(res.Text, docxHiddenMarker); ok {
			res.Text, res.Hidden = rest, true
		}
	case KindText, KindHTML, KindMail:
		// crash.Do (A02 A5): Ein Panic im Parser oder in der Bereinigung
		// wird bereinigt gemeldet; der Anhang gilt als gescheitert
		// ("internal error in extractor: …", ohne Inhalt), die übrigen
		// werden weiter extrahiert.
		err = crash.Do("extractor", func() error {
			var err error
			res, err = x.inProcess(kind, in)
			return err
		})
	default:
		return Result{Status: StatusSkipped, Error: "no text extractor for this type"}
	}
	if err != nil {
		out := Result{Status: StatusFailed, Error: err.Error(), ErrKind: ErrKindInternal}
		var re *RunError
		switch {
		case errors.As(err, &re):
			out.ErrKind, out.Transient = re.Kind, re.Transient
		case errors.Is(err, ErrTimeout):
			out.ErrKind = ErrKindTimeout
		}
		return out
	}
	res.Status = StatusOK
	res.Text = sanitize.Text(strings.ToValidUTF8(res.Text, "�"))
	if len(res.Text) > x.opts.MaxOutput {
		res.Text = strings.ToValidUTF8(res.Text[:x.opts.MaxOutput], "")
		res.Truncated = true
	}
	return res
}

// childEnv schaltet das eigene Programm in den Extraktor-Modus.
const childEnv = "POSTKAUZ_INTERNAL_EXTRACT"

// baseEnv ist die knappe Umgebung der Kindprozesse: Pfade und
// Temporärverzeichnisse, keine Geheimnisse.
func baseEnv() []string {
	var out []string
	for _, k := range []string{"PATH", "SYSTEMROOT", "WINDIR", "TEMP", "TMP", "TMPDIR", "HOME", "USERPROFILE"} {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return append(out, "LC_ALL=C.UTF-8")
}

// limitedBuffer nimmt höchstens max Bytes; danach scheitert Write (der
// Kindprozess bekommt einen Schreibfehler und endet).
type limitedBuffer struct {
	buf  bytes.Buffer
	max  int
	full bool
}

var errOutputFull = errors.New("output limit reached")

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if rest := l.max - l.buf.Len(); len(p) > rest {
		l.buf.Write(p[:max(rest, 0)])
		l.full = true
		return 0, errOutputFull
	}
	return l.buf.Write(p)
}

// run startet einen Kindprozess mit Zeitgrenze und Ausgabegrenze.
func (x *Extractor) run(ctx context.Context, name string, args, env []string, in io.Reader) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, x.opts.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(append(baseEnv(), fmt.Sprintf("GOMEMLIMIT=%d", x.opts.MemoryLimit)), env...)
	cmd.Env = append(cmd.Env, x.opts.Env...)
	cmd.Stdin = in
	out := &limitedBuffer{max: x.opts.MaxOutput + 1}
	errOut := &limitedBuffer{max: 2048}
	cmd.Stdout, cmd.Stderr = out, errOut
	cmd.WaitDelay = 2 * time.Second
	procattr.Prepare(cmd) // Windows: ohne Konsolenfenster (A12 Teil 3 (e))
	if err := cmd.Start(); err != nil {
		// Nur ein Fehler aus Start heißt: das Programm lief nicht an
		// (*fs.PathError „fork/exec …“ oder „exec: …“); vorübergehend.
		return Result{}, &RunError{Kind: ErrKindStart, Transient: true, Msg: firstLine(err.Error()), Err: err}
	}
	err := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return Result{}, fmt.Errorf("%w after %s (process killed)", ErrTimeout, x.opts.Timeout)
	}
	if ctx.Err() != nil {
		// Abbruch über den Kontext (unter Linux „signal: killed“): kein
		// Fehler des Anhangs; der Aufrufer prüft ctx.Err() und gibt den Job
		// zurück.
		return Result{}, fmt.Errorf("extract: %w", ctx.Err())
	}
	if out.full {
		return Result{Text: out.buf.String(), Truncated: true}, nil
	}
	if err != nil {
		// A11 Teil 3: Art des Fehlers erhalten (vorher %s ohne Typ).
		kind, transient := classifyRun(err)
		msg := firstLine(errOut.buf.String())
		if msg == "" || transient {
			// Lief das Programm nicht, ist err der Grund (etwa „exit status
			// 0xc0000142“, „signal: killed“, „fork/exec …“), nicht stderr.
			msg = firstLine(err.Error())
		}
		return Result{}, &RunError{Kind: kind, Transient: transient, Msg: msg, Err: err}
	}
	return Result{Text: out.buf.String()}, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return strings.ToValidUTF8(s, "")
}

// testPanic löst in Tests einen Panic im Extraktor aus (im Prozess und im
// Kindprozess, childMain); nil im Betrieb.
var testPanic func(kind string)

// inProcess liest Text, HTML und Mails ohne Kindprozess.
func (x *Extractor) inProcess(kind Kind, in io.Reader) (Result, error) {
	if testPanic != nil {
		testPanic(string(kind))
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return Result{}, err
	}
	switch kind {
	case KindText:
		return Result{Text: decodeText(data)}, nil
	case KindHTML:
		r := sanitize.HTML(decodeText(data))
		return Result{Text: r.Text, Hidden: r.Hidden}, nil
	case KindMail:
		m, err := mimeparse.Parse(bytes.NewReader(data), mimeparse.Options{Limits: mimeparse.Limits{
			MaxMessageBytes: x.opts.MaxInput, MaxPartBytes: x.opts.MaxInput, MaxTextBytes: int64(x.opts.MaxOutput)}})
		if err != nil {
			return Result{}, errors.New("unreadable forwarded message")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Subject: %s\n", m.Subject)
		if len(m.From) > 0 {
			fmt.Fprintf(&b, "From: %s <%s>\n", m.From[0].Name, m.From[0].Address)
		}
		if !m.Date.IsZero() {
			fmt.Fprintf(&b, "Date: %s\n", m.Date.Format(time.RFC3339))
		}
		b.WriteString("\n")
		res := Result{}
		if m.HTML != "" {
			h := sanitize.HTML(m.HTML)
			res.Hidden = h.Hidden
			if m.Text == "" {
				b.WriteString(h.Text)
			}
		}
		b.WriteString(m.Text)
		res.Text = b.String()
		return res, nil
	}
	return Result{}, errors.New("unsupported kind")
}

// decodeText: UTF-8, UTF-16 mit BOM, sonst Windows-1252.
func decodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xef, 0xbb, 0xbf}):
		b = b[3:]
	case bytes.HasPrefix(b, []byte{0xff, 0xfe}), bytes.HasPrefix(b, []byte{0xfe, 0xff}):
		dec := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
		if out, err := dec.Bytes(b); err == nil {
			return string(out)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	if out, err := charmap.Windows1252.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return strings.ToValidUTF8(string(b), "�")
}
