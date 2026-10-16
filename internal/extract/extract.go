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

	"github.com/KNUFFIXX/postkauz/internal/magic"
	"github.com/KNUFFIXX/postkauz/internal/mimeparse"
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
	// Hidden: Der Anhang (HTML, Mail) enthielt versteckten Text, der
	// entfernt wurde.
	Hidden bool
}

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
		if tool == "" {
			return Result{Status: StatusSkipped, Error: "pdftotext is not installed"}
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
	case KindText, KindHTML, KindMail:
		res, err = x.inProcess(kind, in)
	default:
		return Result{Status: StatusSkipped, Error: "no text extractor for this type"}
	}
	if err != nil {
		return Result{Status: StatusFailed, Error: err.Error()}
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
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Result{}, fmt.Errorf("%w after %s (process killed)", ErrTimeout, x.opts.Timeout)
	}
	if out.full {
		return Result{Text: out.buf.String(), Truncated: true}, nil
	}
	if err != nil {
		msg := firstLine(errOut.buf.String())
		if msg == "" {
			msg = err.Error()
		}
		return Result{}, fmt.Errorf("extractor failed: %s", msg)
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

// inProcess liest Text, HTML und Mails ohne Kindprozess.
func (x *Extractor) inProcess(kind Kind, in io.Reader) (Result, error) {
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
