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

// closableReplaced baut einen Entwurf im sonst schließbaren Zustand: Kopie
// in einem früheren Lauf als fehlend vermerkt (missing), eigene Antwort mit
// anderem Text gesendet. Liefert Entwurf und Antwort.
func closableReplaced(t *testing.T, e *thEnv) (store.Draft, int64) {
	t.Helper()
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	reply := e.send(thSent{text: thRewritten})
	before, _ := e.svc.MissingDrafts(context.Background(), e.acc)
	if rep, err := e.svc.CheckAfterSync(context.Background(), e.acc, CheckOptions{MissingBefore: before}); err != nil || rep.Missing != 1 {
		t.Fatalf("run that finds the copy gone = %+v, %v", rep, err)
	}
	d = e.draft(d.ID)
	if d.RemoteState != RemoteMissing {
		t.Fatalf("remote = %s", d.RemoteState)
	}
	return d, reply
}

// Nachbesserung 2: Die Prüfungen in der Transaktion von Replaced. Jeder Fall
// startet aus einem sonst schließbaren missing-Zustand, verletzt genau eine
// Bedingung (Replaced schließt nicht) und hebt sie wieder auf (Replaced
// schließt dann; so ist belegt, dass genau diese Bedingung gehalten hat).
func TestA06ReplacedRechecksInTransaction(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name         string
		break_, mend func(e *thEnv, d store.Draft)
		staleFprint  bool
	}{
		{name: "content changed (fingerprint)", staleFprint: true},
		{name: "copy back in the Drafts folder", break_: func(e *thEnv, d store.Draft) {
			e.m.link("drafts", e.copyID(d), true)
		}, mend: func(e *thEnv, d store.Draft) {
			e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND message_id = ?`, e.m.boxes["drafts"], e.copyID(d))
		}},
		{name: "remote_state no longer missing", break_: func(e *thEnv, d store.Draft) {
			e.exec(`UPDATE drafts SET remote_state = 'pushed' WHERE id = ?`, d.ID)
		}, mend: func(e *thEnv, d store.Draft) {
			e.exec(`UPDATE drafts SET remote_state = 'missing' WHERE id = ?`, d.ID)
		}},
		{name: "mailbox unknown", break_: func(e *thEnv, d store.Draft) {
			e.exec(`UPDATE drafts SET remote_mailbox_id = NULL WHERE id = ?`, d.ID)
		}, mend: func(e *thEnv, d store.Draft) {
			e.exec(`UPDATE drafts SET remote_mailbox_id = ? WHERE id = ?`, d.RemoteMailboxID, d.ID)
		}},
		{name: "edited under a new Message-ID", break_: func(e *thEnv, d store.Draft) {
			e.send(thSent{role: "drafts", draftFlag: true, messageID: "gmail-edit@example.org", text: "noch in Arbeit", date: e.now.Add(5 * time.Minute)})
		}, mend: func(e *thEnv, d store.Draft) {
			e.exec(`DELETE FROM message_mailboxes WHERE message_id = (SELECT id FROM messages WHERE message_id_header = 'gmail-edit@example.org')`)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d, reply := closableReplaced(t, e)
			fp := Fingerprint(d)
			if c.staleFprint {
				if done, err := e.svc.markReplaced(ctx, d.ID, reply, fp+"-alt"); err != nil || done {
					t.Fatalf("broken: done %v, %v", done, err)
				}
			} else {
				c.break_(e, d)
				if done, err := e.svc.markReplaced(ctx, d.ID, reply, fp); err != nil || done {
					t.Fatalf("broken: done %v, %v", done, err)
				}
				c.mend(e, d)
			}
			if got := e.status(d); got != string(StatusDraft) {
				t.Fatalf("draft after the broken attempt = %s", got)
			}
			if done, err := e.svc.markReplaced(ctx, d.ID, reply, fp); err != nil || !done {
				t.Fatalf("mended: done %v, %v", done, err)
			}
		})
	}
}

// Nachbesserung 2: Erst adoptOrphan, dann checkCopy. Die Kopie eines
// abgebrochenen Ablageversuchs liegt in einem anderen Ordner als die
// verschwundene Kopie; ohne Übernahme hielte checkCopy den Entwurf für
// missing und Replaced schlösse ihn. Nur die Übernahme verhindert das
// Schließen (die übernommene Kopie ist danach die abgelegte).
func TestA06ReplacedAdoptsOrphanFirst(t *testing.T) {
	ctx := context.Background()
	e := newThEnv(t)
	d, reply := closableReplaced(t, e)
	other := e.m.boxes["all"]
	if err := e.svc.Attempt(ctx, d.ID, store.PushAttempt{MailboxID: other, UIDValidity: 1, ContentHash: d.MessageIDHeader + "verwaist",
		MessageHash: "mh", Date: e.now.Format(time.RFC1123Z), Fingerprint: Fingerprint(d)}); err != nil {
		t.Fatal(err)
	}
	e.m.put("all", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "verwaist"})
	d = e.draft(d.ID)
	if done, err := e.svc.markReplaced(ctx, d.ID, reply, Fingerprint(d)); err != nil || done {
		t.Fatalf("orphan copy: done %v, %v", done, err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("draft = %s", got.Status)
	}
}

// Nachbesserung 2, Sicherung (a): „missing schon vor dem Lauf“ gilt für den
// Stand vor Beginn des Sync-Laufs. Setzt PushCheck (Push zu Beginn des
// Laufs) missing erst in diesem Lauf, wird nicht ersetzt, erst im
// Folgelauf.
func TestA06ReplacedMissingSetByPushCheckInSameRun(t *testing.T) {
	ctx := context.Background()
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	// Geändert nach der Ablage: wartet auf den nächsten Push (pending).
	c := Content{To: d.To, Cc: d.Cc, Subject: d.Subject, BodyMarkdown: thOwn + " Danke.", BodyText: d.BodyText + " Danke."}
	if _, err := e.svc.Update(ctx, d.ID, c, Fingerprint(d)); err != nil {
		t.Fatal(err)
	}
	if got := e.draft(d.ID); got.RemoteState != RemotePending {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	e.dropCopy(d)
	e.send(thSent{text: thRewritten})
	// Lauf 1: Stand vor dem Lauf lesen, dann PushCheck (setzt missing).
	before, err := e.svc.MissingDrafts(ctx, e.acc)
	if err != nil || before[d.ID] {
		t.Fatalf("missing before run 1 = %v, %v", before, err)
	}
	if _, st, err := e.svc.PushCheck(ctx, d.ID); err != nil || st != RemoteMissing {
		t.Fatalf("PushCheck = %q, %v", st, err)
	}
	rep, err := e.svc.CheckAfterSync(ctx, e.acc, CheckOptions{MissingBefore: before})
	if err != nil || rep.ReplacedInThread != 0 || e.status(d) != string(StatusDraft) {
		t.Fatalf("run 1 = %+v, %v, draft %s", rep, err, e.status(d))
	}
	// Lauf 2: jetzt war der Entwurf vor dem Lauf missing.
	before, _ = e.svc.MissingDrafts(ctx, e.acc)
	if rep, err := e.svc.CheckAfterSync(ctx, e.acc, CheckOptions{MissingBefore: before}); err != nil || rep.ReplacedInThread != 1 {
		t.Fatalf("run 2 = %+v, %v", rep, err)
	}
}

// Nachbesserung 2, Sicherung (c): Grenze ist die letzte Ablage. Eine
// Gmail-Fassung zwischen Ablage und einer späteren lokalen Änderung hält
// den Entwurf offen.
func TestA06ReplacedEditedBetweenPushAndLocalChange(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, false)
	d = e.pushAt(d, e.now) // Ablage um 09:00
	// Gmail speichert um 09:10 eine weiterbearbeitete Fassung unter neuer
	// Message-ID; lokal geändert (Inhalt) um 09:20.
	e.send(thSent{role: "drafts", draftFlag: true, text: "noch in Arbeit", date: e.now.Add(10 * time.Minute)})
	if err := e.st.ExecForTest(`UPDATE drafts SET content_changed_at = ? WHERE id = ?`, e.now.Add(20*time.Minute).UnixMilli(), d.ID); err != nil {
		t.Fatal(err)
	}
	e.dropCopy(d)
	e.send(thSent{text: thRewritten, date: e.now.Add(30 * time.Minute)})
	for run := 1; run <= 2; run++ {
		if rep := e.check(); rep.ReplacedInThread != 0 {
			t.Fatalf("run %d = %+v", run, rep)
		}
	}
	if got := e.status(d); got != string(StatusDraft) {
		t.Fatalf("draft = %s", got)
	}
}

func (e *thEnv) exec(q string, args ...any) {
	e.t.Helper()
	if err := e.st.ExecForTest(q, args...); err != nil {
		e.t.Fatal(err)
	}
}

// copyID ist die lokale ID der Kopie des Entwurfs (Nachricht mit seiner
// Message-ID), auch wenn sie keinem Ordner mehr zugeordnet ist.
func (e *thEnv) copyID(d store.Draft) int64 {
	e.t.Helper()
	n, err := e.st.QueryIntForTest(`SELECT min(id) FROM messages WHERE message_id_header = ?`, d.MessageIDHeader)
	if err != nil {
		e.t.Fatal(err)
	}
	return int64(n)
}
