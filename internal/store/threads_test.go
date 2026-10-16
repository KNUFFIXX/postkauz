package store

import (
	"context"
	"testing"
	"time"
)

// Threads beim Speichern: Verweise verbinden, eine spätere Nachricht
// vereinigt zwei Threads, eine Antwort ohne Verweise hängt über den
// Betreff an; Kennzahlen stimmen.
func TestThreadsAssignedOnApply(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	day := func(n int) time.Time {
		return time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(n) * 24 * time.Hour)
	}
	msg := func(uid uint32, mid, subject, from string, d int, refs ...string) NewMessage {
		m := newMsg(uid, mid, false)
		m.Subject, m.From, m.DateHeader, m.References = subject, from, day(d), refs
		m.ContentHash = "h-" + mid
		return m
	}
	apply := func(ms ...NewMessage) {
		t.Helper()
		if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: ms}); err != nil {
			t.Fatal(err)
		}
	}
	tid := func(mid string) int64 {
		t.Helper()
		ids, _ := s.MessageIDsByHeader(ctx, acct, mid)
		m, err := s.MessageByID(ctx, ids[0])
		if err != nil || m.ThreadID == 0 {
			t.Fatalf("%s: thread %d, %v", mid, m.ThreadID, err)
		}
		return m.ThreadID
	}
	// a und c kennen sich nicht (c verweist auf das noch fehlende b).
	apply(msg(1, "a@example.org", "Heizung", "Anna <a@example.org>", 0),
		msg(2, "c@example.org", "AW: Heizung", "Bert <b@example.org>", 2, "b@example.org"))
	if tid("a@example.org") == tid("c@example.org") {
		t.Fatal("a and c joined before b arrived")
	}
	// b verweist auf a und wird von c genannt: alle drei ein Thread.
	apply(msg(3, "b@example.org", "Re: Heizung", "Bert <b@example.org>", 1, "a@example.org"))
	// Antwort ohne Verweise, gleicher Betreff, 5 Tage später: hängt an.
	apply(msg(4, "d@example.org", "AW: Heizung", "Anna <a@example.org>", 7))
	// Gleicher Betreff ohne Präfix: eigener Thread.
	apply(msg(5, "e@example.org", "Heizung", "Carl <c@example.org>", 8))
	want := tid("a@example.org")
	for _, mid := range []string{"b@example.org", "c@example.org", "d@example.org"} {
		if tid(mid) != want {
			t.Errorf("%s not in the thread of a", mid)
		}
	}
	if tid("e@example.org") == want {
		t.Error("message without reply prefix joined by subject")
	}
	th, msgs, err := s.ThreadByID(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	if th.MessageCount != 4 || len(msgs) != 4 || th.SubjectNorm != "heizung" || !th.FirstDate.Equal(day(0)) || !th.LastDate.Equal(day(7)) ||
		len(th.Participants) != 2 || msgs[0].Subject != "Heizung" || msgs[3].Subject != "AW: Heizung" {
		t.Fatalf("thread = %+v, messages = %+v", th, msgs)
	}
	if n, _ := s.QueryIntForTest(`SELECT count(*) FROM threads`); n != 2 {
		t.Fatalf("threads = %d, want 2 (merged thread removed)", n)
	}
	if _, _, err := s.ThreadByID(ctx, 9999); err == nil {
		t.Fatal("unknown thread found")
	}
}
