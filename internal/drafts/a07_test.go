package drafts

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A07: Versanderkennung schließen (Teile 1 bis 4).

// draftTo legt einen Entwurf an wie thEnv.draftOf, aber mit eigenen
// Empfängern to (auch leer) und Betreff subject ("" = „Re: Abrechnung“).
func (e *thEnv) draftTo(kind Kind, own string, to []store.Addr, subject string, push bool) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	if subject == "" {
		subject = "Re: Abrechnung"
	}
	in := NewDraft{AccountID: e.acc, Kind: kind, CreatedBy: "key:agent", Content: Content{To: to, Subject: subject,
		BodyMarkdown: own, BodyText: own + "\n\nAm 22.10.2026 08:00 schrieb Anna <anna@example.org>:\n> Bitte prüfen Sie die Abrechnung.\n"}}
	if kind == KindNew {
		in.Content.BodyText = own
	} else {
		in.InReplyToMsg = e.orig
	}
	r, err := e.svc.Create(ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	if !push {
		return r.Draft
	}
	return e.pushAt(r.Draft, e.now)
}

// Teil 1: Antwort-Entwurf ohne Empfänger (derive_recipients false); der
// Mensch trägt den Empfänger im Mailprogramm ein und sendet.
func TestA07NoRecipientsThread(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, nil, "", true)
	if RecipientKey(d.To, d.Cc) != "" {
		t.Fatal("draft has recipients")
	}
	e.dropCopy(d)
	sent := e.send(thSent{text: thOwn})
	rep := e.check()
	if rep.SentByThread != 1 || rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != DetectThread {
		t.Fatalf("audit = %+v", r)
	}
}

// Teil 1: dasselbe, aber stark umgeschrieben → ersetzt (3b), im Lauf nach
// dem, der das Fehlen der Kopie feststellt.
func TestA07NoRecipientsReplaced(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, nil, "", true)
	e.dropCopy(d)
	e.send(thSent{text: thRewritten})
	if rep := e.check(); rep.ReplacedInThread != 0 || rep.Missing != 1 {
		t.Fatalf("first run = %+v", rep)
	}
	rep := e.check()
	if rep.ReplacedInThread != 1 || rep.ReplacedDraftIDs[0] != d.ID {
		t.Fatalf("second run = %+v", rep)
	}
	if got := e.status(d); got != string(StatusAbandoned) {
		t.Fatalf("draft = %s", got)
	}
}

// Teil 1: Fälle, in denen der Entwurf offen bleibt.
func TestA07NoRecipientsStaysOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(e *thEnv) store.Draft
	}{
		{"sent without To and Cc (Bcc only)", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, nil, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, to: []store.Addr{}})
			return d
		}},
		{"sent rewritten without To and Cc", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, nil, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, to: []store.Addr{}})
			return d
		}},
		{"copy still in the Drafts folder", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, nil, "", true)
			e.send(thSent{text: thOwn})
			return d
		}},
		{"with recipients, sent to others", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, thAnna, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, to: []store.Addr{{Address: "bert@example.org"}}})
			return d
		}},
		{"sent reply without In-Reply-To", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, nil, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, inReplyTo: " "})
			return d
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := c.run(e)
			for i := 0; i < 3; i++ {
				if rep := e.check(); len(rep.SentDraftIDs)+rep.ReplacedInThread != 0 {
					t.Fatalf("run %d = %+v", i, rep)
				}
			}
			if got := e.status(d); got != string(StatusDraft) {
				t.Fatalf("draft = %s", got)
			}
		})
	}
}

// takeOver ersetzt im Entwürfe-Ordner die abgelegte Kopie durch eine
// neuere Fassung mit der Message-ID des Entwurfs (das Mailprogramm hat ihn
// gespeichert, wie Gmail) und liefert ihre lokale ID.
func (e *thEnv) takeOver(d store.Draft) int64 {
	e.t.Helper()
	e.dropCopy(d)
	return e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now,
		text: "im Mailprogramm bearbeitet " + d.BodyText})
}

