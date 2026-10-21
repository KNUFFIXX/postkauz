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
	// Antwort ohne Verweise von einem Fremden an das eigene Konto: Das
	// eigene Konto zählt nicht als gemeinsamer Beteiligter, eigener Thread.
	stranger := msg(6, "f@example.org", "AW: Heizung", "Xaver <x@example.net>", 7)
	stranger.To = []byte(`[{"address":"Demo@Example.org"}]`)
	apply(stranger)
	if tid("f@example.org") == tid("a@example.org") {
		t.Fatal("orphan reply of a stranger joined by subject")
	}
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
	if n, _ := s.QueryIntForTest(`SELECT count(*) FROM threads`); n != 3 {
		t.Fatalf("threads = %d, want 3 (merged thread removed)", n)
	}
	if _, _, err := s.ThreadByID(ctx, 9999); err == nil {
		t.Fatal("unknown thread found")
	}
}

// S2.0/A6: Aliasse zählen im Betreff-Rückfall als eigene Adressen. Schreibt
// ein Fremder an den Alias „AW: Rechnung" ohne Verweise, verbindet der Alias
// allein ihn nicht mit dem Gespräch einer anderen Person an denselben Alias
// (ohne Alias am Konto geschah genau das: der Alias galt als gemeinsamer
// Beteiligter). Eine Antwort der echten Gesprächspartnerin hängt weiter an.
func TestThreadSubjectFallbackTreatsAliasesAsOwn(t *testing.T) {
	for _, withAlias := range []bool{false, true} {
		s := openMem(t)
		ctx := context.Background()
		acct := newAccount(t, s)
		if withAlias {
			if err := s.SetAccountAliases(ctx, acct, []string{"post@example.net"}); err != nil {
				t.Fatal(err)
			}
			if a, _ := s.AccountByName(ctx, "demo"); len(a.Aliases) != 1 || a.Aliases[0] != "post@example.net" {
				t.Fatalf("aliases = %v", a.Aliases)
			}
		}
		if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
			t.Fatal(err)
		}
		boxes, _ := s.Mailboxes(ctx, acct)
		msg := func(uid uint32, mid, subject, from string, d int) NewMessage {
			m := newMsg(uid, mid, false)
			m.Subject, m.From = subject, from
			m.To = []byte(`[{"name":"Post","address":"Post@Example.net"}]`)
			m.DateHeader = time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(d) * 24 * time.Hour)
			return m
		}
		if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: []NewMessage{
			msg(1, "anna-1@example.org", "Rechnung", "Anna <anna@example.org>", 0),
			msg(2, "x-1@example.com", "AW: Rechnung", "Xaver <x@example.com>", 2),
			msg(3, "anna-2@example.org", "AW: Rechnung", "Anna <anna@example.org>", 3),
		}}); err != nil {
			t.Fatal(err)
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
		if tid("anna-2@example.org") != tid("anna-1@example.org") {
			t.Fatalf("alias=%v: reply of the real partner not joined", withAlias)
		}
		joined := tid("x-1@example.com") == tid("anna-1@example.org")
		if withAlias && joined {
			t.Fatal("orphan reply of a stranger joined only via the alias")
		}
		if !withAlias && !joined {
			t.Fatal("without the alias the shared address used to join; the test no longer shows the difference")
		}
	}
}
