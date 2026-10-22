package drafts

// Zustandsmaschine (docs/sicherheit.md, Abschnitt 5; A02 E5, E7):
//
//	draft ──request_ready──▶ pending_approval ──approve──▶ ready
//	  ▲  ◀──────reject────────────┘                          │
//	  ├──────── update (Inhalt geändert) ◀────────────────────┤
//	  ├──────── expire (approval_ttl)    ◀────────────────────┤
//	  └─ abandon / mark_sent / sent_detected ─▶ abandoned | sent_by_human
//
// Eine Änderung in pending_approval oder ready führt zurück nach draft und
// markiert die Freigabe superseded; der Ablauf der Freigabe ebenso (expired).
// Nach sent_by_human und abandoned ist jede Änderung draft_closed. Es gibt
// keinen Übergang, der aus ready versendet: ready ist das Signal an den
// Menschen, im Mailclient zu senden.

// Event ist ein Ereignis an einem Entwurf.
type Event string

// Ereignisse.
const (
	// EvUpdate: Inhalt geändert (Agent oder Mensch).
	EvUpdate Event = "update"
	// EvRequestReady: zur Freigabe vorgelegt, Prüf-Hook bestanden.
	EvRequestReady Event = "request_ready"
	// EvApprove: Mensch gibt frei (nur Mensch, E2).
	EvApprove Event = "approve"
	// EvReject: Mensch lehnt ab (nur Mensch).
	EvReject Event = "reject"
	// EvAbandon: Mensch verwirft (nur Mensch).
	EvAbandon Event = "abandon"
	// EvMarkSent: Mensch erklärt den Entwurf für gesendet (nur Mensch).
	EvMarkSent Event = "mark_sent"
	// EvSentDetected: der Sync fand die Nachricht in Gesendet (System, E5).
	EvSentDetected Event = "sent_detected"
	// EvExpire: die Freigabe ist abgelaufen (System, approval_ttl).
	EvExpire Event = "expire"
)

// Events sind alle Ereignisse.
var Events = []Event{EvUpdate, EvRequestReady, EvApprove, EvReject, EvAbandon, EvMarkSent, EvSentDetected, EvExpire}

// ApprovalEffect sagt, was ein Übergang mit der Freigabe des Entwurfs tut.
type ApprovalEffect string

// Wirkungen auf die Freigabe.
const (
	ApprovalNone ApprovalEffect = ""
	// ApprovalCreate: neue Freigabe (pending).
	ApprovalCreate ApprovalEffect = "create"
	// ApprovalApprove: pending → approved.
	ApprovalApprove ApprovalEffect = "approve"
	// ApprovalReject: pending → rejected.
	ApprovalReject ApprovalEffect = "reject"
	// ApprovalSupersede: offene oder erteilte Freigabe → superseded.
	ApprovalSupersede ApprovalEffect = "supersede"
	// ApprovalExpire: offene oder erteilte Freigabe → expired.
	ApprovalExpire ApprovalEffect = "expire"
	// ApprovalCloseOpen: eine noch offene Freigabe → superseded; eine
	// erteilte bleibt (der Mensch hat genau das gesendet).
	ApprovalCloseOpen ApprovalEffect = "close_open"
)

// Transition ist ein erlaubter Übergang.
type Transition struct {
	From     Status
	Event    Event
	To       Status
	Approval ApprovalEffect
}

type edge struct {
	from Status
	ev   Event
}

// transitions ist die vollständige Liste der erlaubten Übergänge. Alles
// andere ist verboten (TestTransitionsTable prüft jedes Paar).
var transitions = map[edge]Transition{
	{StatusDraft, EvUpdate}:                 {StatusDraft, EvUpdate, StatusDraft, ApprovalNone},
	{StatusPendingApproval, EvUpdate}:       {StatusPendingApproval, EvUpdate, StatusDraft, ApprovalSupersede},
	{StatusReady, EvUpdate}:                 {StatusReady, EvUpdate, StatusDraft, ApprovalSupersede},
	{StatusDraft, EvRequestReady}:           {StatusDraft, EvRequestReady, StatusPendingApproval, ApprovalCreate},
	{StatusPendingApproval, EvApprove}:      {StatusPendingApproval, EvApprove, StatusReady, ApprovalApprove},
	{StatusPendingApproval, EvReject}:       {StatusPendingApproval, EvReject, StatusDraft, ApprovalReject},
	{StatusDraft, EvAbandon}:                {StatusDraft, EvAbandon, StatusAbandoned, ApprovalNone},
	{StatusPendingApproval, EvAbandon}:      {StatusPendingApproval, EvAbandon, StatusAbandoned, ApprovalSupersede},
	{StatusReady, EvAbandon}:                {StatusReady, EvAbandon, StatusAbandoned, ApprovalSupersede},
	{StatusDraft, EvMarkSent}:               {StatusDraft, EvMarkSent, StatusSentByHuman, ApprovalNone},
	{StatusPendingApproval, EvMarkSent}:     {StatusPendingApproval, EvMarkSent, StatusSentByHuman, ApprovalCloseOpen},
	{StatusReady, EvMarkSent}:               {StatusReady, EvMarkSent, StatusSentByHuman, ApprovalCloseOpen},
	{StatusDraft, EvSentDetected}:           {StatusDraft, EvSentDetected, StatusSentByHuman, ApprovalNone},
	{StatusPendingApproval, EvSentDetected}: {StatusPendingApproval, EvSentDetected, StatusSentByHuman, ApprovalCloseOpen},
	{StatusReady, EvSentDetected}:           {StatusReady, EvSentDetected, StatusSentByHuman, ApprovalCloseOpen},
	{StatusPendingApproval, EvExpire}:       {StatusPendingApproval, EvExpire, StatusDraft, ApprovalExpire},
	{StatusReady, EvExpire}:                 {StatusReady, EvExpire, StatusDraft, ApprovalExpire},
}

// Next liefert den Übergang für ein Ereignis im Zustand from. Nach
// sent_by_human und abandoned ist jedes Ereignis draft_closed; jedes andere
// nicht erlaubte Paar ist invalid_transition.
func Next(from Status, ev Event) (Transition, error) {
	if from.Closed() {
		return Transition{}, denied(ReasonDraftClosed, "the draft is %s; it can no longer be changed", from)
	}
	if t, ok := transitions[edge{from, ev}]; ok {
		return t, nil
	}
	return Transition{}, denied(ReasonInvalidTransition, "%s is not possible in status %s", ev, from)
}

// AllowedTransitions liefert alle erlaubten Übergänge (Dokumentation, Tests).
func AllowedTransitions() []Transition {
	var out []Transition
	for _, s := range Statuses {
		for _, ev := range Events {
			if t, ok := transitions[edge{s, ev}]; ok {
				out = append(out, t)
			}
		}
	}
	return out
}
