// Package policy entscheidet, ob ein Schlüssel eine Operation ausführen darf.
//
// Die Entscheidungslogik ist bewusst klein und konservativ (MVP =
// draft-only, docs/sicherheit.md): Check für Scope, Mensch, Risiko und
// Trockenlauf, CheckInput (input.go, seit T1b) für die Eingabe nach den
// Regeln der Policy-Datei (file.go, seit S2.1). Die Freigabe-
// Zustandsmaschine der Entwürfe liegt in internal/drafts (seit S2.1).
package policy

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Scope ist eine einzelne Berechtigung eines API-Schlüssels.
type Scope string

// Scopes des MVP. Es gibt bewusst kein mail:send (Versand ab Schnitt 4) und
// keine Regel-Scopes (Regeln ab Schnitt 4).
const (
	ScopeMailRead   Scope = "mail:read"
	ScopeMailSearch Scope = "mail:search"
	ScopeMailDraft  Scope = "mail:draft"
	ScopeMailTag    Scope = "mail:tag"
	// ScopeMailMark: Flags am Server setzen (messages.mark: gelesen,
	// markiert, auf allen Kopien). Eigener Scope seit der Nachbesserung T2
	// (V20), nicht in assistant und triage: Ein vorhandener assistant-
	// Schlüssel bekommt so nach dem Update kein Schreibrecht am Server
	// (per Prompt-Injection „alles gelesen“ verschwänden neue Mails in einem
	// geteilten Posteingang). Lokale Tags bleiben mail:tag.
	ScopeMailMark   Scope = "mail:mark"
	ScopeMailMove   Scope = "mail:move"
	ScopeMailTrash  Scope = "mail:trash"
	ScopeMailSync   Scope = "mail:sync"
	ScopeMailExport Scope = "mail:export"
	ScopeAuditRead  Scope = "audit:read"
	ScopeAdmin      Scope = "admin"
	// ScopeAny steht an Operationen, die jedem gültigen Schlüssel offen
	// stehen, weil sie nur über den Schlüssel selbst Auskunft geben
	// (policy.get, MCP mail_get_policy). Kein Schlüssel trägt ihn.
	ScopeAny Scope = "any"
)

// Risk ist die Risikostufe einer Operation.
type Risk int

// Risikostufen. Höhere Stufen sind strikt gefährlicher.
const (
	R0 Risk = iota // lesen, keine Wirkung
	R1             // lokal ändern (Datenbank, Blob-Store, Entwürfe lokal)
	R2             // serverseitig ändern, reversibel (Flags, Verschieben)
	R3             // extern wirksam oder schwer umkehrbar (Papierkorb, Versand ab S4)
)

func (r Risk) String() string { return fmt.Sprintf("R%d", int(r)) }

// Outcome ist das Ergebnis einer Prüfung.
type Outcome string

const (
	// Allow: Operation darf ausgeführt werden.
	Allow Outcome = "allow"
	// Deny: Operation ist verboten (fehlender Scope, Policy). Kein Handler-Aufruf.
	Deny Outcome = "deny"
	// NeedsApproval: Operation braucht eine Freigabe durch einen Menschen.
	NeedsApproval Outcome = "needs_approval"
)

// Decision ist das Ergebnis von Check.
type Decision struct {
	Outcome Outcome
	// DryRun ist true, wenn der Handler nichts verändern darf und nur
	// beschreiben soll, was geschähe.
	DryRun bool
	// Reason ist ein kurzer, maschinenlesbarer Grund (z. B. "missing_scope").
	Reason string
}

// Covers meldet, ob der Schlüssel den Scope einer Operation erfüllt (für
// die Auswahl, was eine Oberfläche einem Schlüssel überhaupt zeigt; die
// Entscheidung selbst trifft Check).
func (k Key) Covers(s Scope) bool { return s == ScopeAny || k.Has(s) }

// Allowed meldet, ob der Handler laufen darf.
func (d Decision) Allowed() bool { return d.Outcome == Allow }

// Key ist der für die Prüfung relevante Teil eines API-Schlüssels.
// Der Schlüsselwert selbst gehört nie hierher.
type Key struct {
	ID   string
	Name string
	// Role ist die Rolle, aus der die Scopes stammen (nur zur Auskunft).
	Role   string
	Scopes []Scope
	// DryRunDefault: schreibende Operationen laufen als Trockenlauf, solange
	// der Aufrufer nicht ausdrücklich dry_run=false verlangt.
	DryRunDefault bool
	// ExpiresAt ist das Ablaufdatum (Nullzeit = keins; nur zur Auskunft,
	// abgelaufene Schlüssel weist schon die Anmeldung ab).
	ExpiresAt time.Time
	// Human: Der Aufruf kommt vom Menschen am Terminal ohne API-Schlüssel
	// (CLI ohne --key). Nur dann laufen HumanOnly-Operationen. Der Standard
	// false heißt „ein Schlüssel ist im Spiel" (fail closed): Wer einen Key
	// baut und das Feld vergisst, bekommt keine Mensch-Rechte. Ein Schlüssel
	// mit ID "key:<name>" gilt nie als Mensch, auch wenn Human gesetzt ist.
	Human bool
}

