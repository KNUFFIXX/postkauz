package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A12 Teil 1: Übernahme im Mailprogramm auch unter neuer Message-ID
// (weiterbearbeitete Fassung) und umgezogene Kopien (remote.go).

func (e *thEnv) agentUpdate(d store.Draft) error {
	c := Content{To: thAnna, Subject: d.Subject, BodyMarkdown: d.BodyMarkdown + " Neu.", BodyText: d.BodyText + " Neu."}
	_, err := e.svc.As(Who{Actor: "agent", KeyID: "key:agent", Scope: "mail:draft", Operation: "drafts.update"}, false).
		Update(context.Background(), d.ID, c, "")
	return err
}

func (e *thEnv) agentDiscard(d store.Draft) error {
	_, err := e.svc.Discard(context.Background(), DiscardRequest{ID: d.ID, KeyID: "key:agent"})
	return err
}

func (e *thEnv) agentSupersede(d store.Draft) error {
	e.later()
	nw := e.draftOf(Kind(d.Kind), thOwn+" Zweite Fassung.", false)
	if d.Kind == string(KindNew) {
		nw = e.draftTo(KindNew, sjOwn+" Zweite Fassung.", sjTo, d.Subject, false)
	}
	_, err := e.svc.Supersede(context.Background(), SupersedeRequest{OldID: d.ID, NewID: nw.ID, KeyID: "key:agent"})
	return err
}

// lastRemote liefert Grund und Parameter des letzten drafts.remote_check.
func (e *thEnv) lastRemote() (string, map[string]any) {
	e.t.Helper()
	r, ok := e.lastAudit(OpRemoteCheck)
	if !ok {
		e.t.Fatal("no drafts.remote_check")
	}
	var p map[string]any
	_ = json.Unmarshal(r.ParamsRedacted, &p)
	return r.Reason, p
}

// Abgelegte Antwort und neue Mail, Kopie weg, im Entwürfe-Ordner eine nach
// der Ablage datierte Fassung unter neuer Message-ID: taken_over mit
// edited_elsewhere; update, discard und supersede abgelehnt, keine
// Aufräum-Aktion. Verschwindet die Fassung, wird der Entwurf missing, der
// Vermerk ist weg, und „Kopie weg“ greift wie bisher.
func TestA12EditedElsewhereTakenOver(t *testing.T) {
	for _, c := range []struct {
		name string
		mk   func(e *thEnv) store.Draft
		edit func(e *thEnv) int64
	}{
		{"reply", func(e *thEnv) store.Draft { return e.draftOf(KindReply, thOwn, true) }, func(e *thEnv) int64 {
			return e.send(thSent{role: "drafts", text: thOwn + " Nachtrag."})
		}},
		{"new email", func(e *thEnv) store.Draft { return e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true) }, func(e *thEnv) int64 {
			return e.send(thSent{role: "drafts", inReplyTo: " ", subject: sjSubject, to: sjTo, text: sjOwn + sjAdded})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := c.mk(e)
			e.dropCopy(d)
			edited := c.edit(e)
			rep := e.check()
			got := e.draft(d.ID)
			if rep.TakenOver != 1 || got.RemoteState != RemoteTakenOver || got.EditedMessageID != edited || got.Status != string(StatusDraft) {
				t.Fatalf("report %+v, draft %s/%s edited %d (want %d)", rep, got.Status, got.RemoteState, got.EditedMessageID, edited)
			}
			reason, p := e.lastRemote()
			if reason != RemoteTakenOver || p["edited_elsewhere"] != true || p["edited_message_id"] != float64(edited) || p["via"] != ViaEditedElsewhere {
				t.Fatalf("audit %s %v", reason, p)
			}
			e.noContentInAudit()
			if err := e.agentUpdate(got); ReasonOf(err) != ReasonDraftTakenOver {
				t.Fatalf("update = %v", err)
			}
			if err := e.agentDiscard(got); ReasonOf(err) != ReasonCopyChanged {
				t.Fatalf("discard = %v", err)
			}
			if err := e.agentSupersede(got); ReasonOf(err) != ReasonCopyChanged {
				t.Fatalf("supersede = %v", err)
			}
			if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`); n != 0 {
				t.Fatalf("actions = %d", n)
			}
			// Ein zweiter Lauf ändert nichts.
			if rep := e.check(); rep.TakenOver != 0 || rep.Missing != 0 {
				t.Fatalf("second run = %+v", rep)
			}
			// Der Mensch sendet oder löscht seine Fassung: missing, Vermerk weg.
			e.exec(`DELETE FROM message_mailboxes WHERE message_id = ?`, edited)
			if rep := e.check(); rep.Missing != 1 {
				t.Fatalf("edited version gone: %+v", rep)
			}
			if got := e.draft(d.ID); got.RemoteState != RemoteMissing || got.EditedMessageID != 0 {
				t.Fatalf("after the edited version is gone: %s, edited %d", got.RemoteState, got.EditedMessageID)
			}
			if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM meta WHERE key LIKE 'draft_edited/%'`); n != 0 {
				t.Fatalf("edited notes = %d", n)
			}
			e.now = e.now.Add(25 * time.Hour)
			if rep := e.check(); rep.CopyGone != 1 {
				t.Fatalf("copy gone after 25 h: %+v", rep)
			}
		})
	}
}

