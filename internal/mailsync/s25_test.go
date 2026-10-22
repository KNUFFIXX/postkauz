package mailsync

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.5: Job draft_push im Sync, gegen den IMAP-Server im Prozess. Die
// Integrationstests (draftpush_integration_test.go) prüfen dasselbe gegen
// Dovecot, Stalwart und GreenMail.

type pushEnv struct {
	*env
	addr   string
	u      *imapmemserver.User
	svc    *drafts.Service
	runner *Runner
	// offline: Verbindungsaufbau scheitert (Server nicht erreichbar).
	offline atomic.Bool
}

func newPushEnv(t *testing.T, boxes ...string) *pushEnv {
	t.Helper()
	addr, u := memServer(t, false)
	for _, b := range boxes {
		if err := u.Create(b, nil); err != nil {
			t.Fatal(err)
		}
	}
	e := &pushEnv{env: newEnv(t, addr), addr: addr, u: u}
	e.svc = &drafts.Service{Store: e.st}
	deps := ops.Deps{Accounts: e.st, Messages: e.st, Drafts: e.svc, DraftBlobs: e.blobs, Draft: ops.DraftConfig{AttachDir: t.TempDir()}}
	e.runner = &Runner{Store: e.st, Blobs: e.blobs, Pusher: ops.NewDraftPusher(deps),
		Connect: func(ctx context.Context, a store.Account, pw secrets.Secret, idle time.Duration) (RemoteCloser, error) {
			if e.offline.Load() {
				return nil, fmt.Errorf("%w: test server is offline", provider.ErrConnection)
			}
			return imapRemote{connect(t, addr, idle)}, nil
		}}
	return e
}

