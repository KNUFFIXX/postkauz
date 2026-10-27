// Package cli enthält die Kommandozeilen-Adapter. Jede Operation wird über
// eine ausdrückliche Bindung (bindings.go) als Kommando bereitgestellt; die
// Fachlogik, Scope-Prüfung, Policy und Audit liegen im Operationsregister.
package cli

import (
	"errors"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
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

// internalHint steht bei jedem abgefangenen Panic (Code internal).
const internalHint = "this is a bug in postkauz; the log (see doctor) has a cleaned stack trace without message content; please report it"

// classify bildet einen Fehler auf Exit-Code und ErrorInfo ab.
func classify(err error) (int, ErrorInfo) {
	var ue usageError
	var ie *ops.InputError
	var de *ops.DeniedError
	var pe *ops.PolicyError
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
	case errors.Is(err, store.ErrMailboxNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "mailbox not found", Hint: "list mailbox ids with: mailbox list -a <account> --json"}
	case errors.Is(err, store.ErrAttachmentNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "attachment not found", Hint: "list attachment ids with: attach list <message-id>"}
	case errors.Is(err, store.ErrDraftNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "draft not found", Hint: "list drafts with: draft list"}
	case errors.Is(err, store.ErrApprovalNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "approval not found", Hint: "the approval id is shown by draft request and draft list"}
	case errors.Is(err, store.ErrIdentityNotFound):
		return ExitError, ErrorInfo{Code: "not_found", Message: "identity not found", Hint: "list identities with: identity list"}
	case errors.Is(err, store.ErrKeyExists):
		return ExitError, ErrorInfo{Code: "exists", Message: err.Error(), Hint: "key names stay reserved, also after revoke; choose another name"}
	case errors.Is(err, store.ErrKeyNotFound):
		// Ohne den Namen (S2.7: keine Eingabewerte in Fehlermeldungen).
		return ExitError, ErrorInfo{Code: "not_found", Message: "api key not found", Hint: "list keys with: key list"}
	case errors.Is(err, ops.ErrKeyMissing), errors.Is(err, ops.ErrKeyInvalid), errors.Is(err, ops.ErrKeyRevoked),
		errors.Is(err, ops.ErrKeyExpired):
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(), Hint: "create a key with: key create --name <name> --role reader"}
	case errors.Is(err, errAuditMismatch), errors.Is(err, ops.ErrAuditExportChanged):
		return ExitError, ErrorInfo{Code: "mismatch", Message: err.Error(), Hint: "the audit log or its export was changed; see audit verify"}
	case errors.Is(err, policy.ErrInvalidFile):
		return ExitError, ErrorInfo{Code: "policy_invalid", Message: err.Error(), Hint: "fix the policy file (see docs/sicherheit.md, section 5) or check it with: doctor"}
	case errors.Is(err, store.ErrSchemaTooNew):
		return ExitError, ErrorInfo{Code: "schema_too_new", Message: err.Error()}
	case errors.Is(err, store.ErrMigrationBlocked):
		return ExitError, ErrorInfo{Code: "migration_blocked", Message: err.Error(),
			Hint: "stop the running sync --watch task or import of this data directory, then run the command again"}
	case errors.Is(err, store.ErrMigrationMismatch):
		return ExitError, ErrorInfo{Code: "schema_mismatch", Message: err.Error(),
			Hint: "this database belongs to another build of the program; use that build or restore the snapshot taken before its migration"}
	case errors.Is(err, secrets.ErrUnavailable):
		return ExitError, ErrorInfo{Code: "secret_store_unavailable", Message: err.Error(), Hint: "run doctor"}
	case errors.Is(err, ops.ErrNoSecret):
		return ExitError, ErrorInfo{Code: "no_secret", Message: err.Error(), Hint: "add the account again or set the environment variable"}
	case errors.Is(err, provider.ErrAuth):
		return ExitError, ErrorInfo{Code: "auth", Message: err.Error(), Hint: "check user name and password; the server rejected the login (no automatic retry)"}
	case errors.Is(err, provider.ErrConnection):
		return ExitConnection, ErrorInfo{Code: "connection", Message: err.Error(), Hint: "check host, port, TLS mode and network"}
	case errors.Is(err, ops.ErrSyncRunning):
		return ExitError, ErrorInfo{Code: "sync_running", Message: err.Error(), Hint: "wait for the other sync or import of this account (e.g. sync --watch) to finish or stop it with Ctrl+C, then try again; a running sync --watch also puts queued drafts into the Drafts folder"}
	case errors.As(err, &de):
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(), Hint: deniedHint(de.Reason)}
	case errors.As(err, &pe) && pe.Decision.Reason == "human_only":
		// A02 E2 (D4.5): nur der Mensch ohne --key.
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(),
			Hint: "run it without --key; no API key may do this, whatever its role"}
	case errors.As(err, &pe) && pe.Decision.Reason == "missing_scope":
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(),
			Hint: "the key lacks the required scope; see what it may do with: policy show --key <name>"}
	case errors.Is(err, ops.ErrDenied):
		return ExitDenied, ErrorInfo{Code: "denied", Message: err.Error(), Hint: "the key lacks the required scope or the policy forbids this"}
	case errors.Is(err, ops.ErrNeedsApproval):
		return ExitNeedsApproval, ErrorInfo{Code: "needs_approval", Message: err.Error(), Hint: "a human must approve this action"}
	case errors.Is(err, ops.ErrInternal):
		return ExitError, ErrorInfo{Code: "internal", Message: err.Error(), Hint: internalHint}
	case errors.Is(err, ops.ErrUnknownOperation):
		return ExitError, ErrorInfo{Code: "unknown_operation", Message: err.Error()}
	default:
		return ExitError, ErrorInfo{Code: "error", Message: err.Error()}
	}
}

