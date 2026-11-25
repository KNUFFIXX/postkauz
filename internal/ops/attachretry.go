package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// ExtractRetryRepo setzt fehlgeschlagene Textauszüge zurück (A12 Teil 3
// (c); store.SQLite).
type ExtractRetryRepo interface {
	CountFailedExtractions(ctx context.Context, r store.RetrySelect) (int, error)
	RetryFailedExtractions(ctx context.Context, r store.RetrySelect) (int, error)
}

// AttachRetryOutput ist das Ergebnis von attachments.retry.
type AttachRetryOutput struct {
	DryRun   bool   `json:"dry_run"`
	Account  string `json:"account"`
	Count    int    `json:"count"`
	NextStep string `json:"next_step"`
}

// AttachmentsRetry setzt Anhänge mit extract_status failed wieder auf
// pending und löscht ihren Zähler (extract_retry/<id>), damit der nächste
// Job-Takt bzw. Lauf sie erneut extrahiert (A12 Teil 3 (c), Befund B6:
// Anhänge, die am falschen PDF-Programm endgültig scheiterten). Nur der
// Mensch (HumanOnly, nicht über MCP); R1, ändert nur lokal. Auswahl: Konto
// (Pflicht bei mehr als einem), dazu Nachricht, Anhänge oder all_failed;
// ohne Auswahl Fehler. Audit: Konto und Zahl, nie Dateinamen.
func AttachmentsRetry(d Deps) Operation {
	return Operation{
		Name: "attachments.retry", Scope: policy.ScopeMailRead, Risk: policy.R1, HumanOnly: true,
		Summary: "Extract the text of attachments again whose text extraction failed (status failed back to pending).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "the account; required with more than one"},
    "message": {"type": "integer", "minimum": 1, "description": "only failed attachments of this message"},
    "attachments": {"type": "array", "items": {"type": "integer", "minimum": 1}, "maxItems": 1000, "description": "only these attachments"},
    "all_failed": {"type": "boolean", "description": "all failed attachments of the account"}
  },
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "dry_run": {"type": "boolean"}, "account": {"type": "string"},
    "count": {"type": "integer", "description": "attachments set back to pending (dry run: that would be)"},
    "next_step": {"type": "string"}
  },
  "required": ["dry_run", "account", "count", "next_step"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				Account     string  `json:"account"`
				Message     int64   `json:"message"`
				Attachments []int64 `json:"attachments"`
				All         bool    `json:"all_failed"`
			}
			if json.Unmarshal(raw, &in) != nil {
				return json.RawMessage(`{}`), nil
			}
			p := map[string]any{"account": in.Account}
			var targets []string
			if in.Message > 0 {
				p["message"] = in.Message
				targets = append(targets, "message:"+itoa(in.Message))
			}
			if len(in.Attachments) > 0 {
				p["attachments"] = len(in.Attachments)
			}
			if in.All {
				p["all_failed"] = true
			}
			b, _ := json.Marshal(p)
			return b, targets
		},
		AuditCounts: func(out any) map[string]int64 {
			if o, ok := out.(AttachRetryOutput); ok {
				return map[string]int64{"count": int64(o.Count)}
			}
			return nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account     string  `json:"account"`
				Message     int64   `json:"message"`
				Attachments []int64 `json:"attachments"`
				All         bool    `json:"all_failed"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Message <= 0 && len(in.Attachments) == 0 && !in.All {
				return nil, inputErr("choose what to retry: --message <id>, --attachment <id> or --all-failed")
			}
			if d.ExtractRetry == nil {
				return nil, errors.New("attachments are not configured")
			}
			acc, err := retryAccount(ctx, d.Accounts, in.Account)
			if err != nil {
				return nil, err
			}
			sel := store.RetrySelect{AccountID: acc.ID, MessageID: in.Message, Attachments: in.Attachments, All: in.All}
			out := AttachRetryOutput{DryRun: call.DryRun, Account: acc.Name}
			if call.DryRun {
				if out.Count, err = d.ExtractRetry.CountFailedExtractions(ctx, sel); err != nil {
					return nil, err
				}
				out.NextStep = fmt.Sprintf("dry run: %d attachments would be extracted again; run without --dry-run", out.Count)
				return out, nil
			}
			if out.Count, err = d.ExtractRetry.RetryFailedExtractions(ctx, sel); err != nil {
				return nil, err
			}
			switch out.Count {
			case 0:
				out.NextStep = "no failed attachment matches"
			default:
				out.NextStep = fmt.Sprintf("%d attachments wait for the text extraction again: a running %s sync --watch extracts them "+
					"within a few minutes, otherwise the next %s sync; check with: %s attach text <attachment-id>", out.Count, brand.Name,
					brand.Name, brand.Name)
			}
			return out, nil
		},
	}
}

// retryAccount: das genannte Konto, ohne Namen das einzige.
func retryAccount(ctx context.Context, repo AccountRepo, name string) (store.Account, error) {
	if name != "" {
		return ResolveAccount(ctx, repo, name)
	}
	list, err := repo.ListAccounts(ctx)
	if err != nil {
		return store.Account{}, err
	}
	switch len(list) {
	case 0:
		return store.Account{}, inputErr("there is no account")
	case 1:
		return ResolveAccount(ctx, repo, list[0].Name)
	}
	return store.Account{}, inputErr("there is more than one account: choose one with --account <name>")
}
