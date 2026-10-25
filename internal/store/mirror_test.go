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
	// Toleranz (Restfall C2, rc4): lokale Kopie 0 bis maxExtra Bytes größer,
	// nie kleiner; extra ist die Differenz.
	for _, c := range []struct {
		size  int64
		ok    bool
		extra int64
	}{{100, true, 0}, {96, true, 4}, {94, true, 6}, {93, false, 0}, {101, false, 0}} {
		id, extra, ok, err := s.FindLinkCandidateUpTo(ctx, acct, archiv,
			LinkQuery{MessageID: "a@example.org", Size: c.size, MaxExtra: 6, Subject: "Betreff a@example.org"})
		if err != nil || ok != c.ok || extra != c.extra || (ok && id != mid) {
			t.Fatalf("server size %d: id %d extra %d ok %v err %v, want ok %v extra %d", c.size, id, extra, ok, err, c.ok, c.extra)
		}
	}
	// Anderer Betreff oder anderes Date (wiederverwendete Message-ID): keine
	// Toleranz; exakte Größe bleibt unabhängig vom Kopf; negatives MaxExtra
	// gilt als 0.
	for _, q := range []LinkQuery{
		{MessageID: "a@example.org", Size: 98, MaxExtra: 6, Subject: "anderer Betreff"},
		{MessageID: "a@example.org", Size: 98, MaxExtra: 6, Subject: "Betreff a@example.org", DateHeader: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)},
		{MessageID: "a@example.org", Size: 98, MaxExtra: -1, Subject: "Betreff a@example.org"},
	} {
		if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, archiv, q); ok {
			t.Fatalf("%+v: linked despite different header or no tolerance", q)
		}
	}
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, archiv, LinkQuery{MessageID: "a@example.org", Size: 100, MaxExtra: -1, Subject: "x"}); !ok {
		t.Fatal("exact size must link whatever the header")
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

// Toleranz nie für Entwürfe (rc4): Ein Kandidat mit abweichender Größe,
// der \Draft trägt oder im Ordner mit Rolle drafts liegt, wird nicht
// verknüpft; bei exakter Größe bleibt alles wie bisher.
func TestFindLinkCandidateUpToSkipsDrafts(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Drafts", Name: "Drafts", Role: "drafts"}, {Path: "Sent", Name: "Sent", Role: "sent"}}); err != nil {
		t.Fatal(err)
	}
	ids := map[string]int64{}
	boxes, _ := s.Mailboxes(ctx, acct)
	for _, b := range boxes {
		ids[b.Path] = b.ID
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: ids["Drafts"], New: []NewMessage{newMsg(1, "d@example.org", true)},
		State: &MailboxState{UIDValidity: 1, UIDNext: 2, Phase: PhaseReady, Total: 1}}); err != nil {
		t.Fatal(err)
	}
	_ = s.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('x',1,1)`)
	_ = s.ExecForTest(`UPDATE messages SET blob_sha256 = 'x'`)
	dq := func(size int64) LinkQuery {
		return LinkQuery{MessageID: "d@example.org", Size: size, MaxExtra: 6, Subject: "Betreff d@example.org"}
	}
	// Im Entwürfe-Ordner (Größe 100): Server 98 nicht, Server 100 schon.
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); ok {
		t.Fatal("copy in the Drafts folder linked despite size difference")
	}
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(100)); !ok {
		t.Fatal("exact size must still link")
	}
	// Nur \Draft, nicht im Entwürfe-Ordner: ebenso.
	_ = s.ExecForTest(`DELETE FROM message_mailboxes`)
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 1`)
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); ok {
		t.Fatal("\\Draft message linked despite size difference")
	}
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 0`)
	if _, extra, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); !ok || extra != 2 {
		t.Fatalf("plain message: ok %v extra %d, want true 2", ok, extra)
	}
	// Ordnerkopie mit \Draft (nur am Link), in einem gewöhnlichen Ordner.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: ids["INBOX"],
		Links: []Link{{MessageID: e1(t, s, "d@example.org"), Flags: LinkFlags{UID: 5, Draft: true}}}}); err != nil {
		t.Fatal(err)
	}
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 0`) // nur das Flag der Ordnerkopie prüfen
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); ok {
		t.Fatal("folder copy with \\Draft linked despite size difference")
	}
	_ = s.ExecForTest(`DELETE FROM message_mailboxes`)
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 0`) // der Link oben kann das Flag der Nachricht gesetzt haben
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); !ok {
		t.Fatal("plain message again: must link")
	}
	// Ordner, in den PostKauz Entwürfe abgelegt hat (remote_mailbox_id), und
	// die Message-ID eines PostKauz-Entwurfs: je keine Toleranz, exakt ja.
	if err := s.ExecForTest(`INSERT INTO drafts(account_id, kind, message_id_header, created_at, updated_at) VALUES (?, 'new', 'd@example.org', 1, 1)`, acct); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); ok {
		t.Fatal("message with the Message-ID of a PostKauz draft linked despite size difference")
	}
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(100)); !ok {
		t.Fatal("exact size must still link")
	}
	_ = s.ExecForTest(`UPDATE drafts SET message_id_header = 'other@example.org', remote_mailbox_id = ?`, ids["INBOX"])
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: ids["INBOX"],
		Links: []Link{{MessageID: e1(t, s, "d@example.org"), Flags: LinkFlags{UID: 6}}}}); err != nil {
		t.Fatal(err)
	}
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 0`)
	if _, _, ok, _ := s.FindLinkCandidateUpTo(ctx, acct, ids["Sent"], dq(98)); ok {
		t.Fatal("copy in a folder PostKauz pushes drafts to linked despite size difference")
	}
}

