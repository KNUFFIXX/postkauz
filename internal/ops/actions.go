package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
	"github.com/KNUFFIXX/postkauz/internal/tags"
)

// Postfach-Aktionen und Tags (A03 S3.1, S3.2; E1 bis E5, Teil T2). Die
// Fachlogik liegt in internal/actions und internal/tags; hier stehen nur
// Schemas, Eingabe und Ausgabe. Was auf den Server schreibt, ist eine
// Aktion mit Job (mailbox_action), ausgeführt vom Prozess mit
// Zugangsdaten (ADR-009).

// ActionStore ist der Teil des Stores für Aktionen und Tags.
type ActionStore interface {
	actions.Store
	ChangeTags(ctx context.Context, c store.TagChange) (store.TagResult, error)
	MessageTags(ctx context.Context, ids []int64) (map[int64][]string, error)
	ListTags(ctx context.Context, accountID int64) ([]store.TagCount, error)
	CopyAt(ctx context.Context, mailboxID int64, uid uint32) (store.ServerCopy, string, bool, error)
	Approval(ctx context.Context, id int64) (store.Approval, error)
}

// actionOps bündelt die Abhängigkeiten der Aktions-Operationen.
type actionOps struct {
	accounts AccountRepo
	st       ActionStore
	svc      *actions.Service
	keywords bool
	operator string
	drafts   *drafts.Service
}

func newActionOps(d Deps) *actionOps {
	o := &actionOps{accounts: d.Accounts, st: d.Actions, keywords: d.SyncKeywords, operator: d.Draft.Operator, drafts: d.Drafts}
	ttl := time.Duration(0)
	if d.Drafts != nil {
		ttl = d.Drafts.ApprovalTTL
	}
	if d.Actions != nil {
		o.svc = &actions.Service{Store: d.Actions, TTL: ttl}
	}
	return o
}

// actionErr übersetzt Ablehnungen aus internal/actions in DeniedError.
func actionErr(err error) error {
	var ae *actions.Error
	if errors.As(err, &ae) {
		return &DeniedError{Reason: ae.Reason, Msg: ae.Msg}
	}
	return err
}

// Schemateile.
const (
	idsSchema = `"ids": {"type": "array", "items": {"type": "integer", "minimum": 1}, "minItems": 1, "maxItems": 10000,
      "description": "local message ids from search results of the same account (at most 1000 per call)"}`
	accountProp     = `"account": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$", "description": "account (default: the only one)"}`
	confirmProp     = `"confirm_count": {"type": "integer", "minimum": 1, "description": "with a key and more ids than [move] bulk_confirm_threshold: exactly the number of ids"}`
	fromProp        = `"from": {"type": "string", "minLength": 1, "maxLength": 1000, "description": "source mailbox path, needed when a message is in several mailboxes"}`
	actionOutSchema = `{
  "type": "object",
  "properties": {
    "dry_run": {"type": "boolean"}, "account": {"type": "string"}, "kind": {"type": "string"},
    "status": {"type": "string", "enum": ["queued", "needs_approval"], "description": "queued: the job applies it; needs_approval: only a request was filed"},
    "action_id": {"type": "integer"}, "approval_id": {"type": "integer"},
    "messages": {"type": "integer"}, "server_copies": {"type": "integer"},
    "skipped": {"type": "array", "items": {"type": "integer"}, "description": "ids already in the target mailbox"},
    "target": {"type": "object", "properties": {"mailbox": {"type": "string"}, "role": {"type": "string"}}},
    "remote": {"type": "string", "enum": ["pending", "waits_for_approval"]},
    "next_step": {"type": "string"}, "human_only": {"type": "boolean"}, "expires_at": {"type": "string", "format": "date-time"}
  },
  "required": ["dry_run", "account", "kind", "status", "messages", "server_copies", "skipped", "remote", "next_step"]
}`
)

// ActionOutput ist das Ergebnis von messages.mark, messages.move und
// messages.trash.
type ActionOutput struct {
	DryRun       bool          `json:"dry_run"`
	Account      string        `json:"account"`
	Kind         string        `json:"kind"`
	Status       string        `json:"status"`
	ActionID     int64         `json:"action_id,omitempty"`
	ApprovalID   int64         `json:"approval_id,omitempty"`
	Messages     int           `json:"messages"`
	ServerCopies int           `json:"server_copies"`
	Skipped      []int64       `json:"skipped"`
	Target       *ActionTarget `json:"target,omitempty"`
	Remote       string        `json:"remote"`
	NextStep     string        `json:"next_step"`
	HumanOnly    bool          `json:"human_only,omitempty"`
	ExpiresAt    string        `json:"expires_at,omitempty"`
}

// ActionTarget nennt den Zielordner.
type ActionTarget struct {
	Mailbox string `json:"mailbox"`
	Role    string `json:"role,omitempty"`
}

// nextQueued ist der Hinweis zu einer eingereihten Aktion (ehrlich wie bei
// Entwürfen: ADR-009).
func nextQueued(id int64) string {
	if id == 0 {
		return "dry run: nothing was queued; call again with dry_run=false"
	}
	return fmt.Sprintf("queued: a running %s sync --watch applies it on the server within 10 s, otherwise the next %s sync; "+
		"check with: %s action show %d", brand.Name, brand.Name, brand.Name, id)
}

// nextApproval ist der Hinweis zu einem Antrag (wie next_step bei Entwürfen, A2).
func nextApproval(id int64) string {
	if id == 0 {
		return "dry run: no request was filed; call again with dry_run=false to request it"
	}
	return fmt.Sprintf("A human approves with: %s action approve %d — or rejects with: %s action reject %d. "+
		"This server cannot approve; nothing happens before that.", brand.Name, id, brand.Name, id)
}

