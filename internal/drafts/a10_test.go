package drafts

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A10 Teil 1: Gesendet ist nur, was in Gesendet liegt (Befund B4, B5).
// Hat das Konto Gesendet (Rolle sent, virtual_view = 0), zählt in keinem
// Weg eine Nachricht, die nur in „Alle Nachrichten“ liegt.

// a10Own ist eine Nachricht von der Kontoadresse.
const a10Own = "Demo <demo@example.org>"

// a10FallbackMsg ist eine zum Entwurf aus fallbackDraft passende Nachricht
// (gleiche Empfänger, Betreff und Text; neue Message-ID).
func a10FallbackMsg(e *env, subject, mid string) sentMsg {
	return sentMsg{messageID: mid, subject: subject, from: a10Own,
		to: []store.Addr{{Address: "anna@example.org"}, {Address: "bob@example.org"}}, date: e.now.Add(time.Minute), text: fbBody}
}

// Konto mit Gesendet und „Alle Nachrichten“: Eine passende Nachricht nur in
// „Alle Nachrichten“ (eigener Absender, Datum im Fenster, kein \Draft, in
// keinem Entwürfe-Ordner) schließt in keinem der vier Wege.
func TestA10OnlyInAllMailNotSent(t *testing.T) {
	t.Run("message_id", func(t *testing.T) {
		e := newEnv(t)
		m := newMirror(e)
		d := e.reach(StatusReady)
		m.put("all", sentMsg{messageID: d.MessageIDHeader, subject: "anders", from: a10Own, date: e.now, text: "x"})
		if rep := e.check(); !rep.Empty() {
			t.Fatalf("report = %+v", rep)
		}
		if got := e.draft(d.ID); got.Status != string(StatusReady) {
			t.Fatalf("status = %s", got.Status)
		}
	})
	t.Run("fallback", func(t *testing.T) {
		e := newEnv(t)
		m := newMirror(e)
		d := e.fallbackDraft("Re: Termin")
		m.put("all", a10FallbackMsg(e, "Re: Termin", "gmail-1@example.org"))
		if rep := e.check(); rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if got := e.draft(d.ID); got.Status != string(StatusDraft) {
			t.Fatalf("status = %s", got.Status)
		}
	})
	t.Run("thread", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		e.dropCopy(d)
		e.send(thSent{role: "all", from: a10Own, date: e.now.Add(time.Minute)})
		if rep := e.check(); rep.SentByThread != 0 || len(rep.SentDraftIDs) != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if e.status(d) != string(StatusDraft) {
			t.Fatalf("status = %s", e.status(d))
		}
	})
	t.Run("subject", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
		e.dropCopy(d)
		e.send(thSent{subject: sjSubject, text: sjOwn + sjAdded, to: sjTo, role: "all", from: a10Own, inReplyTo: " ",
			date: e.now.Add(time.Minute)})
		if rep := e.check(); rep.SentBySubject != 0 || len(rep.SentDraftIDs) != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if e.status(d) != string(StatusDraft) {
			t.Fatalf("status = %s", e.status(d))
		}
	})
}

// (d) Lauf 1 sieht die Nachricht nur in „Alle Nachrichten“, Lauf 2
// verknüpft sie in Gesendet (gleiche Zeile, keine neue messages.id): Eine
// Antwort ohne In-Reply-To wird sent_by_human (fallback), über die Marke
// des Gesendet-Ordners.
func TestA10SentLinkedLaterFoundByBoxMark(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	d := e.fallbackDraft("Re: Termin")
	if rep := e.check(); !rep.Empty() { // erster Lauf: Marken gesetzt
		t.Fatalf("first run = %+v", rep)
	}
	id := m.put("all", a10FallbackMsg(e, "Re: Termin", "gmail-2@example.org"))
	if rep := e.check(); rep.SentByFallback != 0 || rep.Examined != 0 {
		t.Fatalf("run 1 = %+v", rep)
	}
	m.link("sent", id, false)
	rep := e.check()
	if rep.SentByFallback != 1 || rep.Examined != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("run 2 = %+v", rep)
	}
	if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != id {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	// Die Marke steht danach auf der neuen UID; ein weiterer Lauf liest
	// nichts mehr.
	uv, uid, ok, err := e.st.SentBoxMark(context.Background(), m.boxes["sent"])
	if err != nil || !ok || uv != 1 || uid != m.uid[m.boxes["sent"]] {
		t.Fatalf("mark = %d:%d %v %v", uv, uid, ok, err)
	}
	if rep := e.check(); rep.Examined != 0 || !rep.Empty() {
		t.Fatalf("run 3 = %+v", rep)
	}
}

