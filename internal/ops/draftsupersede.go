package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// SupersedeOutput ist das Ergebnis von drafts.supersede.
type SupersedeOutput struct {
	DryRun    bool   `json:"dry_run"`
	DraftID   int64  `json:"draft_id"`
	ByDraftID int64  `json:"by_draft_id"`
	Status    string `json:"status"`
	// Copies: eigene, unveränderte Kopien, die in den Papierkorb gehen.
	Copies   int    `json:"copies"`
	Trash    string `json:"trash"`
	ActionID int64  `json:"action_id,omitempty"`
	NextStep string `json:"next_step"`
}

// DraftsSupersede schließt einen Entwurf, den der Aufrufer durch eine neue
// Fassung ersetzt hat, und legt seine eigene, unveränderte Kopie (und
// frühere eigene Kopien) in den Papierkorb (A07 Teil 5; Nachtrag zu E2
// und E3, docs/entscheidungen.md). R2, Scope mail:draft, kein HumanOnly:
// Die Aktion (Herkunft draft_supersede) braucht keinen Antrag, weil sie
// nur bytegleiche Kopien trifft, die derselbe Schlüssel abgelegt hat;
// [delete] agent_may = none verbietet sie (PolicyInput, Runner). Die
// Bedingungen prüft drafts.Service.Supersede in einer Transaktion; dass
// beide Entwürfe vom aufrufenden Schlüssel stammen, ist eine Bedingung an
// die Daten (wie das Konto), keine Rechteentscheidung: Ob der Schlüssel die
// Operation überhaupt darf, entscheidet die Policy.
func DraftsSupersede(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name: drafts.OpSupersede, Scope: policy.ScopeMailDraft, Risk: policy.R2,
		Summary: "Close an own draft that a newer own draft replaces (same original or subject) and move its unchanged copy " +
			"in the Drafts folder to the trash (never deleted for good).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer", "minimum": 1, "description": "the old draft (status draft or pending_approval) whose copy goes to the trash"},
    "by_draft_id": {"type": "integer", "minimum": 1, "description": "the newer open draft that replaces it (same kind, same original or same subject)"}
  },
  "required": ["draft_id", "by_draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "dry_run": {"type": "boolean"}, "draft_id": {"type": "integer"}, "by_draft_id": {"type": "integer"},
    "status": {"type": "string", "enum": ["abandoned"], "description": "status of the old draft afterwards"},
    "copies": {"type": "integer", "description": "own unchanged copies that go to the trash"},
    "trash": {"type": "string", "description": "the mailbox with role trash"},
    "action_id": {"type": "integer"}, "next_step": {"type": "string"}
  },
  "required": ["dry_run", "draft_id", "by_draft_id", "status", "copies", "trash", "next_step"]
}`),
		Redact: redactSupersede,
		PolicyInput: func(ctx context.Context, input json.RawMessage) (policy.Facts, error) {
			// Was die Aktion anrichtet: Papierkorb, höchstens die Kopie und
			// die früheren Kopien ([delete] agent_may greift schon hier).
			var in struct {
				DraftID int64 `json:"draft_id"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return policy.Facts{}, err
			}
			f := policy.Facts{Action: policy.ActionTrash, Targets: 1}
			if o.svc != nil {
				if dr, err := o.svc.Store.Draft(ctx, in.DraftID); err == nil {
					f.Targets += len(dr.RemotePrevious)
				}
			}
			return f, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				DraftID   int64 `json:"draft_id"`
				ByDraftID int64 `json:"by_draft_id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if o.svc == nil {
				return nil, errors.New("drafts are not configured")
			}
			res, err := o.as(call, drafts.OpSupersede, policy.ScopeMailDraft).Supersede(ctx, drafts.SupersedeRequest{
				OldID: in.DraftID, NewID: in.ByDraftID, KeyID: call.Key.ID, Human: call.Key.IsHuman(), AuditID: call.AuditID})
			if err != nil {
				return nil, draftErr(err)
			}
			out := SupersedeOutput{DryRun: call.DryRun, DraftID: in.DraftID, ByDraftID: in.ByDraftID, Status: string(drafts.StatusAbandoned),
				Copies: res.Copies, Trash: res.Trash, ActionID: res.ActionID}
			if call.DryRun {
				out.NextStep = "dry run: nothing changed; call again with dry_run=false"
			} else {
				out.NextStep = fmt.Sprintf("draft %d is abandoned; its copy goes to the trash (%s): a running %s sync --watch moves it "+
					"within 10 s, otherwise the next %s sync; check with: %s action show %d", in.DraftID, res.Trash, brand.Name, brand.Name,
					brand.Name, res.ActionID)
			}
			return out, nil
		},
	}
}

// redactSupersede: nur die beiden Draft-IDs (Ziele draft:<id>).
func redactSupersede(raw json.RawMessage) (json.RawMessage, []string) {
	var in struct {
		DraftID   int64 `json:"draft_id"`
		ByDraftID int64 `json:"by_draft_id"`
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	params := map[string]any{}
	var targets []string
	for _, v := range []struct {
		k  string
		id int64
	}{{"draft_id", in.DraftID}, {"by_draft_id", in.ByDraftID}} {
		if v.id > 0 {
			params[v.k] = v.id
			targets = append(targets, "draft:"+strconv.FormatInt(v.id, 10))
		}
	}
	b, _ := json.Marshal(params)
	return b, targets
}
