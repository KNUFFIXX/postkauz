package actions

import (
	"context"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// moveAction legt ein move der Nachrichten ids aus from nach dest an.
func moveAction(t *testing.T, st *store.SQLite, acct int64, ids []int64, from string, dest Target) store.Action {
	t.Helper()
	ctx := context.Background()
	svc := &Service{Store: st}
	plan, err := svc.PlanMove(ctx, ids, from, dest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMove, IDs: ids, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path, TargetRole: dest.Role}})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// V11 der Nachbesserung T2: Meldet der Server eine UID als nicht
// verschoben (es gab sie nicht mehr), ist das Ziel not_found statt ok, und
// der Spiegel trägt keine Kopie um.
func TestRunnerMoveOfVanishedUIDIsNotFound(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	a := moveAction(t, st, acct, []int64{1}, "", Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"})
	f := &fakeRemote{nextUID: 40, gone: map[provider.UID]bool{1: true}}
	rep, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f)
	if err != nil || rep.OK != 0 || rep.Failed != 1 {
		t.Fatalf("run = %+v %v", rep, err)
	}
	got, _ := st.Action(ctx, a.ID)
	if got.Targets[0].Result != store.TargetNotFound || got.State != store.ActionFailed {
		t.Fatalf("action = %+v", got)
	}
	if cs, _ := st.ServerCopies(ctx, 1); len(cs) != 1 || cs[0].Mailbox != "INBOX" {
		t.Fatalf("mirror changed: %+v", cs)
	}
}

// V21 der Nachbesserung T2: Die Köpfe nie selbst geladener Kopien holt der
// Job je Ordner mit einem FETCH, nicht je UID.
func TestRunnerFetchesHeadersOncePerMailbox(t *testing.T) {
	st, acct, _ := env(t)
	ctx := context.Background()
	// Beide Kopien in INBOX gelten als nie geladen.
	if err := st.ExecForTest(`UPDATE message_mailboxes SET content_loaded = 0`); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Store: st}
	yes := true
	plan, err := svc.PlanFlags(ctx, []int64{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionMark, IDs: []int64{1, 2}, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{Mark: &store.MarkParams{Seen: &yes}}})
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{headers: map[provider.UID]string{
		1: "Message-ID: <a@example.org>\r\nSubject: Hallo\r\n\r\n",
		2: "Message-ID: <b@example.org>\r\nSubject: Hallo\r\n\r\n",
		5: "Message-ID: <b@example.org>\r\nSubject: Hallo\r\n\r\n",
	}}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	fetches := map[string]int{}
	for _, c := range f.calls {
		if strings.HasPrefix(c, "FETCH ") {
			fetches[c]++
		}
	}
	if fetches["FETCH INBOX"] != 1 || fetches["FETCH Archiv"] != 1 {
		t.Fatalf("calls = %v", f.calls)
	}
	got, _ := st.Action(ctx, a.ID)
	if got.State != store.ActionDone {
		t.Fatalf("action = %+v", got)
	}
}

// V17 der Nachbesserung T2: Fehlt die lokale Rohmail einer nie geladenen
// Kopie, fragt der Job den Server nicht; das Ziel ist stale, die Aktion
// nennt den Grund no_local_copy.
func TestRunnerNoLocalCopyReason(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	if err := st.ExecForTest(`UPDATE messages SET blob_sha256 = NULL WHERE id = 2`); err != nil {
		t.Fatal(err)
	}
	a := moveAction(t, st, acct, []int64{2}, "INBOX", Target{MailboxID: ids["Archiv"], Path: "Archiv", Role: "archive"})
	f := &fakeRemote{headers: map[provider.UID]string{2: "Message-ID: <b@example.org>\r\nSubject: Hallo\r\n\r\n"}}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Action(ctx, a.ID)
	if len(f.calls) != 0 || got.Targets[0].Result != store.TargetStale || got.Error != ReasonNoLocalCopy {
		t.Fatalf("calls %v, action %+v", f.calls, got)
	}
}

// Quellpfad je Ziel (Nachprüfung zu fd70f19): Hat der Quellordner unter
// seiner ID inzwischen einen anderen Pfad (ID an einen anderen Ordner
// vergeben, dort dieselbe UID unter derselben UIDVALIDITY), geht kein
// Kommando an den Server; das Ziel ist stale, Grund source_changed. Auch
// die Freigabe eines Antrags erkennt es.
func TestRunnerRefusesReusedSourceID(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	a := moveAction(t, st, acct, []int64{2}, "Archiv", Target{MailboxID: ids["INBOX"], Path: "INBOX", Role: "inbox"})
	if a.Targets[0].SourcePath != "Archiv" {
		t.Fatalf("source path not stored: %+v", a.Targets)
	}
	// So sähe der Spiegel aus, wenn die ID von „Archiv“ an einen anderen
	// Ordner mit einer Nachricht unter UID 5 und UIDVALIDITY 8 ginge.
	if err := st.ExecForTest(`UPDATE mailboxes SET path = 'Fremd', name = 'Fremd', role = 'custom' WHERE id = ?`, ids["Archiv"]); err != nil {
		t.Fatal(err)
	}
	f := &fakeRemote{nextUID: 40}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Action(ctx, a.ID)
	if len(f.calls) != 0 || got.Targets[0].Result != store.TargetStale || got.Error != ReasonSourceChanged {
		t.Fatalf("calls %v, action %+v", f.calls, got)
	}
	// Altbestand ohne Quellpfad (NULL): wie bisher ausgeführt.
	st, acct, ids = env(t)
	a = moveAction(t, st, acct, []int64{2}, "Archiv", Target{MailboxID: ids["INBOX"], Path: "INBOX", Role: "inbox"})
	if err := st.ExecForTest(`UPDATE action_targets SET source_path = NULL`); err != nil {
		t.Fatal(err)
	}
	f = &fakeRemote{nextUID: 40}
	if _, err := (&Runner{Store: st, Blobs: blobs()}).Run(ctx, acct, f); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Action(ctx, a.ID); got.State != store.ActionDone || strings.Join(f.calls, "|") != "MOVE Archiv -> INBOX" {
		t.Fatalf("legacy target: calls %v, action %+v", f.calls, got)
	}
}

// Quellpfad je Ziel bei der Freigabe: Ein Antrag, dessen Quellordner unter
// der ID inzwischen anders heißt, wird beim Freigeben stale (nie ausgeführt).
func TestDecideMarksReusedSourceIDStale(t *testing.T) {
	st, acct, ids := env(t)
	ctx := context.Background()
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv", Name: "Archiv", Role: "archive"}, {Path: "Trash", Name: "Trash", Role: "trash"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acct)
	var trash int64
	for _, b := range boxes {
		if b.Path == "Trash" {
			trash = b.ID
		}
	}
	svc := &Service{Store: st}
	dest := Target{MailboxID: trash, Path: "Trash", Role: "trash"}
	plan, err := svc.PlanMove(ctx, []int64{2}, "Archiv", dest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, Request{AccountID: acct, Kind: store.ActionTrash, IDs: []int64{2}, Plan: plan, RequestedBy: "key:agent",
		Approval: true, Params: store.ActionParams{TargetMailboxID: trash, TargetMailbox: "Trash", TargetRole: "trash"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`UPDATE mailboxes SET path = 'Fremd', name = 'Fremd', role = 'custom' WHERE id = ?`, ids["Archiv"]); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Decide(ctx, a.ID, true, "Testperson", "", "cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.ActionFailed || got.Targets[0].Result != store.TargetStale {
		t.Fatalf("decided = %+v", got)
	}
}
