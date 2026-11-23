package mailsync

import (
	"context"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A11 Teil 1 (a): Nach der Ablage spiegelt derselbe Lauf den Ablageordner
// nach, auch wenn er sonst nur INBOX spiegelt; (c): nach einer Änderung
// geht die frühere Kopie in den Papierkorb, am Ende liegt nur die neue im
// Entwürfe-Ordner (Server im Prozess).
func TestA11PushMirrorsAndTrashesEarlierCopy(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Trash")
	e.runner.Actions = &actions.Runner{Store: e.st, Blobs: e.blobs}
	ctx := context.Background()
	// Erst ein Lauf über alle Ordner (Ordnerliste und Rollen im Spiegel).
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	d := e.newDraft(t, "Rückfrage")
	if _, err := e.sync(t, ops.SyncOptions{Mailboxes: []string{"INBOX"}}); err != nil {
		t.Fatal(err)
	}
	d = e.draft(t, d.ID)
	if d.RemoteState != drafts.RemotePushed || d.RemoteUID <= 0 {
		t.Fatalf("draft = %s uid %d", d.RemoteState, d.RemoteUID)
	}
	if _, _, ok, err := e.st.CopyAt(ctx, d.RemoteMailboxID, uint32(d.RemoteUID)); err != nil || !ok {
		t.Fatalf("copy not mirrored after the push: %v %v", ok, err)
	}
	first := d.RemoteUID

	if _, err := e.svc.Update(ctx, d.ID, drafts.Content{To: []store.Addr{{Address: "b@example.org"}}, Subject: "Rückfrage",
		BodyMarkdown: "Hallo, geändert"}, ""); err != nil {
		t.Fatal(err)
	}
	e.settle(t)
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	d = e.draft(t, d.ID)
	acts, err := e.st.DraftCleanupActions(ctx, d)
	if err != nil || len(acts) != 1 || acts[0].Params.Source != store.ActionSourceDraftUpdate || int64(acts[0].Targets[0].UID) != first {
		t.Fatalf("cleanup actions = %+v %v", acts, err)
	}
	// Der nächste Lauf führt die Aktion aus (Aktionen laufen zu Beginn).
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.Action(ctx, acts[0].ID)
	if a.State != store.ActionDone {
		t.Fatalf("action = %s %s %+v", a.State, a.Error, a.Targets)
	}
	client := imapSession(t, e.addr)
	if copies := serverCopies(t, client, "Drafts", d.MessageIDHeader); len(copies) != 1 {
		t.Fatalf("copies in Drafts = %v", copies)
	}
	if copies := serverCopies(t, client, "Trash", d.MessageIDHeader); len(copies) != 1 {
		t.Fatalf("copies in Trash = %v", copies)
	}
}
