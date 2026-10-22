package mailsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	imapprov "github.com/KNUFFIXX/postkauz/internal/provider/imap"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Integrationstests des Jobs draft_push (S2.5) gegen Dovecot, Stalwart und
// GreenMail (POSTKAUZ_IT; `make integration`). Geprüft wird mit einem
// zweiten IMAP-Client (go-imap direkt, wie ein Mailprogramm): Der Entwurf
// liegt im Entwürfe-Ordner mit \Draft \Seen und der beim Anlegen vergebenen
// Message-ID (Akzeptanzkriterium 2). Stalwart und GreenMail haben nur ein
// gemeinsames Testkonto: Jeder Test entfernt seine Kopien am Ende wieder
// (nur der Testclient kann das; PostKauz löscht nie).

var (
	itAssistant = policy.Key{ID: "key:it-assistant", Role: "assistant", Scopes: policy.Roles["assistant"]}
	itHuman     = policy.Key{ID: "cli", Scopes: policy.Roles["admin"], Human: true}
)

type itDraftEnv struct {
	t      *testing.T
	srv    itServer
	user   string
	st     *store.SQLite
	acc    store.Account
	svc    *drafts.Service
	x      *ops.Executor
	runner *Runner
	path   string // Entwürfe-Ordner (Rolle drafts)
	client *imapclient.Client
	mids   []string // angelegte Message-IDs (Aufräumen)
}

func newITDraftEnv(t *testing.T, srv itServer, user string) *itDraftEnv {
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
	b, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	email := user
	if !strings.Contains(email, "@") {
		email = user + "@example.org"
	}
	id, err := st.CreateAccount(ctx, store.Account{Name: "it", Email: email, Host: srv.host, Port: srv.port, TLS: srv.tls, Username: user,
		AuthType: "password", CredentialRef: "account/it", InsecureTestServer: true})
	if err != nil {
		t.Fatal(err)
	}
	e := &itDraftEnv{t: t, srv: srv, user: user, st: st, svc: &drafts.Service{Store: st}}
	e.acc, _ = st.AccountByName(ctx, "it")
	_ = id
	deps := ops.Deps{Accounts: st, Messages: st, Drafts: e.svc, DraftBlobs: b, Draft: ops.DraftConfig{AttachDir: t.TempDir(), Operator: "IT"}}
	reg, err := ops.NewDefaultRegistry(deps)
	if err != nil {
		t.Fatal(err)
	}
	e.x = &ops.Executor{Registry: reg, Audit: st.AuditWriter()}
	e.runner = &Runner{Store: st, Blobs: b, Pusher: ops.NewDraftPusher(deps), CommandTimeout: 15 * time.Second}
	e.client = itClient(t, srv, user)
	if srv.name == "greenmail" {
		// GreenMail legt keine Standardordner an und kennt kein SPECIAL-USE:
		// Entwürfe (Rolle per Name), derselbe Ordner wie im Test der Rollen
		// (internal/provider/imap); ein zweiter Ordner wie Drafts nähme ihm
		// die Rolle.
		_ = e.client.Create("Entwürfe", nil).Wait()
	}
	prov := itProvider(t, srv, user, 0)
	boxes, err := prov.ListMailboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range boxes {
		if m.Role == provider.RoleDrafts {
			e.path = m.Path
		}
	}
	if e.path == "" {
		t.Fatalf("%s: no mailbox with role drafts", srv.name)
	}
	c := prov.Capabilities()
	t.Logf("%s: Drafts folder %q (SPECIAL-USE %v, UIDPLUS %v, IMAP4rev2 %v)", srv.name, e.path, c.SpecialUse, c.UIDPlus, c.IMAP4rev2)
	t.Cleanup(e.cleanup)
	return e
}

// cleanup entfernt die Kopien dieses Tests vom Server (Testclient).
func (e *itDraftEnv) cleanup() {
	c := itClient(e.t, e.srv, e.user)
	if _, err := c.Select(e.path, nil).Wait(); err != nil {
		return
	}
	for _, mid := range e.mids {
		data, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
		if err != nil || len(data.AllUIDs()) == 0 {
			continue
		}
		set := imap.UIDSetNum(data.AllUIDs()...)
		_ = c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close()
		if err := c.UIDExpunge(set).Close(); err != nil {
			_ = c.Expunge().Close()
		}
	}
}