// Keine weiterbearbeitete Fassung: vor der Ablage datiert, Kopie von
// PostKauz (Message-ID eines Entwurfs), anderer Betreff bzw. anderes
// Original → missing wie bisher.
func TestA12EditedElsewhereNot(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(e *thEnv, d store.Draft)
	}{
		{"dated before the push", func(e *thEnv, d store.Draft) {
			e.send(thSent{role: "drafts", text: thOwn + " Nachtrag.", date: e.now.Add(-10 * time.Minute)})
		}},
		{"a copy of PostKauz", func(e *thEnv, d store.Draft) {
			other := e.draftOf(KindReply, thOwn+" Andere.", false)
			e.send(thSent{role: "drafts", messageID: other.MessageIDHeader, text: thOwn + " Nachtrag."})
		}},
		{"other original", func(e *thEnv, d store.Draft) {
			e.send(thSent{role: "drafts", inReplyTo: "<other@example.com>", text: thOwn + " Nachtrag."})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			c.edit(e, d)
			if rep := e.check(); rep.Missing != 1 || rep.TakenOver != 0 {
				t.Fatalf("report = %+v", rep)
			}
		})
	}
}

// Entwurf schon missing (vor dem Programmwechsel), dann die Fassung im
// Entwürfe-Ordner: discard vor dem nächsten Lauf → copy_changed (nichts
// vermerkt); der nächste Lauf setzt taken_over (refreshMissing).
func TestA12MissingThenEditedElsewhere(t *testing.T) {
	e := newThEnv(t)
	d := e.goneDraft()
	edited := e.send(thSent{role: "drafts", text: thOwn + " Nachtrag."})
	if err := e.agentDiscard(d); ReasonOf(err) != ReasonCopyChanged || !strings.Contains(err.Error(), "saved as a new message") {
		t.Fatalf("discard before the run = %v", err)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteMissing || got.Status != string(StatusDraft) || got.EditedMessageID != 0 {
		t.Fatalf("discard wrote something: %+v", got)
	}
	if rep := e.check(); rep.TakenOver != 1 {
		t.Fatalf("run = %+v", rep)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteTakenOver || got.EditedMessageID != edited {
		t.Fatalf("draft = %s, edited %d", got.RemoteState, got.EditedMessageID)
	}
}

// Ohne weiterbearbeitete Fassung schließt discard einen missing-Entwurf
// wie bisher ohne Aktion.
func TestA12DiscardMissingStillCloses(t *testing.T) {
	e := newThEnv(t)
	d := e.goneDraft()
	if err := e.agentDiscard(d); err != nil {
		t.Fatal(err)
	}
	if got := e.status(d); got != string(StatusAbandoned) {
		t.Fatalf("status = %s", got)
	}
}

// Die Versanderkennung prüft den reinen Stand: eine gesendete Antwort im
// Verlauf und daneben ein fremder, nach der Ablage datierter Entwurf zum
// selben Original → weiterhin sent_by_human.
func TestA12SentDetectionUsesRawState(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	e.send(thSent{role: "drafts", text: "Ganz andere Antwort einer Kollegin.", date: e.now.Add(5 * time.Minute)})
	e.send(thSent{})
	e.check()
	if got := e.status(d); got != string(StatusSentByHuman) {
		t.Fatalf("status = %s", got)
	}
}

// Ordner auf dem Server verschwunden: missing wie bisher, auch wenn
// anderswo eine Fassung mit \Draft liegt.
func TestA12MailboxGoneStaysMissing(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.send(thSent{role: "all", draftFlag: true, text: thOwn + " Nachtrag."})
	e.exec(`DELETE FROM mailboxes WHERE id = ?`, e.m.boxes["drafts"])
	if rep := e.check(); rep.Missing != 1 || rep.TakenOver != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteMissing {
		t.Fatalf("remote = %s", got.RemoteState)
	}
}

// moveCopy legt die Kopie des Entwurfs mit denselben Bytes unter eine neue
// UID im Entwürfe-Ordner (wie Gmail bei großen Ablagen) und liefert die UID.
func (e *thEnv) moveCopy(d store.Draft, drop bool) int64 {
	e.t.Helper()
	if drop {
		e.dropCopy(d)
	}
	e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "kopie " + d.BodyMarkdown})
	return int64(e.m.uid[e.m.boxes["drafts"]])
}

