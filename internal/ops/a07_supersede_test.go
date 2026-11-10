package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A07 Teil 5: drafts.supersede (eigene, unveränderte Kopie eines ersetzten
// Entwurfs in den Papierkorb).

var triageKey = policy.Key{ID: "key:nacht", Name: "nacht", Scopes: policy.Roles["triage"]}

// supRemote ist der Server für den Runner: Köpfe je UID, Kommandos gezählt.
type supRemote struct {
	headers map[provider.UID]string
	moves   []string
}

func (r *supRemote) FetchHeaders(_ context.Context, _ string, _ uint32, uids []provider.UID) ([]provider.Header, error) {
	var out []provider.Header
	for _, u := range uids {
		if h, ok := r.headers[u]; ok {
			out = append(out, provider.Header{UID: u, RawHeader: []byte(h)})
		}
	}
	return out, nil
}

func (r *supRemote) SetFlags(context.Context, string, uint32, []provider.UID, provider.FlagPatch) error {
	return nil
}

func (r *supRemote) Move(_ context.Context, from string, _ uint32, uids []provider.UID, to string) (provider.MoveResult, error) {
	res := provider.MoveResult{UIDValidity: 5, UIDs: map[provider.UID]provider.UID{}}
	for _, u := range uids {
		r.moves = append(r.moves, from+"->"+to)
		res.UIDs[u] = u + 100
	}
	return res, nil
}

// supEnv: Vertragsumgebung, Entwürfe 1 und 2 gehören key, Kopie von
// Entwurf 1 unverändert (UID 9), Schlüssel nacht (triage) und assi
// (assistant) im Store (für die Prüfung vor der Ausführung).
func supEnv(t *testing.T, key policy.Key) (*contractEnv, *Executor) {
	t.Helper()
	e := newContractEnv(t)
	ctx := context.Background()
	for _, k := range []struct{ name, role string }{{"nacht", "triage"}, {"assi", "assistant"}} {
		if _, err := e.st.CreateAPIKey(ctx, store.APIKey{Name: k.name, KeyHash: HashKey("k-" + k.name), Role: k.role,
			Scopes: v3Scopes(policy.Roles[k.role])}); err != nil {
			t.Fatal(err)
		}
	}
	prepSupersede(t, e.st, key.ID)
	e.exec(t, `UPDATE message_mailboxes SET content_loaded = 1 WHERE uid = 9`)
	x, _ := actionExec(t, e)
	return e, x
}

func (e *contractEnv) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if err := e.st.ExecForTest(q, args...); err != nil {
		t.Fatal(err)
	}
}

func (e *contractEnv) runActions(t *testing.T, rules policy.Rules, remote *supRemote) {
	t.Helper()
	ctx := context.Background()
	acct, _ := e.st.AccountByName(ctx, "demo")
	r := &actions.Runner{Store: e.st, Blobs: e.blobs, Origin: ActionOriginCheck(e.st, &rules)}
	if _, err := r.Run(ctx, acct.ID, remote); err != nil {
		t.Fatal(err)
	}
}

func deniedReason(err error) string {
	var de *DeniedError
	if errors.As(err, &de) {
		return de.Reason
	}
	var pe *PolicyError
	if errors.As(err, &pe) {
		return pe.Decision.Reason
	}
	return ""
}