// actionInput ist die gemeinsame Eingabe der Aktionen.
type actionInput struct {
	Account      string  `json:"account,omitempty"`
	IDs          []int64 `json:"ids"`
	ConfirmCount *int    `json:"confirm_count,omitempty"`
	From         string  `json:"from,omitempty"`
	// mark
	Seen    *bool `json:"seen,omitempty"`
	Flagged *bool `json:"flagged,omitempty"`
	// move
	Mailbox string `json:"mailbox,omitempty"`
	Role    string `json:"role,omitempty"`
	// tag
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// redactAction: Anzahl, Hash der sortierten IDs, die ersten 20 IDs, Flags,
// Zielrolle, confirm_count, Tag-Namen (S3.1 Akzeptanz 4: Tag-Namen dürfen
// ins Audit); nie Ordnerpfade (Namen des Betreibers) oder Inhalte.
func redactAction(raw json.RawMessage) (json.RawMessage, []string) {
	var in actionInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	ids := actions.Unique(in.IDs)
	first := ids[:min(len(ids), 20)]
	p := map[string]any{"count": len(ids), "ids_hash": actions.IDsHash(ids), "ids": first}
	for k, v := range map[string]*bool{"seen": in.Seen, "flagged": in.Flagged} {
		if v != nil {
			p[k] = *v
		}
	}
	if in.ConfirmCount != nil {
		p["confirm_count"] = *in.ConfirmCount
	}
	if in.Role != "" {
		p["role"] = in.Role
	}
	if in.Mailbox != "" {
		p["mailbox"] = true
	}
	if in.From != "" {
		p["from"] = true
	}
	if t, err := tags.Normalize(in.Add); err == nil && len(t) > 0 {
		p["add"] = t
	}
	if t, err := tags.Normalize(in.Remove); err == nil && len(t) > 0 {
		p["remove"] = t
	}
	b, _ := json.Marshal(p)
	var targets []string
	if AccountNameRE.MatchString(in.Account) {
		targets = append(targets, "account:"+in.Account)
	}
	for _, id := range first {
		targets = append(targets, "message:"+strconv.FormatInt(id, 10))
	}
	return b, targets
}

// prepare löst Konto und IDs auf (gemeinsam für alle Aktionen).
func (o *actionOps) prepare(ctx context.Context, call Call) (actionInput, store.Account, []int64, error) {
	var in actionInput
	if err := decodeInput(call.Input, &in); err != nil {
		return in, store.Account{}, nil, err
	}
	a, err := ResolveAccount(ctx, o.accounts, in.Account)
	if err != nil {
		return in, a, nil, err
	}
	ids := actions.Unique(in.IDs)
	if err := o.svc.CheckAccount(ctx, a.ID, ids); err != nil {
		return in, a, nil, err
	}
	return in, a, ids, nil
}

// facts liefert der Policy die Eingabe einer Aktion (A9).
func (o *actionOps) facts(action string) func(ctx context.Context, raw json.RawMessage) (policy.Facts, error) {
	return func(ctx context.Context, raw json.RawMessage) (policy.Facts, error) {
		var in actionInput
		if err := json.Unmarshal(raw, &in); err != nil {
			return policy.Facts{}, inputErr("invalid input")
		}
		f := policy.Facts{Action: action, Targets: len(actions.Unique(in.IDs)), ConfirmCount: in.ConfirmCount}
		if action == policy.ActionMove {
			f.TargetRole = in.Role
			if in.Mailbox != "" {
				// Rolle des Zielordners aus dem Spiegel (archive_only, Papierkorb).
				a, err := ResolveAccount(ctx, o.accounts, in.Account)
				if err != nil {
					return f, err
				}
				t, ok, err := o.svc.FindMailbox(ctx, a.ID, in.Mailbox, "")
				if err != nil {
					return f, err
				}
				if ok {
					f.TargetRole = t.Role
				}
			}
		}
		return f, nil
	}
}

func (o *actionOps) create(ctx context.Context, call Call, r actions.Request, out *ActionOutput) error {
	out.Messages = len(r.IDs) - len(r.Plan.Skipped)
	out.ServerCopies = len(r.Plan.Targets)
	out.Skipped = r.Plan.Skipped
	if out.Skipped == nil {
		out.Skipped = []int64{}
	}
	if call.DryRun {
		if r.Approval {
			out.NextStep = nextApproval(0)
		} else {
			out.NextStep = nextQueued(0)
		}
		return nil
	}
	r.RequestedBy, r.AuditID = call.Key.ID, call.AuditID
	act, err := o.svc.Create(ctx, r)
	if err != nil {
		return err
	}
	out.ActionID = act.ID
	if r.Approval {
		out.ApprovalID = act.ApprovalID
		out.NextStep = nextApproval(act.ID)
		if act.ApprovalID > 0 {
			if ap, err := o.st.Approval(ctx, act.ApprovalID); err == nil {
				out.ExpiresAt = rfc3339(ap.ExpiresAt)
			}
		}
	} else {
		out.NextStep = nextQueued(act.ID)
	}
	return nil
}

// MessagesMark setzt gelesen/ungelesen bzw. markiert (S3.1, R2, Scope
// mail:mark seit der Nachbesserung T2, V20): sofort im Spiegel (pending),
// auf dem Server per Job.
func MessagesMark(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "messages.mark", Scope: policy.ScopeMailMark, Risk: policy.R2, Idempotent: true,
		Summary: "Mark messages as read or unread, flagged or unflagged; the change shows at once and reaches the server through the sync job.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + accountProp + `, ` + idsSchema + `,
    "seen": {"type": "boolean", "description": "true = read, false = unread"},
    "flagged": {"type": "boolean", "description": "true = flagged, false = not flagged"}, ` + confirmProp + `},
  "required": ["ids"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(actionOutSchema),
		Redact:       redactAction,
		PolicyInput:  o.facts(policy.ActionMark),
		Handler: func(ctx context.Context, call Call) (any, error) {
			in, a, ids, err := o.prepare(ctx, call)
			if err != nil {
				return nil, err
			}
			if in.Seen == nil && in.Flagged == nil {
				return nil, inputErr("nothing to mark: give seen and/or flagged")
			}
			plan, err := o.svc.PlanFlags(ctx, ids)
			if err != nil {
				return nil, actionErr(err)
			}
			out := ActionOutput{DryRun: call.DryRun, Account: a.Name, Kind: store.ActionMark, Status: "queued", Remote: "pending"}
			err = o.create(ctx, call, actions.Request{AccountID: a.ID, AccountName: a.Name, Kind: store.ActionMark, IDs: ids, Plan: plan,
				ConfirmCount: in.ConfirmCount, Params: store.ActionParams{Mark: &store.MarkParams{Seen: in.Seen, Flagged: in.Flagged}}}, &out)
			if err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

// moveRoles sind die Rollen, die messages.move als Ziel nennt (ohne
// trash: der Papierkorb geht nur über messages.trash). junk, sent und
// drafts nur für den Menschen ohne Schlüssel (policy.HumanOnlyMoveRoles,
// V8); das Schema ist für alle Oberflächen gleich, deshalb lehnt die
// Policy ab (target_role_human_only), nicht das Schema.
var moveRoles = []string{"inbox", "archive", "all", "junk", "sent", "drafts"}

// MessagesMove verschiebt Nachrichten (S3.2, R2, Scope mail:move; nur mit
// IMAP MOVE, E2). In Gmail ist „Label entfernen“ das Verschieben aus dem
// Label-Ordner in den Ordner mit Rolle all (Vorgang 1).
func MessagesMove(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "messages.move", Scope: policy.ScopeMailMove, Risk: policy.R2, Idempotent: true,
		Summary: "Move messages to another mailbox of the same account (by path or role; Gmail: role all removes a label); applied by the sync job.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + accountProp + `, ` + idsSchema + `,
    "mailbox": {"type": "string", "minLength": 1, "maxLength": 1000, "description": "target mailbox path"},
    "role": {"type": "string", "enum": ["inbox", "archive", "all", "junk", "sent", "drafts"], "description": "target mailbox by role (archive, or all = Gmail All Mail); junk, sent and drafts only for a human without a key"},
    ` + fromProp + `, ` + confirmProp + `},
  "required": ["ids"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(actionOutSchema),
		Redact:       redactAction,
		PolicyInput:  o.facts(policy.ActionMove),
		Handler: func(ctx context.Context, call Call) (any, error) {
			in, a, ids, err := o.prepare(ctx, call)
			if err != nil {
				return nil, err
			}
			if (in.Mailbox == "") == (in.Role == "") {
				return nil, inputErr("give exactly one of mailbox or role")
			}
			dest, ok, err := o.svc.FindMailbox(ctx, a.ID, in.Mailbox, in.Role)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, &DeniedError{Reason: actions.ReasonTargetNotFound, Msg: "the target mailbox is not in the mirror of this account; run sync first"}
			}
			plan, err := o.svc.PlanMove(ctx, ids, in.From, dest)
			if err != nil {
				return nil, actionErr(err)
			}
			out := ActionOutput{DryRun: call.DryRun, Account: a.Name, Kind: store.ActionMove, Status: "queued", Remote: "pending",
				Target: &ActionTarget{Mailbox: dest.Path, Role: dest.Role}}
			err = o.create(ctx, call, actions.Request{AccountID: a.ID, AccountName: a.Name, Kind: store.ActionMove, IDs: ids, Plan: plan,
				ConfirmCount: in.ConfirmCount, Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}}, &out)
			if err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

// MessagesTrash beantragt das Verschieben in den Papierkorb (S3.2, R3,
// Scope mail:trash, E3): Jeder Aufruf legt nur einen Antrag an
// (needs_approval); ausgeführt wird nach actions.approve durch einen
// Menschen. Nie endgültig: kein Expunge (E2).
func MessagesTrash(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "messages.trash", Scope: policy.ScopeMailTrash, Risk: policy.R3, ApprovalRequest: true,
		Summary: "Request moving messages to the trash (never deleted for good); a human approves the request before anything happens.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + accountProp + `, ` + idsSchema + `, ` + fromProp + `, ` + confirmProp + `},
  "required": ["ids"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(actionOutSchema),
		Redact:       redactAction,
		PolicyInput:  o.facts(policy.ActionTrash),
		Handler: func(ctx context.Context, call Call) (any, error) {
			in, a, ids, err := o.prepare(ctx, call)
			if err != nil {
				return nil, err
			}
			dest, ok, err := o.svc.FindMailbox(ctx, a.ID, "", "trash")
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, &DeniedError{Reason: actions.ReasonNoTrashMailbox, Msg: "the account has no mailbox with role trash in the mirror; run sync first"}
			}
			plan, err := o.svc.PlanMove(ctx, ids, in.From, dest)
			if err != nil {
				return nil, actionErr(err)
			}
			out := ActionOutput{DryRun: call.DryRun, Account: a.Name, Kind: store.ActionTrash, Status: "needs_approval",
				Remote: "waits_for_approval", HumanOnly: true, Target: &ActionTarget{Mailbox: dest.Path, Role: dest.Role}}
			err = o.create(ctx, call, actions.Request{AccountID: a.ID, AccountName: a.Name, Kind: store.ActionTrash, IDs: ids, Plan: plan,
				ConfirmCount: in.ConfirmCount, Approval: true,
				Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}}, &out)
			if err != nil {
				return nil, err
			}
			return out, nil
		},
	}
}

// TagOutput ist das Ergebnis von messages.tag.
type TagOutput struct {
	DryRun   bool             `json:"dry_run"`
	Account  string           `json:"account"`
	Messages []TaggedMessage  `json:"messages"`
	Added    int              `json:"added"`
	Removed  int              `json:"removed"`
	Keywords KeywordSyncState `json:"keywords"`
}

// TaggedMessage nennt die Tags einer Nachricht nach dem Aufruf.
type TaggedMessage struct {
	ID   int64    `json:"id"`
	Tags []string `json:"tags"`
}

// KeywordSyncState sagt, ob die Tags auch als IMAP-Keywords auf den Server
// gehen ([tags] sync_keywords, E5).
type KeywordSyncState struct {
	Status   string `json:"status"` // off | queued | no_server_copy | dry_run | local_only
	ActionID int64  `json:"action_id,omitempty"`
	// LocalOnly: Tags, deren Name ein System-Keyword ist (junk, nonjunk,
	// notjunk …); sie bleiben lokal und gehen nie auf den Server (V16).
	LocalOnly []string `json:"local_only,omitempty"`
}

// MessagesTag setzt und entfernt lokale Tags (S3.1, R1, Scope mail:tag,
// E5). Mit [tags] sync_keywords = true reiht es zusätzlich eine Aktion
// keywords ein (nur Ordner mit PERMANENTFLAGS \*).
func MessagesTag(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "messages.tag", Scope: policy.ScopeMailTag, Risk: policy.R1, Idempotent: true,
		Summary: "Add or remove local tags on messages (lowercase letters, digits, '.', '_', '-'); search them with tag:<name>.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {` + accountProp + `, ` + idsSchema + `,
    "add": {"type": "array", "items": {"type": "string", "maxLength": 100}, "maxItems": 20},
    "remove": {"type": "array", "items": {"type": "string", "maxLength": 100}, "maxItems": 20}, ` + confirmProp + `},
  "required": ["ids"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "dry_run": {"type": "boolean"}, "account": {"type": "string"},
    "messages": {"type": "array", "items": {"type": "object", "properties": {"id": {"type": "integer"},
      "tags": {"type": "array", "items": {"type": "string"}}}, "required": ["id", "tags"]}},
    "added": {"type": "integer"}, "removed": {"type": "integer"},
    "keywords": {"type": "object", "properties": {"status": {"type": "string", "enum": ["off", "queued", "no_server_copy", "dry_run", "local_only"]},
      "action_id": {"type": "integer"},
      "local_only": {"type": "array", "items": {"type": "string"}, "description": "tags named like a system keyword (junk, nonjunk, notjunk …): they stay local and never reach the server"}},
      "required": ["status"]}
  },
  "required": ["dry_run", "account", "messages", "added", "removed", "keywords"]
}`),
		Redact:      redactAction,
		PolicyInput: o.facts(""),
		Handler: func(ctx context.Context, call Call) (any, error) {
			in, a, ids, err := o.prepare(ctx, call)
			if err != nil {
				return nil, err
			}
			add, err := tags.Normalize(in.Add)
			if err != nil {
				return nil, inputErr("add: %v", err)
			}
			remove, err := tags.Normalize(in.Remove)
			if err != nil {
				return nil, inputErr("remove: %v", err)
			}
			if len(add)+len(remove) == 0 {
				return nil, inputErr("nothing to do: give add and/or remove")
			}
			for _, t := range add {
				if slices.Contains(remove, t) {
					return nil, inputErr("a tag is both in add and remove")
				}
			}
			out := TagOutput{DryRun: call.DryRun, Account: a.Name, Keywords: KeywordSyncState{Status: "off"}}
			var cur map[int64][]string
			if call.DryRun {
				if cur, err = o.st.MessageTags(ctx, ids); err != nil {
					return nil, err
				}
				for id, ts := range cur {
					for _, t := range add {
						if !slices.Contains(ts, t) {
							ts = append(ts, t)
							out.Added++
						}
					}
					kept := ts[:0]
					for _, t := range ts {
						if slices.Contains(remove, t) {
							out.Removed++
							continue
						}
						kept = append(kept, t)
					}
					slices.Sort(kept)
					cur[id] = kept
				}
				if o.keywords {
					out.Keywords.Status = "dry_run"
					_, l1 := tags.SplitReserved(add)
					_, l2 := tags.SplitReserved(remove)
					out.Keywords.LocalOnly = append(l1, l2...)
				}
			} else {
				res, err := o.st.ChangeTags(ctx, store.TagChange{AccountID: a.ID, MessageIDs: ids, Add: add, Remove: remove, SetBy: call.Key.ID})
				if err != nil {
					return nil, err
				}
				cur, out.Added, out.Removed = res.Tags, res.Added, res.Removed
				if o.keywords {
					if out.Keywords, err = o.queueKeywords(ctx, call, a, ids, add, remove, in.ConfirmCount); err != nil {
						return nil, err
					}
				}
			}
			for _, id := range ids {
				out.Messages = append(out.Messages, TaggedMessage{ID: id, Tags: cur[id]})
			}
			return out, nil
		},
	}
}

// queueKeywords reiht den Abgleich der Tags als IMAP-Keywords ein (E5),
// für alle Serverkopien der Nachrichten; Nachrichten ohne Serverkopie
// bleiben nur lokal getaggt.
func (o *actionOps) queueKeywords(ctx context.Context, call Call, a store.Account, ids []int64, add, remove []string, confirm *int) (KeywordSyncState, error) {
	// V16: Tags mit dem Namen eines System-Keywords bleiben lokal.
	add, localAdd := tags.SplitReserved(add)
	remove, localRemove := tags.SplitReserved(remove)
	local := append(localAdd, localRemove...)
	if len(add)+len(remove) == 0 {
		return KeywordSyncState{Status: "local_only", LocalOnly: local}, nil
	}
	st, err := o.queueKeywordAction(ctx, call, a, ids, add, remove, confirm)
	st.LocalOnly = local
	return st, err
}

// queueKeywordAction reiht die Aktion keywords für die übrigen Tags ein.
// confirm (confirm_count des Aufrufs) steht in der Aktion, damit die
// Prüfung der Policy vor der Ausführung (V3) dieselbe Zahl sieht.
func (o *actionOps) queueKeywordAction(ctx context.Context, call Call, a store.Account, ids []int64, add, remove []string, confirm *int) (KeywordSyncState, error) {
	var plan actions.Plan
	for _, id := range ids {
		p, err := o.svc.PlanFlags(ctx, []int64{id})
		if err != nil {
			var ae *actions.Error
			if errors.As(err, &ae) {
				continue
			}
			return KeywordSyncState{}, err
		}
		plan.Targets = append(plan.Targets, p.Targets...)
	}
	if len(plan.Targets) == 0 {
		return KeywordSyncState{Status: "no_server_copy"}, nil
	}
	act, err := o.svc.Create(ctx, actions.Request{AccountID: a.ID, AccountName: a.Name, Kind: store.ActionKeywords, IDs: ids, Plan: plan,
		ConfirmCount: confirm, RequestedBy: call.Key.ID, AuditID: call.AuditID, Params: store.ActionParams{AddKeywords: add, RemoveKeywords: remove}})
	if err != nil {
		return KeywordSyncState{}, err
	}
	return KeywordSyncState{Status: "queued", ActionID: act.ID}, nil
}

// ActionView ist eine Aktion zum Lesen (actions.get, actions.list): Art,
// Zustand, Herkunft, Ziele mit Ordner, UID und Ergebnis; nie Inhalt.
type ActionView struct {
	ID           int64              `json:"id"`
	Account      string             `json:"account"`
	Kind         string             `json:"kind"`
	State        string             `json:"state"`
	Messages     int                `json:"messages"`
	IDsHash      string             `json:"ids_hash"`
	ConfirmCount *int               `json:"confirm_count,omitempty"`
	RequestedBy  string             `json:"requested_by"`
	ApprovalID   int64              `json:"approval_id,omitempty"`
	Error        string             `json:"error,omitempty"`
	Seen         *bool              `json:"seen,omitempty"`
	Flagged      *bool              `json:"flagged,omitempty"`
	Target       *ActionTarget      `json:"target,omitempty"`
	AddKeywords  []string           `json:"add_keywords,omitempty"`
	RemoveKW     []string           `json:"remove_keywords,omitempty"`
	CreatedAt    string             `json:"created_at"`
	UpdatedAt    string             `json:"updated_at"`
	DoneAt       string             `json:"done_at,omitempty"`
	Targets      []ActionTargetView `json:"targets,omitempty"`
	NextStep     string             `json:"next_step,omitempty"`
}

// ActionTargetView ist ein Ziel einer Aktion.
type ActionTargetView struct {
	MessageID   int64  `json:"message_id"`
	Mailbox     string `json:"mailbox"`
	UID         uint32 `json:"uid"`
	UIDValidity uint32 `json:"uidvalidity"`
	Result      string `json:"result"`
	NewUID      uint32 `json:"new_uid,omitempty"`
}

const actionViewSchema = `{"type": "object", "properties": {
      "id": {"type": "integer"}, "account": {"type": "string"}, "kind": {"type": "string", "enum": ["mark", "move", "trash", "keywords"]},
      "state": {"type": "string", "enum": ["requested", "queued", "running", "done", "partial", "failed", "rejected", "expired"]},
      "messages": {"type": "integer"}, "ids_hash": {"type": "string"}, "confirm_count": {"type": "integer"},
      "requested_by": {"type": "string"}, "approval_id": {"type": "integer"}, "error": {"type": "string"},
      "seen": {"type": "boolean"}, "flagged": {"type": "boolean"},
      "target": {"type": "object", "properties": {"mailbox": {"type": "string"}, "role": {"type": "string"}}},
      "add_keywords": {"type": "array", "items": {"type": "string"}}, "remove_keywords": {"type": "array", "items": {"type": "string"}},
      "created_at": {"type": "string"}, "updated_at": {"type": "string"}, "done_at": {"type": "string"},
      "targets": {"type": "array", "items": {"type": "object", "properties": {
        "message_id": {"type": "integer"}, "mailbox": {"type": "string"}, "uid": {"type": "integer"}, "uidvalidity": {"type": "integer"},
        "result": {"type": "string", "description": "empty = open; ok, not_found, stale, unsupported, failed, skipped"},
        "new_uid": {"type": "integer"}}, "required": ["message_id", "mailbox", "uid", "uidvalidity", "result"]}},
      "next_step": {"type": "string"}},
    "required": ["id", "account", "kind", "state", "messages", "ids_hash", "requested_by", "created_at", "updated_at"]}`

func (o *actionOps) view(ctx context.Context, a store.Action, withTargets bool) (ActionView, error) {
	name := ""
	if list, err := o.accounts.ListAccounts(ctx); err == nil {
		for _, s := range list {
			if s.ID == a.AccountID {
				name = s.Name
			}
		}
	}
	v := ActionView{ID: a.ID, Account: name, Kind: a.Kind, State: a.State, Messages: a.TargetCount, IDsHash: a.IDsHash,
		ConfirmCount: a.ConfirmCount, RequestedBy: a.RequestedBy, ApprovalID: a.ApprovalID, Error: a.Error,
		AddKeywords: a.Params.AddKeywords, RemoveKW: a.Params.RemoveKeywords,
		CreatedAt: rfc3339(a.CreatedAt), UpdatedAt: rfc3339(a.UpdatedAt), DoneAt: rfc3339(a.DoneAt)}
	if m := a.Params.Mark; m != nil {
		v.Seen, v.Flagged = m.Seen, m.Flagged
	}
	if a.Params.TargetMailboxID > 0 {
		v.Target = &ActionTarget{Mailbox: a.Params.TargetMailbox, Role: a.Params.TargetRole}
	}
	switch a.State {
	case store.ActionRequested:
		v.NextStep = nextApproval(a.ID)
	case store.ActionQueued, store.ActionRunning:
		v.NextStep = nextQueued(a.ID)
	}
	if withTargets {
		v.Targets = []ActionTargetView{}
		for _, t := range a.Targets {
			v.Targets = append(v.Targets, ActionTargetView{MessageID: t.MessageID, Mailbox: t.Mailbox, UID: t.UID, UIDValidity: t.UIDValidity,
				Result: t.Result, NewUID: t.NewUID})
		}
	}
	return v, nil
}

func redactActionID(raw json.RawMessage) (json.RawMessage, []string) {
	var in struct {
		ActionID int64 `json:"action_id"`
		Account  string
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return json.RawMessage(`{"invalid_input":true}`), nil
	}
	var m map[string]json.RawMessage
	_ = json.Unmarshal(raw, &m)
	p := redactFields(m)
	var targets []string
	if in.ActionID > 0 {
		p["action_id"] = in.ActionID
		targets = append(targets, "action:"+strconv.FormatInt(in.ActionID, 10))
	}
	b, _ := json.Marshal(p)
	return b, targets
}

// ActionsGet zeigt eine Aktion mit Zielen und Ergebnis je Ziel (R0).
func ActionsGet(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "actions.get", Scope: policy.ScopeMailRead, Risk: policy.R0, Idempotent: true,
		Summary: "Show a mailbox action (mark, move, trash, keywords) with its state and the result per message.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"action_id": {"type": "integer", "minimum": 1}},
  "required": ["action_id"], "additionalProperties": false}`),
		OutputSchema: json.RawMessage(actionViewSchema),
		Redact:       redactActionID,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				ActionID int64 `json:"action_id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := o.svc.Expire(ctx); err != nil {
				return nil, err
			}
			a, err := o.st.Action(ctx, in.ActionID)
			if err != nil {
				return nil, err
			}
			return o.view(ctx, a, true)
		},
	}
}

// ActionsListOutput ist das Ergebnis von actions.list.
type ActionsListOutput struct {
	Actions []ActionView `json:"actions"`
}

// ActionsList listet Aktionen, neueste zuerst (R0).
func ActionsList(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "actions.list", Scope: policy.ScopeMailRead, Risk: policy.R0, Idempotent: true,
		Summary: "List mailbox actions, newest first; filter by account and state (requested = waiting for a human).",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {` + accountProp + `,
    "state": {"type": "array", "items": {"type": "string", "enum": ["requested", "queued", "running", "done", "partial", "failed", "rejected", "expired"]}},
    "limit": {"type": "integer", "minimum": 1, "maximum": 500}},
  "additionalProperties": false}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"actions": {"type": "array", "items": ` + actionViewSchema + `}},
  "required": ["actions"]}`),
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account string   `json:"account"`
				State   []string `json:"state"`
				Limit   int      `json:"limit"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			f := store.ActionFilter{States: in.State, Limit: in.Limit}
			if in.Account != "" {
				a, err := ResolveAccount(ctx, o.accounts, in.Account)
				if err != nil {
					return nil, err
				}
				f.AccountID = a.ID
			}
			if err := o.svc.Expire(ctx); err != nil {
				return nil, err
			}
			list, err := o.st.ListActions(ctx, f)
			if err != nil {
				return nil, err
			}
			out := ActionsListOutput{Actions: []ActionView{}}
			for _, a := range list {
				v, err := o.view(ctx, a, false)
				if err != nil {
					return nil, err
				}
				out.Actions = append(out.Actions, v)
			}
			return out, nil
		},
	}
}

// ActionDecisionOutput ist das Ergebnis von actions.approve und actions.reject.
type ActionDecisionOutput struct {
	DryRun     bool   `json:"dry_run"`
	ActionID   int64  `json:"action_id"`
	ApprovalID int64  `json:"approval_id"`
	State      string `json:"state"`
	DecidedBy  string `json:"decided_by,omitempty"`
	Stale      int    `json:"stale"`
	NextStep   string `json:"next_step,omitempty"`
}

const actionDecisionSchema = `{"type": "object", "properties": {
    "dry_run": {"type": "boolean"}, "action_id": {"type": "integer"}, "approval_id": {"type": "integer"},
    "state": {"type": "string"}, "decided_by": {"type": "string"},
    "stale": {"type": "integer", "description": "messages whose server copy changed since the request; they are not touched"},
    "next_step": {"type": "string"}},
  "required": ["dry_run", "action_id", "approval_id", "state", "stale"]}`

func (o *actionOps) decide(approve bool) Handler {
	return func(ctx context.Context, call Call) (any, error) {
		var in struct {
			ActionID int64  `json:"action_id"`
			Reason   string `json:"reason"`
		}
		if err := decodeInput(call.Input, &in); err != nil {
			return nil, err
		}
		if err := o.svc.Expire(ctx); err != nil {
			return nil, err
		}
		cur, err := o.st.Action(ctx, in.ActionID)
		if err != nil {
			return nil, err
		}
		out := ActionDecisionOutput{DryRun: call.DryRun, ActionID: cur.ID, ApprovalID: cur.ApprovalID, State: cur.State}
		if cur.State != store.ActionRequested {
			return nil, &DeniedError{Reason: actions.ReasonNotRequested, Msg: fmt.Sprintf("action %d is %s, not waiting for approval", cur.ID, cur.State)}
		}
		by := drafts.DecidedBy(o.operator)
		if call.DryRun {
			out.NextStep = "dry run: nothing was decided"
			return out, nil
		}
		a, err := o.svc.Decide(ctx, in.ActionID, approve, by, in.Reason, call.Key.ID, call.AuditID)
		if err != nil {
			return nil, actionErr(err)
		}
		out.State, out.DecidedBy = a.State, by
		for _, t := range a.Targets {
			if t.Result == store.TargetStale {
				out.Stale++
			}
		}
		if a.State == store.ActionQueued {
			out.NextStep = nextQueued(a.ID)
		}
		return out, nil
	}
}

// ActionsApprove gibt einen Antrag frei (nur der Mensch ohne Schlüssel,
// E3; Scope mail:trash): Die Aktion wird eingereiht; Nachrichten, deren
// Serverkopie sich seit dem Antrag verändert hat, bleiben unberührt
// (stale).
func ActionsApprove(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "actions.approve", Scope: policy.ScopeMailTrash, Risk: policy.R2, HumanOnly: true,
		Summary: "Approve a requested mailbox action (only a human without a key); it is then applied by the sync job.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"action_id": {"type": "integer", "minimum": 1}},
  "required": ["action_id"], "additionalProperties": false}`),
		OutputSchema: json.RawMessage(actionDecisionSchema),
		Redact:       redactActionID,
		Handler:      o.decide(true),
	}
}

