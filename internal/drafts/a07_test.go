package drafts

import (
	"context"
	"strings"
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

// Teil 3: neue Mail mit Nachsatz (Befund 09.11.2026: 29 Wörter Entwurf, 38
// gesendet, Jaccard 0,763). Synthetischer Nachbau mit 29 und 38 Wörtern.
const (
	sjOwn = "Hallo Herr Beispiel, die Heizung im zweiten Stock wird am Donnerstag zwischen acht und zwölf Uhr gewartet. " +
		"Bitte sorgen Sie dafür, dass der Techniker in Ihre Wohnung kommt. Danke"
	sjAdded   = " Falls der Termin nicht passt, melden Sie sich bitte bis Mittwoch im Büro."
	sjSubject = "Wartung Heizung"
)

var sjTo = []store.Addr{{Name: "Beispiel", Address: "mieter@example.net"}}

// sjSend legt eine gesendete neue Mail ab (ohne In-Reply-To).
func (e *thEnv) sjSend(subject, text string, to []store.Addr, role, from string) int64 {
	e.t.Helper()
	return e.send(thSent{subject: subject, text: text, to: to, role: role, from: from, inReplyTo: " "})
}

func TestA07SubjectPostscript(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
	fd := NewFallbackDraft(d, sjOwn, "")
	if n := len(fd.Words); n != 29 {
		t.Fatalf("draft words = %d", n)
	}
	sw := NewSentWords(sjOwn + sjAdded)
	if n := len(sw.Full); n != 38 {
		t.Fatalf("sent words = %d", n)
	}
	if sc := fd.ScoreSent(sw); sc < 0.75 || sc > 0.78 || sc >= FallbackMinJaccard {
		t.Fatalf("score %.3f, want about 0.763", sc)
	}
	e.dropCopy(d)
	sent := e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
	rep := e.check()
	if rep.SentBySubject != 1 || rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID || rep.Empty() {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	r, ok := e.lastAudit(OpSentDetected)
	if !ok || r.Reason != DetectSubject || r.ActorType != "system" {
		t.Fatalf("audit = %+v", r)
	}
	if p := string(r.ParamsRedacted); strings.Contains(p, "Heizung") || strings.Contains(p, "mieter") {
		t.Fatalf("audit params with content: %s", p)
	}
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("second run = %+v", rep)
	}
}

// Teil 3: Fälle, in denen der Weg über den Betreff nicht trifft.
func TestA07SubjectStaysOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(e *thEnv) store.Draft
	}{
		{"other subject", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.dropCopy(d)
			e.sjSend("Wartung Heizung Haus B", sjOwn+sjAdded, sjTo, "", "")
			return d
		}},
		{"reply with In-Reply-To and the same subject", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.dropCopy(d)
			e.send(thSent{subject: sjSubject, text: sjOwn + sjAdded, to: sjTo})
			return d
		}},
		{"other recipients", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.dropCopy(d)
			e.sjSend(sjSubject, sjOwn+sjAdded, thAnna, "", "")
			return d
		}},
		{"copy still in the Drafts folder", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
			return d
		}},
		{"own text with 7 words", func(e *thEnv) store.Draft {
			short := "Die Heizung wird am Donnerstag gewartet heute"
			d := e.draftTo(KindNew, short, sjTo, sjSubject, true)
			if n := len(NewFallbackDraft(d, short, "").Words); n != 7 {
				e.t.Fatalf("words = %d", n)
			}
			e.dropCopy(d)
			e.sjSend(sjSubject, short+" Bitte zugänglich machen.", sjTo, "", "")
			return d
		}},
		{"draft without To and Cc", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, nil, sjSubject, true)
			e.dropCopy(d)
			e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
			return d
		}},
		{"sent Re: X without In-Reply-To", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.dropCopy(d)
			e.sjSend("Re: "+sjSubject, sjOwn+sjAdded, sjTo, "", "")
			return d
		}},
		{"only in All Mail with a foreign sender", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
			e.dropCopy(d)
			e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "all", "fremd@example.net")
			return d
		}},
		{"never put", func(e *thEnv) store.Draft {
			d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, false)
			e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
			return d
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := c.run(e)
			for i := 0; i < 2; i++ {
				if rep := e.check(); rep.SentBySubject != 0 || len(rep.SentDraftIDs) != 0 {
					t.Fatalf("run %d = %+v", i, rep)
				}
			}
			if got := e.status(d); got != string(StatusDraft) {
				t.Fatalf("draft = %s", got)
			}
		})
	}
}

