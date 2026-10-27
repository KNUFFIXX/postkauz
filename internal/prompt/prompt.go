// Package prompt liest ein Geheimnis verdeckt vom Terminal (Passwort bei
// `account add` und `account set --password-stdin`, ab S3.7 auch die
// Anzeige bei `account login`). A03 A3: Unter Windows nahm
// golang.org/x/term.ReadPassword im Windows Terminal mit Windows
// PowerShell 5.1 weder Tippen noch Einfügen an (Befund D5, Ursache offen).
//
// Deshalb liest Windows über ReadConsoleW selbst (console.go): Echo aus,
// Zeilen-Eingabe EIN (die Konsole übernimmt Zeilenbearbeitung und
// Einfügen, Enter schließt ab), Steuerung durch Strg+C bleibt, virtuelle
// Terminal-Eingabe (Escape-Folgen) aus; UTF-16 wird zu UTF-8 (Umlaute
// unabhängig von der Codepage). Andere Systeme bleiben bei x/term. Jeder
// Leser bricht nach einer Zeitgrenze ohne Eingabe ab (Standard 120 s) und
// stellt den Terminal-Modus wieder her.
//
// Ein Report beschreibt das Lesen ohne Geheimnis (Zahl der Bytes und
// Zeichen, SHA-256, Konsolenmodus nur als Flags); ihn zeigt das versteckte
// Diagnose-Kommando `postkauz internal prompt-probe`.
package prompt

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

// DefaultTimeout ist die Zeitgrenze ohne Eingabe (A3 Punkt 4).
const DefaultTimeout = 120 * time.Second

// MaxLen begrenzt die Eingabe (Zeichen bzw. UTF-16-Einheiten); länger ist
// kein Passwort, sondern ein Versehen (etwa ein eingefügter Text).
const MaxLen = 4096

// Fehler, die Aufrufer unterscheiden.
var (
	ErrNotTerminal = errors.New("standard input is not a terminal")
	ErrTimeout     = errors.New("no input")
	ErrTooLong     = errors.New("input too long")
)

// Leser (Options.Reader).
const (
	ReaderAuto    = ""        // Windows: console, sonst term
	ReaderConsole = "console" // ReadConsoleW (nur Windows)
	ReaderTerm    = "term"    // golang.org/x/term.ReadPassword
)

// EnvReader wählt den Leser für account add/set, falls der neue Leser auf
// einem Rechner versagt (Rückweg ohne neues Programm): "term" nimmt
// x/term wie bis 0.2.0-rc8.
const EnvReader = "POSTKAUZ_PASSWORD_READER"

// Options steuert das Lesen.
type Options struct {
	Reader  string        // ReaderAuto, ReaderConsole, ReaderTerm
	Timeout time.Duration // 0 = DefaultTimeout
}

// Report beschreibt einen Lesevorgang, nie den Wert.
type Report struct {
	Reader     string `json:"reader"`
	IsTerminal bool   `json:"is_terminal"`
	// Konsolenmodus (nur Windows): vorher, während und nach dem Lesen,
	// als Flag-Namen (ModeNames).
	ModeBefore string `json:"mode_before,omitempty"`
	ModeDuring string `json:"mode_during,omitempty"`
	ModeAfter  string `json:"mode_after,omitempty"`
	Reads      int    `json:"reads"`                 // Aufrufe des Lesers (ReadConsoleW bzw. 1)
	Units      int    `json:"utf16_units,omitempty"` // gelesene UTF-16-Einheiten (console)
	Bytes      int    `json:"bytes"`                 // Länge des Ergebnisses in UTF-8-Bytes
	Runes      int    `json:"chars"`                 // Zeichen
	NonASCII   int    `json:"non_ascii"`             // Zeichen über U+007F
	Invalid    int    `json:"invalid"`               // ungültiges UTF-8 bzw. einzelne Surrogate
	Escapes    int    `json:"escapes"`               // ESC-Zeichen (Hypothese e: VT-Eingabe)
	Controls   int    `json:"controls"`              // übrige Steuerzeichen außer Tab
	Terminator string `json:"terminator"`            // crlf, cr, lf, eof, none
	SHA256     string `json:"sha256"`                // des Ergebnisses
	Duration   string `json:"duration"`
}

// Env beschreibt stdin und die Konsole (prompt-probe, Hypothesen a und c
// aus A3): Art des Handles, Konsolenmodus, Codepages, Windows Terminal.
type Env struct {
	OS              string `json:"os"`
	StdinType       string `json:"stdin_type"` // char, pipe, disk, unknown
	ConsoleMode     string `json:"console_mode,omitempty"`
	InputCodePage   uint32 `json:"input_code_page,omitempty"`
	OutputCodePage  uint32 `json:"output_code_page,omitempty"`
	ANSICodePage    uint32 `json:"ansi_code_page,omitempty"`
	WindowsTerminal bool   `json:"windows_terminal"` // WT_SESSION gesetzt (Wert nie)
}