func (e *pushEnv) account(t *testing.T) store.Account {
	t.Helper()
	a, err := e.st.AccountByName(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// newDraft legt einen Entwurf an wie drafts.create (mit Job draft_push).
func (e *pushEnv) newDraft(t *testing.T, subject string) store.Draft {
	t.Helper()
	res, err := e.svc.Create(context.Background(), drafts.NewDraft{AccountID: e.acct, Kind: drafts.KindNew, CreatedBy: "cli",
		Content: drafts.Content{To: []store.Addr{{Address: "b@example.org"}}, Subject: subject, BodyMarkdown: "Hallo " + subject}})
	if err != nil {
		t.Fatal(err)
	}
	if res.PushJobID == 0 {
		t.Fatal("no draft_push job")
	}
	return res.Draft
}

func (e *pushEnv) sync(t *testing.T, o ops.SyncOptions) (ops.SyncOutput, error) {
	t.Helper()
	return e.runner.Sync(context.Background(), e.account(t), testPass, o, nil)
}

func (e *pushEnv) draft(t *testing.T, id int64) store.Draft {
	t.Helper()
	d, err := e.st.Draft(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// settle rückt die letzte Änderung aller Entwürfe 31 s zurück, als wäre
// die Ruhezeit nach einer Änderung (Entscheid 43, S2.6) vorbei.
func (e *pushEnv) settle(t *testing.T) {
	t.Helper()
	if err := e.st.ExecForTest(`UPDATE drafts SET updated_at = updated_at - 31000`); err != nil {
		t.Fatal(err)
	}
}

// serverCopies liefert UID und Flags aller Nachrichten mit Message-ID mid
// im Ordner (zweiter Client, wie ein Mailprogramm).
func serverCopies(t *testing.T, c *imapclient.Client, mailbox, mid string) map[imap.UID][]imap.Flag {
	t.Helper()
	if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		t.Fatal(err)
	}
	data, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	out := map[imap.UID][]imap.Flag{}
	uids := data.AllUIDs()
	if len(uids) == 0 {
		return out
	}
	msgs, err := c.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		out[m.UID] = m.Flags
	}
	return out
}

func (e *pushEnv) jobState(t *testing.T) (state string, attempts int) {
	t.Helper()
	db := e.st.DBForTest()
	if err := db.QueryRow(`SELECT state, attempts FROM jobs WHERE type = 'draft_push' ORDER BY id DESC LIMIT 1`).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	return
}

// Akzeptanz S2.5 (Server im Prozess): sync legt zuerst ab und spiegelt
// dann; die Kopie trägt \Draft \Seen und die beim Anlegen vergebene
// Message-ID; der Spiegel erkennt sie (gleicher Inhalts-Hash). Eine
// Änderung legt eine neue Kopie ab, die alte bleibt. Ersetzt ein
// Mailprogramm die Kopie, ist drafts.update draft_taken_over.
func TestSyncPushesDraftsAndDetectsTakeOver(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	ctx := context.Background()
	d := e.newDraft(t, "Termin")
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Drafts == nil || out.Drafts.Pushed != 1 || out.Drafts.Mailbox != "Drafts" || out.Drafts.Error != "" {
		t.Fatalf("drafts = %+v", out.Drafts)
	}
	client := imapSession(t, e.addr)
	copies := serverCopies(t, client, "Drafts", d.MessageIDHeader)
	if len(copies) != 1 {
		t.Fatalf("copies = %v", copies)
	}
	var first imap.UID
	for uid, flags := range copies {
		first = uid
		if !slices.Contains(flags, imap.FlagDraft) || !slices.Contains(flags, imap.FlagSeen) {
			t.Fatalf("flags = %v", flags)
		}
	}
	d = e.draft(t, d.ID)
	if d.RemoteState != drafts.RemotePushed || d.RemoteUID != int64(first) || d.RemoteUIDValidity == 0 || d.PushedDate == "" {
		t.Fatalf("draft = %+v", d)
	}
	// Der Lauf hat danach gespiegelt: Die Kopie ist im Spiegel, mit dem
	// Inhalts-Hash, den der Push vermerkt hat.
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
		JOIN mailboxes b ON b.id = mm.mailbox_id WHERE b.path = 'Drafts' AND mm.uid = ? AND m.message_id_header = ? AND m.content_hash = ?`,
		d.RemoteUID, d.MessageIDHeader, d.PushedContentHash); n != 1 {
		t.Fatalf("mirrored copy with the pushed hash: %d", n)
	}
	if st, _ := e.jobState(t); st != "done" {
		t.Fatalf("job = %s", st)
	}
	// Nichts mehr zu tun.
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts != nil {
		t.Fatalf("second sync: %+v, %v", out.Drafts, err)
	}

	// Änderung: neue Kopie, die alte bleibt (kein Expunge).
	c := drafts.ContentOf(d)
	c.BodyMarkdown = "Hallo geändert"
	if _, err := e.svc.Update(ctx, d.ID, c, ""); err != nil {
		t.Fatal(err)
	}
	e.settle(t)
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts == nil || out.Drafts.Pushed != 1 {
		t.Fatalf("sync after change: %+v, %v", out.Drafts, err)
	}
	d = e.draft(t, d.ID)
	if copies := serverCopies(t, client, "Drafts", d.MessageIDHeader); len(copies) != 2 || d.RemoteUID == int64(first) ||
		len(d.RemotePrevious) != 1 || d.RemotePrevious[0].UID != int64(first) {
		t.Fatalf("copies %v, draft %+v", copies, d)
	}

	// Ein Mailprogramm übernimmt: speichert eine bearbeitete Fassung (neue
	// UID, gleiche Message-ID) und entfernt die Kopie von PostKauz.
	edited := strings.Replace(string(rawOf(t, client, "Drafts", imap.UID(d.RemoteUID))), "geändert", "vom Menschen", 1)
	if _, err := e.u.Append("Drafts", lit([]byte(edited)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	expunge(t, client, "Drafts", imap.UID(d.RemoteUID))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	c.BodyMarkdown = "Agent will nochmal"
	_, err = e.svc.Update(ctx, d.ID, c, "")
	if drafts.ReasonOf(err) != drafts.ReasonDraftTakenOver {
		t.Fatalf("update after take-over = %v", err)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemoteTakenOver || d.BodyMarkdown != "Hallo geändert" {
		t.Fatalf("draft after take-over = %s %q", d.RemoteState, d.BodyMarkdown)
	}
}

// rawOf holt eine Nachricht per UID.
func rawOf(t *testing.T, c *imapclient.Client, mailbox string, uid imap.UID) []byte {
	t.Helper()
	if _, err := c.Select(mailbox, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	sec := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil || len(msgs) != 1 {
		t.Fatalf("fetch %d: %v", uid, err)
	}
	return msgs[0].FindBodySection(sec)
}

// expunge löscht eine Nachricht endgültig, wie ein Mailprogramm nach dem
// Speichern einer neuen Fassung (nur der Testclient; PostKauz kann das nicht).
func expunge(t *testing.T, c *imapclient.Client, mailbox string, uid imap.UID) {
	t.Helper()
	if _, err := c.Select(mailbox, nil).Wait(); err != nil {
		t.Fatal(err)
	}
	if err := c.Store(imap.UIDSetNum(uid), &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.UIDExpunge(imap.UIDSetNum(uid)).Close(); err != nil {
		t.Fatal(err)
	}
}

// Ohne Entwürfe-Ordner: Fehler no_drafts_mailbox, der Sync spiegelt
// trotzdem, der Entwurf bleibt pending mit Hinweis. [draft] mailbox legt
// den Ordner fest.
func TestSyncWithoutDraftsMailbox(t *testing.T) {
	e := newPushEnv(t, "Archiv")
	d := e.newDraft(t, "Ohne Ordner")
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Drafts == nil || out.Drafts.Error != ops.ReasonNoDraftsMailbox || out.Drafts.Pushed != 0 || len(out.Mailboxes) != 2 {
		t.Fatalf("out = %+v, drafts %+v", out, out.Drafts)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemotePending || d.PushNote != drafts.PushNoteNoDraftsMailbox {
		t.Fatalf("draft = %+v", d)
	}
	if st, n := e.jobState(t); st != "queued" || n != 1 {
		t.Fatalf("job = %s, %d attempts", st, n)
	}
	// Ordner aus der Konfiguration; der Job wartet nach dem Fehler, also
	// fällig machen (sonst wiederholt er nach einer Minute).
	e.runner.DraftMailbox = "Archiv"
	_ = e.st.ExecForTest(`UPDATE jobs SET run_at = 0`)
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts == nil || out.Drafts.Pushed != 1 || out.Drafts.Mailbox != "Archiv" {
		t.Fatalf("with [draft] mailbox: %+v, %v", out.Drafts, err)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemotePushed || d.PushNote != "" {
		t.Fatalf("draft = %+v", d)
	}
	// Ein festgelegter Ordner, den es nicht gibt, ist auch no_drafts_mailbox.
	e.runner.DraftMailbox = "Fehlt"
	e.newDraft(t, "Zwei")
	if out, _ := e.sync(t, ops.SyncOptions{}); out.Drafts == nil || out.Drafts.Error != ops.ReasonNoDraftsMailbox {
		t.Fatalf("missing [draft] mailbox: %+v", out.Drafts)
	}
}

// Ohne Verbindung bleibt der Entwurf pending; der nächste sync holt nach.
// Ein Trockenlauf legt nichts ab.
func TestPushWithoutConnectionIsCaughtUp(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	d := e.newDraft(t, "Offline")
	e.offline.Store(true)
	if _, err := e.sync(t, ops.SyncOptions{}); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("offline sync = %v", err)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemotePending {
		t.Fatalf("draft = %s", d.RemoteState)
	}
	if st, n := e.jobState(t); st != "queued" || n != 0 {
		t.Fatalf("job = %s, %d attempts", st, n)
	}
	e.offline.Store(false)
	if out, err := e.sync(t, ops.SyncOptions{DryRun: true}); err != nil || out.Drafts != nil {
		t.Fatalf("dry run: %+v, %v", out.Drafts, err)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemotePending {
		t.Fatalf("dry run pushed: %s", d.RemoteState)
	}
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts == nil || out.Drafts.Pushed != 1 {
		t.Fatalf("sync = %+v, %v", out.Drafts, err)
	}
	if d := e.draft(t, d.ID); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("draft = %s", d.RemoteState)
	}
}

// sync --watch prüft die Jobs auch zwischen zwei Läufen (hier alle 100 ms
// statt 10 s; das Intervall der Läufe ist eine Stunde).
func TestWatchPushesBetweenRounds(t *testing.T) {
	if DefaultJobsInterval != 10*time.Second {
		t.Fatalf("default jobs interval = %s, A02 E1 says 10 s", DefaultJobsInterval)
	}
	e := newPushEnv(t, "Drafts")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rounds atomic.Int32
	var pushed atomic.Int32
	done := make(chan error, 1)
	go func() {
		out, err := e.runner.Sync(ctx, e.account(t), testPass, ops.SyncOptions{Watch: true, Interval: time.Hour,
			JobsInterval: 100 * time.Millisecond}, func(p ops.Progress) {
			switch p.Event {
			case "round":
				rounds.Add(1)
			case "drafts":
				pushed.Add(int32(p.Done))
			}
		})
		if err == nil && (out.Drafts == nil || out.Drafts.Pushed != 1) {
			err = fmt.Errorf("watch output drafts = %+v", out.Drafts)
		}
		done <- err
	}()
	waitFor(t, 5*time.Second, func() bool { return rounds.Load() == 1 })
	d := e.newDraft(t, "Watcher")
	start := time.Now()
	client := imapSession(t, e.addr)
	waitFor(t, 5*time.Second, func() bool { return len(serverCopies(t, client, "Drafts", d.MessageIDHeader)) == 1 })
	t.Logf("draft in the Drafts folder after %s", time.Since(start).Round(time.Millisecond))
	waitFor(t, 2*time.Second, func() bool { return pushed.Load() == 1 })
	if rounds.Load() != 1 {
		t.Fatalf("rounds = %d: the push did not wait for a round", rounds.Load())
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