// Teil 3: nur in „Alle Nachrichten“, aber mit eigenem Absender → trifft.
func TestA07SubjectAllMailOwnSender(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
	e.dropCopy(d)
	e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "all", "Demo <demo@example.org>")
	if rep := e.check(); rep.SentBySubject != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

// Teil 3: zwei offene neue Entwürfe mit gleichem Betreff, eine gesendete
// Mail: Rangfolge wie Teil 2 (gleiche Ähnlichkeit → die jüngere Fassung).
func TestA07SubjectRanking(t *testing.T) {
	e := newThEnv(t)
	older := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
	e.later()
	younger := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
	e.dropCopy(older)
	e.dropCopy(younger)
	sent := e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
	rep := e.check()
	if rep.SentBySubject != 1 || rep.SentDraftIDs[0] != younger.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(younger.ID); got.SentMessageID != sent {
		t.Fatalf("younger sent %d", got.SentMessageID)
	}
	if got := e.status(older); got != string(StatusDraft) {
		t.Fatalf("older = %s", got)
	}
}

// Teil 3: läuft auch bei KeepMark.
func TestA07SubjectKeepMark(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
	e.dropCopy(d)
	e.sjSend(sjSubject, sjOwn+sjAdded, sjTo, "", "")
	rep, err := e.svc.CheckAfterSync(context.Background(), e.acc, CheckOptions{KeepMark: true})
	if err != nil || rep.SentBySubject != 1 {
		t.Fatalf("report = %+v, %v", rep, err)
	}
}

// Teil 4: Auffangregel „Kopie weg“.

// goneDraft legt eine abgelegte Antwort an, entfernt ihre Kopie und lässt
// einen Lauf das Fehlen bemerken (remote missing, Audit remote_check).
func (e *thEnv) goneDraft() store.Draft {
	e.t.Helper()
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	if rep := e.check(); rep.Missing != 1 {
		e.t.Fatalf("run that finds the copy gone = %+v", rep)
	}
	return e.draft(d.ID)
}

func TestA07CopyGoneAfter25h(t *testing.T) {
	e := newThEnv(t)
	d := e.goneDraft()
	noticed := e.now
	e.now = e.now.Add(23 * time.Hour)
	if rep := e.check(); rep.CopyGone != 0 || e.status(d) != string(StatusDraft) {
		t.Fatalf("after 23 h = %+v, %s", rep, e.status(d))
	}
	e.now = noticed.Add(25 * time.Hour)
	rep := e.check()
	if rep.CopyGone != 1 || len(rep.CopyGoneDraftIDs) != 1 || rep.CopyGoneDraftIDs[0] != d.ID || len(rep.SentDraftIDs) != 0 || rep.Empty() {
		t.Fatalf("after 25 h = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusAbandoned) || got.SentMessageID != 0 || got.ClosedAt.IsZero() {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	r, ok := e.lastAudit(OpCopyGoneDetected)
	if !ok || r.Reason != ReasonCopyGone || r.ActorType != "system" || r.KeyID != "system" {
		t.Fatalf("audit = %+v", r)
	}
	p := string(r.ParamsRedacted)
	if !strings.Contains(p, `"missing_since":"`+noticed.Format(time.RFC3339)+`"`) || !strings.Contains(p, `"draft_id":`) {
		t.Fatalf("audit params = %s", p)
	}
	ev, ok, err := e.st.DraftClosedBy(context.Background(), d.ID, got.ClosedAt)
	if err != nil || !ok || ev.Reason != ReasonCopyGone || ev.Operation != OpCopyGoneDetected {
		t.Fatalf("DraftClosedBy = %+v, %v, %v", ev, ok, err)
	}
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("run after = %+v", rep)
	}
}

// Teil 4: Eine Freigabe wird superseded.
func TestA07CopyGoneSupersedesApproval(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	if _, err := e.svc.RequestReady(context.Background(), d.ID, "key:agent", HookPass, Checked{MessageHash: "mh"}); err != nil {
		t.Fatal(err)
	}
	d = e.pushAt(e.draft(d.ID), e.now)
	e.dropCopy(d)
	e.check()
	e.now = e.now.Add(25 * time.Hour)
	if rep := e.check(); rep.CopyGone != 1 {
		t.Fatalf("report = %+v", rep)
	}
	a, err := e.st.Approval(context.Background(), e.draft(d.ID).ApprovalID)
	if err != nil || a.Decision != DecisionSuperseded {
		t.Fatalf("approval = %+v, %v", a, err)
	}
}

// Teil 4: Fälle, in denen die Regel nicht greift.
func TestA07CopyGoneStaysOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(e *thEnv, d store.Draft) CheckOptions
	}{
		{"copy back in the Drafts folder", func(e *thEnv, d store.Draft) CheckOptions {
			e.exec(`INSERT INTO message_mailboxes(message_id, mailbox_id, uid, flag_draft) VALUES (?, ?, ?, 1)`, e.copyID(d), e.m.boxes["drafts"], d.RemoteUID)
			return CheckOptions{}
		}},
		{"keep mark", func(e *thEnv, d store.Draft) CheckOptions { return CheckOptions{KeepMark: true} }},
		{"skip sent", func(e *thEnv, d store.Draft) CheckOptions { return CheckOptions{SkipSent: true} }},
		{"switched off", func(e *thEnv, d store.Draft) CheckOptions {
			e.svc.CopyGoneAfter = -1
			return CheckOptions{}
		}},
		{"mailbox gone", func(e *thEnv, d store.Draft) CheckOptions {
			e.exec(`UPDATE drafts SET remote_mailbox_id = NULL WHERE id = ?`, d.ID)
			return CheckOptions{}
		}},
		{"mailbox not mirrored", func(e *thEnv, d store.Draft) CheckOptions {
			e.exec(`UPDATE mailboxes SET sync_phase = 'new' WHERE id = ?`, e.m.boxes["drafts"])
			return CheckOptions{}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := e.goneDraft()
			e.now = e.now.Add(25 * time.Hour)
			o := c.prep(e, d)
			rep, err := e.svc.CheckAfterSync(context.Background(), e.acc, o)
			if err != nil || rep.CopyGone != 0 {
				t.Fatalf("report = %+v, %v", rep, err)
			}
			if got := e.status(d); got != string(StatusDraft) {
				t.Fatalf("draft = %s", got)
			}
		})
	}
}

