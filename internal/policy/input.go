package policy

// Zentrale Prüfung der Eingabe (A03 A9): Was eine Operation mit ihrer
// Eingabe anrichten darf (Zahl der Ziele, Rolle des Zielordners, Konto),
// entscheidet die Policy, nicht der Handler. Die Operation liefert dafür
// aus der schon gegen ihr Schema geprüften Eingabe die Facts; der Executor
// ruft CheckInput nach Check und vor dem Start-Eintrag des Audits auf. Eine
// Ablehnung ist damit ein einzelner Eintrag denied, vor jedem started/ok.
//
// Die Regeln stammen aus der Policy-Datei ([move], [delete]; E4). Die
// Operationen, die sie brauchen (move, trash, mark), baut Teil T2; bis dahin
// prüfen Unit-Tests und der Vertragstest den Weg mit Testoperationen.

// Arten von Aktionen, nach denen CheckInput unterscheidet.
const (
	ActionMove  = "move"
	ActionTrash = "trash"
	ActionMark  = "mark"
)

// MaxTargets: höchstens so viele Ziele je Aufruf, für jeden Aufrufer (E4).
const MaxTargets = 1000

// Facts ist der für die Policy relevante Teil einer geprüften Eingabe.
// Nie Inhalte: nur Zähler, Rollen, Wahrheitswerte.
type Facts struct {
	// Action ist ActionMove, ActionTrash, ActionMark oder "" (keine
	// Postfach-Aktion; dann zählen nur die Ziele).
	Action string
	// Targets ist die Zahl der Ziele (Nachrichten-IDs) des Aufrufs.
	Targets int
	// ConfirmCount ist confirm_count der Eingabe (nil = nicht angegeben).
	ConfirmCount *int
	// TargetRole ist die Rolle des Zielordners beim Verschieben ("" = keine).
	TargetRole string
	// CrossAccount: Ziel liegt in einem anderen Konto als die Nachrichten.
	CrossAccount bool
}

// Rules sind die Regeln der Policy-Datei, die CheckInput auswertet.
type Rules struct {
	// MoveAgentMay: none | archive_only | within_account.
	MoveAgentMay string
	// DeleteAgentMay: none | trash_only.
	DeleteAgentMay string
	// BulkConfirmThreshold: über so vielen Zielen verlangt ein Schlüssel
	// confirm_count gleich der Zahl der Ziele.
	BulkConfirmThreshold int
}

// DefaultRules sind die Regeln ohne Policy-Datei (wie defaults in file.go).
func DefaultRules() Rules {
	return Rules{MoveAgentMay: "within_account", DeleteAgentMay: "trash_only", BulkConfirmThreshold: DefaultBulkConfirmThreshold}
}

// Rules liefert die Regeln der geladenen Policy-Datei.
func (f File) Rules() Rules {
	return Rules{MoveAgentMay: f.Doc.Move.AgentMay, DeleteAgentMay: f.Doc.Delete.AgentMay,
		BulkConfirmThreshold: f.Doc.Move.BulkConfirmThreshold}.withDefaults()
}

// withDefaults füllt leere Werte mit DefaultRules.
func (r Rules) withDefaults() Rules {
	d := DefaultRules()
	if r.MoveAgentMay == "" {
		r.MoveAgentMay = d.MoveAgentMay
	}
	if r.DeleteAgentMay == "" {
		r.DeleteAgentMay = d.DeleteAgentMay
	}
	if r.BulkConfirmThreshold <= 0 {
		r.BulkConfirmThreshold = d.BulkConfirmThreshold
	}
	return r
}

// CheckInput prüft die Facts einer erlaubten Operation (nach Check). Für
// jeden Aufrufer gilt die Obergrenze MaxTargets (too_many_ids); die übrigen
// Regeln gelten nur mit Schlüssel (der Mensch am Terminal handelt selbst):
//
//   - trash mit [delete] agent_may = none: policy_forbids.
//   - move mit [move] agent_may = none: policy_forbids; archive_only: nur
//     Ziele mit Rolle archive oder all; within_account: kein Ziel in einem
//     anderen Konto (cross_account).
//   - mehr Ziele als [move] bulk_confirm_threshold: confirm_count muss
//     angegeben sein (confirm_count_required) und genau der Zahl der Ziele
//     entsprechen (confirm_count_mismatch).
//
// Ergebnis ist Allow (Grund "ok") oder Deny mit Grund.
func CheckInput(key Key, f Facts, r Rules) Decision {
	deny := func(reason string) Decision { return Decision{Outcome: Deny, Reason: reason} }
	if f.Targets > MaxTargets {
		return deny("too_many_ids")
	}
	if key.IsHuman() {
		return Decision{Outcome: Allow, Reason: "ok"}
	}
	r = r.withDefaults()
	switch f.Action {
	case ActionTrash:
		if r.DeleteAgentMay != "trash_only" {
			return deny("policy_forbids")
		}
	case ActionMove:
		switch r.MoveAgentMay {
		case "within_account":
			if f.CrossAccount {
				return deny("cross_account")
			}
		case "archive_only":
			if f.CrossAccount || (f.TargetRole != "archive" && f.TargetRole != "all") {
				return deny("policy_forbids")
			}
		default:
			return deny("policy_forbids")
		}
	}
	if f.Targets > r.BulkConfirmThreshold {
		switch {
		case f.ConfirmCount == nil:
			return deny("confirm_count_required")
		case *f.ConfirmCount != f.Targets:
			return deny("confirm_count_mismatch")
		}
	}
	return Decision{Outcome: Allow, Reason: "ok"}
}
