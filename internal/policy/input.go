package policy

import "slices"

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

// HumanOnlyMoveRoles sind Rollen, in die nur der Mensch ohne Schlüssel
// verschiebt (V8 der Nachbesserung T2, Betreiber 28.10.2026): Spam, Entwürfe
// und Gesendet. Ein Schlüssel könnte sonst ohne Freigabe Mails in den
// Spam-Ordner schieben (dort verschwinden sie für den Menschen, Filter des
// Anbieters lernen daraus) oder Entwürfe und Gesendetes umsortieren, auf
// die sich die Versanderkennung stützt. Gilt auch, wenn der Zielordner per
// Pfad genannt ist (die Rolle kommt dann aus dem Spiegel).
var HumanOnlyMoveRoles = []string{"junk", "drafts", "sent"}

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
	// HumanOnlyOption: Die Eingabe nutzt eine Option, die nur der Mensch
	// ohne Schlüssel nutzen darf (accounts.remove mit force; Nachbesserung
	// T3, A9). Mit Schlüssel: deny human_only.
	HumanOnlyOption bool
}

// HumanOnlyOptions nennt die Eingaben, die nur der Mensch ohne Schlüssel
// nutzen darf, gleich welche Rolle (policy.get, für einen Schlüssel).
var HumanOnlyOptions = []string{"accounts.remove with force", "messages.move to a folder with role junk, drafts or sent"}

// Rules sind die Regeln der Policy-Datei, die CheckInput auswertet.
type Rules struct {
	// MoveAgentMay: none | archive_only | within_account.
	MoveAgentMay string
	// DeleteAgentMay: none | trash_only.
	DeleteAgentMay string
	// BulkConfirmThreshold: über so vielen Zielen verlangt ein Schlüssel
	// confirm_count gleich der Zahl der Ziele.
	BulkConfirmThreshold int
	// Keys sind die verengten Regeln je Schlüsselname ([keys.<name>];
	// fehlende Werte schon mit denen des Standards gefüllt).
	Keys map[string]Rules
}

// DefaultRules sind die Regeln ohne Policy-Datei (wie defaults in file.go).
func DefaultRules() Rules {
	return Rules{MoveAgentMay: "within_account", DeleteAgentMay: "trash_only", BulkConfirmThreshold: DefaultBulkConfirmThreshold}
}

// Rules liefert die Regeln der geladenen Policy-Datei, mit den verengten
// Regeln je Schlüssel ([keys.<name>]).
func (f File) Rules() Rules {
	base := Rules{MoveAgentMay: f.Doc.Move.AgentMay, DeleteAgentMay: f.Doc.Delete.AgentMay,
		BulkConfirmThreshold: f.Doc.Move.BulkConfirmThreshold}.withDefaults()
	if len(f.Doc.Keys) > 0 {
		base.Keys = map[string]Rules{}
		for n, k := range f.Doc.Keys {
			r := Rules{MoveAgentMay: base.MoveAgentMay, DeleteAgentMay: base.DeleteAgentMay, BulkConfirmThreshold: base.BulkConfirmThreshold}
			if k.Move.AgentMay != "" {
				r.MoveAgentMay = k.Move.AgentMay
			}
			if k.Delete.AgentMay != "" {
				r.DeleteAgentMay = k.Delete.AgentMay
			}
			if k.Move.BulkConfirmThreshold > 0 {
				r.BulkConfirmThreshold = k.Move.BulkConfirmThreshold
			}
			base.Keys[n] = r
		}
	}
	return base
}

// For liefert die Regeln für einen Schlüssel (verengt, falls [keys.<name>]).
func (r Rules) For(key Key) Rules {
	if k, ok := r.Keys[key.Name]; ok && key.Name != "" && !key.IsHuman() {
		return k.withDefaults()
	}
	r.Keys = nil
	return r
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
//   - move in einen Ordner mit Rolle trash: target_is_trash, für jeden
//     (der Papierkorb geht nur über trash mit Freigabe, E3).
//   - trash mit [delete] agent_may = none: policy_forbids.
//   - move in einen Ordner mit Rolle junk, drafts oder sent
//     (HumanOnlyMoveRoles): target_role_human_only (V8).
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
	// Der Papierkorb ist R3 mit Freigabe (E3); Verschieben dorthin wäre ein
	// Umweg darum herum, auch für den Menschen (er nutzt trash --yes).
	if f.Action == ActionMove && f.TargetRole == "trash" {
		return deny("target_is_trash")
	}
	if key.IsHuman() {
		return Decision{Outcome: Allow, Reason: "ok"}
	}
	if f.HumanOnlyOption {
		return deny("human_only")
	}
	if f.Action == ActionMove && slices.Contains(HumanOnlyMoveRoles, f.TargetRole) {
		return deny("target_role_human_only")
	}
	r = r.For(key).withDefaults()
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