// ActionsReject lehnt einen Antrag ab (nur der Mensch ohne Schlüssel).
// Die Begründung steht nur in approvals.reason, nicht im Audit.
func ActionsReject(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "actions.reject", Scope: policy.ScopeMailTrash, Risk: policy.R1, HumanOnly: true,
		Summary: "Reject a requested mailbox action (only a human without a key).",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"action_id": {"type": "integer", "minimum": 1},
    "reason": {"type": "string", "maxLength": 1000}},
  "required": ["action_id"], "additionalProperties": false}`),
		OutputSchema: json.RawMessage(actionDecisionSchema),
		Redact:       redactActionID,
		Handler:      o.decide(false),
	}
}

// TagsListOutput ist das Ergebnis von tags.list.
type TagsListOutput struct {
	Account string     `json:"account"`
	Tags    []TagCount `json:"tags"`
}

// TagCount ist ein Tag mit der Zahl seiner Nachrichten.
type TagCount struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
}

// TagsList listet die Tags eines Kontos (R0).
func TagsList(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "tags.list", Scope: policy.ScopeMailRead, Risk: policy.R0, Idempotent: true,
		Summary:     "List the local tags of an account with the number of messages.",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {` + accountProp + `}, "additionalProperties": false}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"account": {"type": "string"},
    "tags": {"type": "array", "items": {"type": "object", "properties": {"name": {"type": "string"}, "messages": {"type": "integer"}},
      "required": ["name", "messages"]}}}, "required": ["account", "tags"]}`),
		Redact: redactAccountRef,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				Account string `json:"account"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			a, err := ResolveAccount(ctx, o.accounts, in.Account)
			if err != nil {
				return nil, err
			}
			list, err := o.st.ListTags(ctx, a.ID)
			if err != nil {
				return nil, err
			}
			out := TagsListOutput{Account: a.Name, Tags: []TagCount{}}
			for _, t := range list {
				out.Tags = append(out.Tags, TagCount{Name: t.Name, Messages: t.Messages})
			}
			return out, nil
		},
	}
}

// CleanupOutput ist das Ergebnis von drafts.cleanup_copies.
type CleanupOutput struct {
	DryRun   bool   `json:"dry_run"`
	DraftID  int64  `json:"draft_id"`
	Copies   int    `json:"copies"`
	Skipped  int    `json:"skipped"`
	ActionID int64  `json:"action_id,omitempty"`
	NextStep string `json:"next_step"`
}

// DraftsCleanupCopies legt frühere Kopien eines Entwurfs im Entwürfe-Ordner
// in den Papierkorb (Entscheid 43; nur der Mensch, R2). Ziel ist nur eine
// Kopie, die der Spiegel unter Ordner, UID und UIDVALIDITY aus
// remote_previous mit genau dem Inhalts-Hash dieser früheren Kopie führt
// (A14: remote_previous allein genügt nicht); alles andere wird
// übersprungen. Kein Antrag: Der Mensch handelt selbst (wie trash --yes).
func DraftsCleanupCopies(d Deps) Operation {
	o := newActionOps(d)
	return Operation{
		Name: "drafts.cleanup_copies", Scope: policy.ScopeMailDraft, Risk: policy.R2, HumanOnly: true,
		Summary: "Move earlier copies of a draft from the Drafts folder to the trash (only copies whose content PostKauz put there).",
		InputSchema: json.RawMessage(`{"type": "object", "properties": {"draft_id": {"type": "integer", "minimum": 1}},
  "required": ["draft_id"], "additionalProperties": false}`),
		OutputSchema: json.RawMessage(`{"type": "object", "properties": {"dry_run": {"type": "boolean"}, "draft_id": {"type": "integer"},
    "copies": {"type": "integer", "description": "earlier copies that will go to the trash"},
    "skipped": {"type": "integer", "description": "earlier copies not touched (gone, changed or without own hash)"},
    "action_id": {"type": "integer"}, "next_step": {"type": "string"}},
  "required": ["dry_run", "draft_id", "copies", "skipped", "next_step"]}`),
		Redact: redactDraftInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in struct {
				DraftID int64 `json:"draft_id"`
			}
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if o.drafts == nil {
				return nil, errors.New("drafts are not configured")
			}
			dr, err := o.drafts.Store.Draft(ctx, in.DraftID)
			if err != nil {
				return nil, err
			}
			out := CleanupOutput{DryRun: call.DryRun, DraftID: dr.ID}
			var plan actions.Plan
			var ids []int64
			for _, p := range dr.RemotePrevious {
				if p.UIDValidity == 0 || p.UID <= 0 || p.ContentHash == "" {
					out.Skipped++
					continue
				}
				c, hash, ok, err := o.st.CopyAt(ctx, p.MailboxID, uint32(p.UID))
				if err != nil {
					return nil, err
				}
				if !ok || c.UIDValidity != p.UIDValidity || hash != p.ContentHash {
					out.Skipped++
					continue
				}
				plan.Targets = append(plan.Targets, store.ActionTarget{MessageID: c.MessageID, MailboxID: c.MailboxID, UID: c.UID,
					UIDValidity: c.UIDValidity, Mailbox: c.Mailbox, Role: c.Role})
				ids = append(ids, c.MessageID)
			}
			out.Copies = len(plan.Targets)
			if out.Copies == 0 {
				out.NextStep = "no earlier copy to clean up"
				return out, nil
			}
			dest, ok, err := o.svc.FindMailbox(ctx, dr.AccountID, "", "trash")
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, &DeniedError{Reason: actions.ReasonNoTrashMailbox, Msg: "the account has no mailbox with role trash in the mirror; run sync first"}
			}
			if call.DryRun {
				out.NextStep = nextQueued(0)
				return out, nil
			}
			name := ""
			if list, err := o.accounts.ListAccounts(ctx); err == nil {
				for _, s := range list {
					if s.ID == dr.AccountID {
						name = s.Name
					}
				}
			}
			act, err := o.svc.Create(ctx, actions.Request{AccountID: dr.AccountID, AccountName: name, Kind: store.ActionTrash, IDs: ids, Plan: plan,
				RequestedBy: call.Key.ID, AuditID: call.AuditID,
				Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role, Source: "draft_cleanup"}})
			if err != nil {
				return nil, err
			}
			out.ActionID, out.NextStep = act.ID, nextQueued(act.ID)
			return out, nil
		},
	}
}

// ActionOriginCheck prüft vor der Ausführung einer Aktion den Schlüssel,
// der sie angelegt hat (A8, frisch aus api_keys wie beim Entwurf):
// widerrufen, unbekannt oder ohne Scope → nicht ausführen. Ein
// abgelaufener Schlüssel hält eine Aktion auf, außer ein Mensch hat sie
// freigegeben (T2 Punkt 7, wie bei Entwürfen). "cli" und "system" sind
// keine Schlüssel.
//
// V3 der Nachbesserung T2: Danach prüft sie die Aktion noch einmal mit
// policy.CheckInput gegen rules, die Regeln der Policy-Datei, die der
// ausführende Prozess (sync, sync --watch) beim Start geladen hat. Wurde
// [move] agent_may, [delete] agent_may oder [keys.<name>] seit dem Antrag
// verengt, scheitert die Aktion mit dem Grund der Policy, auch nach einer
// Freigabe (fail closed). Ohne Regeln (nil) läuft keine Aktion eines
// Schlüssels (policy_rules_missing).
func ActionOriginCheck(keys KeyRepo, rules *policy.Rules) actions.OriginCheck {
	return func(ctx context.Context, o actions.Origin) (string, error) {
		name, ok := strings.CutPrefix(o.KeyID, "key:")
		if !ok {
			return "", nil
		}
		k, err := keys.APIKeyByName(ctx, name)
		if errors.Is(err, store.ErrKeyNotFound) {
			return "key_invalid", nil
		}
		if err != nil {
			return "", err
		}
		switch KeyStatus(k, time.Now()) {
		case "revoked":
			return "key_revoked", nil
		case "expired":
			if !o.Approved {
				return "key_expired", nil
			}
		}
		pk := PolicyKey(k)
		if dec := policy.Check(pk, policy.Requirement{Name: "actions.run", Scope: o.Scope, Risk: policy.R2}, false); !dec.Allowed() {
			return dec.Reason, nil
		}
		if rules == nil {
			return "policy_rules_missing", nil
		}
		if dec := policy.CheckInput(pk, o.Facts, *rules); !dec.Allowed() {
			return dec.Reason, nil
		}
		return "", nil
	}
}
