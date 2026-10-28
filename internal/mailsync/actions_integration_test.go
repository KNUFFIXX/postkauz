package mailsync

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
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

func TestIntegrationMailboxActions(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
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
		acc, _ := st.AccountByName(ctx, "it")
		deps := ops.Deps{Accounts: st, Messages: st, Actions: st}
		reg, err := ops.NewDefaultRegistry(deps)
		if err != nil {
			t.Fatal(err)
		}
		rules := policy.DefaultRules()
		x := &ops.Executor{Registry: reg, Audit: st.AuditWriter(), Rules: &rules}
		do := func(key policy.Key, op string, in any) (ops.Result, error) {
			raw, _ := json.Marshal(in)
			actor := audit.ActorAgent
			if key.IsHuman() {
				actor = audit.ActorHuman
			}
			no := false
			return x.Execute(ctx, ops.Request{Key: key, Actor: actor, Operation: op, Input: raw, DryRun: &no})
		}
		runner := &Runner{Store: st, Blobs: b, CommandTimeout: 15 * time.Second,
			Actions: &actions.Runner{Store: st, Blobs: b, Origin: ops.ActionOriginCheck(st)}}

		// Ordner: Ziel des Verschiebens und der Papierkorb (Rolle trash).
		c := itClient(t, srv, user)
		tag := fmt.Sprintf("%d", time.Now().UnixNano())
		target := "PKITZiel" + tag
		if err := c.Create(target, nil).Wait(); err != nil {
			t.Fatal(err)
		}
		trash := ""
		boxes, err := itProvider(t, srv, user, 0).ListMailboxes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range boxes {
			if m.Role == provider.RoleTrash {
				trash = m.Path
			}
		}
		if trash == "" {
			t.Skipf("%s: no mailbox with role trash", srv.name)
		}
		mids := []string{"mark-" + tag + "@example.org", "move-" + tag + "@example.org", "trash-" + tag + "@example.org"}
		for _, mid := range mids {
			raw := []byte("Message-ID: <" + mid + ">\r\nFrom: a@example.org\r\nTo: it@example.org\r\nSubject: T2 " + mid +
				"\r\nDate: Wed, 28 Oct 2026 10:00:00 +0100\r\n\r\nText\r\n")
			app := c.Append("INBOX", int64(len(raw)), nil)
			if _, err := app.Write(raw); err != nil {
				t.Fatal(err)
			}
			if err := app.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := app.Wait(); err != nil {
				t.Fatal(err)
			}
		}
		t.Cleanup(func() {
			cc := itClient(t, srv, user)
			for _, box := range []string{"INBOX", target, trash} {
				if _, err := cc.Select(box, nil).Wait(); err != nil {
					continue
				}
				for _, mid := range mids {
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
			_ = cc.Delete(target).Wait()
		})
		// Nur die drei Ordner spiegeln (gemeinsame Testkonten).
		sync := func() ops.SyncOutput {
			t.Helper()
			out, err := runner.Sync(ctx, acc, provider.PasswordCredential(itPass), ops.SyncOptions{Mailboxes: []string{"INBOX", target, trash}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		sync()
		id := func(mid string) int64 {
			t.Helper()
			ids, err := st.MessageIDsByHeader(ctx, acc.ID, mid)
			if err != nil || len(ids) != 1 {
				t.Fatalf("message %s in the mirror: %v, %v", mid, ids, err)
			}
			return ids[0]
		}
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
