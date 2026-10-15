package ops

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

type fakeRunner struct {
	got  SyncOptions
	acct string
	pw   secrets.Secret
	err  error
}

func (f *fakeRunner) Sync(_ context.Context, a store.Account, pw secrets.Secret, o SyncOptions, progress func(Progress)) (SyncOutput, error) {
	f.got, f.acct, f.pw = o, a.Name, pw
	if progress != nil {
		progress(Progress{Event: "window", Mailbox: "INBOX", Done: 1, Total: 1})
	}
	if f.err != nil {
		return SyncOutput{}, f.err
	}
	return SyncOutput{Account: a.Name, Mode: "incremental", MessagesMirrored: 1,
		Mailboxes: []SyncMailbox{{Path: "INBOX", Role: "inbox", New: 1, Total: 1, Mirrored: 1}}}, nil
}

type mapBlobs map[string]string

func (m mapBlobs) Get(sha string) (io.ReadCloser, error) {
	v, ok := m[sha]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(strings.NewReader(v)), nil
}

type mirrorEnv struct {
	*accountsEnv
	runner *fakeRunner
	acct   int64
}

func newMirrorEnv(t *testing.T) *mirrorEnv {
	t.Helper()
	e := newAccountsEnv(t)
	m := &mirrorEnv{accountsEnv: e, runner: &fakeRunner{}}
	for _, op := range []Operation{SyncRun(e.st, e.sec, m.runner), MailboxesList(e.st, e.st),
		MessagesGet(e.st, e.st, mapBlobs{"sha-1": "Message-ID: <a@example.org>\r\nSubject: Hallo\r\n\r\nText"})} {
		if err := e.x.Registry.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.run("accounts.add", demoInput(), canary, false); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.AccountByName(context.Background(), "demo")
	m.acct = a.ID
	return m
}

func TestSyncRun(t *testing.T) {
	e := newMirrorEnv(t)
	var events []Progress
	b, _ := json.Marshal(map[string]any{"full": true, "window": 50})
	res, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "sync.run", Input: b,
		Progress: func(p Progress) { events = append(events, p) }})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(SyncOutput)
	if out.Account != "demo" || out.MessagesMirrored != 1 || e.runner.acct != "demo" || !e.runner.got.Full || e.runner.got.Window != 50 {
		t.Fatalf("out = %+v, runner = %+v", out, e.runner)
	}
	if e.runner.pw != canary {
		t.Fatal("runner did not get the stored password")
	}
	if len(events) != 1 || events[0].Mailbox != "INBOX" {
		t.Fatalf("progress = %+v", events)
	}
	ev := e.au.Events()
	last := ev[len(ev)-1]
	if last.Operation != "sync.run" || last.Scope != string(policy.ScopeMailSync) || strings.Contains(string(last.ParamsRedacted), canary) {
		t.Fatalf("audit = %+v", last)
	}
	// Eingabefehler
	for _, in := range []map[string]any{{"window": 0, "interval_seconds": 5}, {"watch": true, "idle_restart_seconds": 60},
		{"account": "Nope_1"}, {"bogus": 1}} {
		b, _ := json.Marshal(in)
		_, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: b})
		var ie *InputError
		if !errors.As(err, &ie) {
			t.Errorf("%v: want input error, got %v", in, err)
		}
	}
	// Watch-Optionen kommen als Dauer an.
	b, _ = json.Marshal(map[string]any{"watch": true, "interval_seconds": 30, "idle_restart_seconds": 420})
	if _, err := e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: b}); err != nil {
		t.Fatal(err)
	}
	if !e.runner.got.Watch || e.runner.got.Interval != 30*time.Second || e.runner.got.IdleRestart != 7*time.Minute {
		t.Fatalf("watch options = %+v", e.runner.got)
	}
	// Ein Fehler des Runners darf das Passwort nicht weitertragen.
	e.runner.err = errors.New("server said " + canary)
	_, err = e.x.Execute(context.Background(), Request{Key: adminKey, Operation: "sync.run", Input: json.RawMessage(`{}`)})
	if err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("err = %v", err)
	}
	// Ein reader-Schlüssel darf nicht syncen (mail:sync fehlt).
	_, err = e.x.Execute(context.Background(), Request{Key: policy.Key{Scopes: policy.Roles["reader"]}, Operation: "sync.run", Input: json.RawMessage(`{}`)})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("reader: %v", err)
	}
}