// Umzug: gleiche Message-ID, gleicher Inhalt, neue UID im selben Ordner →
// moved, pushed, remote_uid neu; danach geht drafts.update durch.
func TestA12MovedCopy(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	old := d.RemoteUID
	nu := e.moveCopy(d, true)
	if rep := e.check(); rep.TakenOver != 0 || rep.Missing != 0 {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.RemoteState != RemotePushed || got.RemoteUID != nu || len(got.RemotePrevious) != 0 {
		t.Fatalf("draft = %s uid %d prev %v", got.RemoteState, got.RemoteUID, got.RemotePrevious)
	}
	reason, p := e.lastRemote()
	if reason != ReasonMoved || p["old_uid"] != float64(old) || p["new_uid"] != float64(nu) || p["loaded"] != true {
		t.Fatalf("audit %s %v", reason, p)
	}
	if err := e.agentUpdate(got); err != nil {
		t.Fatalf("update after the move = %v", err)
	}
}

// Alte UID in einem Lauf entfernt, neue erst im nächsten gespiegelt:
// zwischendurch missing (discard → draft_changed, sobald die neue da ist),
// dann moved → pushed, kein „Kopie weg“.
func TestA12MovedAcrossRuns(t *testing.T) {
	e := newThEnv(t)
	d := e.goneDraft()
	e.moveCopy(d, false)
	if err := e.agentDiscard(d); ReasonOf(err) != ReasonDraftChanged || !strings.Contains(err.Error(), "moved") {
		t.Fatalf("discard before the run = %v", err)
	}
	if rep := e.check(); rep.TakenOver != 0 {
		t.Fatalf("run = %+v", rep)
	}
	if got := e.draft(d.ID); got.RemoteState != RemotePushed {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	if reason, _ := e.lastRemote(); reason != ReasonMoved {
		t.Fatalf("audit = %s", reason)
	}
	if _, ok, err := e.st.DraftMissingSince(context.Background(), d.ID, LastPushAt(e.draft(d.ID))); err != nil || ok {
		t.Fatalf("missing since after the move: %v %v", ok, err)
	}
	e.now = e.now.Add(25 * time.Hour)
	if rep := e.check(); rep.CopyGone != 0 || e.status(d) != string(StatusDraft) {
		t.Fatalf("after 25 h: %+v", rep)
	}
}

// Zwei neuere Kopien, eine mit anderem Inhalt → taken_over (changed);
// gleiche Message-ID mit anderem Inhalt → taken_over; nicht geladene
// neuere Kopie (Altbestand) → Zustand bleibt.
func TestA12NewerCopies(t *testing.T) {
	t.Run("one of two differs", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		e.moveCopy(d, true)
		e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "anders"})
		if rep := e.check(); rep.TakenOver != 1 {
			t.Fatalf("report = %+v", rep)
		}
		if _, p := e.lastRemote(); p["changed"] != true || p["via"] != ViaNewerSameID || p["loaded"] != true {
			t.Fatalf("audit %v", p)
		}
	})
	t.Run("not loaded", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		nu := e.moveCopy(d, true)
		e.exec(`UPDATE message_mailboxes SET content_loaded = 0 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], nu)
		if rep := e.check(); rep.TakenOver != 0 || rep.Missing != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if got := e.draft(d.ID); got.RemoteState != RemotePushed || got.RemoteUID != d.RemoteUID {
			t.Fatalf("draft = %s uid %d", got.RemoteState, got.RemoteUID)
		}
	})
	t.Run("head checked only", func(t *testing.T) {
		// content_loaded = 1 aus dem Kopfvergleich vor einer Aktion zählt
		// nicht als geladen.
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		nu := e.moveCopy(d, true)
		e.exec(`INSERT INTO actions(id, account_id, kind, state, params, target_count, ids_hash, requested_by, created_at, updated_at)
			VALUES (77, ?, 'mark', 'done', '{}', 1, 'x', 'cli', 0, 0)`, e.acc)
		e.exec(`INSERT INTO action_targets(action_id, message_id, mailbox_id, uid, uidvalidity) SELECT 77, message_id, mailbox_id, uid, 1
			FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], nu)
		if rep := e.check(); rep.TakenOver != 0 || rep.Missing != 0 {
			t.Fatalf("report = %+v", rep)
		}
		if got := e.draft(d.ID); got.RemoteUID != d.RemoteUID {
			t.Fatalf("moved although only the header was checked: uid %d", got.RemoteUID)
		}
	})
}

// Ein taken_over-Entwurf (B2: rc8/rc9 hielten den Umzug für eine
// Bearbeitung) wird über moved wieder pushed.
func TestA12TakenOverHealsByMove(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.moveCopy(d, true)
	e.exec(`UPDATE drafts SET remote_state = 'taken_over' WHERE id = ?`, d.ID)
	e.check()
	if got := e.draft(d.ID); got.RemoteState != RemotePushed {
		t.Fatalf("remote = %s", got.RemoteState)
	}
}

