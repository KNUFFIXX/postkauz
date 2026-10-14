// Package cli enthält die Kommandozeilen-Adapter. Jede Operation wird über
// eine ausdrückliche Bindung (bindings.go) als Kommando bereitgestellt; die
// Fachlogik, Scope-Prüfung, Policy und Audit liegen im Operationsregister.
package cli

import (
	"errors"

	"github.com/KNUFFIXX/mailwerk/internal/ops"
)

// Exit-Codes (docs/cli.md). Skripte und Agenten dürfen sich darauf verlassen.
const (
	ExitOK            = 0
	ExitError         = 1 // allgemeiner Fehler
	ExitUsage         = 2 // Eingabefehler (unbekanntes Kommando, falsche Flags)
	ExitDenied        = 3 // Policy oder Scope verweigert
	ExitNeedsApproval = 4 // Freigabe nötig
	ExitConnection    = 5 // Verbindung zum Server fehlgeschlagen
)

// usageError markiert Eingabefehler.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// ErrorInfo ist die maschinenlesbare Fehlerdarstellung (--json, stderr).
type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// classify bildet einen Fehler auf Exit-Code und ErrorInfo ab.
func classify(err error) (int, ErrorInfo) {
	var ue usageError
	switch {
	case errors.As(err, &ue):
		return ExitUsage, ErrorInfo{Code: "usage", Message: err.Error(), Hint: "see --help"}
	case errors.Is(err, ops.ErrDenied):
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(), Hint: "the key lacks the required scope or the policy forbids this"}
	case errors.Is(err, ops.ErrNeedsApproval):
		return ExitNeedsApproval, ErrorInfo{Code: "needs_approval", Message: err.Error(), Hint: "a human must approve this action"}
	case errors.Is(err, ops.ErrUnknownOperation):
		return ExitError, ErrorInfo{Code: "unknown_operation", Message: err.Error()}
	default:
		return ExitError, ErrorInfo{Code: "error", Message: err.Error()}
	}
}
