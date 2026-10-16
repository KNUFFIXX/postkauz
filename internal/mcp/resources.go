package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Resources (docs/mcp.md, Abschnitt 3): Lesequellen für @-Verweise. Jede
// Resource ruft Operationen über den Executor auf, mit denselben Scopes,
// demselben Audit und derselben Hülle wie die Tools. Sichtbar ist nur, was
// der Schlüssel lesen darf.

type resourceBinding struct {
	uri      string // feste URI oder URI-Vorlage (RFC 6570)
	template bool
	name     string
	desc     string
	ops      []string // benötigte Operationen (Scope-Filter)
}

var resources = []resourceBinding{
	{uri: "mail://accounts", name: "accounts", ops: []string{"accounts.list"},
		desc: "Accounts with last sync time and unread counts."},
	{uri: "mail://policy", name: "policy", ops: []string{"policy.get"},
		desc: "The policy of the key this server runs with: scopes, allowed operations, budgets."},
	{uri: "mail://audit", name: "audit", ops: []string{"audit.list"},
		desc: "Audit log of the last 24 hours (never message content); mail://audit?since=7d for longer."},
	{uri: "mail://audit{?since}", template: true, name: "audit-since", ops: []string{"audit.list"},
		desc: "Audit log since a time: duration back from now (10m, 24h, 7d), a date or RFC 3339."},
	{uri: "mail://{account}/mailboxes", template: true, name: "mailboxes", ops: []string{"mailboxes.list"},
		desc: "Mailboxes of an account with roles and counters."},
	{uri: "mail://{account}/mailbox/{id}/overview{?since}", template: true, name: "mailbox-overview",
		ops:  []string{"mailboxes.list", "messages.search"},
		desc: "Newest headers of a mailbox (id from the mailboxes resource): unread first, then read; optional since (date or duration)."},
	{uri: "mail://{account}/message/{id}", template: true, name: "message", ops: []string{"messages.get"},
		desc: "One message: headers, sender checks, cleaned text without quotes (untrusted), links, attachments."},
	{uri: "mail://{account}/thread/{id}", template: true, name: "thread", ops: []string{"threads.get"},
		desc: "A thread in date order with the new text of each message (untrusted)."},
	{uri: "mail://{account}/message/{id}/attachment/{n}", template: true, name: "attachment", ops: []string{"attachments.text"},
		desc: "Extracted text of attachment n (attachment id from the message) of a message (untrusted)."},
}

func (h *handler) covers(names []string) bool {
	for _, n := range names {
		op, err := h.o.Executor.Registry.Get(n)
		if err != nil || !h.o.Key.Covers(op.Scope) {
			return false
		}
	}
	return true
}

func (h *handler) addResources(s *sdk.Server) error {
	for _, r := range resources {
		if !h.covers(r.ops) {
			continue
		}
		if r.template {
			s.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: r.uri, Name: r.name, Description: r.desc,
				MIMEType: "application/json"}, h.readResource)
		} else {
			s.AddResource(&sdk.Resource{URI: r.uri, Name: r.name, Description: r.desc, MIMEType: "application/json"}, h.readResource)
		}
	}
	return nil
}