func TestResolveAccount(t *testing.T) {
	e := newMirrorEnv(t)
	ctx := context.Background()
	if a, err := ResolveAccount(ctx, e.st, ""); err != nil || a.Name != "demo" {
		t.Fatalf("single account: %v %v", a.Name, err)
	}
	in := demoInput()
	in["name"] = "zweites"
	if _, err := e.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	var ie *InputError
	if _, err := ResolveAccount(ctx, e.st, ""); !errors.As(err, &ie) || !strings.Contains(err.Error(), "--account") {
		t.Fatalf("two accounts: %v", err)
	}
	if _, err := ResolveAccount(ctx, e.st, "fehlt"); !errors.Is(err, store.ErrAccountNotFound) {
		t.Fatalf("unknown: %v", err)
	}
}

func (e *mirrorEnv) seed(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	if _, _, err := e.st.SyncMailboxes(ctx, e.acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	_ = e.st.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('sha-1',40,1)`)
	_, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.acct, MailboxID: boxes[0].ID,
		New: []store.NewMessage{{Link: store.LinkFlags{UID: 5, Seen: true, Keywords: []string{"$Erledigt"}}, MessageID: "a@example.org",
			Subject: "Hallo", From: "Absender <a@example.org>", To: json.RawMessage(`[{"name":"B","address":"b@example.org"}]`),
			Size: 40, BlobSHA256: "sha-1", BlobSize: 40, TextPlain: "Text",
			DateHeader: time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC), InternalDate: time.Date(2025, 1, 6, 9, 1, 0, 0, time.UTC)}},
		State: &store.MailboxState{UIDValidity: 3, UIDNext: 6, Phase: store.PhaseReady, Total: 1}})
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "a@example.org")
	return ids[0]
}

func TestMailboxesList(t *testing.T) {
	e := newMirrorEnv(t)
	e.seed(t)
	out, err := e.run("mailboxes.list", map[string]any{"account": "demo"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(MailboxesListOutput)
	if len(o.Mailboxes) != 1 {
		t.Fatalf("out = %+v", o)
	}
	m := o.Mailboxes[0]
	if m.Path != "INBOX" || m.Role != "inbox" || m.Total != 1 || m.Mirrored != 1 || m.Unread != 0 || m.SyncPhase != "ready" || m.LastSyncAt == "" {
		t.Fatalf("mailbox = %+v", m)
	}
	// reader darf listen.
	if _, err := e.x.Execute(context.Background(), Request{Key: policy.Key{Scopes: policy.Roles["reader"]}, Operation: "mailboxes.list",
		Input: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
}

func TestMessagesGet(t *testing.T) {
	e := newMirrorEnv(t)
	id := e.seed(t)
	out, err := e.run("messages.get", map[string]any{"id": id, "headers": true}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(MessageOutput)
	if o.Subject != "Hallo" || strings.Join(o.Flags, ",") != "seen" || o.Keywords[0] != "$Erledigt" || o.Text != nil ||
		o.Headers != "Message-ID: <a@example.org>\nSubject: Hallo\n" || len(o.Mailboxes) != 1 || o.Mailboxes[0].UID != 5 ||
		o.Date != "2025-01-06T09:00:00Z" || o.DeletedRemote {
		t.Fatalf("out = %+v (headers %q)", o, o.Headers)
	}
	out, err = e.run("messages.get", map[string]any{"message_id": "<a@example.org>"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if o := out.(MessageOutput); o.ID != id || o.Text == nil || *o.Text != "Text" || o.Headers != "" {
		t.Fatalf("by message-id = %+v", o)
	}
	for _, in := range []map[string]any{{}, {"id": id, "message_id": "x"}} {
		var ie *InputError
		if _, err := e.run("messages.get", in, "", false); !errors.As(err, &ie) {
			t.Errorf("%v: %v", in, err)
		}
	}
	if _, err := e.run("messages.get", map[string]any{"id": 999}, "", false); !errors.Is(err, store.ErrMessageNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if _, err := e.run("messages.get", map[string]any{"id": id, "account": "andere"}, "", false); !errors.Is(err, store.ErrMessageNotFound) {
		t.Fatalf("other account: %v", err)
	}
	// Audit nennt nur das Ziel, keinen Inhalt.
	ev := e.au.Events()
	last := ev[len(ev)-1]
	if strings.Contains(string(last.ParamsRedacted), "Hallo") {
		t.Fatalf("audit leaks content: %s", last.ParamsRedacted)
	}
}
