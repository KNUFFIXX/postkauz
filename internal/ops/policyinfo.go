package ops

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// PolicyOperation ist eine Operation, die der Schlüssel aufrufen darf.
type PolicyOperation struct {
	Name     string `json:"name"`
	Scope    string `json:"scope"`
	Risk     string `json:"risk"`
	ReadOnly bool   `json:"read_only"`
	// NeedsApproval: R3, ein Aufruf legt nur einen Antrag an; ausgeführt
	// wird nach der Freigabe eines Menschen (T2, E3).
	NeedsApproval bool `json:"needs_approval,omitempty"`
}

// PolicyLimits sind die Regeln der Policy-Datei für Postfach-Aktionen,
// wie sie für diesen Schlüssel gelten ([move], [delete], [keys.<name>]).
type PolicyLimits struct {
	MoveAgentMay         string `json:"move_agent_may"`
	DeleteAgentMay       string `json:"delete_agent_may"`
	BulkConfirmThreshold int    `json:"bulk_confirm_threshold"`
	MaxIDs               int    `json:"max_ids"`
}

// PolicyOutput ist die Ausgabe von policy.get: was der aufrufende Schlüssel
// darf. Sie enthält nie den Schlüssel selbst.
type PolicyOutput struct {
	Key           string            `json:"key"`
	Role          string            `json:"role,omitempty"`
	Scopes        []string          `json:"scopes"`
	DryRunDefault bool              `json:"dry_run_default"`
	ExpiresAt     string            `json:"expires_at,omitempty"`
	MaxChars      int               `json:"max_chars"`
	DraftOnly     bool              `json:"draft_only"`
	Operations    []PolicyOperation `json:"operations"`
	// Limits gelten für Aufrufe mit Schlüssel (der Mensch: nur max_ids).
	Limits PolicyLimits `json:"limits"`
	// HumanOnly nennt für einen Schlüssel die Operationen, die nur der
	// Mensch ohne Schlüssel ausführen darf (A02 E2; Entscheidung human_only,
	// gleich welche Rolle). Sichtbar, damit klar ist, was der Schlüssel nie
	// darf (Entscheid 40); für den Menschen leer (sie stehen in operations).
	HumanOnly []PolicyOperation `json:"human_only"`
	// HumanOnlyOptions nennt für einen Schlüssel die Eingaben, die nur der
	// Mensch ohne Schlüssel nutzen darf (policy.HumanOnlyOptions, z. B.
	// accounts.remove mit force); für den Menschen leer.
	HumanOnlyOptions []string `json:"human_only_options,omitempty"`
}

// PolicyGet beschreibt die Richtlinie des aufrufenden Schlüssels: Scopes,
// erlaubte Operationen mit Risikostufe, Trockenlauf-Standard, Budget.
// HumanOnly-Operationen stehen nur für den Menschen ohne Schlüssel in der
// Liste (ein Schlüssel darf sie nie, A02 E2); für einen Schlüssel stehen
// sie in der eigenen Liste human_only (Entscheid 40).
// Jeder Schlüssel darf sie lesen (ScopeAny, R0).
//
// file ist die geladene Policy-Datei (Budget und Regeln je Schlüssel, T2);
// nil = Standards.
func PolicyGet(maxChars int, file *policy.File) Operation {
	if maxChars <= 0 {
		maxChars = DefaultMaxChars
	}
	return Operation{
		Name:       "policy.get",
		Scope:      policy.ScopeAny,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Describe what the calling key may do: scopes, allowed operations with risk level, dry-run default, budget.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "key": {"type": "string"}, "role": {"type": "string"},
    "scopes": {"type": "array", "items": {"type": "string"}},
    "dry_run_default": {"type": "boolean"}, "expires_at": {"type": "string", "format": "date-time"},
    "max_chars": {"type": "integer"}, "draft_only": {"type": "boolean"},
    "operations": {"type": "array", "items": {"type": "object", "properties": {
      "name": {"type": "string"}, "scope": {"type": "string"}, "risk": {"type": "string", "enum": ["R0", "R1", "R2", "R3"]},
      "read_only": {"type": "boolean"}, "needs_approval": {"type": "boolean"}},
      "required": ["name", "scope", "risk", "read_only"]}},
    "limits": {"type": "object", "properties": {"move_agent_may": {"type": "string"}, "delete_agent_may": {"type": "string"},
      "bulk_confirm_threshold": {"type": "integer"}, "max_ids": {"type": "integer"}},
      "required": ["move_agent_may", "delete_agent_may", "bulk_confirm_threshold", "max_ids"]},
    "human_only": {"type": "array", "description": "operations only a human without a key may run (decision human_only)",
      "items": {"type": "object", "properties": {
      "name": {"type": "string"}, "scope": {"type": "string"}, "risk": {"type": "string", "enum": ["R0", "R1", "R2", "R3"]},
      "read_only": {"type": "boolean"}},
      "required": ["name", "scope", "risk", "read_only"]}},
    "human_only_options": {"type": "array", "description": "inputs only a human without a key may use (decision human_only)", "items": {"type": "string"}}
  },
  "required": ["key", "scopes", "dry_run_default", "max_chars", "draft_only", "operations", "limits", "human_only"]
}`),
		Handler: func(_ context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			k := call.Key
			out := PolicyOutput{Key: k.ID, Role: k.Role, Scopes: []string{}, DryRunDefault: k.DryRunDefault,
				ExpiresAt: rfc3339(k.ExpiresAt), MaxChars: maxChars, DraftOnly: true, Operations: []PolicyOperation{},
				HumanOnly: []PolicyOperation{}}
			rules := policy.DefaultRules()
			if file != nil {
				rules = file.Rules()
				if !k.IsHuman() {
					out.MaxChars = file.MaxCharsFor(k.Name)
				}
			}
			if !k.IsHuman() {
				out.HumanOnlyOptions = slices.Clone(policy.HumanOnlyOptions)
			}
			r := rules.For(k)
			out.Limits = PolicyLimits{MoveAgentMay: r.MoveAgentMay, DeleteAgentMay: r.DeleteAgentMay,
				BulkConfirmThreshold: r.BulkConfirmThreshold, MaxIDs: policy.MaxTargets}
			for _, s := range k.Scopes {
				out.Scopes = append(out.Scopes, string(s))
			}
			slices.Sort(out.Scopes)
			// Der Katalog ohne Abhängigkeiten dient hier nur als Liste der
			// Namen, Scopes und Risikostufen; kein Handler läuft.
			for _, op := range Catalog(Deps{}) {
				po := PolicyOperation{Name: op.Name, Scope: string(op.Scope), Risk: op.Risk.String(), ReadOnly: op.Risk == policy.R0,
					NeedsApproval: op.Risk == policy.R3}
				switch {
				case op.HumanOnly && !k.IsHuman():
					out.HumanOnly = append(out.HumanOnly, po)
				case k.Covers(op.Scope):
					out.Operations = append(out.Operations, po)
				}
			}
			return out, nil
		},
	}
}