// Erfolg mit triage- und assistant-Schlüssel: alter Entwurf abandoned
// (superseded_by_draft), Aktion ohne Antrag, vom Runner wirklich
// ausgeführt (MOVE Entwürfe → Papierkorb), Audit ohne Inhalt, draft show
// nennt Grund und Nachfolger.
func TestA07SupersedeRuns(t *testing.T) {
	for _, key := range []policy.Key{triageKey, assistantKey} {
		t.Run(key.Name, func(t *testing.T) {
			e, x := supEnv(t, key)
			ctx := context.Background()
			res, err := execJSON(t, x, key, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
			if err != nil {
				t.Fatal(err)
			}
			out := res.Output.(SupersedeOutput)
			if out.Status != "abandoned" || out.Copies != 1 || out.ActionID == 0 || out.Trash != "Papierkorb" || out.DryRun {
				t.Fatalf("output = %+v", out)
			}
			old, _ := e.st.Draft(ctx, 1)
			if old.Status != string(drafts.StatusAbandoned) || old.ClosedAt.IsZero() || old.SentMessageID != 0 {
				t.Fatalf("old draft = %s", old.Status)
			}
			if nw, _ := e.st.Draft(ctx, 2); nw.Status != string(drafts.StatusPendingApproval) {
				t.Fatalf("new draft = %s", nw.Status)
			}
			a, err := e.st.Action(ctx, out.ActionID)
			if err != nil || a.State != store.ActionQueued || a.ApprovalID != 0 || a.Params.Source != store.ActionSourceDraftSupersede ||
				len(a.Targets) != 1 || a.Targets[0].UID != 9 || a.RequestedBy != key.ID {
				t.Fatalf("action = %+v, %v", a, err)
			}
			recs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: drafts.OpSupersede})
			var found bool
			for _, r := range recs {
				if r.Reason == drafts.ReasonSupersededByDraft {
					found = true
					p := string(r.ParamsRedacted)
					if !strings.Contains(p, `"by_draft_id":2`) || !strings.Contains(p, `"copies":1`) || !strings.Contains(p, `"draft_id":1`) ||
						strings.Contains(p, "Entwurf") || r.KeyID != key.ID {
						t.Fatalf("audit = %+v %s", r, p)
					}
				}
			}
			if !found {
				t.Fatalf("no audit entry superseded_by_draft: %+v", recs)
			}
			gres, err := execJSON(t, x, assistantKey, "drafts.get", `{"draft_id":1}`, false)
			if err != nil {
				t.Fatal(err)
			}
			got := gres.Output.(DraftDetail)
			if got.ClosedReason != drafts.ReasonSupersededByDraft || got.SupersededByDraftID != 2 {
				t.Fatalf("drafts.get: %q %d", got.ClosedReason, got.SupersededByDraftID)
			}
			remote := &supRemote{}
			e.runActions(t, policy.DefaultRules(), remote)
			a, _ = e.st.Action(ctx, out.ActionID)
			if a.State != store.ActionDone || len(remote.moves) != 1 || remote.moves[0] != "Entwuerfe->Papierkorb" {
				t.Fatalf("after run: %s %s, moves %v", a.State, a.Error, remote.moves)
			}
		})
	}
}

