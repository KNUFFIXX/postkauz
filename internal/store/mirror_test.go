package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func newAccount(t *testing.T, s *SQLite) int64 {
	t.Helper()
	id, err := s.CreateAccount(context.Background(), Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1",
		Port: 143, TLS: "none", Username: "u", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newMsg(uid uint32, mid string, seen bool) NewMessage {
	return NewMessage{Link: LinkFlags{UID: uid, Seen: seen}, MessageID: mid, Subject: "Betreff " + mid,
		From: "Absender <a@example.org>", To: json.RawMessage(`[{"address":"b@example.org"}]`), Size: 100,
		InternalDate: time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC), ContentHash: "h-" + mid, TextPlain: "Text"}
}

func TestMirrorBatchIdempotentLinksFlagsRemoval(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	added, removed, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv", Name: "Archiv", Role: "archive"}, {Path: "[Gmail]/All Mail", Name: "All Mail", Role: "all"}})
	if err != nil || len(added) != 3 || len(removed) != 0 {
		t.Fatalf("SyncMailboxes = %v %v %v", added, removed, err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	if boxes[0].Path != "INBOX" || boxes[0].Phase != PhaseNew {
		t.Fatalf("boxes = %+v", boxes)
	}
	inbox, archiv := boxes[0].ID, boxes[1].ID
	if boxes[1].Path != "Archiv" {
		archiv = boxes[2].ID
	}
	b := MirrorBatch{AccountID: acct, MailboxID: inbox, New: []NewMessage{newMsg(1, "a@example.org", false), newMsg(2, "b@example.org", true)},
		State: &MailboxState{UIDValidity: 7, UIDNext: 3, HighestModSeq: 10, Phase: PhaseReady, Total: 2}}
	res, err := s.ApplyBatch(ctx, b)
	if err != nil || res.New != 2 {
		t.Fatalf("apply = %+v %v", res, err)
	}
	// Wiederholtes Fenster (Absturz vor dem Speichern): keine Doppelten.
	if res, err := s.ApplyBatch(ctx, b); err != nil || res.New != 0 {
		t.Fatalf("repeat = %+v %v", res, err)
	}
	if n, _ := s.CountPresentMessages(ctx, acct); n != 2 {
		t.Fatalf("present = %d", n)
	}
	mb, _ := s.MailboxByID(ctx, inbox)
	if mb.UIDValidity != 7 || mb.UIDNext != 3 || mb.HighestModSeq != 10 || mb.Phase != PhaseReady || mb.Mirrored != 2 || mb.Unread != 1 {
		t.Fatalf("mailbox = %+v", mb)
	}
	// Dieselbe Nachricht im Archiv: Kandidat per Message-ID und Größe.
	mid, ok, err := s.FindLinkCandidate(ctx, acct, archiv, "a@example.org", 100)
	if err != nil || ok {
		t.Fatalf("message without blob must not be a link candidate: %v %v", ok, err)
	}
	_ = s.ExecForTest(`UPDATE messages SET blob_sha256 = NULL`)
	_ = s.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('x',1,1)`)
	_ = s.ExecForTest(`UPDATE messages SET blob_sha256 = 'x'`)
	mid, ok, err = s.FindLinkCandidate(ctx, acct, archiv, "a@example.org", 100)
	if err != nil || !ok {
		t.Fatalf("candidate: %v %v", ok, err)
	}
	if _, ok, _ := s.FindLinkCandidate(ctx, acct, inbox, "a@example.org", 100); ok {
		t.Fatal("already linked in INBOX: no candidate")
	}
	if _, ok, _ := s.FindLinkCandidate(ctx, acct, archiv, "a@example.org", 101); ok {
		t.Fatal("different size: no candidate")
	}
	if res, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: archiv,
		Links: []Link{{MessageID: mid, Flags: LinkFlags{UID: 40, Flagged: true, Keywords: []string{"$Label1"}}}}}); err != nil || res.Linked != 1 {
		t.Fatalf("link = %+v %v", res, err)
	}
	var seen, flagged int
	var kw string
	_ = s.db.QueryRow(`SELECT flag_seen, flag_flagged, keywords FROM messages WHERE id = ?`, mid).Scan(&seen, &flagged, &kw)
	if seen != 0 || flagged != 1 || kw != `["$Label1"]` {
		t.Fatalf("summary flags = %d %d %s", seen, flagged, kw)
	}
	// Flag-Änderung von außen: nur echte Änderungen zählen.
	res, err = s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, Flags: []LinkFlags{{UID: 1, Seen: true}, {UID: 2, Seen: true}, {UID: 99, Seen: true}}})
	if err != nil || res.Updated != 1 {
		t.Fatalf("flags = %+v %v", res, err)
	}
	_ = s.db.QueryRow(`SELECT flag_seen FROM messages WHERE id = ?`, mid).Scan(&seen)
	if seen != 1 {
		t.Fatal("seen not summarised")
	}
	// Löschung im INBOX: Nachricht 1 bleibt (Archiv-Kopie), Nachricht 2 wird remote gelöscht.
	res, err = s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, Removed: []uint32{1, 2, 5}})
	if err != nil || res.Removed != 2 {
		t.Fatalf("remove = %+v %v", res, err)
	}
	var deleted int
	_ = s.db.QueryRow(`SELECT count(*) FROM messages WHERE deleted_remote_at IS NOT NULL`).Scan(&deleted)
	if deleted != 1 {
		t.Fatalf("deleted_remote = %d", deleted)
	}
	if n, _ := s.CountPresentMessages(ctx, acct); n != 1 {
		t.Fatalf("present after removal = %d", n)
	}
	// Reset (UIDVALIDITY-Wechsel) im Archiv: Zuordnung gelöst, Archiv behält die Zeile.
	res, err = s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: archiv, Reset: true})
	if err != nil || res.Unlinked != 1 {
		t.Fatalf("reset = %+v %v", res, err)
	}
	var total int
	_ = s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&total)
	if n, _ := s.CountPresentMessages(ctx, acct); n != 0 || total != 2 {
		t.Fatalf("after reset: present %d, total %d", n, total)
	}
	// Wieder zuordnen hebt deleted_remote_at auf.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: archiv, Links: []Link{{MessageID: mid, Flags: LinkFlags{UID: 1}}}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.CountPresentMessages(ctx, acct); n != 1 {
		t.Fatalf("relinked present = %d", n)
	}
	// Verschwundener Ordner: Zuordnungen weg, Nachricht bleibt im Archiv.
	_, removedBoxes, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}})
	if err != nil || len(removedBoxes) != 2 {
		t.Fatalf("removed boxes = %v %v", removedBoxes, err)
	}
	_ = s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&total)
	if n, _ := s.CountPresentMessages(ctx, acct); n != 0 || total != 2 {
		t.Fatalf("after folder removal: present %d, total %d", n, total)
	}
}