// readResource übersetzt eine mail://-URI in Operationen (eine Tabelle von
// Fällen, keine Fachlogik) und liefert deren Ausgabe als JSON.
func (h *handler) readResource(ctx context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
	uri := req.Params.URI
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "mail" {
		return nil, sdk.ResourceNotFoundError(uri)
	}
	seg := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(seg) == 1 && seg[0] == "" {
		seg = nil
	}
	q := u.Query()
	in := map[string]any{}
	var op string
	var out any
	switch {
	case u.Host == "accounts" && len(seg) == 0:
		op = "accounts.list"
	case u.Host == "policy" && len(seg) == 0:
		op = "policy.get"
	case u.Host == "audit" && len(seg) == 0:
		op = "audit.list"
		if v := q.Get("since"); v != "" {
			in["since"] = v
		}
	case len(seg) == 1 && seg[0] == "mailboxes":
		op, in["account"] = "mailboxes.list", u.Host
	case len(seg) == 3 && seg[0] == "mailbox" && seg[2] == "overview":
		out, err = h.overview(ctx, u.Host, seg[1], q.Get("since"))
		if err != nil {
			return nil, resourceErr(uri, err)
		}
	case len(seg) == 2 && seg[0] == "message":
		id, ok := positive(seg[1])
		if !ok {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		op, in = "messages.get", map[string]any{"id": id, "account": u.Host, "strip_quotes": true, "max_chars": ops.DefaultMaxChars}
	case len(seg) == 2 && seg[0] == "thread":
		id, ok := positive(seg[1])
		if !ok {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		op, in = "threads.get", map[string]any{"id": id, "text": true, "max_messages": threadMaxMessages, "max_chars_per_message": threadMaxChars}
	case len(seg) == 4 && seg[0] == "message" && seg[2] == "attachment":
		msg, ok1 := positive(seg[1])
		att, ok2 := positive(seg[3])
		if !ok1 || !ok2 {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		op, in = "attachments.text", map[string]any{"id": att, "max_chars": ops.DefaultMaxChars}
		res, err := h.callMap(ctx, op, in)
		if err != nil {
			return nil, resourceErr(uri, err)
		}
		// Der Anhang muss zu Nachricht und Konto der URI gehören.
		if o, ok := res.Output.(ops.AttachmentTextOutput); !ok || o.MessageID != msg || !h.inAccount(ctx, msg, u.Host) {
			return nil, sdk.ResourceNotFoundError(uri)
		}
		out = res.Output
	default:
		return nil, sdk.ResourceNotFoundError(uri)
	}
	if out == nil {
		res, err := h.callMap(ctx, op, in)
		if err != nil {
			return nil, resourceErr(uri, err)
		}
		out = res.Output
		// Ein Thread gehört zu genau einem Konto: Er muss das der URI sein.
		if t, ok := out.(ops.ThreadOutput); ok && t.Account != u.Host {
			return nil, sdk.ResourceNotFoundError(uri)
		}
	}
	_, text, err := outputValue(out)
	if err != nil {
		return nil, err
	}
	return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: "application/json", Text: string(text)}}}, nil
}

func (h *handler) callMap(ctx context.Context, op string, in map[string]any) (ops.Result, error) {
	b, err := json.Marshal(in)
	if err != nil {
		return ops.Result{}, err
	}
	return h.call(ctx, op, b)
}

// inAccount prüft über messages.get, ob die Nachricht zum Konto gehört.
func (h *handler) inAccount(ctx context.Context, id int64, account string) bool {
	_, err := h.callMap(ctx, "messages.get", map[string]any{"id": id, "account": account, "headers": true, "max_chars": 1})
	return err == nil
}

// overviewOutput: neueste Köpfe eines Ordners, ungelesene zuerst. Beide
// Teile sind unveränderte Ausgaben von messages.search.
type overviewOutput struct {
	Account string           `json:"account"`
	Mailbox string           `json:"mailbox"`
	Unread  ops.SearchOutput `json:"unread"`
	Read    ops.SearchOutput `json:"read"`
}

func (h *handler) overview(ctx context.Context, account, idStr, since string) (any, error) {
	id, ok := positive(idStr)
	if !ok {
		return nil, store.ErrMessageNotFound
	}
	res, err := h.callMap(ctx, "mailboxes.list", map[string]any{"account": account})
	if err != nil {
		return nil, err
	}
	path := ""
	for _, m := range res.Output.(ops.MailboxesListOutput).Mailboxes {
		if m.ID == id {
			path = m.Path
		}
	}
	if path == "" {
		return nil, store.ErrMessageNotFound
	}
	query := "is:unread"
	if since != "" {
		t, err := ops.ParseSince(since, time.Now())
		if err != nil {
			return nil, err
		}
		query += " after:" + t.Local().Format("2006-01-02")
	}
	out := overviewOutput{Account: account, Mailbox: path}
	for i, q := range []string{query, strings.Replace(query, "is:unread", "is:read", 1)} {
		r, err := h.callMap(ctx, "messages.search", map[string]any{"query": q, "account": account, "mailbox": path, "limit": 25})
		if err != nil {
			return nil, err
		}
		if i == 0 {
			out.Unread = r.Output.(ops.SearchOutput)
		} else {
			out.Read = r.Output.(ops.SearchOutput)
		}
	}
	return out, nil
}

func positive(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// resourceErr: Nicht gefunden bleibt „nicht gefunden"; verweigert und
// andere Fehler kommen mit Grund zurück (ohne Inhalte).
func resourceErr(uri string, err error) error {
	te := classify(err)
	if te.Code == "not_found" {
		return sdk.ResourceNotFoundError(uri)
	}
	var pe *ops.PolicyError
	if errors.As(err, &pe) {
		return errors.New(te.Status + ": " + te.Reason)
	}
	return errors.New(cleanString(te.Message))
}