// Environment liefert Env für in.
func Environment(in *os.File) Env { return platformEnv(in) }

// Password schreibt label auf out, liest verdeckt eine Zeile von in und
// liefert sie ohne Zeilenende. ctx bricht ab (Strg+C); nach o.Timeout ohne
// abgeschlossene Eingabe kommt ErrTimeout. Der Terminal-Modus wird in
// jedem Fall wiederhergestellt.
func Password(ctx context.Context, in *os.File, out interface{ Write([]byte) (int, error) }, label string, o Options) ([]byte, Report, error) {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Reader == ReaderAuto {
		o.Reader = os.Getenv(EnvReader)
	}
	if o.Reader == ReaderAuto {
		o.Reader = defaultReader
	}
	rep := Report{Reader: o.Reader, IsTerminal: isTerminal(in)}
	if !rep.IsTerminal {
		return nil, rep, ErrNotTerminal
	}
	if label != "" {
		_, _ = out.Write([]byte(label))
	}
	start := time.Now()
	var b []byte
	var err error
	switch o.Reader {
	case ReaderConsole:
		b, err = readConsole(ctx, in, o.Timeout, &rep)
	case ReaderTerm:
		b, err = readTerm(ctx, in, o.Timeout, &rep)
	default:
		return nil, rep, fmt.Errorf("unknown password reader %q (use console or term)", o.Reader)
	}
	_, _ = out.Write([]byte("\n"))
	rep.Duration = time.Since(start).Round(time.Millisecond).String()
	describe(b, &rep)
	return b, rep, err
}

// describe füllt die Zähler des Reports aus dem Ergebnis.
func describe(b []byte, rep *Report) {
	sum := sha256.Sum256(b)
	rep.SHA256 = hex.EncodeToString(sum[:])
	rep.Bytes = len(b)
	rep.Runes, rep.NonASCII, rep.Escapes, rep.Controls = 0, 0, 0, 0
	invalid := 0
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		i += n
		rep.Runes++
		switch {
		case r == utf8.RuneError && n == 1:
			invalid++
		case r == 0x1b:
			rep.Escapes++
		case r < 0x20 && r != '\t', r == 0x7f:
			rep.Controls++
		case r > 0x7f:
			rep.NonASCII++
		}
	}
	rep.Invalid += invalid
}

// Konsolenmodus (Werte der Windows-API, hier ohne golang.org/x/sys/windows,
// damit Leser und Test auf jedem System laufen).
const (
	modeProcessedInput = 0x1
	modeLineInput      = 0x2
	modeEchoInput      = 0x4
	modeWindowInput    = 0x8
	modeMouseInput     = 0x10
	modeInsertMode     = 0x20
	modeQuickEditMode  = 0x40
	modeExtendedFlags  = 0x80
	modeAutoPosition   = 0x100
	modeVTInput        = 0x200
)

var modeNames = []struct {
	bit  uint32
	name string
}{
	{modeProcessedInput, "PROCESSED_INPUT"}, {modeLineInput, "LINE_INPUT"}, {modeEchoInput, "ECHO_INPUT"},
	{modeWindowInput, "WINDOW_INPUT"}, {modeMouseInput, "MOUSE_INPUT"}, {modeInsertMode, "INSERT_MODE"},
	{modeQuickEditMode, "QUICK_EDIT_MODE"}, {modeExtendedFlags, "EXTENDED_FLAGS"}, {modeAutoPosition, "AUTO_POSITION"},
	{modeVTInput, "VIRTUAL_TERMINAL_INPUT"},
}

// ModeNames schreibt einen Konsolen-Eingabemodus als Flag-Namen (mit dem
// Zahlenwert, unbekannte Bits als Hex).
func ModeNames(m uint32) string {
	var parts []string
	rest := m
	for _, f := range modeNames {
		if m&f.bit != 0 {
			parts = append(parts, f.name)
			rest &^= f.bit
		}
	}
	if rest != 0 {
		parts = append(parts, fmt.Sprintf("0x%x", rest))
	}
	if len(parts) == 0 {
		parts = []string{"none"}
	}
	return fmt.Sprintf("0x%04x %s", m, strings.Join(parts, "|"))
}

// passwordMode ist der Modus während des Lesens: Echo aus, Zeilen-Eingabe
// und Strg+C ein, VT-Eingabe aus; die übrigen Bits (Einfügemodus,
// Schnellbearbeitung) bleiben, wie der Nutzer sie eingestellt hat.
func passwordMode(m uint32) uint32 {
	return (m &^ (modeEchoInput | modeVTInput)) | modeLineInput | modeProcessedInput
}
