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
	// HumanOnly nennt für einen Schlüssel die Operationen, die nur der
	// Mensch ohne Schlüssel ausführen darf (A02 E2; Entscheidung human_only,
	// gleich welche Rolle). Sichtbar, damit klar ist, was der Schlüssel nie
	// darf (Entscheid 40); für den Menschen leer (sie stehen in operations).
	HumanOnly []PolicyOperation `json:"human_only"`
}

// PolicyGet beschreibt die Richtlinie des aufrufenden Schlüssels: Scopes,
// erlaubte Operationen mit Risikostufe, Trockenlauf-Standard, Budget.
// HumanOnly-Operationen stehen nur für den Menschen ohne Schlüssel in der
// Liste (ein Schlüssel darf sie nie, A02 E2); für einen Schlüssel stehen
// sie in der eigenen Liste human_only (Entscheid 40).
// Jeder Schlüssel darf sie lesen (ScopeAny, R0).
func PolicyGet(maxChars int) Operation {
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
      "read_only": {"type": "boolean"}},
      "required": ["name", "scope", "risk", "read_only"]}},
    "human_only": {"type": "array", "description": "operations only a human without a key may run (decision human_only)",
      "items": {"type": "object", "properties": {
      "name": {"type": "string"}, "scope": {"type": "string"}, "risk": {"type": "string", "enum": ["R0", "R1", "R2", "R3"]},
      "read_only": {"type": "boolean"}},
      "required": ["name", "scope", "risk", "read_only"]}}
  },
  "required": ["key", "scopes", "dry_run_default", "max_chars", "draft_only", "operations", "human_only"]
}`),
		Handler: func(_ context.Context, call Call) (any, error) {
			if err := decodeInput(call.Input, &struct{}{}); err != nil {
				return nil, err
			}
			k := call.Key
			out := PolicyOutput{Key: k.ID, Role: k.Role, Scopes: []string{}, DryRunDefault: k.DryRunDefault,
				ExpiresAt: rfc3339(k.ExpiresAt), MaxChars: maxChars, DraftOnly: true, Operations: []PolicyOperation{},
				HumanOnly: []PolicyOperation{}}
			for _, s := range k.Scopes {
				out.Scopes = append(out.Scopes, string(s))
			}
			slices.Sort(out.Scopes)
			// Der Katalog ohne Abhängigkeiten dient hier nur als Liste der
			// Namen, Scopes und Risikostufen; kein Handler läuft.
			for _, op := range Catalog(Deps{}) {
				po := PolicyOperation{Name: op.Name, Scope: string(op.Scope), Risk: op.Risk.String(), ReadOnly: op.Risk == policy.R0}
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
