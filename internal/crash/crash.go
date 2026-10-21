// Package crash beschreibt einen abgefangenen Panic, ohne Inhalte
// preiszugeben (S1.11, docs/sicherheit.md, Abschnitt 10).
//
// Ein Panic kann einen Wert tragen, in dem Mailtext, ein Suchbegriff oder
// ein Pfad steht (panic(fmt.Sprintf("… %v", nachricht))), und der Stack
// eines Go-Programms nennt die Argumente jeder Funktion. Report gibt deshalb
// nur aus:
//
//   - die Meldung der Go-Laufzeit (runtime.Error, z. B. „index out of
//     range [5] with length 3"), die nie Daten des Programms enthält; jeden
//     anderen Wert nur mit seinem Typ ("panic value withheld (string)");
//   - je Rahmen den Funktionsnamen und Datei:Zeile, ohne Argumente und
//     ohne Programmzähler-Versatz.
//
// Seit S2.0 (A02 A5, Frage 15) fangen Do und Go Panics in eigenen
// Hilfsgoroutinen und Arbeitsschritten (IDLE-Watcher, Sync-Ordner,
// Import-Quellen, Extraktor, Watchdog, Suche) mit demselben Bericht ab:
// Der Bericht geht ins Log (SetLogger), der Aufrufer bekommt einen
// *PanicError (errors.Is(err, ErrInternal)) statt eines Programmabbruchs.
package crash

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
)

// ErrInternal kennzeichnet einen abgefangenen Panic (ops.ErrInternal ist
// derselbe Wert: CLI-Code internal, Audit-Grund panic).
var ErrInternal = errors.New("internal error")

// PanicError ist ein abgefangener Panic in einem Arbeitsschritt (Do, Go).
// Die Meldung enthält nur den Namen des Schritts und Report.Message.
type PanicError struct {
	Name   string
	Report Report
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("internal error in %s: %s", e.Name, e.Report.Message)
}

// Is: errors.Is(err, ErrInternal).
func (e *PanicError) Is(target error) bool { return target == ErrInternal }

var logger atomic.Pointer[slog.Logger]

// SetLogger setzt das Log für Berichte aus Do und Go (die CLI setzt ihr
// Log beim Öffnen des Datenverzeichnisses); ohne Logger bleibt nur der
// Fehler beim Aufrufer.
func SetLogger(l *slog.Logger) { logger.Store(l) }

func currentLogger() *slog.Logger {
	if l := logger.Load(); l != nil {
		return l
	}
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Do führt fn aus und fängt einen Panic ab: Der bereinigte Bericht geht
// ins Log, der Rückgabewert ist dann ein *PanicError. Sonst liefert Do den
// Fehler von fn unverändert.
func Do(name string, fn func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			rep := FromPanic(v, debug.Stack())
			currentLogger().Error("panic in background task", "task", name, "message", rep.Message, "stack", rep.Stack)
			err = &PanicError{Name: name, Report: rep}
		}
	}()
	return fn()
}

// Go startet fn in einer eigenen Goroutine wie Do. Der Kanal liefert genau
// einen Wert (nil, den Fehler von fn oder einen *PanicError), dann nichts
// mehr; er ist gepuffert, die Goroutine endet auch, wenn niemand liest.
func Go(name string, fn func() error) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- Do(name, fn) }()
	return ch
}

// Report ist ein bereinigter Panic.
type Report struct {
	// Message ist die Meldung der Laufzeit oder ein Platzhalter mit dem Typ.
	Message string `json:"message"`
	// Stack: Rahmen als "funktion (datei:zeile)", innerster zuerst.
	Stack []string `json:"stack"`
}

// FromPanic bereinigt einen Panic-Wert und den Stack (debug.Stack()).
func FromPanic(v any, stack []byte) Report {
	return Report{Message: Message(v), Stack: SanitizeStack(stack)}
}

// Message liefert eine Meldung ohne Daten des Programms.
func Message(v any) string {
	if v == nil {
		return "panic with nil value"
	}
	if re, ok := v.(runtime.Error); ok && reflect.TypeOf(v).PkgPath() == "runtime" ||
		ok && isRuntimePointer(v) {
		return clip(re.Error(), 300)
	}
	return fmt.Sprintf("panic value withheld (%s)", reflect.TypeOf(v).String())
}

// isRuntimePointer erkennt Laufzeitfehler, die als Zeiger auf einen Typ des
// Pakets runtime ankommen (z. B. *runtime.TypeAssertionError).
func isRuntimePointer(v any) bool {
	t := reflect.TypeOf(v)
	return t.Kind() == reflect.Pointer && t.Elem().PkgPath() == "runtime"
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var (
	// Funktionszeile: Name, dann die Argumentliste in Klammern am Ende.
	funcLine = regexp.MustCompile(`^(\S.*?)\((?:[^()]|\([^()]*\))*\)$`)
	// Dateizeile: Tab, Pfad:Zeile, optional " +0x…".
	fileLine = regexp.MustCompile(`^\t(.+?):(\d+)(?: \+0x[0-9a-f]+)?$`)
)

// SanitizeStack macht aus der Ausgabe von debug.Stack() eine Liste von
// Rahmen ohne Argumente. Rahmen von runtime/debug und runtime/panic.go
// fallen weg; „created by"-Zeilen bleiben (ohne Goroutinen-ID).
func SanitizeStack(stack []byte) []string {
	var out []string
	lines := strings.Split(strings.ReplaceAll(string(stack), "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(l, "goroutine ") || strings.TrimSpace(l) == "" {
			continue
		}
		var fn string
		switch {
		case strings.HasPrefix(l, "created by "):
			fn = strings.TrimPrefix(l, "created by ")
			if j := strings.Index(fn, " in goroutine "); j >= 0 {
				fn = fn[:j]
			}
			fn = "created by " + fn
		default:
			m := funcLine.FindStringSubmatch(l)
			if m == nil {
				continue // unbekannte Zeile: lieber weglassen
			}
			fn = m[1]
		}
		where := ""
		if i+1 < len(lines) {
			if m := fileLine.FindStringSubmatch(lines[i+1]); m != nil {
				where = shortPath(m[1]) + ":" + m[2]
				i++
			}
		}
		if skipFrame(fn, where) {
			continue
		}
		if where != "" {
			fn += " (" + where + ")"
		}
		out = append(out, fn)
	}
	return out
}

func skipFrame(fn, where string) bool {
	return strings.HasPrefix(fn, "runtime/debug.") || strings.HasPrefix(fn, "panic") ||
		strings.HasPrefix(where, "runtime/panic.go")
}

// shortPath kürzt einen Pfad auf das Modul bzw. die Standardbibliothek
// (ohne -trimpath stünden dort Verzeichnisse des Build-Rechners).
func shortPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	for _, marker := range []string{"/src/", "/pkg/mod/"} {
		if i := strings.LastIndex(p, marker); i >= 0 {
			return p[i+len(marker):]
		}
	}
	if i := strings.Index(p, "/internal/"); i >= 0 {
		return p[i+1:]
	}
	if i := strings.Index(p, "/cmd/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// String gibt den Bericht mehrzeilig aus (stderr, Log).
func (r Report) String() string {
	var b strings.Builder
	b.WriteString(r.Message)
	for _, f := range r.Stack {
		b.WriteString("\n    ")
		b.WriteString(f)
	}
	return b.String()
}
