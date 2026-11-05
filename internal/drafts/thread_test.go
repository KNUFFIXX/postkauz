package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A05 Teil 2: dritter Weg über den Verlauf (threadmatch.go).

const (
	thOrigID = "orig-1@example.com"
	// thOwn: eigener Text mit 16 verschiedenen Wörtern.
	thOwn   = "Hallo Anna,\n\nvielen Dank für die Unterlagen. Wir prüfen die Abrechnung und melden uns bis Freitag.\n\nViele Grüße"
	thAdded = "Der Hausmeister kommt am Montag vorbei."
)

var thAnna = []store.Addr{{Name: "Anna", Address: "anna@example.org"}}

// thEnv ist ein Spiegel mit Ausgangsnachricht für die Tests des Wegs über
// den Verlauf.
type thEnv struct {
	*env
	m    *mirror
	orig int64
}

func newThEnv(t *testing.T) *thEnv {
	e := newEnv(t)
	m := newMirror(e)
	orig := m.put("inbox", sentMsg{messageID: thOrigID, subject: "Abrechnung", from: "Anna <anna@example.org>",
		to: []store.Addr{{Address: "demo@example.org"}}, date: e.now.Add(-time.Hour), text: "Bitte prüfen Sie die Abrechnung."})
	return &thEnv{env: e, m: m, orig: orig}
}

// draftOf legt einen Entwurf an (kind, Text own, Empfänger Anna) und legt
// ihn im Entwürfe-Ordner ab (push=false: nie abgelegt).
func (e *thEnv) draftOf(kind Kind, own string, push bool) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	in := NewDraft{AccountID: e.acc, Kind: kind, CreatedBy: "key:agent", Content: Content{To: thAnna, Subject: "Re: Abrechnung",
		BodyMarkdown: own, BodyText: own + "\n\nAm 22.10.2026 08:00 schrieb Anna <anna@example.org>:\n> Bitte prüfen Sie die Abrechnung.\n"}}
	if kind != KindNew {
		in.InReplyToMsg = e.orig
	}
	r, err := e.svc.Create(ctx, in)
	if err != nil {
		e.t.Fatal(err)
	}
	if !push {
		return r.Draft
	}
	d := r.Draft
	id := e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, draftFlag: true, date: e.now, text: "kopie " + own})
	var uid int64
	var hash string
	if err := e.st.DBForTest().QueryRow(`SELECT mm.uid, m.content_hash FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
		WHERE m.id = ?`, id).Scan(&uid, &hash); err != nil {
		e.t.Fatal(err)
	}
	if _, _, err := e.svc.Pushed(ctx, d.ID, Fingerprint(e.draft(d.ID)), PushedCopy{MailboxID: e.m.boxes["drafts"], UIDValidity: 1,
		UID: uid, ContentHash: hash}); err != nil {
		e.t.Fatal(err)
	}
	return e.draft(d.ID)
}

// dropCopy entfernt die Kopie aus dem Entwürfe-Ordner (das Mailprogramm
// hat gesendet).
func (e *thEnv) dropCopy(d store.Draft) {
	e.t.Helper()
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], d.RemoteUID); err != nil {
		e.t.Fatal(err)
	}
}

// sent beschreibt die gesendete Nachricht; Nullwerte = passend.
type thSent struct {
	role, inReplyTo, from, text, messageID string
	to                                     []store.Addr
	date                                   time.Time
	draftFlag                              bool
}

func (e *thEnv) send(s thSent) int64 {
	e.t.Helper()
	if s.role == "" {
		s.role = "sent"
	}
	if s.inReplyTo == "" {
		s.inReplyTo = "<" + thOrigID + ">"
	}
	if s.from == "" {
		s.from = "demo@example.org"
	}
	if s.text == "" {
		s.text = strings.Replace(thOwn, "Freitag.", "Freitag. "+thAdded, 1)
	}
	if s.messageID == "" {
		s.messageID = "gmail-" + time.Now().Format("150405.000000000") + "@example.org"
	}
	if s.to == nil {
		s.to = thAnna
	}
	if s.date.IsZero() {
		s.date = e.now.Add(10 * time.Minute)
	}
	box := e.m.boxes[s.role]
	e.m.uid[box]++
	irt := strings.Trim(s.inReplyTo, "<> ")
	nm := store.NewMessage{Link: store.LinkFlags{UID: e.m.uid[box], Seen: true, Draft: s.draftFlag}, MessageID: s.messageID,
		Subject: "Re: Abrechnung (geändert)", From: s.from, To: addrs(s.to), Cc: addrs(nil), DateHeader: s.date, InternalDate: s.date,
		Size: int64(200 + len(s.text) + int(e.m.uid[box])), TextPlain: s.text, TextUnquoted: s.text, ContentHash: s.messageID + s.text,
		InReplyTo: irt, References: []string{irt}, CleanVersion: 1, Links: json.RawMessage("[]")}
	if _, err := e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: e.acc, MailboxID: box, New: []store.NewMessage{nm},
		State: &store.MailboxState{UIDValidity: 1, UIDNext: e.m.uid[box] + 1, Phase: store.PhaseReady}}); err != nil {
		e.t.Fatal(err)
	}
	n, err := e.st.QueryIntForTest(`SELECT max(id) FROM messages`)
	if err != nil {
		e.t.Fatal(err)
	}
	return int64(n)
}

