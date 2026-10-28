package actions

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// fakeRemote merkt sich die Kommandos und liefert Köpfe aus headers.
type fakeRemote struct {
	headers map[provider.UID]string
	calls   []string
	moveErr error
	flagErr error
	nextUID provider.UID
}

func (f *fakeRemote) FetchHeaders(_ context.Context, mailbox string, _ uint32, uids []provider.UID) ([]provider.Header, error) {
	f.calls = append(f.calls, "FETCH "+mailbox)
	var out []provider.Header
	for _, u := range uids {
		if h, ok := f.headers[u]; ok {
			out = append(out, provider.Header{UID: u, RawHeader: []byte(h)})
		}
	}
	return out, nil
}

func (f *fakeRemote) SetFlags(_ context.Context, mailbox string, _ uint32, uids []provider.UID, p provider.FlagPatch) error {
	f.calls = append(f.calls, "STORE "+mailbox)
	return f.flagErr
}

func (f *fakeRemote) Move(_ context.Context, from string, _ uint32, uids []provider.UID, to string) (provider.MoveResult, error) {
	f.calls = append(f.calls, "MOVE "+from+" -> "+to)
	if f.moveErr != nil {
		return provider.MoveResult{}, f.moveErr
	}
	res := provider.MoveResult{UIDValidity: 8, UIDs: map[provider.UID]provider.UID{}}
	for _, u := range uids {
		f.nextUID++
		res.UIDs[u] = f.nextUID
	}
	return res, nil
}

type mapBlobs map[string]string

func (m mapBlobs) Get(sha string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(m[sha])), nil
}

const rawMail = "Message-ID: <a@example.org>\r\nSubject: Hallo\r\n\r\nText\r\n"

// env: INBOX (UIDVALIDITY 7) mit Nachricht 1 (UID 1, selbst geladen) und
// Nachricht 2 (UID 2, nur verknüpft), Archiv (8).
func env(t *testing.T) (*store.SQLite, int64, map[string]int64) {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	ctx := context.Background()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	acct, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "u", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv", Name: "Archiv", Role: "archive"}}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	boxes, _ := st.Mailboxes(ctx, acct)
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	msg := func(uid uint32, mid string) store.NewMessage {
		return store.NewMessage{Link: store.LinkFlags{UID: uid}, MessageID: mid, Subject: "Hallo", From: "a@example.org", Size: 40,
			BlobSHA256: "sha-" + mid, BlobSize: 40, ContentHash: "h-" + mid}
	}
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids["INBOX"], New: []store.NewMessage{msg(1, "a@example.org")},
		State: &store.MailboxState{UIDValidity: 7, UIDNext: 3, Phase: store.PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids["Archiv"], New: []store.NewMessage{msg(5, "b@example.org")},
		State: &store.MailboxState{UIDValidity: 8, UIDNext: 6, Phase: store.PhaseReady, Total: 1}}); err != nil {
		t.Fatal(err)
	}
	// Nachricht 2 zusätzlich in INBOX, nur per Message-ID verknüpft.
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: ids["INBOX"],
		Links: []store.Link{{MessageID: 2, Flags: store.LinkFlags{UID: 2}}}}); err != nil {
		t.Fatal(err)
	}
	return st, acct, ids
}

func blobs() mapBlobs {
	return mapBlobs{"sha-a@example.org": rawMail, "sha-b@example.org": "Message-ID: <b@example.org>\r\nSubject: Hallo\r\n\r\nB\r\n"}
}

// E1: mark wird angewendet, die Markierung fällt; move trägt die Kopie im
// Spiegel um (neue UID aus COPYUID).
func TestRunnerMarkAndMove(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	svc := &Service{Store: st}
	yes := true
	plan, err := svc.PlanFlags(ctx, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMark, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{Mark: &store.MarkParams{Seen: &yes}}}); err != nil {
		t.Fatal(err)
	}
	dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
	plan, err = svc.PlanMove(ctx, []int64{1}, "", dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}}); err != nil {
		t.Fatal(err)
	}
	r := &Runner{Store: st, Blobs: blobs()}
	f := &fakeRemote{nextUID: 40}
	rep, err := r.Run(ctx, acct, f)
	if err != nil || rep.Actions != 2 || rep.OK != 2 {
		t.Fatalf("run = %+v %v", rep, err)
	}
	if strings.Join(f.calls, "|") != "STORE INBOX|MOVE INBOX -> Archiv" {
		t.Fatalf("calls = %v", f.calls)
	}
	if n, _ := st.PendingFlagCopies(ctx, 1); n != 0 {
		t.Fatal("pending flag left")
	}
	cs, _ := st.ServerCopies(ctx, 1)
	if len(cs) != 1 || cs[0].Mailbox != "Archiv" || cs[0].UID != 41 {
		t.Fatalf("copies = %+v", cs)
	}
	list, _ := st.ListActions(ctx, store.ActionFilter{})
	for _, a := range list {
		if a.State != store.ActionDone {
			t.Fatalf("action %d is %s", a.ID, a.State)
		}
	}
	if rep, err := r.Run(ctx, acct, f); err != nil || rep.Actions != 0 {
		t.Fatalf("second run = %+v %v", rep, err)
	}
}

