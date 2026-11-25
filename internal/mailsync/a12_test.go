package mailsync

import (
	"bytes"
	"context"
	"testing"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// A12 Teil 1 (d): Umzug einer Kopie durch den Server (Befund B2) und
// Verknüpfen nur nach Abruf im Entwürfe-Ordner (Server im Prozess).

// appendRaw legt raw im Ordner ab (wie ein Mailprogramm oder Gmail) und
// liefert die neue UID.
func (e *pushEnv) appendRaw(t *testing.T, mailbox string, raw []byte, flags ...imap.Flag) imap.UID {
	t.Helper()
	c := imapSession(t, e.addr)
	cmd := c.Append(mailbox, int64(len(raw)), &imap.AppendOptions{Flags: flags})
	if _, err := cmd.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := cmd.Wait()
	if err != nil {
		t.Fatal(err)
	}
	return data.UID
}

// Gleiche Bytes unter neuer UID, alte UID im selben Lauf weg: moved, der
// Entwurf bleibt pushed und führt die neue UID; danach geht drafts.update.
func TestA12ServerMovesCopySameRun(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	d := e.pushed(t, "Groß")
	c := imapSession(t, e.addr)
	raw := rawOf(t, c, "Drafts", imap.UID(d.RemoteUID))
	nu := e.appendRaw(t, "Drafts", raw, imap.FlagDraft)
	expunge(t, c, "Drafts", imap.UID(d.RemoteUID))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	got := e.draft(t, d.ID)
	if got.RemoteState != drafts.RemotePushed || got.RemoteUID != int64(nu) {
		t.Fatalf("draft = %s uid %d, want pushed uid %d", got.RemoteState, got.RemoteUID, nu)
	}
	if _, err := e.svc.Update(context.Background(), d.ID, drafts.Content{To: got.To, Subject: "Groß", BodyMarkdown: "Hallo, neu"}, ""); err != nil {
		t.Fatalf("update after the move = %v", err)
	}
}

// Alte UID in einem Lauf weg, neue erst im nächsten: zwischendurch missing,
// dann moved → pushed. Die neue Kopie wird geladen (content_loaded = 1),
// nicht per Message-ID und Größe ohne Abruf verknüpft.
func TestA12ServerMovesCopyAcrossRuns(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	d := e.pushed(t, "Groß")
	c := imapSession(t, e.addr)
	raw := rawOf(t, c, "Drafts", imap.UID(d.RemoteUID))
	expunge(t, c, "Drafts", imap.UID(d.RemoteUID))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := e.draft(t, d.ID); got.RemoteState != drafts.RemoteMissing {
		t.Fatalf("after the first run: %s", got.RemoteState)
	}
	nu := e.appendRaw(t, "Drafts", raw, imap.FlagDraft)
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	got := e.draft(t, d.ID)
	if got.RemoteState != drafts.RemotePushed || got.RemoteUID != int64(nu) {
		t.Fatalf("draft = %s uid %d, want pushed uid %d", got.RemoteState, got.RemoteUID, nu)
	}
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes WHERE uid = ? AND content_loaded = 1`, int64(nu)); n != 1 {
		t.Fatalf("new copy not loaded: %d", n)
	}
}

// Gleiche Message-ID und gleiche Größe, anderer Inhalt (in einem früheren
// Lauf verschwand die alte Kopie): kein Verknüpfen ohne Abruf, eigene
// Nachricht, taken_over.
func TestA12SameSizeOtherContentTakenOver(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	d := e.pushed(t, "Groß")
	c := imapSession(t, e.addr)
	raw := rawOf(t, c, "Drafts", imap.UID(d.RemoteUID))
	expunge(t, c, "Drafts", imap.UID(d.RemoteUID))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	other := bytes.Replace(raw, []byte("Hallo"), []byte("Hullo"), 1)
	if len(other) != len(raw) || bytes.Equal(other, raw) {
		t.Fatal("test message not changed in place")
	}
	e.appendRaw(t, "Drafts", other, imap.FlagDraft)
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := e.draft(t, d.ID); got.RemoteState != drafts.RemoteTakenOver {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = ?`, d.MessageIDHeader); n != 2 {
		t.Fatalf("messages with the draft's Message-ID = %d, want 2", n)
	}
}

// Außerhalb der Entwürfe-Ordner (hier ein gewöhnlicher Ordner wie „Alle
// Nachrichten“) wird dieselbe Nachricht weiter ohne Abruf verknüpft.
func TestA12OtherMailboxLinksWithoutFetch(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Alle")
	d := e.pushed(t, "Groß")
	c := imapSession(t, e.addr)
	raw := rawOf(t, c, "Drafts", imap.UID(d.RemoteUID))
	uid := e.appendRaw(t, "Alle", raw, imap.FlagDraft)
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT count(*) FROM messages WHERE message_id_header = ?`, d.MessageIDHeader); n != 1 {
		t.Fatalf("messages = %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM message_mailboxes mm JOIN mailboxes b ON b.id = mm.mailbox_id
		WHERE b.path = 'Alle' AND mm.uid = ? AND mm.content_loaded = 0`, int64(uid)); n != 1 {
		t.Fatalf("copy in Alle not linked without fetch: %d", n)
	}
}