func (e *thEnv) status(d store.Draft) string { return e.draft(d.ID).Status }

// Satz eingefügt → thread; Audit mit Grund thread, ohne Inhalt; der
// Betreff zählt nicht (die gesendete Nachricht hat einen anderen).
func TestThreadSentenceAdded(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	sent := e.send(thSent{})
	rep := e.check()
	if rep.SentByThread != 1 || rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	r, ok := e.lastAudit(OpSentDetected)
	if !ok || r.Reason != DetectThread || r.ActorType != "system" || strings.Contains(string(r.ParamsRedacted), "Hausmeister") ||
		strings.Contains(string(r.ParamsRedacted), "anna") {
		t.Fatalf("audit = %+v", r)
	}
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("second run = %+v", rep)
	}
}

// Jede Bedingung einzeln verletzt → kein Treffer; der Entwurf bleibt offen.
func TestThreadConditions(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(e *thEnv) store.Draft
	}{
		{"(a) kind new", func(e *thEnv) store.Draft {
			d := e.draftOf(KindNew, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{})
			return d
		}},
		{"(a) original deleted", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{})
			if err := e.st.ExecForTest(`DELETE FROM messages WHERE id = ?`, e.orig); err != nil {
				e.t.Fatal(err)
			}
			if got := e.draft(d.ID); got.InReplyToMsg != 0 {
				e.t.Fatalf("in_reply_to_msg = %d", got.InReplyToMsg)
			}
			return d
		}},
		{"(b) only in the inbox", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{role: "inbox"})
			return d
		}},
		{"(b) flagged \\Draft", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{draftFlag: true})
			return d
		}},
		{"(b) in the Drafts folder", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			id := e.send(thSent{})
			e.m.link("drafts", id, false)
			return d
		}},
		{"(b) All Mail, other sender", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{role: "all", from: "Anna <anna@example.org>"})
			return d
		}},
		// Nachtrag 2, F8: Byte für Byte eine Kopie, die PostKauz abgelegt
		// hat (Inhalts-Hash), zählt nie, auch nicht in Gesendet.
		{"(b) copy put by PostKauz (content hash)", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			id := e.send(thSent{})
			if err := e.st.ExecForTest(`UPDATE messages SET content_hash = ? WHERE id = ?`, d.PushedContentHash, id); err != nil {
				e.t.Fatal(err)
			}
			return d
		}},
		{"(c) other In-Reply-To", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{inReplyTo: "<other@example.com>"})
			return d
		}},
		{"(d) other recipient", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{to: []store.Addr{{Address: "bob@example.org"}}})
			return d
		}},
		{"(e) after the window", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{date: e.now.Add(FallbackAfter + time.Minute)})
			return d
		}},
		{"(e) before the window", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{date: e.now.Add(-FallbackBefore - time.Minute)})
			return d
		}},
		{"(f) copy still in the Drafts folder", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.send(thSent{})
			return d
		}},
		{"(f) never put", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, false)
			e.send(thSent{})
			return d
		}},
		{"(f) edited copy in the Drafts folder", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: "bearbeitet", draftFlag: true, date: e.now, text: "neu"})
			e.send(thSent{})
			return d
		}},
		{"(g) heavily rewritten", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, thOwn, true)
			e.dropCopy(d)
			e.send(thSent{text: "Hallo Anna,\n\ndie Abrechnung stimmt leider nicht, bitte schicken Sie uns die Belege von Januar bis März noch einmal.\n\nViele Grüße"})
			return d
		}},
		{"(g) own text below 8 words", func(e *thEnv) store.Draft {
			d := e.draftOf(KindReply, "Vielen Dank, erledigt.", true)
			e.dropCopy(d)
			e.send(thSent{text: "Vielen Dank!"})
			return d
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := c.run(e)
			rep := e.check()
			if strings.HasPrefix(c.name, "(g)") {
				// Seit A06 Nachbesserung 1 ersetzt erst der Lauf nach dem,
				// der das Fehlen der Kopie feststellt.
				if rep.ReplacedInThread != 0 || e.status(d) != string(StatusDraft) {
					t.Fatalf("first run = %+v", rep)
				}
				rep = e.check()
			}
			if rep.SentByThread+rep.SentByFallback+rep.SentByMessageID != 0 {
				t.Fatalf("report = %+v", rep)
			}
			// Seit A06 Teil 4: Sind (a) bis (f) erfüllt und nur (g) nicht,
			// hat eine eigene Antwort den Entwurf ersetzt (abandoned, nie
			// sent_by_human); sonst bleibt er offen.
			want := string(StatusDraft)
			if strings.HasPrefix(c.name, "(g)") {
				want = string(StatusAbandoned)
				if rep.ReplacedInThread != 1 {
					t.Fatalf("report = %+v", rep)
				}
			} else if rep.ReplacedInThread != 0 {
				t.Fatalf("report = %+v", rep)
			}
			if got := e.status(d); got != want {
				t.Fatalf("draft = %s, want %s", got, want)
			}
		})
	}
}