// errLogInfo liefert, was von einem Fehler ins Log darf: der Code wie in
// --json (classify) und der Grund einer Ablehnung (Policy oder fachlich),
// nie die Meldung selbst (sie kann Eingaben des Aufrufers enthalten).
func errLogInfo(err error) (code, reason string) {
	_, info := classify(err)
	var de *ops.DeniedError
	var pe *ops.PolicyError
	switch {
	case errors.As(err, &de):
		reason = de.Reason
	case errors.As(err, &pe):
		reason = pe.Decision.Reason
	}
	return info.Code, reason
}

// deniedHint erklärt eine fachliche Ablehnung (ops.DeniedError) mit dem
// nächsten Schritt; ohne eigenen Text der allgemeine.
func deniedHint(reason string) string {
	if h, ok := deniedHints[reason]; ok {
		return h
	}
	return "nothing was changed; see the reason in the message"
}

var deniedHints = map[string]string{
	"draft_closed":         "the draft is sent or abandoned; create a new one",
	"draft_taken_over":     "the draft was changed in the mail client; change it there",
	"approval_stale":       "the draft changed after it was submitted; submit it again with: draft request <id>",
	"approval_expired":     "the approval ran out; submit the draft again with: draft request <id>",
	"approval_decided":     "this approval was already decided; see: approval show <id>",
	"invalid_transition":   "not possible in the current status; see: draft show <id>",
	"attach_outside_dir":   "attach files only from attach_dir (see: doctor)",
	"attach_not_found":     "the file is not in attach_dir (see: doctor)",
	"hidden_text_in_draft": "the HTML contains text a reader would not see; remove it",
	"too_large":            "shorten the field named in the message (limits: docs/cli.md)",
	"draft_changed":        "the draft was changed by someone else meanwhile; check it with: draft show <id>, then try again",
	"no_recipients":        "the draft has no recipient; add one with: draft edit <id> --to <address>, then request again",
}
