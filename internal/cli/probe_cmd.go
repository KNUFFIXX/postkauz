package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KNUFFIXX/postkauz/internal/prompt"
)

// internalCmd sammelt Diagnose-Kommandos: nicht in der Hilfe, nicht im
// Register (keine Operation, kein Audit, kein MCP-Tool), öffnen weder
// Datenverzeichnis noch Schlüsselbund.
func internalCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{Use: "internal", Short: "Diagnostic commands (not part of the interface)", Hidden: true, Args: cobra.NoArgs}
	cmd.AddCommand(promptProbeCmd(a))
	return cmd
}

// probeNote steht in Aufforderung und Ausgabe von prompt-probe (W2).
const probeNote = "test values only: never enter a real password here"

// PromptProbe ist die Ausgabe von internal prompt-probe. HashMatch gibt
// es nur mit --expect-sha256 und nur bei gelesener Eingabe; einen Hash
// der Eingabe gibt das Kommando nie aus (W2: ungesalzen ließe er sich
// bei einem echten Passwort zurückrechnen).
type PromptProbe struct {
	Env       prompt.Env    `json:"env"`
	Report    prompt.Report `json:"report"`
	HashMatch *bool         `json:"hash_match,omitempty"`
	Note      string        `json:"note"`
	Error     string        `json:"error,omitempty"`
}

// promptProbeCmd ist die Diagnose der verdeckten Passwortabfrage (A3,
// Befund D5): Sie liest wie account add eine Zeile verdeckt und zeigt
// nur, wie gelesen wurde (Art von stdin, Konsolenmodus vorher, während
// und nachher, Codepages, Bytes, Zeichen, Zeilenende), nie den Wert und
// nie seinen Hash. Das Abnahme-Skript gibt den SHA-256 eines bekannten
// Testwerts mit --expect-sha256 vor und bekommt nur hash_match zurück
// (docs/abnahme_A3.ps1).
func promptProbeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "prompt-probe",
		Short:  "Read one hidden line like account add and describe how it arrived, never the value",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reader, _ := cmd.Flags().GetString("reader")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			asJSON, _ := cmd.Flags().GetBool("json")
			expect, _ := cmd.Flags().GetString("expect-sha256")
			if reader == "auto" {
				reader = prompt.ReaderAuto
			}
			if reader != prompt.ReaderAuto && reader != prompt.ReaderConsole && reader != prompt.ReaderTerm {
				return usageError{errors.New("--reader must be auto, console or term")}
			}
			if timeout <= 0 || timeout > 10*time.Minute {
				return usageError{errors.New("--timeout must be between 1s and 10m")}
			}
			if expect != "" && !isSHA256Hex(expect) {
				return usageError{errors.New("--expect-sha256 must be 64 hex digits")}
			}
			p := PromptProbe{Note: probeNote}
			f, ok := a.stdin.(*os.File)
			if !ok {
				return usageError{errNotTerminal}
			}
			p.Env = prompt.Environment(f)
			b, rep, err := prompt.Password(cmd.Context(), f, a.stderr, "Type or paste the test value (never a real password), then press Enter: ",
				prompt.Options{Reader: reader, Timeout: timeout})
			if expect != "" && err == nil {
				m := prompt.MatchesSHA256(b, expect)
				p.HashMatch = &m
			}
			clear(b)
			p.Report = rep
			if err != nil {
				p.Error = err.Error()
			}
			if err := writePromptProbe(a, p, asJSON); err != nil {
				return err
			}
			switch {
			case errors.Is(err, prompt.ErrTimeout):
				// Eigene Meldung: prompt_timeout nennt die Frist von account add.
				return fmt.Errorf("prompt probe: no input within %s", timeout)
			case err != nil:
				return fmt.Errorf("prompt probe: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().String("reader", "auto", "auto (Windows: console, otherwise term), console (ReadConsoleW) or term (golang.org/x/term, as up to 0.2.0-rc8)")
	cmd.Flags().Duration("timeout", prompt.DefaultTimeout, "give up after this long without input")
	cmd.Flags().String("expect-sha256", "", "SHA-256 (hex) of the known test value; the output says only whether it matches")
	return cmd
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// writePromptProbe gibt das Ergebnis als JSON oder Text aus.
func writePromptProbe(a *app, p PromptProbe, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(a.stdout).Encode(p)
	}
	printPromptProbe(a, p)
	return nil
}

func printPromptProbe(a *app, p PromptProbe) {
	w := a.stdout
	e, r := p.Env, p.Report
	yn := map[bool]string{true: "yes", false: "no"}
	fmt.Fprintf(w, "os:            %s\n", e.OS)
	fmt.Fprintf(w, "stdin:         %s (terminal: %s)\n", e.StdinType, yn[r.IsTerminal])
	if e.OS == "windows" {
		fmt.Fprintf(w, "console:       %s\n", e.ConsoleMode)
		fmt.Fprintf(w, "code pages:    input %d, output %d, ansi %d\n", e.InputCodePage, e.OutputCodePage, e.ANSICodePage)
		fmt.Fprintf(w, "terminal app:  windows terminal %s\n", yn[e.WindowsTerminal])
	}
	fmt.Fprintf(w, "reader:        %s\n", r.Reader)
	if r.ModeBefore != "" {
		fmt.Fprintf(w, "mode before:   %s\n", r.ModeBefore)
	}
	if r.ModeDuring != "" {
		fmt.Fprintf(w, "mode during:   %s\n", r.ModeDuring)
	}
	if r.ModeAfter != "" {
		fmt.Fprintf(w, "mode after:    %s\n", r.ModeAfter)
	}
	fmt.Fprintf(w, "reads:         %d (utf-16 units %d)\n", r.Reads, r.Units)
	fmt.Fprintf(w, "read:          %d bytes, %d chars (non-ascii %d, invalid %d, escape %d, other control %d)\n",
		r.Bytes, r.Runes, r.NonASCII, r.Invalid, r.Escapes, r.Controls)
	fmt.Fprintf(w, "line end:      %s\n", r.Terminator)
	switch {
	case p.HashMatch != nil:
		fmt.Fprintf(w, "hash match:    %s\n", yn[*p.HashMatch])
	default:
		fmt.Fprintln(w, "hash match:    not checked (no --expect-sha256 or no input)")
	}
	fmt.Fprintf(w, "duration:      %s\n", r.Duration)
	if p.Error != "" {
		fmt.Fprintf(w, "error:         %s\n", termLine(p.Error))
	} else {
		fmt.Fprintln(w, "error:         none")
	}
	fmt.Fprintf(w, "note:          %s\n", probeNote)
}