func (e *itDraftEnv) do(key policy.Key, op string, in any) (any, error) {
	b, _ := json.Marshal(in)
	actor := audit.ActorAgent
	if key.IsHuman() {
		actor = audit.ActorHuman
	}
	no := false
	res, err := e.x.Execute(context.Background(), ops.Request{Key: key, Actor: actor, Operation: op, Input: b, DryRun: &no})
	return res.Output, err
}

func (e *itDraftEnv) create(subject string) ops.DraftsWriteOutput {
	e.t.Helper()
	out, err := e.do(itAssistant, "drafts.create", map[string]any{"kind": "new", "account": "it", "to": []string{"b@example.org"},
		"subject": subject, "body_markdown": "Text für " + subject})
	if err != nil {
		e.t.Fatal(err)
	}
	o := out.(ops.DraftsWriteOutput)
	e.mids = append(e.mids, o.MessageID)
	return o
}

func (e *itDraftEnv) sync(o ops.SyncOptions) (ops.SyncOutput, error) {
	o.Mailboxes = []string{e.path} // nur der Entwürfe-Ordner (gemeinsame Testkonten)
	return e.runner.Sync(context.Background(), e.acc, itPass, o, nil)
}

func (e *itDraftEnv) draft(id int64) store.Draft {
	e.t.Helper()
	d, err := e.st.Draft(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

// copies liefert UIDs, Flags und Message-ID-Kopfzeile der Kopien mit
// Message-ID mid im Entwürfe-Ordner, gesehen vom zweiten Client.
type itCopy struct {
	uid    imap.UID
	flags  []imap.Flag
	header string
}

func (e *itDraftEnv) copies(mid string) []itCopy {
	e.t.Helper()
	c := e.client
	if _, err := c.Select(e.path, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		e.t.Fatal(err)
	}
	data, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
	if err != nil {
		e.t.Fatal(err)
	}
	if len(data.AllUIDs()) == 0 {
		return nil
	}
	sec := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader, Peek: true}
	msgs, err := c.Fetch(imap.UIDSetNum(data.AllUIDs()...), &imap.FetchOptions{UID: true, Flags: true,
		BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil {
		e.t.Fatal(err)
	}
	var out []itCopy
	for _, m := range msgs {
		h := ""
		for _, line := range strings.Split(string(m.FindBodySection(sec)), "\r\n") {
			if strings.HasPrefix(strings.ToLower(line), "message-id:") {
				h = strings.TrimSpace(line[len("message-id:"):])
			}
		}
		out = append(out, itCopy{uid: m.UID, flags: m.Flags, header: h})
	}
	slices.SortFunc(out, func(a, b itCopy) int { return int(a.uid) - int(b.uid) })
	return out
}

// Akzeptanz S2.5 je Server: Ablage mit \Draft \Seen und der vorab
// vergebenen Message-ID; der Spiegel erkennt die Kopie (Inhalts-Hash); nach
// einer Änderung eine neue Kopie, die alte bleibt; ein Mailprogramm ersetzt
// die Kopie → drafts.update draft_taken_over.
func TestIntegrationDraftPush(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITDraftEnv(t, srv, user)
		out := e.create("Termin " + srv.name)
		res, err := e.sync(ops.SyncOptions{})
		if err != nil {
			t.Fatal(err)
		}
		if res.Drafts == nil || res.Drafts.Pushed != 1 || res.Drafts.Mailbox != e.path {
			t.Fatalf("drafts = %+v", res.Drafts)
		}
		cs := e.copies(out.MessageID)
		if len(cs) != 1 || !slices.Contains(cs[0].flags, imap.FlagDraft) || !slices.Contains(cs[0].flags, imap.FlagSeen) ||
			cs[0].header != "<"+out.MessageID+">" {
			t.Fatalf("copies = %+v", cs)
		}
		d := e.draft(out.DraftID)
		if d.RemoteState != drafts.RemotePushed || d.RemoteUID != int64(cs[0].uid) || d.RemoteUIDValidity == 0 {
			t.Fatalf("draft = %+v, server uid %d", d, cs[0].uid)
		}
		// Der Spiegel hat die Kopie mit dem Hash, den der Push vermerkt hat
		// (Server ändert die Bytes nicht), sonst meldete E6 Fehlalarme.
		var n int
		if err := e.st.DBForTest().QueryRow(`SELECT count(*) FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
			WHERE mm.mailbox_id = ? AND mm.uid = ? AND m.content_hash = ?`, d.RemoteMailboxID, d.RemoteUID, d.PushedContentHash).Scan(&n); err != nil || n != 1 {
			t.Fatalf("mirrored copy with the pushed content hash: %d, %v", n, err)
		}

		// Änderung: neue Kopie, die alte bleibt.
		if _, err := e.do(itAssistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "body_markdown": "Geänderter Text"}); err != nil {
			t.Fatal(err)
		}
		if res, err := e.sync(ops.SyncOptions{}); err != nil || res.Drafts == nil || res.Drafts.Pushed != 1 {
			t.Fatalf("sync after change: %+v, %v", res.Drafts, err)
		}
		cs = e.copies(out.MessageID)
		d = e.draft(out.DraftID)
		if len(cs) != 2 || d.RemoteUID != int64(cs[1].uid) || len(d.RemotePrevious) != 1 || d.RemotePrevious[0].UID != int64(cs[0].uid) {
			t.Fatalf("copies %+v, draft uid %d, previous %+v", cs, d.RemoteUID, d.RemotePrevious)
		}
		got, err := e.do(itAssistant, "drafts.get", map[string]any{"draft_id": out.DraftID})
		if err != nil || len(got.(ops.DraftDetail).RemotePreviousUIDs) != 1 {
			t.Fatalf("drafts.get = %+v, %v", got, err)
		}

		// Ein Mailprogramm übernimmt: neue Fassung (gleiche Message-ID),
		// die Kopie von PostKauz verschwindet.
		c := e.client
		if _, err := c.Select(e.path, nil).Wait(); err != nil {
			t.Fatal(err)
		}
		sec := &imap.FetchItemBodySection{Peek: true}
		msgs, err := c.Fetch(imap.UIDSetNum(cs[1].uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
		if err != nil || len(msgs) != 1 {
			t.Fatalf("fetch: %v", err)
		}
		// Bearbeitet: eine Kopfzeile mehr (der Inhalt ist anders, die
		// Message-ID bleibt, wie bei Thunderbird).
		edited := append([]byte("X-Edited-By: human\r\n"), msgs[0].FindBodySection(sec)...)
		app := c.Append(e.path, int64(len(edited)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}})
		if _, err := app.Write(edited); err != nil {
			t.Fatal(err)
		}
		if err := app.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := app.Wait(); err != nil {
			t.Fatal(err)
		}
		set := imap.UIDSetNum(cs[1].uid)
		if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.UIDExpunge(set).Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := e.sync(ops.SyncOptions{}); err != nil {
			t.Fatal(err)
		}
		_, err = e.do(itAssistant, "drafts.update", map[string]any{"draft_id": out.DraftID, "subject": "Agent will nochmal"})
		var de *ops.DeniedError
		if !errors.As(err, &de) || de.Reason != drafts.ReasonDraftTakenOver {
			t.Fatalf("update after take-over = %v", err)
		}
		if d := e.draft(out.DraftID); d.RemoteState != drafts.RemoteTakenOver {
			t.Fatalf("remote = %s", d.RemoteState)
		}
		if len(e.copies(out.MessageID)) != 2 {
			t.Fatal("PostKauz touched the copies in the Drafts folder")
		}
	})
}

// Ohne Entwürfe-Ordner (hier: [draft] mailbox nennt einen Ordner, den es
// nicht gibt) no_drafts_mailbox; ein Entwurf in ready mit geänderter
// Identität wird nicht abgelegt und fällt auf draft zurück.
func TestIntegrationDraftPushRefusals(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITDraftEnv(t, srv, user)
		ctx := context.Background()
		out := e.create("Kein Ordner " + srv.name)
		e.runner.DraftMailbox = fmt.Sprintf("PKITFehlt%d", time.Now().UnixNano())
		res, err := e.sync(ops.SyncOptions{})
		if err != nil || res.Drafts == nil || res.Drafts.Error != ops.ReasonNoDraftsMailbox {
			t.Fatalf("drafts = %+v, %v", res.Drafts, err)
		}
		if d := e.draft(out.DraftID); d.RemoteState != drafts.RemotePending || d.PushNote != drafts.PushNoteNoDraftsMailbox {
			t.Fatalf("draft = %+v", d)
		}
		if _, err := e.svc.Abandon(ctx, out.DraftID); err != nil {
			t.Fatal(err)
		}

		// ready, danach ändert sich die Identität (Signatur).
		e.runner.DraftMailbox = ""
		if _, err := e.st.AddIdentity(ctx, store.Identity{AccountID: e.acc.ID, Name: "IT", Email: e.acc.Email, SignatureText: "Gruß IT"}); err != nil {
			t.Fatal(err)
		}
		r := e.create("Freigegeben " + srv.name)
		reqOut, err := e.do(itAssistant, "drafts.request_ready", map[string]any{"draft_id": r.DraftID})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.do(itHuman, "drafts.approve", map[string]any{"approval_id": reqOut.(ops.DraftsRequestOutput).ApprovalID}); err != nil {
			t.Fatal(err)
		}
		if err := e.st.RemoveIdentity(ctx, e.draft(r.DraftID).IdentityID); err != nil {
			t.Fatal(err)
		}
		_ = e.st.ExecForTest(`UPDATE jobs SET run_at = 0`) // nach dem Fehler oben sonst erst in einer Minute fällig
		res, err = e.sync(ops.SyncOptions{})
		if err != nil || res.Drafts == nil || res.Drafts.Stale != 1 || res.Drafts.Pushed != 0 {
			t.Fatalf("drafts = %+v, %v", res.Drafts, err)
		}
		if d := e.draft(r.DraftID); d.Status != "draft" || d.PushNote != drafts.PushNoteApprovalStale {
			t.Fatalf("draft = %s, note %q", d.Status, d.PushNote)
		}
		if cs := e.copies(r.MessageID); len(cs) != 0 {
			t.Fatalf("stale ready draft was pushed: %+v", cs)
		}
	})
}