// Ablehnungen, je ein eigener Grund; nichts ändert sich.
func TestA07SupersedeDenied(t *testing.T) {
	for _, c := range []struct {
		name, want string
		key        policy.Key
		owner      string // created_by/updated_by der Entwürfe; "" = key.ID
		prep       func(t *testing.T, e *contractEnv)
		input      string
	}{
		{"other key", drafts.ReasonNotOwnDraft, triageKey, "key:assi", nil, ""},
		{"updated by another key", drafts.ReasonChangedByOther, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET updated_by = 'key:assi' WHERE id = 1`)
		}, ""},
		{"copy changed", drafts.ReasonCopyChanged, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET pushed_content_hash = 'other' WHERE id = 1`)
		}, ""},
		{"copy taken over", drafts.ReasonCopyChanged, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET remote_state = 'taken_over' WHERE id = 1`)
		}, ""},
		{"copy gone from the mirror", drafts.ReasonCopyMissing, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `DELETE FROM message_mailboxes WHERE uid = 9`)
		}, ""},
		{"new one older", drafts.ReasonSuccessorNotNewer, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET created_at = created_at - 600000 WHERE id = 2`)
		}, ""},
		{"other original", drafts.ReasonOtherOriginal, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET kind = 'reply', in_reply_to_msg = 1 WHERE id = 1`)
			e.exec(t, `UPDATE drafts SET kind = 'reply', in_reply_to_msg = NULL WHERE id = 2`)
		}, ""},
		{"other subject", drafts.ReasonOtherSubject, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET subject = 'Anderes' WHERE id = 2`)
		}, ""},
		{"other kind", drafts.ReasonKindMismatch, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET kind = 'forward', in_reply_to_msg = 1 WHERE id = 2`)
		}, ""},
		{"new one closed", drafts.ReasonSuccessorClosed, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET status = 'abandoned', closed_at = updated_at WHERE id = 2`)
		}, ""},
		{"old one ready", drafts.ReasonApprovedByHuman, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE drafts SET status = 'ready' WHERE id = 1`)
		}, ""},
		{"no trash mailbox", drafts.ReasonNoTrashMailbox, triageKey, "", func(t *testing.T, e *contractEnv) {
			e.exec(t, `UPDATE mailboxes SET role = 'custom' WHERE path = 'Papierkorb'`)
		}, ""},
		{"same draft", drafts.ReasonSameDraft, triageKey, "", nil, `{"draft_id":1,"by_draft_id":1}`},
		{"key without mail:draft", "missing_scope", policy.Key{ID: "key:leser", Name: "leser", Scopes: policy.Roles["reader"]}, "", nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			owner := policy.Key{ID: c.owner}
			if c.owner == "" {
				owner = c.key
			}
			e, x := supEnv(t, owner)
			if c.prep != nil {
				c.prep(t, e)
			}
			in := c.input
			if in == "" {
				in = `{"draft_id":1,"by_draft_id":2}`
			}
			before, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`)
			_, err := execJSON(t, x, c.key, "drafts.supersede", in, false)
			if !errors.Is(err, ErrDenied) || deniedReason(err) != c.want {
				t.Fatalf("err = %v (reason %q), want %s", err, deniedReason(err), c.want)
			}
			if after, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`); after != before {
				t.Fatalf("an action was created: %d -> %d", before, after)
			}
			if d, _ := e.st.Draft(context.Background(), 1); drafts.Status(d.Status).Closed() {
				t.Fatalf("old draft closed: %s", d.Status)
			}
		})
	}
}

// Der Mensch ohne Schlüssel darf jeden Entwurf ersetzen (created_by und
// updated_by zählen nicht).
func TestA07SupersedeHuman(t *testing.T) {
	e, x := supEnv(t, policy.Key{ID: "key:assi"})
	if _, err := execJSON(t, x, humanKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.st.Draft(context.Background(), 1); d.Status != string(drafts.StatusAbandoned) {
		t.Fatalf("old draft = %s", d.Status)
	}
}

// Trockenlauf ändert nichts und nennt, was geschähe.
func TestA07SupersedeDryRun(t *testing.T) {
	e, x := supEnv(t, triageKey)
	before := e.fingerprint(t)
	res, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, true)
	if err != nil {
		t.Fatal(err)
	}
	if out := res.Output.(SupersedeOutput); !out.DryRun || out.Copies != 1 || out.ActionID != 0 || !strings.Contains(out.NextStep, "dry run") {
		t.Fatalf("output = %+v", out)
	}
	if e.fingerprint(t) != before {
		t.Fatal("dry run changed the database")
	}
	if d, _ := e.st.Draft(context.Background(), 1); d.Status != string(drafts.StatusDraft) {
		t.Fatalf("old draft = %s", d.Status)
	}
}

// [delete] agent_may = none: im Runner zusätzlich geprüft (die Policy
// wurde nach dem Aufruf verengt); nichts geht an den Server.
func TestA07SupersedeRunnerPolicy(t *testing.T) {
	e, x := supEnv(t, triageKey)
	res, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	remote := &supRemote{}
	e.runActions(t, policy.Rules{MoveAgentMay: "within_account", DeleteAgentMay: "none", BulkConfirmThreshold: 50}, remote)
	a, _ := e.st.Action(context.Background(), res.Output.(SupersedeOutput).ActionID)
	if a.State != store.ActionFailed || a.Error != "policy_forbids" || len(remote.moves) != 0 {
		t.Fatalf("action = %s/%s, moves %v", a.State, a.Error, remote.moves)
	}
}

// Eine fremde Nachricht mit derselben Message-ID wird nie Ziel; eine
// frühere eigene Kopie (Ordner, UID, UIDVALIDITY, Hash) schon.
func TestA07SupersedeTargetsOnlyOwnCopies(t *testing.T) {
	e, x := supEnv(t, triageKey)
	ctx := context.Background()
	acct, _ := e.st.AccountByName(ctx, "demo")
	boxes, _ := e.st.Mailboxes(ctx, acct.ID)
	ids := map[string]int64{}
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	// Fremde Nachricht mit der Message-ID der Kopie im Posteingang und eine
	// frühere eigene Kopie (UID 11) im Entwürfe-Ordner.
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct.ID, MailboxID: ids["INBOX"],
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 77}, MessageID: "pk.alt@example.org", Subject: "Fremd", From: "x@example.net",
			Size: 99, ContentHash: "hash-fremd"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct.ID, MailboxID: ids["Entwuerfe"],
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 11, Draft: true}, MessageID: "pk.alt@example.org", Subject: "Entwurf",
			From: "demo@example.org", Size: 12, ContentHash: "hash-frueher"}}}); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE drafts SET remote_previous = json_array(json_object('mailbox_id', ?, 'uid', 11, 'uidvalidity', 6,
		'content_hash', 'hash-frueher')) WHERE id = 1`, ids["Entwuerfe"])
	res, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.Action(ctx, res.Output.(SupersedeOutput).ActionID)
	if len(a.Targets) != 2 {
		t.Fatalf("targets = %+v", a.Targets)
	}
	for _, tg := range a.Targets {
		if tg.MailboxID != ids["Entwuerfe"] || (tg.UID != 9 && tg.UID != 11) {
			t.Fatalf("target outside the own copies: %+v", tg)
		}
	}
}