// Marke bei geänderter UIDVALIDITY: Der Ordner wurde neu gespiegelt, die
// UIDs gelten nicht mehr. Der Lauf prüft jeden offenen Entwurf über sein
// ganzes Fenster (die Nachricht wird gefunden) und setzt die Marke neu.
func TestA10BoxMarkUIDValidityChanged(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	d := e.fallbackDraft("Re: Termin")
	m.put("sent", sentMsg{messageID: "alt@example.org", subject: "Re: Alt", from: a10Own, to: thAnna, date: e.now.Add(-time.Hour), text: "alt"})
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("first run = %+v", rep)
	}
	id := m.put("all", a10FallbackMsg(e, "Re: Termin", "gmail-3@example.org"))
	if rep := e.check(); rep.SentByFallback != 0 {
		t.Fatalf("run 1 = %+v", rep)
	}
	// Neuer UIDVALIDITY, die Kopie in Gesendet hat eine kleinere UID als
	// die alte Marke.
	sent := m.boxes["sent"]
	if err := e.st.ExecForTest(`UPDATE mailboxes SET uidvalidity = 7 WHERE id = ?`, sent); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE mailbox_id = ?`, sent); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`INSERT INTO message_mailboxes(message_id, mailbox_id, uid) VALUES (?, ?, 1)`, id, sent); err != nil {
		t.Fatal(err)
	}
	rep := e.check()
	if rep.SentByFallback != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("after UIDVALIDITY change = %+v", rep)
	}
	uv, uid, ok, err := e.st.SentBoxMark(context.Background(), sent)
	if err != nil || !ok || uv != 7 || uid != 1 {
		t.Fatalf("mark = %d:%d %v %v", uv, uid, ok, err)
	}
}

// (b) Gesendet im Erstabgleich: wie ein ausgelassener Ordner (KeepMark):
// keine Marke, kein copy_gone, kein „ersetzt“.
func TestA10SentInitialKeepsMark(t *testing.T) {
	e := newThEnv(t)
	ctx := context.Background()
	d := e.draftOf(KindReply, thOwn, true)
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("first run = %+v", rep)
	}
	e.dropCopy(d)
	if rep := e.check(); rep.Missing != 1 {
		t.Fatalf("missing run = %+v", rep)
	}
	last, _, _ := e.st.SentCheckMark(ctx, e.acc)
	e.m.put("inbox", sentMsg{messageID: "neu@example.org", subject: "Neu", from: "Anna <anna@example.org>", date: e.now, text: "neu"})
	if err := e.st.ExecForTest(`UPDATE mailboxes SET sync_phase = 'initial' WHERE id = ?`, e.m.boxes["sent"]); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(25 * time.Hour)
	if rep := e.check(); rep.CopyGone != 0 || rep.ReplacedInThread != 0 {
		t.Fatalf("initial Sent = %+v", rep)
	}
	if got, _, _ := e.st.SentCheckMark(ctx, e.acc); got != last {
		t.Fatalf("mark moved from %d to %d", last, got)
	}
	if e.status(d) != string(StatusDraft) {
		t.Fatalf("status = %s", e.status(d))
	}
	// Fertig gespiegelt: Die Auffangregel greift wieder.
	if err := e.st.ExecForTest(`UPDATE mailboxes SET sync_phase = 'ready' WHERE id = ?`, e.m.boxes["sent"]); err != nil {
		t.Fatal(err)
	}
	if rep := e.check(); rep.CopyGone != 1 {
		t.Fatalf("ready Sent = %+v", rep)
	}
}

// Eine Nachricht zusätzlich in einer virtuellen Sicht (virtual_view = 1),
// hier sogar mit Rolle sent: zählt nicht als Gesendet.
func TestA10VirtualViewIsNotSent(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	d := e.fallbackDraft("Re: Termin")
	if err := e.st.ExecForTest(`INSERT INTO mailboxes(account_id, path, name, role, virtual_view, sync_phase, uidvalidity)
		VALUES (?, '[Gmail]/Wichtig', 'Wichtig', 'sent', 1, 'ready', 1)`, e.acc); err != nil {
		t.Fatal(err)
	}
	virt, err := e.st.QueryIntForTest(`SELECT id FROM mailboxes WHERE virtual_view = 1`)
	if err != nil {
		t.Fatal(err)
	}
	m.boxes["virtual"] = int64(virt)
	id := m.put("all", a10FallbackMsg(e, "Re: Termin", "gmail-4@example.org"))
	m.link("virtual", id, false)
	if rep := e.check(); rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("status = %s", got.Status)
	}
	boxes, err := e.st.SentBoxes(context.Background(), e.acc)
	if err != nil || len(boxes) != 1 || boxes[0].ID != m.boxes["sent"] {
		t.Fatalf("sent boxes = %+v, %v", boxes, err)
	}
}

// (c) Konto ohne Gesendet: „Alle Nachrichten“ mit eigenem Absender zählt
// wie bisher, aber nicht mit INTERNALDATE mehr als 2 Minuten nach dem Lauf.
func TestA10NoSentAccount(t *testing.T) {
	for _, c := range []struct {
		name     string
		internal time.Duration
		sent     bool
	}{
		{"date past", -time.Hour, true},
		{"within 2 minutes", 2 * time.Minute, true},
		{"internal date in the future", 3 * 24 * time.Hour, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			m := newMirror(e)
			m.noSent()
			d := e.fallbackDraft("Re: Termin")
			msg := a10FallbackMsg(e, "Re: Termin", "gmail-5@example.org")
			msg.date = e.now.Add(c.internal)
			id := m.put("all", msg)
			rep := e.check()
			if got := e.draft(d.ID); (got.Status == string(StatusSentByHuman)) != c.sent || (c.sent && got.SentMessageID != id) {
				t.Fatalf("draft = %s (report %+v)", got.Status, rep)
			}
		})
	}
	// Auch die Message-ID: eine Nachricht mit der Message-ID des Entwurfs und
	// Termin in der Zukunft ist nicht gesendet.
	e := newEnv(t)
	m := newMirror(e)
	m.noSent()
	d := e.reach(StatusReady)
	m.put("all", sentMsg{messageID: d.MessageIDHeader, subject: "x", from: a10Own, date: e.now.Add(3 * 24 * time.Hour), text: "x"})
	if rep := e.check(); rep.SentByMessageID != 0 {
		t.Fatalf("future message id = %+v", rep)
	}
}

// B5 innerhalb eines Laufs: Gmail speichert eine Zwischenfassung (neue
// Message-ID, kein \Draft); der Spiegel sieht sie nur in „Alle
// Nachrichten“, weil „Alle Nachrichten“ vor „Entwürfe“ gespiegelt wird. Im
// nächsten Lauf liegt sie im Entwürfe-Ordner oder ist weg: nie gesendet.
// Erst die echte Nachricht in Gesendet schließt den Entwurf.
func TestA10IntermediateVersionNeverSent(t *testing.T) {
	for _, next := range []string{"in drafts", "gone"} {
		t.Run(next, func(t *testing.T) {
			e := newThEnv(t)
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			mid := e.send(thSent{role: "all", from: a10Own, date: e.now.Add(time.Minute)})
			if rep := e.check(); len(rep.SentDraftIDs) != 0 {
				t.Fatalf("run 1 = %+v", rep)
			}
			if next == "in drafts" {
				e.m.link("drafts", mid, false)
			} else {
				if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, mid); err != nil {
					t.Fatal(err)
				}
				if err := e.st.ExecForTest(`UPDATE messages SET deleted_remote_at = 1 WHERE id = ?`, mid); err != nil {
					t.Fatal(err)
				}
			}
			if rep := e.check(); len(rep.SentDraftIDs) != 0 {
				t.Fatalf("run 2 = %+v", rep)
			}
			if e.status(d) != string(StatusDraft) {
				t.Fatalf("status = %s", e.status(d))
			}
			realID := e.send(thSent{from: a10Own, date: e.now.Add(11 * time.Minute)})
			if rep := e.check(); rep.SentByThread != 1 {
				t.Fatalf("real send = %+v", rep)
			}
			if got := e.draft(d.ID); got.SentMessageID != realID {
				t.Fatalf("sent message = %d, want %d", got.SentMessageID, realID)
			}
		})
	}
}

// „ersetzt“ (3b) mit einer eigenen Antwort nur in „Alle Nachrichten“:
// nicht ersetzt (dieselbe Antwort in Gesendet ersetzt).
func TestA10ReplacedNeedsSent(t *testing.T) {
	for _, role := range []string{"all", "sent"} {
		t.Run(role, func(t *testing.T) {
			e := newThEnv(t)
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			if rep := e.check(); rep.Missing != 1 {
				t.Fatalf("missing run = %+v", rep)
			}
			e.send(thSent{role: role, from: a10Own, text: "Ganz anderer Text ohne Bezug.", date: e.now.Add(time.Minute)})
			rep := e.check()
			if want := role == "sent"; (rep.ReplacedInThread == 1) != want {
				t.Fatalf("report = %+v", rep)
			}
		})
	}
}
