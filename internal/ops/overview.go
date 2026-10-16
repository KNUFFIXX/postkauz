package ops

import (
	"context"
	"encoding/json"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// MailboxOverviewOutput ist die Ausgabe von mailboxes.overview: die
// neuesten Köpfe eines Ordners, ungelesene zuerst. Beide Teile sind
// unveränderte Ausgaben einer Suche (messages.search) mit ihrer Hülle.
type MailboxOverviewOutput struct {
	Envelope
	Account string       `json:"account"`
	Mailbox string       `json:"mailbox"`
	Unread  SearchOutput `json:"unread"`
	Read    SearchOutput `json:"read"`
}

// Grenzen von mailboxes.overview.
const (
	DefaultOverviewLimit = 25
	MaxOverviewLimit     = 100
)

// MailboxesOverview zeigt die neuesten Köpfe eines Ordners: ungelesene und
// gelesene getrennt, je höchstens limit, neueste zuerst, optional seit
// einem Datum. Lesend (R0). Scope mail:read (wie mailboxes.list; der
// Ordner wird per ID aus dessen Ausgabe gewählt). Angeboten als MCP-Resource
// mail://{account}/mailbox/{id}/overview und als CLI mailbox overview.
func MailboxesOverview(accounts AccountRepo, boxes MailboxRepo, repo SearchRepo) Operation {
	return Operation{
		Name:       "mailboxes.overview",
		Scope:      policy.ScopeMailRead,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Show the newest headers of a mailbox: unread first, then read (no message bodies).",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "default: the only account"},
    "mailbox": {"type": "integer", "minimum": 1, "description": "mailbox id from mailboxes.list"},
    "since": {"type": "string", "description": "only messages since: duration back from now (10m, 24h, 7d), a date or RFC 3339"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 100, "description": "headers per part (default 25)"}
  },
  "required": ["mailbox"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    ` + envelopeProps + `,
    "account": {"type": "string"}, "mailbox": {"type": "string"},
    "unread": ` + searchOutputSchema + `,
    "read": ` + searchOutputSchema + `
  },
  "required": ["untrusted", "source", "notice", "account", "mailbox", "unread", "read"]
}`),
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				Account string `json:"account"`
				Mailbox int64  `json:"mailbox"`
				Since   string `json:"since"`
				Limit   int    `json:"limit"`
			}
			if json.Unmarshal(raw, &in) != nil {
				return json.RawMessage(`{"invalid_input":true}`), nil
			}
			b, _ := json.Marshal(map[string]any{"since": in.Since != "", "limit": in.Limit})
			var targets []string
			if AccountNameRE.MatchString(in.Account) {
				targets = append(targets, "account:"+in.Account)
			}
			if in.Mailbox > 0 {
				targets = append(targets, "mailbox:"+itoa(in.Mailbox))
			}
			return b, targets
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account string `json:"account,omitempty"`
				Mailbox int64  `json:"mailbox"`
				Since   string `json:"since,omitempty"`
				Limit   int    `json:"limit,omitempty"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if in.Limit == 0 {
				in.Limit = DefaultOverviewLimit
			}
			a, err := ResolveAccount(ctx, accounts, in.Account)
			if err != nil {
				return nil, err
			}
			list, err := boxes.Mailboxes(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			path := ""
			for _, m := range list {
				if m.ID == in.Mailbox {
					path = m.Path
				}
			}
			if path == "" {
				return nil, store.ErrMailboxNotFound
			}
			query := "is:unread"
			if in.Since != "" {
				t, err := ParseSince(in.Since, time.Now())
				if err != nil {
					return nil, err
				}
				query += " after:" + t.Local().Format("2006-01-02")
			}
			out := MailboxOverviewOutput{Envelope: emailEnvelope(), Account: a.Name, Mailbox: path}
			if out.Unread, err = runSearch(ctx, accounts, repo, searchInput{Query: query, Account: a.Name, Mailbox: path, Limit: in.Limit}); err != nil {
				return nil, err
			}
			query = "is:read" + query[len("is:unread"):]
			if out.Read, err = runSearch(ctx, accounts, repo, searchInput{Query: query, Account: a.Name, Mailbox: path, Limit: in.Limit}); err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}