// dropAll entfernt alle Fassungen des Entwurfs aus dem Entwürfe-Ordner.
func (e *thEnv) dropAll(d store.Draft) {
	e.t.Helper()
	e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND message_id IN (SELECT id FROM messages WHERE message_id_header = ?)`,
		e.m.boxes["drafts"], d.MessageIDHeader)
}

// auditCount zählt die Audit-Einträge mit Operation op.
func (e *thEnv) auditCount(op string) int {
	e.t.Helper()
	recs, err := e.st.AuditQuery(context.Background(), store.AuditFilter{Operation: op})
	if err != nil {
		e.t.Fatal(err)
	}
	return len(recs)
}

// later rückt die Uhr vor (die nächste Fassung ist jünger).
func (e *thEnv) later() { e.now = e.now.Add(time.Minute) }

// Teil 2: zwei Fassungen derselben Antwort, beide im Mailprogramm
// übernommen; gesendet wurde die jüngere unverändert, die ältere hat der
// Mensch gelöscht. Die jüngere ist gesendet, die ältere wird in Lauf N
// missing (taken_over → missing) und in Lauf N+1 ersetzt.
func TestA07TwoTakenOverVersionsYoungerSent(t *testing.T) {
	e := newThEnv(t)
	older := e.draftOf(KindReply, thOwn, true)
	e.later()
	younger := e.draftOf(KindReply, thOwn, true)
	e.takeOver(older)
	e.takeOver(younger)
	if rep := e.check(); rep.TakenOver != 2 {
		t.Fatalf("takeover run = %+v", rep)
	}
	e.dropAll(older)
	e.dropAll(younger)
	sent := e.send(thSent{text: thOwn})
	rep := e.check() // Lauf N
	if rep.SentByThread != 1 || rep.SentDraftIDs[0] != younger.ID || rep.Missing != 1 || rep.ReplacedInThread != 0 {
		t.Fatalf("run N = %+v", rep)
	}
	if got := e.draft(younger.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("younger = %s, sent %d", got.Status, got.SentMessageID)
	}
	if got := e.draft(older.ID); got.Status != string(StatusDraft) || got.RemoteState != RemoteMissing {
		t.Fatalf("older after run N = %s/%s", got.Status, got.RemoteState)
	}
	if r, ok := e.lastAudit(OpRemoteCheck); !ok || r.Reason != RemoteMissing || r.ActorType != "system" {
		t.Fatalf("remote_check audit = %+v", r)
	}
	rep = e.check() // Lauf N+1
	if rep.ReplacedInThread != 1 || rep.ReplacedDraftIDs[0] != older.ID {
		t.Fatalf("run N+1 = %+v", rep)
	}
	if got := e.status(older); got != string(StatusAbandoned) {
		t.Fatalf("older = %s", got)
	}
}

// Teil 2: Gleichstand bei gleicher Ähnlichkeit, im Verlauf und im Rückfall.
func TestA07TieBreak(t *testing.T) {
	for _, c := range []struct {
		name string
		// subject der gesendeten Nachricht: "Re: Abrechnung" = Rückfall,
		// "" = anderer Betreff, nur der Verlauf trifft.
		subject, reason string
		// olderTaken: die ältere Fassung ist übernommen (taken_over).
		olderTaken bool
		wantOlder  bool
	}{
		{"thread, both pushed: younger wins", "", DetectThread, false, false},
		{"thread, older taken over wins", "", DetectThread, true, true},
		{"fallback, both pushed: younger wins", "Re: Abrechnung", DetectFallback, false, false},
		{"fallback, older taken over wins", "Re: Abrechnung", DetectFallback, true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			older := e.draftOf(KindReply, thOwn, true)
			e.later()
			younger := e.draftOf(KindReply, thOwn, true)
			if c.olderTaken {
				e.takeOver(older)
				if rep := e.check(); rep.TakenOver != 1 {
					t.Fatalf("takeover run = %+v", rep)
				}
			}
			if c.reason == DetectThread {
				e.dropAll(older)
				e.dropAll(younger)
			}
			sent := e.send(thSent{text: thOwn, subject: c.subject})
			rep := e.check()
			if len(rep.SentDraftIDs) != 1 {
				t.Fatalf("report = %+v", rep)
			}
			win, lose := younger, older
			if c.wantOlder {
				win, lose = older, younger
			}
			if got := e.draft(win.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
				t.Fatalf("winner %d = %s", win.ID, got.Status)
			}
			if got := e.status(lose); got != string(StatusDraft) {
				t.Fatalf("loser = %s", got)
			}
			if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != c.reason {
				t.Fatalf("audit = %+v", r)
			}
		})
	}
}

// Teil 2: Bei verschiedener Ähnlichkeit entscheidet weiter die Ähnlichkeit:
// zwei Fassungen pushed, die ältere wurde gesendet.
func TestA07SimilarityStillDecides(t *testing.T) {
	e := newThEnv(t)
	older := e.draftOf(KindReply, thOwn, true)
	e.later()
	younger := e.draftOf(KindReply, thOwn+" Bitte rufen Sie uns vorher kurz an, damit wir den Termin abstimmen.", true)
	e.dropAll(older)
	e.dropAll(younger)
	sent := e.send(thSent{text: thOwn})
	if rep := e.check(); len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != older.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(older.ID); got.SentMessageID != sent {
		t.Fatalf("older sent %d", got.SentMessageID)
	}
}

// Teil 2 (b): taken_over → missing nur, wenn auch die übernommene Fassung
// weg ist; ein Lauf ohne Änderung schreibt keinen remote_check-Eintrag.
func TestA07TakenOverRefresh(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.takeOver(d)
	if rep := e.check(); rep.TakenOver != 1 {
		t.Fatalf("takeover run = %+v", rep)
	}
	n := e.auditCount(OpRemoteCheck)
	for i := 0; i < 2; i++ {
		if rep := e.check(); !rep.Empty() {
			t.Fatalf("run without change = %+v", rep)
		}
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteTakenOver || e.auditCount(OpRemoteCheck) != n {
		t.Fatalf("copy still there: remote %s, audit %d (was %d)", got.RemoteState, e.auditCount(OpRemoteCheck), n)
	}
	e.dropAll(d)
	if rep := e.check(); rep.Missing != 1 || e.draft(d.ID).RemoteState != RemoteMissing || e.auditCount(OpRemoteCheck) != n+1 {
		t.Fatalf("copy gone: %+v, remote %s", rep, e.draft(d.ID).RemoteState)
	}
	if r, _ := e.lastAudit(OpRemoteCheck); r.Reason != RemoteMissing || r.ActorType != "system" {
		t.Fatalf("audit = %+v", r)
	}
	if rep := e.check(); !rep.Empty() || e.auditCount(OpRemoteCheck) != n+1 {
		t.Fatalf("run after = %+v", rep)
	}
}
