package mailsync

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Integrationstest der Postfach-Aktionen (T2, S3.1 Akzeptanz 2 und 3)
// gegen die Testserver (POSTKAUZ_IT). Ein zweiter Client prüft: mark
// --seen setzt \Seen, move legt die Nachricht im Zielordner ab (neue UID im
// Spiegel), trash wirkt erst nach der Freigabe durch den Menschen und
// landet im Ordner mit der Rolle trash. Jeder Test räumt seine Nachrichten
// mit dem Testclient wieder ab (PostKauz löscht nie).

var itOperator = policy.Key{ID: "key:it-operator", Role: "operator", Scopes: policy.Roles["operator"]}

// itActionEnv: Spiegel mit Konto "it" auf dem Testserver, Executor mit
// den Standardregeln, Runner mit dem Job mailbox_action und dem Job sync,
// ein Zielordner und drei Testnachrichten in der INBOX.
type itActionEnv struct {
	t      *testing.T
	srv    itServer
	user   string
	st     *store.SQLite
	acc    store.Account
	x      *ops.Executor
	runner *Runner
	c      *imapclient.Client
	target string
	trash  string
	mids   []string
}

func newITActionEnv(t *testing.T, srv itServer, user string) *itActionEnv {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scopes := []string{}
	for _, sc := range itOperator.Scopes {
		scopes = append(scopes, string(sc))
	}
	if _, err := st.CreateAPIKey(ctx, store.APIKey{Name: strings.TrimPrefix(itOperator.ID, "key:"), KeyHash: ops.HashKey("pk_it_operator"),
		Role: itOperator.Role, Scopes: scopes, CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	b, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAccount(ctx, store.Account{Name: "it", Email: "it@example.org", Host: srv.host, Port: srv.port, TLS: srv.tls,
		Username: user, AuthType: "password", CredentialRef: "account/it", InsecureTestServer: true}); err != nil {
		t.Fatal(err)
	}
	e := &itActionEnv{t: t, srv: srv, user: user, st: st}
	e.acc, _ = st.AccountByName(ctx, "it")
	deps := ops.Deps{Accounts: st, Messages: st, Actions: st, Drafts: &drafts.Service{Store: st}}
	reg, err := ops.NewDefaultRegistry(deps)
	if err != nil {
		t.Fatal(err)
	}
	rules := policy.DefaultRules()
	e.x = &ops.Executor{Registry: reg, Audit: st.AuditWriter(), Rules: &rules}
	e.runner = &Runner{Store: st, Blobs: b, CommandTimeout: 15 * time.Second, Requests: ops.NewSyncJobs(deps),
		Actions: &actions.Runner{Store: st, Blobs: b, Origin: ops.ActionOriginCheck(st, &rules)}}

	// Ordner: Ziel des Verschiebens und der Papierkorb (Rolle trash).
	e.c = itClient(t, srv, user)
	tag := fmt.Sprintf("%d", time.Now().UnixNano())
	e.target = "PKITZiel" + tag
	if err := e.c.Create(e.target, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	boxes, err := itProvider(t, srv, user, 0).ListMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range boxes {
		if m.Role == provider.RoleTrash {
			e.trash = m.Path
		}
	}
	if e.trash == "" {
		t.Skipf("%s: no mailbox with role trash", srv.name)
	}
	e.mids = []string{"mark-" + tag + "@example.org", "move-" + tag + "@example.org", "trash-" + tag + "@example.org"}
	for _, mid := range e.mids {
		e.append("INBOX", mid)
	}
	t.Cleanup(e.cleanup)
	return e
}

func (e *itActionEnv) append(box, mid string) {
	e.t.Helper()
	raw := []byte("Message-ID: <" + mid + ">\r\nFrom: a@example.org\r\nTo: it@example.org\r\nSubject: T2 " + mid +
		"\r\nDate: Wed, 28 Oct 2026 10:00:00 +0100\r\n\r\nText\r\n")
	app := e.c.Append(box, int64(len(raw)), nil)
	if _, err := app.Write(raw); err != nil {
		e.t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		e.t.Fatal(err)
	}
	if _, err := app.Wait(); err != nil {
		e.t.Fatal(err)
	}
}

// cleanup entfernt die Testnachrichten und den Zielordner (Testclient).
func (e *itActionEnv) cleanup() {
	cc := itClient(e.t, e.srv, e.user)
	for _, box := range []string{"INBOX", e.target, e.trash} {
		if _, err := cc.Select(box, nil).Wait(); err != nil {
			continue
		}
		for _, mid := range e.mids {
			data, err := cc.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
			if err != nil || len(data.AllUIDs()) == 0 {
				continue
			}
			set := imap.UIDSetNum(data.AllUIDs()...)
			_ = cc.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close()
			if err := cc.UIDExpunge(set).Close(); err != nil {
				_ = cc.Expunge().Close()
			}
		}
	}
	_ = cc.Unselect().Wait()
	_ = cc.Delete(e.target).Wait()
}

func (e *itActionEnv) do(key policy.Key, op string, in any) (ops.Result, error) {
	raw, _ := json.Marshal(in)
	actor := audit.ActorAgent
	if key.IsHuman() {
		actor = audit.ActorHuman
	}
	no := false
	return e.x.Execute(context.Background(), ops.Request{Key: key, Actor: actor, Operation: op, Input: raw, DryRun: &no})
}

// sync spiegelt nur die drei Ordner des Tests (gemeinsame Testkonten).
func (e *itActionEnv) sync() ops.SyncOutput {
	e.t.Helper()
	out, err := e.runner.Sync(context.Background(), e.acc, provider.PasswordCredential(itPass),
		ops.SyncOptions{Mailboxes: []string{"INBOX", e.target, e.trash}}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return out
}

func (e *itActionEnv) id(mid string) int64 {
	e.t.Helper()
	ids, err := e.st.MessageIDsByHeader(context.Background(), e.acc.ID, mid)
	if err != nil || len(ids) != 1 {
		e.t.Fatalf("message %s in the mirror: %v, %v", mid, ids, err)
	}
	return ids[0]
}

func TestIntegrationMailboxActions(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		ctx := context.Background()
		e := newITActionEnv(t, srv, user)
		st, c, mids, target, trash, do, sync := e.st, e.c, e.mids, e.target, e.trash, e.do, e.sync
		sync()
		id := e.id
		markID, moveID, trashID := id(mids[0]), id(mids[1]), id(mids[2])

		// mark --seen (Schlüssel, R2) und move (Schlüssel, R2): ein Lauf.
		if _, err := do(itOperator, "messages.mark", map[string]any{"account": "it", "ids": []int64{markID}, "seen": true}); err != nil {
			t.Fatal(err)
		}
		res, err := do(itOperator, "messages.move", map[string]any{"account": "it", "ids": []int64{moveID}, "mailbox": target})
		if err != nil {
			t.Fatal(err)
		}
		if o := res.Output.(ops.ActionOutput); o.Status != "queued" || o.ServerCopies != 1 {
			t.Fatalf("move = %+v", o)
		}
		start := time.Now()
		out := sync()
		if out.Actions == nil || out.Actions.OK != 2 || out.Actions.Failed != 0 || out.Actions.Stale != 0 {
			t.Fatalf("sync actions = %+v", out.Actions)
		}
		t.Logf("%s: mark and move applied in one sync (%s)", srv.name, time.Since(start).Round(time.Millisecond))
		if fl := itFlags(t, c, "INBOX", mids[0]); len(fl) != 1 || !slices.Contains(fl[0], imap.FlagSeen) {
			t.Fatalf("%s: flags of the marked message: %v", srv.name, fl)
		}
		if n := len(itFlags(t, c, "INBOX", mids[1])); n != 0 {
			t.Fatalf("%s: moved message still in INBOX (%d)", srv.name, n)
		}
		inTarget := itUIDs(t, c, target, mids[1])
		if len(inTarget) != 1 {
			t.Fatalf("%s: moved message in the target: %v", srv.name, inTarget)
		}
		if m, _ := st.MessageByID(ctx, markID); !m.Seen || m.PendingFlags {
			t.Fatalf("mirror after mark: seen %v, pending %v", m.Seen, m.PendingFlags)
		}
		cs, _ := st.ServerCopies(ctx, moveID)
		if len(cs) != 1 || cs[0].Mailbox != target || cs[0].UID != uint32(inTarget[0]) {
			t.Fatalf("mirror after move: %+v, server uid %v", cs, inTarget)
		}

		// trash: der Schlüssel beantragt nur (R3); erst die Freigabe durch
		// den Menschen lässt den Job laufen.
		res, err = do(itOperator, "messages.trash", map[string]any{"account": "it", "ids": []int64{trashID}})
		if err != nil {
			t.Fatal(err)
		}
		o := res.Output.(ops.ActionOutput)
		if o.Status != "needs_approval" || o.ActionID == 0 {
			t.Fatalf("trash = %+v", o)
		}
		if out := sync(); out.Actions != nil && out.Actions.Actions != 0 {
			t.Fatalf("trash ran before approval: %+v", out.Actions)
		}
		if len(itUIDs(t, c, "INBOX", mids[2])) != 1 {
			t.Fatalf("%s: message left INBOX before approval", srv.name)
		}
		if _, err := do(itHuman, "actions.approve", map[string]any{"action_id": o.ActionID}); err != nil {
			t.Fatal(err)
		}
		if out := sync(); out.Actions == nil || out.Actions.OK != 1 {
			t.Fatalf("sync after approval: %+v", out.Actions)
		}
		if len(itUIDs(t, c, "INBOX", mids[2])) != 0 || len(itUIDs(t, c, trash, mids[2])) != 1 {
			t.Fatalf("%s: trashed message not (only) in %q", srv.name, trash)
		}
		if a, _ := st.Action(ctx, o.ActionID); a.State != store.ActionDone {
			t.Fatalf("trash action state %s", a.State)
		}
	})
}

// itUIDs liefert die UIDs der Kopien mit Message-ID mid im Ordner box.
func itUIDs(t *testing.T, c *imapclient.Client, box, mid string) []imap.UID {
	t.Helper()
	if _, err := c.Select(box, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	return data.AllUIDs()
}

// itFlags liefert die Flags der Kopien mit Message-ID mid im Ordner box.
func itFlags(t *testing.T, c *imapclient.Client, box, mid string) [][]imap.Flag {
	t.Helper()
	uids := itUIDs(t, c, box, mid)
	if len(uids) == 0 {
		return nil
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	var out [][]imap.Flag
	for _, m := range msgs {
		out = append(out, m.Flags)
	}
	return out
}

// A03 S3.1 Akzeptanz 2 und S3.5 Akzeptanz 5 gegen den Testserver: Mit
// laufendem sync --watch (Standardtakt der Jobs, 10 s) kommt mark --seen
// binnen 10 s auf den Server, und mail_sync_now (sync.request) für einen
// Ordner ohne IDLE spiegelt eine neue Nachricht binnen 10 s.
func TestIntegrationWatchActionsAndSyncNow(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITActionEnv(t, srv, user)
		e.sync()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var rounds atomic.Int32
		done := make(chan error, 1)
		go func() {
			_, err := e.runner.Sync(ctx, e.acc, provider.PasswordCredential(itPass), ops.SyncOptions{Watch: true, Interval: time.Hour,
				Mailboxes: []string{"INBOX", e.target, e.trash}}, func(p ops.Progress) {
				if p.Event == "round" {
					rounds.Add(1)
				}
			})
			done <- err
		}()
		waitFor(t, 30*time.Second, func() bool { return rounds.Load() >= 1 })

		markID := e.id(e.mids[0])
		start := time.Now()
		if _, err := e.do(itOperator, "messages.mark", map[string]any{"account": "it", "ids": []int64{markID}, "seen": true}); err != nil {
			t.Fatal(err)
		}
		waitFor(t, 11*time.Second, func() bool {
			fl := itFlags(t, e.c, "INBOX", e.mids[0])
			return len(fl) == 1 && slices.Contains(fl[0], imap.FlagSeen)
		})
		t.Logf("%s: mark --seen on the server after %s", srv.name, time.Since(start).Round(time.Millisecond))

		mid := "now-" + strings.TrimPrefix(e.target, "PKITZiel") + "@example.org"
		e.mids = append(e.mids, mid)
		e.append(e.target, mid)
		start = time.Now()
		res, err := e.do(itOperator, "sync.request", map[string]any{"account": "it", "mailbox": e.target})
		if err != nil || res.Output.(ops.SyncRequestOutput).Status != "queued" {
			t.Fatalf("sync.request = %+v, %v", res.Output, err)
		}
		waitFor(t, 11*time.Second, func() bool {
			ids, _ := e.st.MessageIDsByHeader(context.Background(), e.acc.ID, mid)
			return len(ids) == 1
		})
		t.Logf("%s: requested sync mirrored the new message after %s", srv.name, time.Since(start).Round(time.Millisecond))
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
