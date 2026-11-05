package drafts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A06 Teil 4: durch eine eigene Antwort ersetzte Entwürfe (replaced.go).

// thRewritten ist die eigene Antwort des Menschen: dieselben Empfänger, im
// Verlauf des Originals, aber fast ganz neu geschrieben (Befund: 0,29).
const thRewritten = "Hallo Anna,\n\ndie Abrechnung stimmt leider nicht, bitte schicken Sie uns die Belege von Januar bis März noch einmal.\n\nViele Grüße"

// (a) bis (f) erfüllt, Ähnlichkeit um 0,3 → abandoned, Grund replaced,
// Akteur system, ohne sent_message_id; eine Freigabe wird superseded.
func TestA06ReplacedByOwnReply(t *testing.T) {
	e := newThEnv(t)
	ctx := context.Background()
	d := e.draftOf(KindReply, thOwn, true)
	// Vorgelegt: Die Freigabe wird superseded wie bei EvAbandon.
	if _, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: "mh"}); err != nil {
		t.Fatal(err)
	}
	// Nach der Vorlage legt der Push die Kopie neu ab (wie im Betrieb).
	d = e.pushAt(e.draft(d.ID), e.now)
	if d.RemoteState != RemotePushed {
		t.Fatalf("remote = %s", d.RemoteState)
	}
	fd := NewFallbackDraft(d, d.BodyMarkdown, "")
	if sc := fd.ScoreSent(NewSentWords(thRewritten)); sc < 0.2 || sc >= ThreadMinJaccard {
		t.Fatalf("score %.2f, want about 0.3", sc)
	}
	e.dropCopy(d)
	reply := e.send(thSent{text: thRewritten})
	// Nachbesserung 1, Sicherung (a): Im Lauf, der das Fehlen der Kopie
	// erst feststellt, wird nicht ersetzt (eine eben gesendete Nachricht
	// könnte noch kommen und den Entwurf als gesendet schließen).
	rep := e.check()
	if rep.ReplacedInThread != 0 || rep.Missing != 1 || e.status(d) != string(StatusPendingApproval) {
		t.Fatalf("first run = %+v, draft %s", rep, e.status(d))
	}
	rep = e.check()
	if rep.ReplacedInThread != 1 || len(rep.ReplacedDraftIDs) != 1 || rep.ReplacedDraftIDs[0] != d.ID || len(rep.SentDraftIDs) != 0 ||
		rep.Missing != 0 || rep.Empty() {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusAbandoned) || got.SentMessageID != 0 || got.ClosedAt.IsZero() {
		t.Fatalf("draft = %s, sent %d, closed %v", got.Status, got.SentMessageID, got.ClosedAt)
	}
	a, err := e.st.Approval(ctx, d.ApprovalID)
	if err != nil || a.Decision != DecisionSuperseded {
		t.Fatalf("approval = %+v, %v", a, err)
	}
	r, ok := e.lastAudit(OpReplacedDetected)
	if !ok || r.Reason != ReasonReplaced || r.ActorType != "system" || r.KeyID != "system" {
		t.Fatalf("audit = %+v", r)
	}
	p := string(r.ParamsRedacted)
	for _, want := range []string{`"draft_id":`, `"reply_message_id":`, `"to":"abandoned"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("audit params %s lack %s", p, want)
		}
	}
	if strings.Contains(p, "Belege") || strings.Contains(p, "anna") || strings.Contains(p, "Abrechnung") {
		t.Fatalf("audit params with content: %s", p)
	}
	if rid, ok, err := e.st.DraftReplacedBy(ctx, d.ID, got.ClosedAt); err != nil || !ok || rid != reply {
		t.Fatalf("DraftReplacedBy = %d, %v, %v (want %d)", rid, ok, err, reply)
	}
	if _, ok := e.lastAudit(OpSentDetected); ok {
		t.Fatal("replaced draft counted as sent")
	}
	// Ein zweiter Lauf ändert nichts.
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("second run = %+v", rep)
	}
}

// Zwei Fassungen derselben Antwort: eine unverändert gesendet (Weg über den
// Verlauf), die andere gelöscht. Die zweite wird abandoned, auch wenn ihre
// Kopie erst im nächsten Lauf verschwindet; die Antwort ist dann schon der
// ersten zugeordnet und zählt trotzdem.
func TestA06TwoVersionsOneSent(t *testing.T) {
	e := newThEnv(t)
	first := e.draftOf(KindReply, thOwn, true)
	second := e.draftOf(KindReply, strings.Replace(thOwn, "melden uns bis Freitag", "rufen Sie am Montag an", 1), true)
	e.dropCopy(first)
	sent := e.send(thSent{text: thOwn})
	rep := e.check()
	if rep.SentByThread != 1 || rep.ReplacedInThread != 0 {
		t.Fatalf("first run = %+v", rep)
	}
	if got := e.draft(first.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("first = %s", got.Status)
	}
	if got := e.status(second); got != string(StatusDraft) {
		t.Fatalf("second while its copy is there = %s", got)
	}
	e.dropCopy(second)
	if rep = e.check(); rep.Missing != 1 || rep.ReplacedInThread != 0 {
		t.Fatalf("run that finds the copy gone = %+v", rep)
	}
	rep = e.check()
	if rep.ReplacedInThread != 1 || rep.ReplacedDraftIDs[0] != second.ID || len(rep.SentDraftIDs) != 0 {
		t.Fatalf("second run = %+v", rep)
	}
	if got := e.draft(second.ID); got.Status != string(StatusAbandoned) || got.SentMessageID != 0 {
		t.Fatalf("second = %s, sent %d", got.Status, got.SentMessageID)
	}
	if got := e.draft(first.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("first afterwards = %s", got.Status)
	}
}

// Jede zusätzliche Bedingung einzeln verletzt → der Entwurf bleibt offen.
func TestA06ReplacedConditions(t *testing.T) {
	for _, c := range []struct {
		name  string
		run   func(e *thEnv) (store.Draft, CheckOptions)
		after func(e *thEnv, d store.Draft) // nach dem ersten Lauf
	}{
		{"copy still in the Drafts folder", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, nil},
		{"never put", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, false)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, nil},
		{"other recipients", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, to: []store.Addr{{Address: "bert@example.org"}}})
			return d, CheckOptions{}
		}, nil},
		{"without In-Reply-To", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, inReplyTo: " "})
			return d, CheckOptions{}
		}, nil},
		{"keep mark", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{KeepMark: true}
		}, nil},
		{"reply dated before the content change", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, date: e.now.Add(-10 * time.Minute)})
			return d, CheckOptions{}
		}, nil},
		{"reply dated before the last push", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, false)
			d = e.pushAt(d, e.now.Add(20*time.Minute))
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, date: e.now.Add(10 * time.Minute)})
			return d, CheckOptions{}
		}, nil},
		{"reply only in the inbox", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, role: "inbox"})
			return d, CheckOptions{}
		}, nil},
		{"kind new", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindNew, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, nil},
		// Nachbesserung 1, Sicherung (b): Ablageordner nicht mehr bekannt
		// (Ordner entfernt, ON DELETE SET NULL) bzw. nicht gespiegelt.
		{"mailbox gone", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, func(e *thEnv, d store.Draft) {
			if err := e.st.ExecForTest(`UPDATE drafts SET remote_mailbox_id = NULL WHERE id = ?`, d.ID); err != nil {
				e.t.Fatal(err)
			}
		}},
		{"mailbox not mirrored", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, func(e *thEnv, d store.Draft) {
			if err := e.st.ExecForTest(`UPDATE mailboxes SET sync_phase = 'new' WHERE id = ?`, e.m.boxes["drafts"]); err != nil {
				e.t.Fatal(err)
			}
		}},
		// Sicherung (c): In Gmail unter neuer Message-ID weiterbearbeitet
		// (Entwurf mit In-Reply-To = Original, nach der Ablage datiert).
		{"edited under a new Message-ID", func(e *thEnv) (store.Draft, CheckOptions) {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{role: "drafts", draftFlag: true, text: "noch in Arbeit", date: e.now.Add(5 * time.Minute)})
			e.send(thSent{text: thRewritten})
			return d, CheckOptions{}
		}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d, o := c.run(e)
			// Zwei Läufe: der erste stellt das Fehlen der Kopie fest, erst
			// der zweite dürfte ersetzen.
			for run := 1; run <= 2; run++ {
				rep, err := e.svc.CheckAfterSync(context.Background(), e.acc, o)
				if err != nil {
					t.Fatal(err)
				}
				if rep.ReplacedInThread != 0 || len(rep.SentDraftIDs) != 0 {
					t.Fatalf("run %d: report = %+v", run, rep)
				}
				if run == 1 && c.after != nil {
					c.after(e, d)
				}
			}
			if got := e.status(d); got != string(StatusDraft) {
				t.Fatalf("draft = %s", got)
			}
		})
	}
	// Gegenprobe zu „before the last push“: nach der Ablage datiert → ersetzt.
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, false)
	d = e.pushAt(d, e.now.Add(20*time.Minute))
	e.dropCopy(d)
	e.send(thSent{text: thRewritten, date: e.now.Add(30 * time.Minute)})
	e.check()
	if rep := e.check(); rep.ReplacedInThread != 1 {
		t.Fatalf("reply after the push: %+v", rep)
	}
}

// pushAt legt die Kopie ab wie draftOf, mit Date der Kopie at.
func (e *thEnv) pushAt(d store.Draft, at time.Time) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	id := e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: at, text: "kopie " + d.BodyText})
	var uid int64
	var hash string
	if err := e.st.DBForTest().QueryRow(`SELECT mm.uid, m.content_hash FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
		WHERE m.id = ?`, id).Scan(&uid, &hash); err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.svc.Pushed(ctx, d.ID, Fingerprint(e.draft(d.ID)), PushedCopy{MailboxID: e.m.boxes["drafts"], UIDValidity: 1,
		UID: uid, ContentHash: hash, Date: at.Format(time.RFC1123Z)}); err != nil {
		e.t.Fatal(err)
	}
	return e.draft(d.ID)
}

// Inhalt ändert sich während der Prüfung (Fingerabdruck) bzw. eine neue
// Kopie liegt im Entwürfe-Ordner (f): Replaced schließt nicht.
func TestA06ReplacedRechecksInTransaction(t *testing.T) {
	ctx := context.Background()
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	reply := e.send(thSent{text: thRewritten})
	// Der verglichene Stand ist nicht mehr der gespeicherte (ein anderer
	// Schreiber hat den Inhalt zwischen Vergleich und Schließen geändert):
	// anderer Fingerabdruck.
	stale := Fingerprint(e.draft(d.ID)) + "-alt"
	if done, err := e.svc.markReplaced(ctx, d.ID, reply, stale); err != nil || done {
		t.Fatalf("changed content: done %v, %v", done, err)
	}
	if got := e.status(d); got != string(StatusDraft) {
		t.Fatalf("draft = %s", got)
	}
	// Die Kopie ist wieder da (laut Spiegel): (f) gilt nicht mehr.
	e2 := newThEnv(t)
	d2 := e2.draftOf(KindReply, thOwn, true)
	reply2 := e2.send(thSent{text: thRewritten})
	if done, err := e2.svc.markReplaced(ctx, d2.ID, reply2, Fingerprint(e2.draft(d2.ID))); err != nil || done {
		t.Fatalf("copy present: done %v, %v", done, err)
	}
	if got := e2.status(d2); got != string(StatusDraft) {
		t.Fatalf("draft = %s", got)
	}
}

// Nachbesserung 1: Erst adoptOrphan, dann checkCopy. Liegt die Kopie eines
// abgebrochenen Ablageversuchs im Entwürfe-Ordner, übernimmt Replaced sie
// und schließt nicht (wie draft_changed).
func TestA06ReplacedAdoptsOrphanFirst(t *testing.T) {
	ctx := context.Background()
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	reply := e.send(thSent{text: thRewritten})
	if rep := e.check(); rep.Missing != 1 {
		t.Fatalf("first run = %+v", rep)
	}
	d = e.draft(d.ID)
	if err := e.svc.Attempt(ctx, d.ID, store.PushAttempt{MailboxID: e.m.boxes["drafts"], UIDValidity: 1, ContentHash: d.MessageIDHeader + "verwaist",
		MessageHash: "mh", Date: e.now.Format(time.RFC1123Z), Fingerprint: Fingerprint(d)}); err != nil {
		t.Fatal(err)
	}
	e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "verwaist"})
	d = e.draft(d.ID)
	if done, err := e.svc.markReplaced(ctx, d.ID, reply, Fingerprint(d)); err != nil || done {
		t.Fatalf("orphan copy: done %v, %v", done, err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("draft = %s", got.Status)
	}
}