// Teil 4: Zeitpunkt = jüngster remote_check mit Grund missing, nicht ein
// älterer mit anderem Grund (taken_over).
func TestA07CopyGoneUsesLatestMissing(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.takeOver(d)
	if rep := e.check(); rep.TakenOver != 1 {
		t.Fatalf("takeover = %+v", rep)
	}
	takenAt := e.now
	e.now = e.now.Add(2 * time.Hour)
	e.dropAll(d)
	if rep := e.check(); rep.Missing != 1 {
		t.Fatalf("missing run = %+v", rep)
	}
	missingAt := e.now
	e.now = takenAt.Add(25 * time.Hour) // 23 h nach missing
	if rep := e.check(); rep.CopyGone != 0 {
		t.Fatalf("measured from taken_over: %+v", rep)
	}
	e.now = missingAt.Add(25 * time.Hour)
	if rep := e.check(); rep.CopyGone != 1 {
		t.Fatalf("after 25 h missing = %+v", rep)
	}
	r, _ := e.lastAudit(OpCopyGoneDetected)
	if !strings.Contains(string(r.ParamsRedacted), missingAt.Format(time.RFC3339)) {
		t.Fatalf("audit params = %s", r.ParamsRedacted)
	}
}

// Teil 4: Trifft im selben Lauf ein anderer Weg, gewinnt er.
func TestA07CopyGoneOtherWayWins(t *testing.T) {
	e := newThEnv(t)
	d := e.goneDraft()
	e.now = e.now.Add(25 * time.Hour)
	e.send(thSent{text: thOwn, date: e.now.Add(-time.Hour)})
	rep := e.check()
	if rep.SentByThread != 1 || rep.CopyGone != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.status(d); got != string(StatusSentByHuman) {
		t.Fatalf("draft = %s", got)
	}
}

// Teil 4: Ohne Audit-Eintrag (etwa nach einer Rotation) gilt die Regel
// nicht: remote missing ohne drafts.remote_check.
func TestA07CopyGoneWithoutAuditEntry(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	e.exec(`UPDATE drafts SET remote_state = 'missing' WHERE id = ?`, d.ID)
	if n := e.auditCount(OpRemoteCheck); n != 0 {
		t.Fatalf("remote_check entries = %d", n)
	}
	e.now = e.now.Add(48 * time.Hour)
	if rep := e.check(); rep.CopyGone != 0 || e.status(d) != string(StatusDraft) {
		t.Fatalf("report = %+v", rep)
	}
}
