package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A10 Teil 2: Geplant erkennen (scheduled.go).

// schedule legt die geplante Nachricht an, die Gmail beim Planen eines
// Entwurfs anlegt (B1): neue Message-ID, nur in „Alle Nachrichten“, kein
// \Draft, Date und INTERNALDATE = Termin. inReplyTo: "" = Original des
// Verlaufs (Antwort), " " = keins (neue Mail).
func (e *thEnv) schedule(at time.Time, subject, text string, to []store.Addr, inReplyTo, from string) int64 {
	e.t.Helper()
	if from == "" {
		from = a10Own
	}
	return e.send(thSent{role: "all", from: from, subject: subject, text: text, to: to, inReplyTo: inReplyTo, date: at,
		messageID: "sched-" + at.Format("20060102150405") + "-" + strings.ReplaceAll(subject, " ", "") + "@mail.example.com"})
}

// scheduledReply plant eine Antwort auf thOwn mit Termin at (Kopie weg).
func (e *thEnv) scheduledReply(at time.Time) (store.Draft, int64) {
	e.t.Helper()
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	return d, e.schedule(at, "Re: Abrechnung", thOwn, thAnna, "", "")
}

func (e *thEnv) vermerk(id int64) (store.Scheduled, bool) {
	e.t.Helper()
	sc, ok, err := e.st.ScheduledOf(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return sc, ok
}

// B1 nachgestellt (Termin + 3 Tage und + 34 Tage): Der Entwurf wird
// geplant, nie sent_by_human, und nach 25 Stunden schließt „Kopie weg“
// ihn nicht.
func TestA10ScheduledB1(t *testing.T) {
	for _, days := range []int{3, 34} {
		t.Run(time.Duration(days*24).String(), func(t *testing.T) {
			e := newThEnv(t)
			at := e.now.Add(time.Duration(days) * 24 * time.Hour)
			d, mid := e.scheduledReply(at)
			rep := e.check()
			if rep.Scheduled != 1 || len(rep.ScheduledDraftIDs) != 1 || rep.ScheduledDraftIDs[0] != d.ID || len(rep.SentDraftIDs) != 0 {
				t.Fatalf("report = %+v", rep)
			}
			sc, ok := e.vermerk(d.ID)
			if !ok || sc.MessageID != mid || !sc.At.Equal(at) || sc.State != store.ScheduledActive {
				t.Fatalf("vermerk = %+v %v", sc, ok)
			}
			r, ok := e.lastAudit(OpScheduledDetected)
			if !ok || r.ActorType != "system" || r.Reason != ReasonScheduled {
				t.Fatalf("audit = %+v", r)
			}
			var p map[string]any
			_ = json.Unmarshal(r.ParamsRedacted, &p)
			if p["draft_id"] != float64(d.ID) || p["message_id"] != float64(mid) || p["scheduled_for"] != at.UTC().Format(time.RFC3339) {
				t.Fatalf("params = %v", p)
			}
			if strings.Contains(string(r.ParamsRedacted), "anna") || strings.Contains(string(r.ParamsRedacted), "Abrechnung") {
				t.Fatalf("audit names content: %s", r.ParamsRedacted)
			}
			for i := 0; i < 3; i++ {
				e.now = e.now.Add(25 * time.Hour / 3)
				rep := e.check()
				if rep.CopyGone != 0 || len(rep.SentDraftIDs) != 0 || rep.ReplacedInThread != 0 || rep.Scheduled != 0 {
					t.Fatalf("run %d = %+v", i, rep)
				}
			}
			if e.status(d) != string(StatusDraft) {
				t.Fatalf("status = %s", e.status(d))
			}
			if sc, ok := e.vermerk(d.ID); !ScheduledActive(sc, ok) {
				t.Fatalf("vermerk after 25 h = %+v", sc)
			}
		})
	}
}

// Die geplante Nachricht zusätzlich in einer virtuellen Sicht (Gmail
// „Wichtig“): weiter geplant.
func TestA10ScheduledInVirtualView(t *testing.T) {
	e := newThEnv(t)
	if err := e.st.ExecForTest(`INSERT INTO mailboxes(account_id, path, name, role, virtual_view, sync_phase, uidvalidity)
		VALUES (?, '[Gmail]/Wichtig', 'Wichtig', 'custom', 1, 'ready', 1)`, e.acc); err != nil {
		t.Fatal(err)
	}
	virt, _ := e.st.QueryIntForTest(`SELECT id FROM mailboxes WHERE virtual_view = 1`)
	e.m.boxes["virtual"] = int64(virt)
	d, mid := e.scheduledReply(e.now.Add(72 * time.Hour))
	e.m.link("virtual", mid, false)
	if rep := e.check(); rep.Scheduled != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if sc, ok := e.vermerk(d.ID); !ScheduledActive(sc, ok) {
		t.Fatal("not scheduled")
	}
}

// (i) Die geplante Nachricht liegt in Gesendet → sent_by_human, Grund
// scheduled; der Vermerk ist danach weg.
func TestA10ScheduledSent(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(5 * time.Minute)
	d, mid := e.scheduledReply(at)
	if rep := e.check(); rep.Scheduled != 1 {
		t.Fatalf("report = %+v", rep)
	}
	e.now = at.Add(time.Minute)
	e.m.link("sent", mid, false)
	rep := e.check()
	if rep.SentByScheduled != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusSentByHuman) || got.SentMessageID != mid {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != DetectScheduled {
		t.Fatalf("audit = %+v", r)
	}
	if keys, _ := e.st.ScheduledKeysForTest(context.Background()); len(keys) != 0 {
		t.Fatalf("vermerk left: %v", keys)
	}
}

// (i) Konto ohne Gesendet: Termin + 2 Minuten verstrichen, weiter nur in
// „Alle Nachrichten“ → sent_by_human, Grund scheduled. Vorher geplant.
func TestA10ScheduledSentNoSentFolder(t *testing.T) {
	e := newThEnv(t)
	e.m.noSent()
	at := e.now.Add(time.Hour)
	d, _ := e.scheduledReply(at)
	if rep := e.check(); rep.Scheduled != 1 || len(rep.SentDraftIDs) != 0 {
		t.Fatalf("report = %+v", rep)
	}
	e.now = at.Add(time.Minute)
	if rep := e.check(); len(rep.SentDraftIDs) != 0 {
		t.Fatalf("within 2 minutes = %+v", rep)
	}
	// Die vermerkte Nachricht ist für die Wege gesperrt: Ohne Gesendet
	// zählt „Alle Nachrichten“, doch es schließt der Übergang (i), nicht
	// der Verlauf.
	e.now = at.Add(3 * time.Minute)
	if rep := e.check(); rep.SentByScheduled != 1 || rep.SentByThread != 0 || rep.SentByFallback != 0 {
		t.Fatalf("after the time = %+v", rep)
	}
	if e.status(d) != string(StatusSentByHuman) {
		t.Fatalf("status = %s", e.status(d))
	}
}

// (ii) Plan abgebrochen: Die Nachricht liegt wieder im Entwürfe-Ordner
// (B2). Der Entwurf ist taken_over (Audit schedule_cancelled) und bleibt es
// auch im zweiten Lauf (refreshRemote); plant der Mensch erneut, greift
// die Regel wieder.
func TestA10ScheduleCancelled(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(72 * time.Hour)
	d, mid := e.scheduledReply(at)
	if rep := e.check(); rep.Scheduled != 1 {
		t.Fatalf("report = %+v", rep)
	}
	e.m.link("drafts", mid, false)
	rep := e.check()
	if rep.ScheduleCancelled != 1 || rep.Missing != 0 {
		t.Fatalf("cancel run = %+v", rep)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteTakenOver {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	if r, ok := e.lastAudit(OpRemoteCheck); !ok || r.Reason != ReasonScheduleCancelled {
		t.Fatalf("audit = %+v", r)
	}
	for i := 0; i < 2; i++ {
		if rep := e.check(); !rep.Empty() {
			t.Fatalf("run %d = %+v", i, rep)
		}
		if got := e.draft(d.ID); got.RemoteState != RemoteTakenOver || got.Status != string(StatusDraft) {
			t.Fatalf("run %d: %s %s", i, got.Status, got.RemoteState)
		}
	}
	// Erneut geplant: dieselbe Nachricht verlässt den Entwürfe-Ordner.
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ? AND mailbox_id = ?`, mid, e.m.boxes["drafts"]); err != nil {
		t.Fatal(err)
	}
	e.check()
	if sc, ok := e.vermerk(d.ID); !ScheduledActive(sc, ok) || sc.MessageID != mid {
		t.Fatalf("vermerk = %+v %v", sc, ok)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteMissing {
		t.Fatalf("remote = %s", got.RemoteState)
	}
}

// (iii) Die geplante Nachricht ist weg (gelöscht in Gmail) → missing,
// Audit missing; danach greift „Kopie weg“ nach der Frist ab diesem Lauf.
func TestA10ScheduledGone(t *testing.T) {
	for _, how := range []string{"deleted", "trash"} {
		t.Run(how, func(t *testing.T) {
			e := newThEnv(t)
			at := e.now.Add(72 * time.Hour)
			d, mid := e.scheduledReply(at)
			e.check()
			e.now = e.now.Add(30 * time.Hour) // länger als copy_gone_after seit der ersten Prüfung
			if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, mid); err != nil {
				t.Fatal(err)
			}
			if how == "trash" {
				if err := e.st.ExecForTest(`INSERT INTO mailboxes(account_id, path, name, role, sync_phase, uidvalidity)
					VALUES (?, 'Trash', 'Trash', 'trash', 'ready', 1)`, e.acc); err != nil {
					t.Fatal(err)
				}
				tr, _ := e.st.QueryIntForTest(`SELECT id FROM mailboxes WHERE role = 'trash'`)
				e.m.boxes["trash"] = int64(tr)
				e.m.link("trash", mid, false)
			}
			rep := e.check()
			if rep.ScheduledGone != 1 || rep.CopyGone != 0 {
				t.Fatalf("gone run = %+v", rep)
			}
			if r, ok := e.lastAudit(OpRemoteCheck); !ok || r.Reason != RemoteMissing {
				t.Fatalf("audit = %+v", r)
			}
			if sc, ok := e.vermerk(d.ID); !ok || sc.State != store.ScheduledGone || ScheduledActive(sc, ok) {
				t.Fatalf("vermerk = %+v", sc)
			}
			e.now = e.now.Add(23 * time.Hour)
			if rep := e.check(); rep.CopyGone != 0 {
				t.Fatalf("before the deadline = %+v", rep)
			}
			e.now = e.now.Add(2 * time.Hour)
			if rep := e.check(); rep.CopyGone != 1 {
				t.Fatalf("after the deadline = %+v", rep)
			}
		})
	}
}

// (iii) Fenster: Nach dem Verschwinden reicht das Zeitfenster der Wege für
// diesen Entwurf bis Termin + 14 Tage (falls Gmail beim Versand eine neue
// Nachricht anlegt). Hier 34 Tage: der Verlauf schließt mit der neuen.
func TestA10ScheduledGoneWindow(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(34 * 24 * time.Hour)
	d, mid := e.scheduledReply(at)
	e.check()
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, mid); err != nil {
		t.Fatal(err)
	}
	e.now = at.Add(time.Minute)
	sent := e.send(thSent{from: a10Own, date: at})
	rep := e.check()
	if rep.SentByThread != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(d.ID); got.SentMessageID != sent {
		t.Fatalf("sent = %d", got.SentMessageID)
	}
}

// (iv) Konto mit Gesendet: Termin verstrichen, die Nachricht liegt weiter
// nur in „Alle Nachrichten“ → noch eine Stunde geplant, danach wie (iii).
func TestA10ScheduledOverdue(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(10 * time.Minute)
	d, _ := e.scheduledReply(at)
	e.check()
	e.now = at.Add(59 * time.Minute)
	if rep := e.check(); rep.ScheduledGone != 0 {
		t.Fatalf("within the hour = %+v", rep)
	}
	if sc, ok := e.vermerk(d.ID); !ScheduledActive(sc, ok) {
		t.Fatal("not scheduled within the hour")
	}
	e.now = at.Add(61 * time.Minute)
	if rep := e.check(); rep.ScheduledGone != 1 {
		t.Fatalf("after the hour = %+v", rep)
	}
	if sc, ok := e.vermerk(d.ID); !ok || sc.State != store.ScheduledGone {
		t.Fatalf("vermerk = %+v", sc)
	}
}

// Neue Mail über den Betreff; Entwurf, der vorher taken_over war; Kopie
// weg bei gespeichertem Stand pushed (greift im selben Lauf).
func TestA10ScheduledWays(t *testing.T) {
	t.Run("new mail by subject", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftTo(KindNew, sjOwn, sjTo, sjSubject, true)
		e.dropCopy(d)
		e.schedule(e.now.Add(72*time.Hour), sjSubject, sjOwn+sjAdded, sjTo, " ", "")
		if rep := e.check(); rep.Scheduled != 1 {
			t.Fatalf("report = %+v", rep)
		}
	})
	t.Run("taken over before", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		if err := e.st.ExecForTest(`UPDATE drafts SET remote_state = 'taken_over' WHERE id = ?`, d.ID); err != nil {
			t.Fatal(err)
		}
		e.dropCopy(d)
		e.schedule(e.now.Add(72*time.Hour), "Re: Abrechnung", thOwn, thAnna, "", "")
		if rep := e.check(); rep.Scheduled != 1 {
			t.Fatalf("report = %+v", rep)
		}
	})
	t.Run("stored pushed, same run", func(t *testing.T) {
		e := newThEnv(t)
		d, _ := e.scheduledReply(e.now.Add(72 * time.Hour))
		if d.RemoteState != RemotePushed {
			t.Fatalf("remote = %s", d.RemoteState)
		}
		if rep := e.check(); rep.Scheduled != 1 || rep.Missing != 0 {
			t.Fatalf("report = %+v", rep)
		}
	})
	t.Run("same message id", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		e.dropCopy(d)
		e.send(thSent{role: "all", from: a10Own, messageID: d.MessageIDHeader, text: "ganz anders", date: e.now.Add(72 * time.Hour)})
		if rep := e.check(); rep.Scheduled != 1 {
			t.Fatalf("report = %+v", rep)
		}
	})
}

// Zwei Entwürfe, eine geplante Nachricht: Rangfolge wie A07 Teil 2 (bei
// gleicher Ähnlichkeit die jüngere Fassung); die Nachricht gehört nur
// einem.
func TestA10ScheduledRanking(t *testing.T) {
	e := newThEnv(t)
	older := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(older)
	e.now = e.now.Add(time.Hour)
	younger := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(younger)
	e.schedule(e.now.Add(72*time.Hour), "Re: Abrechnung", thOwn, thAnna, "", "")
	rep := e.check()
	if rep.Scheduled != 1 || rep.ScheduledDraftIDs[0] != younger.ID {
		t.Fatalf("report = %+v", rep)
	}
	if _, ok := e.vermerk(older.ID); ok {
		t.Fatal("older draft scheduled too")
	}
}

// Nicht geplant: fremder Absender mit INTERNALDATE in der Zukunft; Konto
// ohne „Alle Nachrichten“.
func TestA10NotScheduled(t *testing.T) {
	t.Run("other sender", func(t *testing.T) {
		e := newThEnv(t)
		d := e.draftOf(KindReply, thOwn, true)
		e.dropCopy(d)
		e.schedule(e.now.Add(72*time.Hour), "Re: Abrechnung", thOwn, thAnna, "", "Anna <anna@example.org>")
		if rep := e.check(); rep.Scheduled != 0 {
			t.Fatalf("report = %+v", rep)
		}
	})
	t.Run("no All Mail", func(t *testing.T) {
		e := newThEnv(t)
		if err := e.st.ExecForTest(`UPDATE mailboxes SET role = 'archive' WHERE id = ?`, e.m.boxes["all"]); err != nil {
			t.Fatal(err)
		}
		d := e.draftOf(KindReply, thOwn, true)
		e.dropCopy(d)
		e.schedule(e.now.Add(72*time.Hour), "Re: Abrechnung", thOwn, thAnna, "", "")
		if rep := e.check(); rep.Scheduled != 0 {
			t.Fatalf("report = %+v", rep)
		}
	})
}

// Eine geplante Nachricht ohne passenden Entwurf bleibt nur an der
// Nachricht erkennbar (Teil 3: ScheduledAt); kein Entwurf wird geplant.
func TestA10ScheduledMessageWithoutDraft(t *testing.T) {
	e := newThEnv(t)
	at := e.now.Add(72 * time.Hour)
	mid := e.schedule(at, "Re: Etwas", "Nur eine Nachricht.", thAnna, " ", "")
	if rep := e.check(); rep.Scheduled != 0 {
		t.Fatalf("report = %+v", rep)
	}
	got, err := e.st.ScheduledAt(context.Background(), []int64{mid, e.orig}, e.now.Add(ScheduledLead))
	if err != nil || len(got) != 1 || !got[mid].Equal(at) {
		t.Fatalf("ScheduledAt = %v, %v", got, err)
	}
}

// Ein Agent ändert einen geplanten Entwurf → draft_scheduled (Vorrang vor
// draft_taken_over), auch Vorlage und Supersede; der Mensch kann verwerfen
// oder für gesendet erklären, der Vermerk endet in derselben Transaktion.
func TestA10ScheduledDenied(t *testing.T) {
	e := newThEnv(t)
	ctx := context.Background()
	d, _ := e.scheduledReply(e.now.Add(72 * time.Hour))
	e.check()
	if got := e.draft(d.ID); got.RemoteState != RemoteMissing {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	c := ContentOf(e.draft(d.ID))
	c.BodyMarkdown += " Noch ein Satz."
	if _, err := e.svc.Update(ctx, d.ID, c, ""); ReasonOf(err) != ReasonDraftScheduled {
		t.Fatalf("update = %v", err)
	}
	if _, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{}); ReasonOf(err) != ReasonDraftScheduled {
		t.Fatalf("request_ready = %v", err)
	}
	if err := e.svc.CheckScheduled(ctx, d.ID); ReasonOf(err) != ReasonDraftScheduled {
		t.Fatalf("check = %v", err)
	}
	e.now = e.now.Add(time.Minute)
	nw := e.draftOf(KindReply, thOwn+" Neu.", false)
	if _, err := e.svc.Supersede(ctx, SupersedeRequest{OldID: d.ID, NewID: nw.ID, KeyID: "key:agent"}); ReasonOf(err) != ReasonDraftScheduled {
		t.Fatalf("supersede = %v", err)
	}
	if _, err := e.svc.MarkSent(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.vermerk(d.ID); ok {
		t.Fatal("vermerk survives mark-sent")
	}
	if keys, _ := e.st.ScheduledKeysForTest(ctx); len(keys) != 0 {
		t.Fatalf("keys = %v", keys)
	}
}

// Der Vermerk übersteht eine Rotation des Audits (er steht in meta).
func TestA10ScheduledSurvivesAuditRotation(t *testing.T) {
	e := newThEnv(t)
	d, mid := e.scheduledReply(e.now.Add(72 * time.Hour))
	e.check()
	// Rotation wie store.RotateAudit: Sperre kurz aufheben, alles löschen.
	for _, q := range []string{`DROP TRIGGER audit_events_no_delete`, `DELETE FROM audit_events`} {
		if err := e.st.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	e.now = e.now.Add(26 * time.Hour)
	if rep := e.check(); rep.CopyGone != 0 || rep.Scheduled != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if sc, ok := e.vermerk(d.ID); !ScheduledActive(sc, ok) || sc.MessageID != mid {
		t.Fatalf("vermerk = %+v %v", sc, ok)
	}
}