// IsHuman meldet, ob ohne API-Schlüssel gehandelt wird (siehe Key.Human).
func (k Key) IsHuman() bool { return k.Human && !strings.HasPrefix(k.ID, "key:") }

// Has meldet, ob der Schlüssel den Scope besitzt. „admin" ersetzt keine
// anderen Scopes: Er erlaubt Verwaltung, nicht Mailzugriff.
func (k Key) Has(s Scope) bool { return slices.Contains(k.Scopes, s) }

// Requirement beschreibt, was eine Operation vom Aufrufer verlangt.
// ops.Operation liefert sie; policy kennt ops nicht (keine Importzyklen).
type Requirement struct {
	Name  string
	Scope Scope
	Risk  Risk
	// HumanOnly: nur für den Menschen ohne API-Schlüssel (A02, E2), gleich
	// welche Rolle der Schlüssel hat, auch admin.
	HumanOnly bool
}

// Roles bündelt Scopes zu den Standardrollen (docs/sicherheit.md).
var Roles = map[string][]Scope{
	"reader":    {ScopeMailRead, ScopeMailSearch, ScopeAuditRead},
	"assistant": {ScopeMailRead, ScopeMailSearch, ScopeAuditRead, ScopeMailDraft, ScopeMailTag, ScopeMailSync},
	// triage: der Scope-Satz der unbeaufsichtigten Nacht-Triage (Lastenheft
	// Vorgang 5, T2 Punkt 5): lesen, suchen, Entwürfe, lokale Tags; kein
	// Markieren am Server (V20), kein Verschieben, kein Papierkorb, kein
	// Sync, kein Audit-Lesen (TestTriageRoleCanDoNothingElse).
	"triage": {ScopeMailRead, ScopeMailSearch, ScopeMailDraft, ScopeMailTag},
	"operator": {ScopeMailRead, ScopeMailSearch, ScopeAuditRead, ScopeMailDraft, ScopeMailTag, ScopeMailMark, ScopeMailSync,
		ScopeMailMove, ScopeMailTrash, ScopeMailExport},
	"admin": {ScopeMailRead, ScopeMailSearch, ScopeAuditRead, ScopeMailDraft, ScopeMailTag, ScopeMailMark, ScopeMailSync,
		ScopeMailMove, ScopeMailTrash, ScopeMailExport, ScopeAdmin},
}

// EffectiveDryRun löst den Trockenlauf-Standard eines Schlüssels auf.
// explicit ist nil, wenn der Aufrufer nichts angegeben hat.
func EffectiveDryRun(k Key, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	return k.DryRunDefault
}

// Check ist der Policy-Schritt der Kette Scope → Policy → Freigabe → Audit →
// Handler (vor CheckInput). Regeln:
//
//   - HumanOnly und ein API-Schlüssel im Spiel (Key.IsHuman false): Deny mit
//     Grund human_only, vor allen anderen Regeln (A02, E2; S2.0 minimal,
//     vollständig ab S2.1).
//   - fehlender Scope: Deny (ScopeAny fehlt nie).
//   - R0: Allow (lesen; ein Trockenlauf ist bedeutungslos).
//   - R3: NeedsApproval, auch im Trockenlauf und auch für den Menschen
//     (A14: der Trockenlauf geht R3 nicht vor; sonst bekäme ein Schlüssel
//     mit dry_run_default „erlaubt im Trockenlauf“ statt needs_approval).
//     DryRun sagt nur, ob ein Antrag entstehen darf: Eine Operation, die
//     ihren Antrag selbst anlegt (ops.Operation.ApprovalRequest, der
//     Papierkorb, E3), läuft dann; die Ausführung gibt erst die Freigabe
//     eines Menschen frei. Es gibt keinen Weg, eine R3-Operation ohne
//     Mensch auszuführen.
//   - R1/R2 mit dryRun: Allow mit DryRun=true (Handler verändert nichts).
//   - R1 (nur lokale Wirkung) und R2: Allow.
func Check(key Key, op Requirement, dryRun bool) Decision {
	if op.HumanOnly && !key.IsHuman() {
		return Decision{Outcome: Deny, Reason: "human_only"}
	}
	if op.Scope != ScopeAny && !key.Has(op.Scope) {
		return Decision{Outcome: Deny, Reason: "missing_scope"}
	}
	switch {
	case op.Risk == R0:
		return Decision{Outcome: Allow, Reason: "ok"}
	case op.Risk >= R3:
		return Decision{Outcome: NeedsApproval, DryRun: dryRun, Reason: "risk_r3"}
	case dryRun:
		return Decision{Outcome: Allow, DryRun: true, Reason: "dry_run"}
	default:
		return Decision{Outcome: Allow, Reason: "ok"}
	}
}
