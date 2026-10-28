package actions

import (
	"context"
	"io"
	"slices"
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
	// moveErrFor: Fehler nur für MOVE aus diesem Quellordner.
	moveErrFor map[string]error
	flagErr    error
	nextUID    provider.UID
	// gone: UIDs, die es am Server nicht mehr gibt (MOVE meldet sie als
	// Missing, V11).
	gone map[provider.UID]bool
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
	if err := f.moveErrFor[from]; err != nil {
		return provider.MoveResult{}, err
	}
	res := provider.MoveResult{UIDValidity: 8, UIDs: map[provider.UID]provider.UID{}}
	for _, u := range uids {
		if f.gone[u] {
			res.Missing = append(res.Missing, u)
			continue
		}
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
	r := &Runner{Store: st, Blobs: blobs(), Origin: func(_ context.Context, o Origin) (string, error) {
		if o.KeyID != "key:weg" || o.Scope != policy.ScopeMailMark || o.Approved || o.Facts.Action != policy.ActionMark || o.Facts.Targets != 1 {
			t.Errorf("origin check got %+v", o)
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
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path}})
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

// V1: Wird die ID des Zielordners nach zwei Ordnerabgleichen an einen
// anderen Ordner (hier den Papierkorb) vergeben, geht kein MOVE an den
// Server; alle Ziele sind stale (target_changed).
func TestRunnerRefusesReusedTargetID(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	svc := &Service{Store: st}
	dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
	plan, err := svc.PlanMove(ctx, []int64{1}, "", dest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}})
	if err != nil {
		t.Fatal(err)
	}
	// Seit Migration 0022 vergibt der Store Ordner-IDs nicht neu (A14); die
	// Prüfung V1 schützt weiter Bestände, in denen das vorher geschah. Der
	// Test stellt diesen Zustand her: ohne Höchststand und Trigger.
	if err := st.ExecForTest(`DROP TRIGGER mailboxes_id_floor`); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`DELETE FROM meta WHERE key = 'id_floor_mailboxes'`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Papierkorb", Name: "Papierkorb", Role: "trash"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acct)
	reused := false
	for _, b := range boxes {
		reused = reused || (b.ID == dest.MailboxID && b.Path == "Papierkorb")
	}
	if !reused {
		t.Fatalf("test setup: id %d not reused: %+v", dest.MailboxID, boxes)
	}
	f := &fakeRemote{nextUID: 40}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Action(ctx, a.ID)
	if len(f.calls) != 0 || got.Error != "target_changed" || got.Targets[0].Result != store.TargetStale {
		t.Fatalf("calls %v, action %+v", f.calls, got)
	}
	if cs, _ := st.ServerCopies(ctx, 1); len(cs) != 1 || cs[0].Mailbox != "INBOX" {
		t.Fatalf("mirror changed: %+v", cs)
	}
}

// V1: Wechselt der Zielordner die Rolle (hier zu trash), geht kein MOVE an
// den Server, auch wenn die Aktion den Ordner nur per Pfad nannte.
func TestRunnerRefusesTargetRoleChange(t *testing.T) {
	for _, role := range []string{"archive", ""} {
		t.Run("role="+role, func(t *testing.T) {
			st, acct, ids := env(t)
			ctx := context.Background()
			svc := &Service{Store: st}
			dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
			plan, err := svc.PlanMove(ctx, []int64{1}, "", dest)
			if err != nil {
				t.Fatal(err)
			}
			a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
				Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: role}})
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
				{Path: "Archiv", Name: "Archiv", Role: "trash"}}); err != nil {
				t.Fatal(err)
			}
			f := &fakeRemote{nextUID: 40}
			if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
				t.Fatal(err)
			}
			got, _ := st.Action(ctx, a.ID)
			if len(f.calls) != 0 || got.Error != "target_changed" || got.Targets[0].Result != store.TargetStale {
				t.Fatalf("calls %v, action %+v", f.calls, got)
			}
		})
	}
}

