package store

import (
	"context"
	"testing"
)

// A05 Teil 2, Nachtrag 2 (F8): SentReplies vergleicht In-Reply-To mit der
// Message-ID des Originals ohne Spitzklammern und Leerraum, auf beiden
// Seiten, und findet nur Nachrichten in Gesendet bzw. „Alle Nachrichten".
func TestSentRepliesNormalizesInReplyTo(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Sent", Name: "Sent", Role: "sent"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	inbox, sent := boxes[0].ID, boxes[1].ID
	put := func(box int64, uid uint32, mid, irt string) {
		t.Helper()
		m := newMsg(uid, mid, true)
		m.InReplyTo = irt
		if irt != "" {
			m.References = []string{irt}
		}
		if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: box, New: []NewMessage{m},
			State: &MailboxState{UIDValidity: 1, UIDNext: uid + 1, Phase: PhaseReady}}); err != nil {
			t.Fatal(err)
		}
	}
	put(inbox, 1, "orig@example.com", "")
	put(sent, 1, "reply-1@example.org", "orig@example.com")
	put(sent, 2, "reply-2@example.org", "orig@example.com")
	put(inbox, 2, "reply-inbox@example.org", "orig@example.com") // nicht gesendet
	put(sent, 3, "other@example.org", "anders@example.com")
	// Schreibweisen, die der Parser so nicht liefert, aber ältere Stände
	// oder andere Wege speichern könnten: Klammern und Leerraum.
	for _, q := range []string{
		`UPDATE messages SET message_id_header = ' <orig@example.com>' || char(9) WHERE message_id_header = 'orig@example.com'`,
		`UPDATE messages SET in_reply_to = '<orig@example.com> ' || char(13,10) WHERE message_id_header = 'reply-2@example.org'`,
	} {
		if err := s.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	orig, err := s.QueryIntForTest(`SELECT id FROM messages WHERE message_id_header LIKE '%orig@example.com%'`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.SentReplies(ctx, acct, int64(orig))
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64]bool{}
	for _, m := range list {
		got[m.ID] = true
	}
	for mid, want := range map[string]bool{"reply-1@example.org": true, "reply-2@example.org": true,
		"reply-inbox@example.org": false, "other@example.org": false} {
		id, _ := s.QueryIntForTest(`SELECT id FROM messages WHERE message_id_header = ?`, mid)
		if got[int64(id)] != want {
			t.Errorf("%s: found %v, want %v", mid, got[int64(id)], want)
		}
	}
	// Ohne Message-ID des Originals: nichts.
	if err := s.ExecForTest(`UPDATE messages SET message_id_header = '  ' WHERE id = ?`, orig); err != nil {
		t.Fatal(err)
	}
	if list, err := s.SentReplies(ctx, acct, int64(orig)); err != nil || len(list) != 0 {
		t.Fatalf("without Message-ID: %d, %v", len(list), err)
	}
}
