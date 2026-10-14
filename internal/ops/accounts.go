package ops

import (
	"context"
	"encoding/json"

	"github.com/KNUFFIXX/mailwerk/internal/policy"
)

// AccountInfo ist die Ausgabe von accounts.list je Konto. Enthält nie
// Zugangsdaten, nur den Verweis-Namen im Schlüsselbund wäre erlaubt, wird
// hier aber gar nicht erst geführt.
type AccountInfo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Provider    string `json:"provider"`
	LastSyncAt  string `json:"last_sync_at,omitempty"` // RFC 3339, UTC
	UnreadTotal int    `json:"unread_total"`
}

// AccountSource ist die Datenquelle von accounts.list (später der Store).
type AccountSource interface {
	ListAccounts(ctx context.Context) ([]AccountInfo, error)
}

// AccountsList ist die Beispieloperation des Gerüsts: lesend (R0), idempotent.
// Sie zeigt das Muster, nach dem alle weiteren Operationen entstehen:
// Schemas als Daten, Handler als Funktion über einer schmalen Schnittstelle.
func AccountsList(src AccountSource) Operation {
	return Operation{
		Name:       "accounts.list",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "List configured accounts with their sync state.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {},
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "accounts": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "id": {"type": "integer"},
          "name": {"type": "string"},
          "email": {"type": "string"},
          "provider": {"type": "string"},
          "last_sync_at": {"type": "string", "format": "date-time"},
          "unread_total": {"type": "integer"}
        },
        "required": ["id", "name", "email", "provider", "unread_total"]
      }
    }
  },
  "required": ["accounts"]
}`),
		Handler: func(ctx context.Context, _ Call) (any, error) {
			accounts, err := src.ListAccounts(ctx)
			if err != nil {
				return nil, err
			}
			if accounts == nil {
				accounts = []AccountInfo{} // JSON: [] statt null
			}
			return struct {
				Accounts []AccountInfo `json:"accounts"`
			}{accounts}, nil
		},
	}
}

// EmptyAccounts ist eine Quelle ohne Konten (Phase 0, bis der Store steht).
type EmptyAccounts struct{}

func (EmptyAccounts) ListAccounts(context.Context) ([]AccountInfo, error) { return nil, nil }