// Ist eine Kopie nur über Message-ID und Größe verknüpft (nie geladen) und
// weicht ihr Kopf ab, ist sie stale, und nichts wird verschoben, auch nicht
// die passende frühere Kopie.
func TestA07SupersedeStaleMovesNothing(t *testing.T) {
	e, x := supEnv(t, triageKey)
	ctx := context.Background()
	acct, _ := e.st.AccountByName(ctx, "demo")
	box, _ := e.st.QueryIntForTest(`SELECT id FROM mailboxes WHERE path = 'Entwuerfe'`)
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct.ID, MailboxID: int64(box),
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 11, Draft: true}, MessageID: "pk.frueher@example.org", Subject: "Entwurf",
			From: "demo@example.org", Size: 12, ContentHash: "hash-frueher"}}}); err != nil {
		t.Fatal(err)
	}
	e.exec(t, `UPDATE message_mailboxes SET content_loaded = 1 WHERE uid = 11`)
	e.exec(t, `UPDATE drafts SET remote_previous = json_array(json_object('mailbox_id', ?, 'uid', 11, 'uidvalidity', 6,
		'content_hash', 'hash-frueher')) WHERE id = 1`, box)
	// Die aktuelle Kopie: nie geladen, lokaler Kopf liegt vor, der Server
	// meldet einen anderen.
	e.blobs.m["hdr-sup"] = "Subject: Entwurf\r\nMessage-ID: <pk.alt@example.org>\r\n\r\nText"
	e.exec(t, `INSERT INTO blobs(sha256, size, created_at) VALUES ('hdr-sup', 60, 0)`)
	e.exec(t, `UPDATE messages SET blob_sha256 = 'hdr-sup' WHERE message_id_header = 'pk.alt@example.org'`)
	e.exec(t, `UPDATE message_mailboxes SET content_loaded = 0 WHERE uid = 9`)
	res, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false)
	if err != nil {
		t.Fatal(err)
	}
	remote := &supRemote{headers: map[provider.UID]string{9: "Subject: Etwas anderes\r\n\r\n"}}
	e.runActions(t, policy.DefaultRules(), remote)
	a, _ := e.st.Action(ctx, res.Output.(SupersedeOutput).ActionID)
	if len(remote.moves) != 0 {
		t.Fatalf("moved %v", remote.moves)
	}
	for _, tg := range a.Targets {
		if tg.Result != store.TargetStale {
			t.Fatalf("target %d = %s (action %s)", tg.UID, tg.Result, a.State)
		}
	}
}

// Nach supersede schließt kein späterer Lauf den alten Entwurf als
// gesendet, auch wenn eine Nachricht mit seiner Message-ID in Gesendet
// auftaucht.
func TestA07SupersedeStaysAbandoned(t *testing.T) {
	e, x := supEnv(t, triageKey)
	ctx := context.Background()
	if _, err := execJSON(t, x, triageKey, "drafts.supersede", `{"draft_id":1,"by_draft_id":2}`, false); err != nil {
		t.Fatal(err)
	}
	old, _ := e.st.Draft(ctx, 1)
	acct, _ := e.st.AccountByName(ctx, "demo")
	boxes, _ := e.st.Mailboxes(ctx, acct.ID)
	specs := []store.MailboxSpec{{Path: "Gesendet", Name: "Gesendet", Role: "sent"}}
	for _, b := range boxes {
		specs = append(specs, store.MailboxSpec{Path: b.Path, Name: b.Name, Role: b.Role})
	}
	if _, _, err := e.st.SyncMailboxes(ctx, acct.ID, specs); err != nil {
		t.Fatal(err)
	}
	sent, _ := e.st.QueryIntForTest(`SELECT id FROM mailboxes WHERE path = 'Gesendet'`)
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct.ID, MailboxID: int64(sent),
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 1}, MessageID: old.MessageIDHeader, Subject: old.Subject, From: "demo@example.org",
			Size: 50, ContentHash: "gesendet"}}}); err != nil {
		t.Fatal(err)
	}
	rep, err := e.deps.Drafts.CheckAfterSync(ctx, acct.ID, drafts.CheckOptions{})
	if err != nil || len(rep.SentDraftIDs) != 0 {
		t.Fatalf("check = %+v, %v", rep, err)
	}
	if d, _ := e.st.Draft(ctx, 1); d.Status != string(drafts.StatusAbandoned) {
		t.Fatalf("old draft = %s", d.Status)
	}
}
