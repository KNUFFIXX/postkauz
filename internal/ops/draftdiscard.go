package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// DiscardOutput ist das Ergebnis von drafts.discard.
type DiscardOutput struct {
	DryRun  bool   `json:"dry_run"`
	DraftID int64  `json:"draft_id"`
	Status  string `json:"status"`
	// Copies: eigene, unveränderte Kopien, die in den Papierkorb gehen (0:
	// der Entwurf hatte keine Kopie im Entwürfe-Ordner).
	Copies int `json:"copies"`
	// Skipped: eigene Kopien, die liegen bleiben oder schon in einer
	// wartenden Aktion stehen.
	Skipped  int    `json:"skipped"`
	Trash    string `json:"trash,omitempty"`
	ActionID int64  `json:"action_id,omitempty"`
	NextStep string `json:"next_step"`
}

// DraftsDiscard schließt einen eigenen Entwurf, den niemand mehr braucht,
// als abandoned (Grund discarded) und legt seine eigene, unveränderte Kopie
// in den Papierkorb (A11 Teil 2; Nachtrag zu E2, docs/entscheidungen.md).
// R2, Scope mail:draft, kein HumanOnly; wer drafts.supersede darf, darf
// drafts.discard. Die Bedingungen prüft drafts.Service.Discard in einer
// Transaktion. [delete] agent_may = none verbietet den Aufruf über
// PolicyInput nur, wenn es ein Ziel gäbe (ohne Kopie wird nur
// geschlossen).
func DraftsDiscard(d Deps) Operation {
	o := newDraftOps(d)
	return Operation{
		Name: drafts.OpDiscard, Scope: policy.ScopeMailDraft, Risk: policy.R2,
		Summary: "Close an own draft that nobody needs any more (status abandoned, never sent) and move its unchanged copy in the " +
			"Drafts folder to the trash (never deleted for good).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "draft_id": {"type": "integer", "minimum": 1, "description": "the draft (status draft or pending_approval, created and last changed with your key)"}
  },
  "required": ["draft_id"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "dry_run": {"type": "boolean"}, "draft_id": {"type": "integer"},
    "status": {"type": "string", "enum": ["abandoned"], "description": "status of the draft afterwards"},
    "copies": {"type": "integer", "description": "own unchanged copies that go to the trash (0: the draft had no copy in the Drafts folder)"},
    "skipped": {"type": "integer", "description": "own copies not moved by this call: outside the Drafts folders or changed in the mail client (they stay where they are), or already in a mailbox action that is requested, queued or running"},
    "trash": {"type": "string", "description": "the mailbox with role trash"},
    "action_id": {"type": "integer"}, "next_step": {"type": "string"}
  },
  "required": ["dry_run", "draft_id", "status", "copies", "skipped", "next_step"]
}`),
		Redact: redactDraftInput,
		PolicyInput: func(ctx context.Context, input json.RawMessage) (policy.Facts, error) {
			var in struct {
				DraftID int64 `json:"draft_id"`
			}
			if err := json.Unmarshal(input, &in); err != nil {
				return policy.Facts{}, err
			}
			if o.svc == nil {
				return policy.Facts{}, nil
			}
			dr, err := o.svc.Store.Draft(ctx, in.DraftID)
			if err != nil {
				return policy.Facts{}, nil // der Handler meldet „nicht gefunden“
			}
			if n := drafts.DiscardTargets(dr); n > 0 {
				return policy.Facts{Action: policy.ActionTrash, Targets: n}, nil
			}
			return policy.Facts{}, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				DraftID int64 `json:"draft_id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if o.svc == nil {
				return nil, errors.New("drafts are not configured")
			}
			res, err := o.as(call, drafts.OpDiscard, policy.ScopeMailDraft).Discard(ctx, drafts.DiscardRequest{
				ID: in.DraftID, KeyID: call.Key.ID, Human: call.Key.IsHuman(), AuditID: call.AuditID})
			if err != nil {
				return nil, draftErr(err)
			}
			out := DiscardOutput{DryRun: call.DryRun, DraftID: in.DraftID, Status: string(drafts.StatusAbandoned), Copies: res.Copies,
				Skipped: res.Skipped, Trash: res.Trash, ActionID: res.ActionID}
			switch {
			case call.DryRun:
				out.NextStep = "dry run: nothing changed; call again with dry_run=false"
			case res.ActionID > 0:
				out.NextStep = fmt.Sprintf("draft %d is abandoned (not sent); its copy is queued for the trash (%s, action %d): a running %s "+
					"sync --watch moves it within 10 s, otherwise the next %s sync; check with: %s action show %d", in.DraftID, res.Trash,
					res.ActionID, brand.Name, brand.Name, brand.Name, res.ActionID)
			case res.Copies == 0 && res.Skipped == 0:
				out.NextStep = fmt.Sprintf("draft %d is abandoned (not sent); it had no copy in the Drafts folder", in.DraftID)
			default:
				out.NextStep = fmt.Sprintf("draft %d is abandoned (not sent); its copies are already queued for the trash by an earlier action "+
					"or stay where they are (see drafts.get cleanup_action_ids)", in.DraftID)
			}
			return out, nil
		},
	}
}
