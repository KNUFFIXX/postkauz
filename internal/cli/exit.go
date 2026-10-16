// Package cli enthält die Kommandozeilen-Adapter. Jede Operation wird über
// eine ausdrückliche Bindung (bindings.go) als Kommando bereitgestellt; die
// Fachlogik, Scope-Prüfung, Policy und Audit liegen im Operationsregister.
package cli

import (
	"errors"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
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
	var ie *ops.InputError
	switch {
	case errors.As(err, &ue):
		return ExitUsage, ErrorInfo{Code: "usage", Message: err.Error(), Hint: "see --help"}
	case errors.As(err, &ie):
		return ExitUsage, ErrorInfo{Code: "invalid_input", Message: err.Error(), Hint: "see --help"}
	case errors.Is(err, store.ErrAccountExists):
		return ExitError, ErrorInfo{Code: "exists", Message: err.Error(), Hint: "choose another name or remove the account first"}
	case errors.Is(err, store.ErrAccountNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: err.Error(), Hint: "list accounts with: accounts list"}
	case errors.Is(err, store.ErrMessageNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "message not found", Hint: "the id is the local id shown by sync/search, or give the Message-ID"}
	case errors.Is(err, store.ErrThreadNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "thread not found", Hint: "the thread id is shown by show <message-id>; or use thread --message <message-id>"}
	case errors.Is(err, store.ErrAttachmentNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "attachment not found", Hint: "list attachment ids with: attach list <message-id>"}
	case errors.Is(err, store.ErrSchemaTooNew):
		return ExitError, ErrorInfo{Code: "schema_too_new", Message: err.Error()}
	case errors.Is(err, secrets.ErrUnavailable):
		return ExitError, ErrorInfo{Code: "secret_store_unavailable", Message: err.Error(), Hint: "run doctor"}
	case errors.Is(err, ops.ErrNoSecret):
		return ExitError, ErrorInfo{Code: "no_secret", Message: err.Error(), Hint: "add the account again or set the environment variable"}
	case errors.Is(err, provider.ErrAuth):
		return ExitError, ErrorInfo{Code: "auth", Message: err.Error(), Hint: "check user name and password; the server rejected the login (no automatic retry)"}
	case errors.Is(err, provider.ErrConnection):
		return ExitConnection, ErrorInfo{Code: "connection", Message: err.Error(), Hint: "check host, port, TLS mode and network"}
	case errors.Is(err, ops.ErrSyncRunning):
		return ExitError, ErrorInfo{Code: "sync_running", Message: err.Error(), Hint: "wait for the other sync (e.g. sync --watch) to finish or stop it with Ctrl+C, then try again"}
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
