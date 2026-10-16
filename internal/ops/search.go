package ops

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Volltextsuche (S1.7): Abfragesprache in docs/cli.md, Abschnitt 2d;
// Index und Ranking in docs/datenmodell.md, „Volltextsuche".

// SearchRepo führt übersetzte Suchen aus (internal/store).
type SearchRepo interface {
	Search(ctx context.Context, p store.SearchParams) (store.SearchResult, error)
	IndexPending(ctx context.Context, accountID int64) (int, error)
}

// SearchHitOutput ist ein Treffer von messages.search: Kopf und Snippet,
// keine Bodies (Scope mail:search, docs/sicherheit.md).
type SearchHitOutput struct {
	ID             int64    `json:"id"`
	Account        string   `json:"account"`
	ThreadID       int64    `json:"thread_id,omitempty"`
	Date           string   `json:"date,omitempty"`
	From           string   `json:"from"`
	Subject        string   `json:"subject"`
	Snippet        string   `json:"snippet"`
	Flags          []string `json:"flags"`
	HasAttachments bool     `json:"has_attachments"`
	DeletedRemote  bool     `json:"deleted_remote"`
	Score          float64  `json:"score"`
	// Cursor ist die Position nach diesem Treffer: Mit ihm setzt eine
	// Folgeanfrage genau dahinter fort (auch mitten in einer NDJSON-Liste).
	Cursor string `json:"cursor"`
}

// SearchExplain zeigt die Übersetzung der Anfrage (--explain).
type SearchExplain struct {
	Query   string          `json:"query"`
	Scope   string          `json:"scope"` // Konto oder "all accounts"
	Order   string          `json:"order"` // relevance | date
	Terms   []search.Term   `json:"terms"`
	Filters []search.Filter `json:"filters"`
	Ignored []string        `json:"ignored,omitempty"`
	SQL     string          `json:"sql"` // Anweisung mit Parametern (?), ohne Nutzertext
}

// SearchOutput ist die Ausgabe von messages.search (Betreff, Absender und
// Snippet stammen aus Mails: Untrusted-Hülle).
type SearchOutput struct {
	Envelope
	Hits []SearchHitOutput `json:"hits"`
	// Total zählt alle Treffer der Anfrage (0 auf einer leeren Folgeseite).
	Total      int    `json:"total"`
	NextCursor string `json:"next_cursor,omitempty"`
	Order      string `json:"order"`
	// IndexPending: so viele Nachrichten warten noch auf den Index (der
	// nächste Sync nimmt sie auf); sie fehlen in den Treffern.
	IndexPending int            `json:"index_pending"`
	Explain      *SearchExplain `json:"explain,omitempty"`
}

// searchOutputSchema ist das Ausgabeschema von messages.search (auch Teil
// von mailboxes.overview).
const searchOutputSchema = `{
  "type": "object",
  "properties": {
    ` + envelopeProps + `,
    "hits": {"type": "array", "items": {"type": "object", "properties": {
      "id": {"type": "integer"}, "account": {"type": "string"}, "thread_id": {"type": "integer"},
      "date": {"type": "string", "format": "date-time"}, "from": {"type": "string"}, "subject": {"type": "string"},
      "snippet": {"type": "string"},
      "flags": {"type": "array", "items": {"type": "string", "enum": ["seen", "flagged", "answered", "draft"]}},
      "has_attachments": {"type": "boolean"}, "deleted_remote": {"type": "boolean"}, "score": {"type": "number"},
      "cursor": {"type": "string"}},
      "required": ["id", "account", "from", "subject", "snippet", "flags", "has_attachments", "deleted_remote", "score", "cursor"]}},
    "total": {"type": "integer"}, "next_cursor": {"type": "string"},
    "order": {"type": "string", "enum": ["relevance", "date"]}, "index_pending": {"type": "integer"},
    "explain": {"type": "object", "properties": {
      "query": {"type": "string"}, "scope": {"type": "string"}, "order": {"type": "string"},
      "terms": {"type": "array"}, "filters": {"type": "array"}, "ignored": {"type": "array"}, "sql": {"type": "string"}},
      "required": ["query", "scope", "order", "terms", "filters", "sql"]}
  },
  "required": ["untrusted", "source", "notice", "hits", "total", "order", "index_pending"]
}`

// Grenzen von messages.search.
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 500
)