// moveAt legt die Kopie unter uid mit denselben Bytes (text) unter eine
// neue UID (Umzug durch den Server) und liefert die neue UID.
func (e *cuEnv) moveAt(d store.Draft, uid int64, text string) int64 {
	e.t.Helper()
	box := e.m.boxes["drafts"]
	e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, box, uid)
	e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, from: "demo@example.org", text: text, date: e.now, draftFlag: true})
	return int64(e.m.uid[box])
}

// Die aktuelle Kopie zieht um, danach eine Änderung: die Aufräum-Aktion
// nach dem Push trifft die neue UID.
func TestA12MovedCurrentThenCleanup(t *testing.T) {
	e := newCuEnv(t)
	d := e.newDraft("Probe")
	u1 := e.push(d, "eins", true)
	nu := e.moveAt(d, u1, "eins")
	e.check()
	if got := e.draft(d.ID); got.RemoteUID != nu || got.RemoteState != RemotePushed {
		t.Fatalf("after the move: uid %d %s", got.RemoteUID, got.RemoteState)
	}
	d = e.update(d, "Probe")
	e.push(d, "zwei", true)
	acts := e.actionsOf(d.ID)
	if len(acts) != 1 || len(acts[0].Targets) != 1 || int64(acts[0].Targets[0].UID) != nu {
		t.Fatalf("actions = %+v", acts)
	}
	e.run()
	if e.inDrafts(nu) || e.draftCopies() != 1 {
		t.Fatalf("copies left = %d", e.draftCopies())
	}
}

// Eine frühere Kopie zieht um, bevor ihre Aufräum-Aktion läuft: die Aktion
// endet not_found, der Nachlauf legt eine neue mit der neuen UID an (kein
// Fehlversuch), danach liegt nur die neueste Kopie im Entwürfe-Ordner.
func TestA12MovedEarlierCopyRetried(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Probe")
	e.push(d, "zwei", true)
	first := e.actionsOf(d.ID)
	if len(first) != 1 {
		t.Fatalf("actions = %+v", first)
	}
	nu := e.moveAt(d, u1, "eins")
	e.run()
	if a, _ := e.st.Action(ctx, first[0].ID); a.Targets[0].Result == store.TargetOK {
		t.Fatalf("first action hit the old uid: %+v", a)
	}
	if _, err := e.svc.RetryCleanup(ctx, e.acc); err != nil {
		t.Fatal(err)
	}
	acts := e.actionsOf(d.ID)
	if len(acts) != 2 || int64(acts[0].Targets[0].UID) != nu {
		t.Fatalf("actions after the retry = %+v", acts)
	}
	got := e.draft(d.ID)
	if len(got.RemotePrevious) != 1 || got.RemotePrevious[0].UID != nu {
		t.Fatalf("remote_previous = %+v", got.RemotePrevious)
	}
	if n, ok, _ := e.st.CleanupCountOf(ctx, got.RemotePrevious[0]); !ok || n.N != 1 {
		t.Fatalf("count of the moved copy = %+v %v", n, ok)
	}
	e.run()
	if e.inDrafts(nu) || e.draftCopies() != 1 {
		t.Fatalf("copies left = %d", e.draftCopies())
	}
}

// A12 Teil 0 (c): Der Zähler einer Kopie, die ein offener Entwurf führt,
// bleibt über CleanupWindow hinaus; nach drei Fehlschlägen beginnt keine
// neue Runde. Ist die Kopie laut Spiegel weg, fällt er weg.
func TestA12CleanupCountKeptForOpenDraft(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Abrechnung")
	u1 := e.push(d, "eins", true)
	d = e.update(d, "Abrechnung neu")
	e.push(d, "zwei", true)
	stale := func() {
		e.exec(`UPDATE message_mailboxes SET content_loaded = 0 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u1)
		e.run()
	}
	stale()
	for i := 2; i <= 3; i++ {
		if _, err := e.svc.RetryCleanup(ctx, e.acc); err != nil {
			t.Fatal(err)
		}
		stale()
	}
	e.now = e.now.Add(CleanupWindow + time.Hour)
	rep, err := e.svc.RetryCleanup(ctx, e.acc)
	if err != nil || rep.Actions != 0 {
		t.Fatalf("after 7 days = %+v %v", rep, err)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 1 {
		t.Fatalf("counter = %d", n)
	}
	// Die Kopie ist weg (der Mensch hat sie gelöscht): Zähler weg.
	e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u1)
	if _, err := e.svc.RetryCleanup(ctx, e.acc); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 0 {
		t.Fatalf("counter after the copy is gone = %d", n)
	}
}