// sync --watch legt einen Entwurf innerhalb von 10 s ab (Standard
// --jobs-interval), auch wenn der nächste Lauf erst in einer Stunde käme.
// Ohne Verbindung bleibt er pending; der nächste sync holt nach.
func TestIntegrationDraftPushWatchAndOffline(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITDraftEnv(t, srv, user)
		// Offline: Verbindungsaufbau gegen einen geschlossenen Port.
		var offline atomic.Bool
		offline.Store(true)
		e.runner.Connect = func(ctx context.Context, a store.Account, pw secrets.Secret, idle time.Duration) (RemoteCloser, error) {
			port := a.Port
			if offline.Load() {
				port = 1
			}
			p, err := imapprov.Connect(ctx, imapprov.Config{Host: a.Host, Port: port, TLS: a.TLS, Username: a.Username,
				InsecureTestServer: true, CommandTimeout: 15 * time.Second, IdleRestart: idle}, pw)
			if err != nil {
				return nil, err
			}
			return imapRemote{p}, nil
		}
		off := e.create("Offline " + srv.name)
		if _, err := e.sync(ops.SyncOptions{}); !errors.Is(err, provider.ErrConnection) {
			t.Fatalf("offline sync = %v", err)
		}
		if d := e.draft(off.DraftID); d.RemoteState != drafts.RemotePending {
			t.Fatalf("offline draft = %s", d.RemoteState)
		}
		offline.Store(false)
		if res, err := e.sync(ops.SyncOptions{}); err != nil || res.Drafts == nil || res.Drafts.Pushed != 1 || len(e.copies(off.MessageID)) != 1 {
			t.Fatalf("catch-up sync = %+v, %v", res.Drafts, err)
		}

		// Watcher mit Standardwerten für die Jobs (10 s).
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var rounds atomic.Int32
		done := make(chan error, 1)
		go func() {
			_, err := e.runner.Sync(ctx, e.acc, itPass, ops.SyncOptions{Watch: true, Interval: time.Hour, Mailboxes: []string{e.path}},
				func(p ops.Progress) {
					if p.Event == "round" {
						rounds.Add(1)
					}
				})
			done <- err
		}()
		waitFor(t, 30*time.Second, func() bool { return rounds.Load() == 1 })
		w := e.create("Watcher " + srv.name)
		start := time.Now()
		waitFor(t, 15*time.Second, func() bool { return len(e.copies(w.MessageID)) == 1 })
		took := time.Since(start)
		t.Logf("%s: draft in the Drafts folder %s after drafts.create", srv.name, took.Round(time.Millisecond))
		if took > 10*time.Second+time.Second { // 10 s plus Ablage
			t.Fatalf("watcher pushed after %s", took)
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}