// e1 liefert die ID der (einzigen) Nachricht mit dieser Message-ID.
func e1(t *testing.T, s *SQLite, mid string) int64 {
	t.Helper()
	n, err := s.QueryIntForTest(`SELECT id FROM messages WHERE message_id_header = ?`, mid)
	if err != nil {
		t.Fatal(err)
	}
	return int64(n)
}

// Zwei tolerante Kandidaten (+2 und +5, gleicher Kopf): die kleinere
// Differenz gewinnt, auch wenn sie die höhere ID hat.
func TestFindLinkCandidateUpToSmallestDifference(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv", Name: "Archiv", Role: "archive"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	inbox, archiv := boxes[0].ID, boxes[1].ID
	if boxes[0].Path != "INBOX" {
		inbox, archiv = archiv, inbox
	}
	big, small := newMsg(1, "z@example.org", false), newMsg(2, "z@example.org", false)
	big.Size, small.Size = 105, 102
	small.ContentHash = "h-other"
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, New: []NewMessage{big, small},
		State: &MailboxState{UIDValidity: 1, UIDNext: 3, Phase: PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.QueryIntForTest(`SELECT count(*) FROM messages`); n != 2 {
		t.Fatalf("messages = %d, want 2 (test setup)", n)
	}
	_ = s.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('x',1,1)`)
	_ = s.ExecForTest(`UPDATE messages SET blob_sha256 = 'x'`)
	_ = s.ExecForTest(`DELETE FROM message_mailboxes`)
	smallID, _ := s.QueryIntForTest(`SELECT id FROM messages WHERE size = 102`)
	id, extra, ok, err := s.FindLinkCandidateUpTo(ctx, acct, archiv,
		LinkQuery{MessageID: "z@example.org", Size: 100, MaxExtra: 6, Subject: "Betreff z@example.org"})
	if err != nil || !ok || id != int64(smallID) || extra != 2 {
		t.Fatalf("id %d extra %d ok %v err %v, want %d / 2", id, extra, ok, err, smallID)
	}
}

// MessageNearImport (rc6/rc7): gespeicherte Nachricht 1 bis maxExtra Bytes
// kleiner als die importierte Kopie, gleicher Betreff und gleiches Date;
// gleiche Größe ist nie „dieselbe Nachricht mit ergänztem Ende", ein
// Entwurf und eine Nachricht ohne Rohmail nie ein Kandidat.
func TestMessageNearImport(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	m := newMsg(1, "n@example.org", false)
	m.DateHeader = time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: []NewMessage{m},
		State: &MailboxState{UIDValidity: 1, UIDNext: 2, Phase: PhaseReady, Total: 1}}); err != nil {
		t.Fatal(err)
	}
	near := func(size int64, subject string, date time.Time) bool {
		t.Helper()
		_, ok, err := s.MessageNearImport(ctx, acct, "n@example.org", size, 6, subject, date)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	d, subj := m.DateHeader, "Betreff n@example.org"
	if near(102, subj, d) {
		t.Fatal("message without raw mail must not be a candidate")
	}
	_ = s.ExecForTest(`INSERT INTO blobs(sha256,size,created_at) VALUES ('x',1,1)`)
	_ = s.ExecForTest(`UPDATE messages SET blob_sha256 = 'x'`)
	for _, c := range []struct {
		size int64
		subj string
		date time.Time
		want bool
	}{
		{102, subj, d, true}, {106, subj, d, true}, {107, subj, d, false},
		{100, subj, d, false}, // gleiche Größe: anderer Inhalt (Hash passte nicht)
		{99, subj, d, false},  // Import kleiner
		{102, "anders", d, false}, {102, subj, d.Add(time.Hour), false}, {102, subj, time.Time{}, false},
	} {
		if got := near(c.size, c.subj, c.date); got != c.want {
			t.Errorf("size %d subject %q date %v: %v, want %v", c.size, c.subj, c.date, got, c.want)
		}
	}
	_ = s.ExecForTest(`UPDATE messages SET flag_draft = 1`)
	if near(102, subj, d) {
		t.Fatal("draft must not be a candidate")
	}
}