// MessagesSearch durchsucht den lokalen Spiegel: Freitext mit deutscher
// Stammform und Teilwörtern, Phrasen, Ausschluss, Operatoren; Ranking aus
// BM25 und Datum, Cursor-Paginierung. Lesend (R0), Scope mail:search.
func MessagesSearch(accounts AccountRepo, repo SearchRepo) Operation {
	return Operation{
		Name:       "messages.search",
		Scope:      policy.ScopeMailSearch,
		Risk:       policy.R0,
		Idempotent: true,
		Summary:    "Search mirrored messages: words (German word forms, parts of compounds), \"phrases\", -exclusions and operators such as from:, has:attachment, after:.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "query": {"type": "string", "minLength": 1, "maxLength": 1024, "description": "query language, see docs/cli.md"},
    "account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "only this account (default: all)"},
    "mailbox": {"type": "string", "minLength": 1, "description": "only this mailbox: path (with subfolders) or role, like in:"},
    "limit": {"type": "integer", "minimum": 1, "maximum": 500, "description": "hits per page (default 20)"},
    "cursor": {"type": "string", "description": "next_cursor (or a hit's cursor) of the same query"},
    "explain": {"type": "boolean", "description": "include how the query was translated"}
  },
  "required": ["query"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(searchOutputSchema),
		// Ins Audit: Zahl der Suchbegriffe und Art der Operatoren, nie der
		// Suchtext (er kann Namen und Inhalte enthalten).
		Redact: func(raw json.RawMessage) (json.RawMessage, []string) {
			var in struct {
				Query   string `json:"query"`
				Account string `json:"account"`
				Mailbox string `json:"mailbox"`
				Limit   int    `json:"limit"`
				Cursor  string `json:"cursor"`
				Explain bool   `json:"explain"`
			}
			if json.Unmarshal(raw, &in) != nil {
				return json.RawMessage(`{"invalid_input":true}`), nil
			}
			p := map[string]any{"limit": in.Limit, "cursor": in.Cursor != "", "explain": in.Explain, "mailbox": in.Mailbox != ""}
			if q, err := search.Parse(in.Query, nil); err == nil {
				kinds := []string{}
				for _, f := range q.Filters {
					kinds = append(kinds, f.Kind)
				}
				for _, t := range q.Terms {
					if t.Field != "" {
						kinds = append(kinds, t.Field)
					}
				}
				p["terms"], p["operators"] = len(q.Terms), kinds
			} else {
				p["query_error"] = true
			}
			b, _ := json.Marshal(p)
			if AccountNameRE.MatchString(in.Account) {
				return b, []string{"account:" + in.Account}
			}
			return b, nil
		},
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in searchInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			return runSearch(ctx, accounts, repo, in)
		},
	}
}

// searchInput ist die Eingabe von messages.search.
type searchInput struct {
	Query   string `json:"query"`
	Account string `json:"account,omitempty"`
	Mailbox string `json:"mailbox,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Cursor  string `json:"cursor,omitempty"`
	Explain bool   `json:"explain,omitempty"`
}

// runSearch führt eine Suche aus (messages.search, mailboxes.overview).
func runSearch(ctx context.Context, accounts AccountRepo, repo SearchRepo, in searchInput) (SearchOutput, error) {
	switch {
	case in.Limit == 0:
		in.Limit = DefaultSearchLimit
	case in.Limit < 0 || in.Limit > MaxSearchLimit:
		return SearchOutput{}, inputErr("limit must be between 1 and %d", MaxSearchLimit)
	}
	q, err := search.Parse(in.Query, nil)
	if err != nil {
		return SearchOutput{}, queryErr(err)
	}
	if in.Mailbox != "" {
		// wie in:<mailbox> in der Anfrage (MCP: eigenes Feld)
		q.Filters = append(q.Filters, search.Filter{Kind: "mailbox", Value: in.Mailbox})
	}
	params := store.SearchParams{Query: q, Limit: in.Limit, Now: time.Now()}
	scope := "all accounts"
	if in.Account != "" {
		a, err := ResolveAccount(ctx, accounts, in.Account)
		if err != nil {
			return SearchOutput{}, err
		}
		params.AccountID, scope = a.ID, "account "+a.Name
	}
	fp := q.Fingerprint(scope)
	if in.Cursor != "" {
		c, err := search.DecodeCursor(in.Cursor, fp)
		if err != nil {
			return SearchOutput{}, queryErr(err)
		}
		params.After, params.Now = &c, time.UnixMilli(c.Now)
	}
	res, err := repo.Search(ctx, params)
	if err != nil {
		return SearchOutput{}, err
	}
	out := SearchOutput{Envelope: emailEnvelope(), Hits: []SearchHitOutput{}, Total: res.Total, Order: q.Order()}
	if out.IndexPending, err = repo.IndexPending(ctx, params.AccountID); err != nil {
		return SearchOutput{}, err
	}
	for _, h := range res.Hits {
		c := search.Cursor{Fingerprint: fp, Now: params.Now.UnixMilli(), Score: h.Score, ID: h.ID}
		o := SearchHitOutput{ID: h.ID, Account: h.Account, ThreadID: h.ThreadID, Date: rfc3339(h.Date), From: h.From,
			Subject: h.Subject, Snippet: h.Snippet, Flags: []string{}, HasAttachments: h.HasAttachments,
			DeletedRemote: h.DeletedRemote, Score: h.Score, Cursor: c.Encode()}
		for _, f := range []struct {
			on   bool
			name string
		}{{h.Seen, "seen"}, {h.Flagged, "flagged"}, {h.Answered, "answered"}, {h.Draft, "draft"}} {
			if f.on {
				o.Flags = append(o.Flags, f.name)
			}
		}
		out.Hits = append(out.Hits, o)
	}
	if len(out.Hits) == in.Limit {
		out.NextCursor = out.Hits[len(out.Hits)-1].Cursor
	}
	if in.Explain {
		out.Explain = &SearchExplain{Query: in.Query, Scope: scope, Order: q.Order(), Terms: q.Terms, Filters: q.Filters,
			Ignored: q.Ignored, SQL: store.SearchSQL(params)}
	}
	return out, nil
}

// queryErr macht aus Fehlern der Abfragesprache Eingabefehler (Exit 2).
func queryErr(err error) error {
	var qe *search.QueryError
	if errors.As(err, &qe) {
		return inputErr("%s", qe.Msg)
	}
	return err
}