// T2 Punkt 3: Eine nur verknüpfte Kopie wird erst nach dem Kopfvergleich
// angefasst; weicht der Kopf ab, ist das Ziel stale und kein MOVE geht
// an den Server. Passt er, gilt die Kopie danach als geladen.
func TestRunnerChecksHeaderOfLinkedCopy(t *testing.T) {
	for _, tc := range []struct {
		name, header string
		want         string
	}{
		{"other header", "Message-ID: <b@example.org>\r\nSubject: Fremd\r\n\r\n", store.TargetStale},
		{"same header", "Message-ID: <b@example.org>\nSubject: Hallo\n\n", store.TargetOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, acct, ids := env(t)
			ctx := context.Background()
			svc := &Service{Store: st}
			dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
			plan, err := svc.PlanMove(ctx, []int64{2}, "INBOX", dest)
			if err != nil {
				t.Fatal(err)
			}
			a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{2}, Plan: plan, RequestedBy: "cli",
				Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path}})
			if err != nil {
				t.Fatal(err)
			}
			f := &fakeRemote{headers: map[provider.UID]string{2: tc.header}}
			if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
				t.Fatal(err)
			}
			got, _ := st.Action(ctx, a.ID)
			if got.Targets[0].Result != tc.want {
				t.Fatalf("result = %+v", got.Targets)
			}
			moved := strings.Contains(strings.Join(f.calls, "|"), "MOVE")
			if moved != (tc.want == store.TargetOK) {
				t.Fatalf("calls = %v", f.calls)
			}
		})
	}
}

// A8: Ist der Schlüssel hinter der Aktion widerrufen, geht kein Kommando
// an den Server, und die lokale Änderung wird zurückgenommen.
func TestRunnerRefusesRevokedOrigin(t *testing.T) {
	st, acct, _ := env(t)
	ctx := context.Background()
	svc := &Service{Store: st}
	yes := true
	plan, _ := svc.PlanFlags(ctx, []int64{1})
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMark, IDs: []int64{1}, Plan: plan, RequestedBy: "key:weg",
		Params: store.ActionParams{Mark: &store.MarkParams{Seen: &yes}}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{}
	r := &Runner{Store: st, Blobs: blobs(), Origin: func(_ context.Context, key string, scope policy.Scope, approved bool) (string, error) {
		if key != "key:weg" || scope != policy.ScopeMailTag || approved {
			t.Errorf("origin check got %s %s %v", key, scope, approved)
		}
		return "key_revoked", nil
	}}
	if _, err := r.Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Action(ctx, a.ID)
	if len(f.calls) != 0 || got.State != store.ActionFailed || got.Error != "key_revoked" {
		t.Fatalf("calls %v, action %+v", f.calls, got)
	}
	m, _ := st.MessageByID(ctx, 1)
	if m.Seen || m.PendingFlags {
		t.Fatalf("local change kept: seen=%v pending=%v", m.Seen, m.PendingFlags)
	}
}

// E2: Ohne MOVE meldet der Provider unsupported; die Aktion scheitert je
// Ziel, der Spiegel bleibt. Ein Verbindungsfehler bricht den Lauf ab, die
// Aktion bleibt offen und läuft beim nächsten Lauf.
func TestRunnerUnsupportedAndConnectionError(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	svc := &Service{Store: st}
	dest := Target{MailboxID: ids["Archiv"], Path: "Archiv"}
	plan, _ := svc.PlanMove(ctx, []int64{1}, "", dest)
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{moveErr: provider.ErrConnection}
	r := &Runner{Store: st, Blobs: blobs()}
	if _, err := r.Run(ctx, acct, f); err == nil {
		t.Fatal("connection error not returned")
	}
	if got, _ := st.Action(ctx, a.ID); got.State != store.ActionRunning {
		t.Fatalf("after connection error: %s", got.State)
	}
	f.moveErr = provider.ErrUnsupported
	if _, err := r.Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Action(ctx, a.ID)
	if got.State != store.ActionFailed || got.Targets[0].Result != store.TargetUnsupported {
		t.Fatalf("unsupported: %+v", got)
	}
	if cs, _ := st.ServerCopies(ctx, 1); len(cs) != 1 || cs[0].Mailbox != "INBOX" {
		t.Fatalf("mirror changed: %+v", cs)
	}
}
