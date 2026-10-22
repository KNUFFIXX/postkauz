// Package drafts ist der Dienst für Entwürfe (Schnitt 2, A02 Schritt S2.1):
// Zustandsmaschine der Freigabe (docs/sicherheit.md, Abschnitt 5; A02 E7),
// preview_hash über die kanonische Form eines Entwurfs, Grenzen der
// Eingaben, Message-ID beim Anlegen (E11), Identitäten und eigene Adressen
// (E9) und die Speicherfunktionen in kurzen Transaktionen (BEGIN
// IMMEDIATE, store.DraftTx).
//
// Der Dienst sendet nie und berührt keinen Server: Den Nachrichtenbau macht
// internal/compose (S2.2), die Ablage per APPEND der Job draft_push (S2.5),
// den Prüf-Hook internal/hook (S2.4). Operationen im Register kommen mit
// S2.3; bis dahin ruft niemand außer Tests diesen Dienst.
//
// Entwurfsinhalt ist Daten: Betreff, Adressen und Text erscheinen nie in
// Fehlermeldungen, Audit oder Log; Fehler nennen nur IDs, Feldnamen,
// Zähler und Gründe.
package drafts

import (
	"errors"
	"fmt"
	"time"
)

// Status ist der Zustand eines Entwurfs (Spalte drafts.status).
type Status string

// Zustände (docs/sicherheit.md, Abschnitt 5).
const (
	StatusDraft           Status = "draft"
	StatusPendingApproval Status = "pending_approval"
	StatusReady           Status = "ready"
	StatusSentByHuman     Status = "sent_by_human"
	StatusAbandoned       Status = "abandoned"
)

// Statuses sind alle Zustände in der Reihenfolge des Lebenslaufs.
var Statuses = []Status{StatusDraft, StatusPendingApproval, StatusReady, StatusSentByHuman, StatusAbandoned}

// Closed meldet, ob der Entwurf abgeschlossen ist (jede Änderung: draft_closed).
func (s Status) Closed() bool { return s == StatusSentByHuman || s == StatusAbandoned }

// Kind ist die Art eines Entwurfs.
type Kind string

// Arten (Spalte drafts.kind).
const (
	KindNew      Kind = "new"
	KindReply    Kind = "reply"
	KindReplyAll Kind = "reply_all"
	KindForward  Kind = "forward"
)

// ValidKind meldet, ob k eine bekannte Art ist.
func ValidKind(k Kind) bool {
	switch k {
	case KindNew, KindReply, KindReplyAll, KindForward:
		return true
	}
	return false
}

// Entscheidungen einer Freigabe (Spalte approvals.decision).
const (
	DecisionPending    = "pending"
	DecisionApproved   = "approved"
	DecisionRejected   = "rejected"
	DecisionSuperseded = "superseded"
	DecisionExpired    = "expired"
)

// Stand der Serverkopie (Spalte drafts.remote_state; E6, S2.5).
const (
	RemotePending   = "pending"
	RemotePushed    = "pushed"
	RemoteTakenOver = "taken_over"
	RemoteMissing   = "missing"
)

// Ergebnisse des Prüf-Hooks (Spalte drafts.hook_result; E4, S2.4).
const (
	HookPass    = "pass"
	HookFail    = "fail"
	HookTimeout = "timeout"
	HookError   = "error"
)

// DefaultApprovalTTL ist der Standard von [draft] approval_ttl (E7): So
// lange gilt eine offene oder erteilte Freigabe.
const DefaultApprovalTTL = 168 * time.Hour

// Gründe einer Ablehnung (DeniedError.Reason). Sie erscheinen im Audit und
// in Ausgaben und enthalten nie Inhalt.
const (
	ReasonDraftClosed       = "draft_closed"
	ReasonInvalidTransition = "invalid_transition"
	ReasonApprovalStale     = "approval_stale"
	ReasonApprovalExpired   = "approval_expired"
	ReasonApprovalDecided   = "approval_decided"
	ReasonDraftTakenOver    = "draft_taken_over"
	ReasonTooLarge          = "too_large"
	ReasonTooManyRecipients = "too_many_recipients"
	ReasonInvalidAddress    = "invalid_address"
	ReasonInvalidHeader     = "invalid_header"
	ReasonIdentityMismatch  = "identity_mismatch"
)

// ErrDenied ist die Wurzel aller Ablehnungen (errors.Is). Die Oberflächen
// bilden sie ab S2.3 auf „denied" (CLI Exit 3) ab.
var ErrDenied = errors.New("drafts: denied")

// DeniedError ist eine fachliche Ablehnung mit maschinenlesbarem Grund.
// Msg ist englisch und nennt nie Inhalt des Entwurfs.
type DeniedError struct {
	Reason string
	Msg    string
}

func (e *DeniedError) Error() string {
	if e.Msg == "" {
		return "denied (" + e.Reason + ")"
	}
	return fmt.Sprintf("denied (%s): %s", e.Reason, e.Msg)
}

// Is macht jede Ablehnung zu ErrDenied.
func (e *DeniedError) Is(target error) bool { return target == ErrDenied }

func denied(reason, format string, args ...any) error {
	return &DeniedError{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}

// ReasonOf liefert den Grund einer Ablehnung ("" bei anderen Fehlern).
func ReasonOf(err error) string {
	var d *DeniedError
	if errors.As(err, &d) {
		return d.Reason
	}
	return ""
}