// Wie (g) stark umgeschrieben: Der Wert liegt wirklich unter 0,5, und der
// Satz-eingefügt-Fall wirklich zwischen 0,5 und 0,9 (die Tests prüfen,
// was sie sollen).
func TestThreadScores(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, false)
	fd := NewFallbackDraft(d, d.BodyMarkdown, "")
	added := fd.ScoreSent(NewSentWords(strings.Replace(thOwn, "Freitag.", "Freitag. "+thAdded, 1)))
	if added < ThreadMinJaccard || added >= FallbackMinJaccard {
		t.Fatalf("sentence added scores %.2f", added)
	}
	if len(fd.Words) < ThreadMinWords {
		t.Fatalf("own text has %d words", len(fd.Words))
	}
}

// remote_state taken_over (Gmail speichert beim Bearbeiten, sync --watch
// sieht die neue Fassung), danach gesendet: Kopie weg → thread.
func TestThreadAfterTakenOver(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.dropCopy(d)
	edited := e.m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: "bearbeitet", draftFlag: true, date: e.now, text: "neu"})
	if rep := e.check(); rep.TakenOver != 1 {
		t.Fatalf("taken over = %+v", rep)
	}
	if got := e.draft(d.ID); got.RemoteState != RemoteTakenOver {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ?`, edited); err != nil {
		t.Fatal(err)
	}
	e.send(thSent{})
	if rep := e.check(); rep.SentByThread != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

// Die gesendete Nachricht ist schon gespiegelt (und geprüft), die Kopie
// verschwindet erst später: thread im späteren Lauf (keine Marke).
func TestThreadSentBeforeCopyGone(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true)
	e.send(thSent{})
	if rep := e.check(); rep.SentByThread != 0 {
		t.Fatalf("first run = %+v", rep)
	}
	e.dropCopy(d)
	if rep := e.check(); rep.SentByThread != 1 {
		t.Fatalf("later run = %+v", rep)
	}
}

// Weiterleitung mit geändertem Text (seit rc2 mit In-Reply-To) → thread.
func TestThreadForward(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindForward, thOwn, true)
	e.dropCopy(d)
	e.send(thSent{text: strings.Replace(thOwn, "Freitag.", "Freitag. "+thAdded, 1) +
		"\n\n---------- Forwarded message ----------\nFrom: Anna <anna@example.org>\n\nBitte prüfen Sie die Abrechnung."})
	if rep := e.check(); rep.SentByThread != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

// Eine gesendete Nachricht schließt höchstens einen Entwurf: die höchste
// Ähnlichkeit gewinnt.
func TestThreadOneDraftPerMessage(t *testing.T) {
	e := newThEnv(t)
	near := e.draftOf(KindReply, thOwn, true)
	far := e.draftOf(KindReply, strings.Replace(thOwn, "melden uns bis Freitag", "rufen nächste Woche wegen der Belege an", 1), true)
	e.dropCopy(near)
	e.dropCopy(far)
	sent := e.send(thSent{})
	rep := e.check()
	if rep.SentByThread != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(near.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("near = %s", got.Status)
	}
	// Seit A06 Teil 4: Die Antwort ersetzt die zweite Fassung (abandoned,
	// ohne sent_message_id), seit Nachbesserung 1 im Lauf danach.
	if got := e.status(far); got != string(StatusDraft) {
		t.Fatalf("far in the first run = %s", got)
	}
	rep = e.check()
	if got := e.draft(far.ID); got.Status != string(StatusAbandoned) || got.SentMessageID != 0 || rep.ReplacedInThread != 1 {
		t.Fatalf("far = %s, sent %d, report %+v", got.Status, got.SentMessageID, rep)
	}
}

// Änderung während der Prüfung und neue Kopie: SentDetected mit Grund
// thread schließt nur, solange die Kopie fehlt (in der Transaktion geprüft).
func TestThreadRechecksCopyOnClose(t *testing.T) {
	e := newThEnv(t)
	d := e.draftOf(KindReply, thOwn, true) // Kopie liegt noch da
	sent := e.send(thSent{})
	_, err := e.svc.SentDetected(context.Background(), d.ID, sent, DetectThread, Fingerprint(e.draft(d.ID)))
	if ReasonOf(err) != ReasonDraftChanged {
		t.Fatalf("close with the copy present = %v", err)
	}
	if got := e.status(d); got != string(StatusDraft) {
		t.Fatalf("draft = %s", got)
	}
}
