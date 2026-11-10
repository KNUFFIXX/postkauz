package drafts

import (
	"context"
	"testing"

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