// V7: Ein MOVE mit mehreren UIDs aus einem Ordner legt die neuen
// Verknüpfungen mit content_loaded = 0 an (die Zuordnung alt → neu ist
// unsicher); die nächste Aktion prüft deshalb erst den Kopf. Ein MOVE mit
// einer UID behält den Stand.
func TestMoveOfSeveralUIDsNeedsHeaderCheckLater(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	svc := &Service{Store: st}
	dest := Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
	// Nachricht 2 liegt in INBOX (nur verknüpft) und Archiv: Quelle INBOX.
	plan, err := svc.PlanMove(ctx, []int64{1, 2}, "INBOX", dest)
	if err != nil || len(plan.Targets) != 2 {
		t.Fatalf("plan = %+v %v", plan, err)
	}
	if _, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1, 2}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}}); err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{nextUID: 40, headers: map[provider.UID]string{2: "Message-ID: <b@example.org>\r\nSubject: Hallo\r\n\r\n"}}
	if rep, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil || rep.OK != 2 {
		t.Fatalf("run = %+v %v (calls %v)", rep, err, f.calls)
	}
	cs, _ := st.ServerCopies(ctx, 1)
	if len(cs) != 1 || cs[0].Mailbox != "Archiv" || cs[0].ContentLoaded {
		t.Fatalf("copies of 1 = %+v", cs)
	}
	// Nächste Aktion an der neuen Kopie: erst der Kopf.
	yes := true
	fp, _ := svc.PlanFlags(ctx, []int64{1})
	if _, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMark, IDs: []int64{1}, Plan: fp, RequestedBy: "cli",
		Params: store.ActionParams{Mark: &store.MarkParams{Seen: &yes}}}); err != nil {
		t.Fatal(err)
	}
	f2 := &fakeRemote{headers: map[provider.UID]string{41: rawMail, 42: rawMail}}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f2); err != nil {
		t.Fatal(err)
	}
	if len(f2.calls) == 0 || f2.calls[0] != "FETCH Archiv" {
		t.Fatalf("calls = %v", f2.calls)
	}

	// Gegenprobe: eine UID allein behält content_loaded = 1.
	st, acct, ids = env(t)
	svc = &Service{Store: st}
	dest = Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"}
	plan, _ = svc.PlanMove(ctx, []int64{1}, "", dest)
	if _, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}}); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, &fakeRemote{nextUID: 40}); err != nil {
		t.Fatal(err)
	}
	if cs, _ := st.ServerCopies(ctx, 1); len(cs) != 1 || !cs[0].ContentLoaded {
		t.Fatalf("single move: %+v", cs)
	}
}

// V4: Die Ergebnisse je Quellordner werden nach dessen MOVE sofort
// gespeichert. Bricht danach die Verbindung ab (zweiter Ordner Archiv), kennt der
// Spiegel das erste Ergebnis schon; die Aktion bleibt running, und der
// nächste Lauf verschiebt nur noch den Rest.
func TestRunnerSavesResultsPerMailbox(t *testing.T) {
	st, acct, ids := envAll(t, []store.MailboxSpec{{Path: "Projekte", Name: "Projekte", Role: "custom"}}, nil)
	ctx := context.Background()
	svc := &Service{Store: st}
	var plan Plan
	for _, want := range []struct {
		msg  int64
		path string
	}{{1, "INBOX"}, {2, "Archiv"}} {
		cs, _ := st.ServerCopies(ctx, want.msg)
		for _, c := range cs {
			if c.Mailbox == want.path {
				plan.Targets = append(plan.Targets, target(c))
			}
		}
	}
	if len(plan.Targets) != 2 {
		t.Fatalf("targets = %+v", plan.Targets)
	}
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: []int64{1, 2}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: ids["Projekte"], TargetMailbox: "Projekte", TargetRole: "custom"}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{nextUID: 40, moveErrFor: map[string]error{"Archiv": provider.ErrConnection}}
	r := &Runner{Store: st, Blobs: blobs()}
	// Nachprüfung zu fd70f19, Punkt 6: Der Bericht zählt das schon
	// gespeicherte Ergebnis der ersten Gruppe (V6-Zeile).
	if rep, err := r.Run(ctx, acct, f); err == nil || rep.Actions != 1 || rep.OK != 1 || rep.Failed != 0 || rep.Stale != 0 {
		t.Fatalf("run with connection error = %+v, %v", rep, err)
	}
	got, _ := st.Action(ctx, a.ID)
	res := map[int64]string{}
	for _, tg := range got.Targets {
		res[tg.MessageID] = tg.Result
	}
	if got.State != store.ActionRunning || res[1] != store.TargetOK || res[2] != "" {
		t.Fatalf("after abort: state %s, results %v (calls %v)", got.State, res, f.calls)
	}
	// Die Kopie von 1 ist aus INBOX ausgetragen (eine neue UID trägt der
	// Fake nicht ein: seine UIDVALIDITY passt nicht zu Projekte).
	if cs, _ := st.ServerCopies(ctx, 1); slices.ContainsFunc(cs, func(c store.ServerCopy) bool { return c.Mailbox == "INBOX" }) {
		t.Fatalf("mirror of 1 not updated: %+v", cs)
	}
	f.moveErrFor = nil
	f.calls = nil
	if rep, err := r.Run(ctx, acct, f); err != nil || rep.Actions != 1 || rep.OK != 1 {
		t.Fatalf("second run = %+v, %v", rep, err)
	}
	got, _ = st.Action(ctx, a.ID)
	if got.State != store.ActionDone || strings.Join(f.calls, "|") != "MOVE Archiv -> Projekte" {
		t.Fatalf("second run: state %s, calls %v", got.State, f.calls)
	}
}
