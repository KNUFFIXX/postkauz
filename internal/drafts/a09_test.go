package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A09 Teil 1: Antwort mit im Mailprogramm ergänztem Empfänger (Offene
// Frage 4). Im Weg über den Verlauf gilt (d) auch, wenn die Empfänger des
// Entwurfs eine echte, nicht leere Teilmenge der gesendeten sind.

var (
	a09A = store.Addr{Name: "Anna", Address: "anna@example.org"}
	a09B = store.Addr{Name: "Bert", Address: "bert@example.org"}
)

// a09Added liest recipients_added aus dem letzten Audit-Eintrag
// drafts.sent_detected (-1: kein Parameter).
func (e *thEnv) a09Added() int {
	e.t.Helper()
	r, ok := e.lastAudit(OpSentDetected)
	if !ok {
		e.t.Fatal("no sent_detected audit entry")
	}
	var p map[string]any
	if err := json.Unmarshal(r.ParamsRedacted, &p); err != nil {
		e.t.Fatal(err)
	}
	v, ok := p["recipients_added"]
	if !ok {
		return -1
	}
	return int(v.(float64))
}

// Entwurf an A, gesendet an A+B mit In-Reply-To = Original, Text
// unverändert, Kopie weg → sent_by_human/thread, recipients_added 1, ohne
// Adressen im Audit.
func TestA09RecipientAddedThread(t *testing.T) {
	for _, c := range []struct {
		name   string
		to, cc []store.Addr
	}{
		{"added in To", []store.Addr{a09A, a09B}, nil},
		{"added in Cc", []store.Addr{a09A}, []store.Addr{a09B}},
		{"other case", []store.Addr{{Address: "ANNA@example.org"}, a09B}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newThEnv(t)
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
			e.dropCopy(d)
			sent := e.send(thSent{text: thOwn, to: c.to, cc: c.cc})
			rep := e.check()
			if rep.SentByThread != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
				t.Fatalf("report = %+v", rep)
			}
			if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
				t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
			}
			r, _ := e.lastAudit(OpSentDetected)
			if r.Reason != DetectThread || r.ActorType != "system" {
				t.Fatalf("audit = %+v", r)
			}
			if n := e.a09Added(); n != 1 {
				t.Fatalf("recipients_added = %d", n)
			}
			if p := string(r.ParamsRedacted); strings.Contains(p, "anna") || strings.Contains(p, "bert") || strings.Contains(p, "example") {
				t.Fatalf("audit params name an address: %s", p)
			}
			ev, ok, err := e.st.DraftSentBy(context.Background(), d.ID, e.draft(d.ID).ClosedAt)
			if err != nil || !ok || ev.Reason != DetectThread {
				t.Fatalf("DraftSentBy = %+v, %v, %v", ev, ok, err)
			}
		})
	}
}

// Gleiche Empfänger (auch mit Bcc an B: Bcc steht nicht im Spiegel und
// zählt nicht) → wie bisher, kein recipients_added.
func TestA09SameRecipientsNoParam(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
	e.dropCopy(d)
	e.send(thSent{text: thOwn, to: []store.Addr{a09A}}) // gesendet an A + Bcc B
	if rep := e.check(); rep.SentByThread != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if n := e.a09Added(); n != -1 {
		t.Fatalf("recipients_added = %d, want none", n)
	}
}

// Fälle, in denen der Entwurf offen bleibt.
func TestA09RecipientsStayOpen(t *testing.T) {
	for _, c := range []struct {
		name string
		run  func(e *thEnv) store.Draft
	}{
		{"draft to A and B, sent to A", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A, a09B}, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, to: []store.Addr{a09A}})
			return d
		}},
		{"draft to A, sent to B", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, to: []store.Addr{a09B}})
			return d
		}},
		{"draft to A, sent to A and B without In-Reply-To", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thOwn, to: []store.Addr{a09A, a09B}, inReplyTo: " "})
			return d
		}},
		{"draft to A, sent to A and B, copy still there", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
			e.send(thSent{text: thOwn, to: []store.Addr{a09A, a09B}})
			return d
		}},
		{"draft to A, sent to A and B, rewritten (g)", func(e *thEnv) store.Draft {
			d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
			e.dropCopy(d)
			e.send(thSent{text: thRewritten, to: []store.Addr{a09A, a09B}})
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

// Entwurf ohne Empfänger: wie A07 Teil 1 (sent_by_human/thread), kein
// recipients_added, auch wenn die Antwort zwei Empfänger hat.
func TestA09NoRecipientsUnchanged(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, nil, "", true)
	e.dropCopy(d)
	e.send(thSent{text: thOwn, to: []store.Addr{a09A, a09B}})
	if rep := e.check(); rep.SentByThread != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	if n := e.a09Added(); n != -1 {
		t.Fatalf("recipients_added = %d, want none", n)
	}
}

// Zwei Entwürfe, eine gesendete Nachricht an A+B: der mit gleichen
// Empfängern gewinnt, auch wenn der andere (Teilmenge) jünger ist und nach
// pairBefore sonst vorn läge.
func TestA09SameRecipientsWin(t *testing.T) {
	e := newThEnv(t)
	same := e.draftTo(KindReply, thOwn, []store.Addr{a09A, a09B}, "", true)
	e.later()
	subset := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
	e.dropCopy(same)
	e.dropCopy(subset)
	e.send(thSent{text: thOwn, to: []store.Addr{a09A, a09B}})
	rep := e.check()
	if rep.SentByThread != 1 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != same.ID {
		t.Fatalf("report = %+v (same %d, subset %d)", rep, same.ID, subset.ID)
	}
	if e.status(subset) != string(StatusDraft) {
		t.Fatalf("subset draft = %s", e.status(subset))
	}
	if n := e.a09Added(); n != -1 {
		t.Fatalf("recipients_added = %d, want none", n)
	}
}

// Die Teilmenge greift vor „Kopie weg“ im selben Lauf: Kopie seit 25 h
// weg, die gesendete Antwort an A+B wird erst jetzt gespiegelt.
func TestA09SubsetBeforeCopyGone(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
	e.dropCopy(d)
	if rep := e.check(); rep.Missing != 1 {
		t.Fatalf("first run = %+v", rep)
	}
	e.now = e.now.Add(25 * time.Hour)
	e.send(thSent{text: thOwn, to: []store.Addr{a09A, a09B}, date: e.now.Add(-time.Hour)})
	rep := e.check()
	if rep.SentByThread != 1 || rep.CopyGone != 0 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.status(d); got != string(StatusSentByHuman) {
		t.Fatalf("draft = %s", got)
	}
	if n := e.a09Added(); n != 1 {
		t.Fatalf("recipients_added = %d", n)
	}
}

// 3b bleibt streng: Entwurf an A, eigene Antwort an A+B mit Ähnlichkeit
// unter 0,5 → nicht ersetzt (auch nicht im Lauf danach).
func TestA09ReplacedStaysStrict(t *testing.T) {
	e := newThEnv(t)
	d := e.draftTo(KindReply, thOwn, []store.Addr{a09A}, "", true)
	e.dropCopy(d)
	e.send(thSent{text: thRewritten, to: []store.Addr{a09A, a09B}})
	for i := 0; i < 3; i++ {
		if rep := e.check(); rep.ReplacedInThread != 0 || len(rep.SentDraftIDs) != 0 {
			t.Fatalf("run %d = %+v", i, rep)
		}
	}
	if got := e.status(d); got != string(StatusDraft) {
		t.Fatalf("draft = %s", got)
	}
}

// threadRecipientsAdded und threadRecipientsMatch einzeln.
func TestA09RecipientFunctions(t *testing.T) {
	sent := func(to ...store.Addr) store.SentMessage { return store.SentMessage{To: to} }
	key := func(a ...store.Addr) string { return RecipientKey(a) }
	for _, c := range []struct {
		name    string
		draft   string
		c       store.SentMessage
		ok      bool
		added   int
		matchOK bool
	}{
		{"same", key(a09A), sent(a09A), true, 0, true},
		{"one added", key(a09A), sent(a09A, a09B), true, 1, false},
		{"two added", key(a09A), sent(a09A, a09B, store.Addr{Address: "carla@example.org"}), true, 2, false},
		{"one removed", key(a09A, a09B), sent(a09A), false, 0, false},
		{"swapped", key(a09A), sent(a09B), false, 0, false},
		{"draft empty", "", sent(a09A, a09B), true, 0, true},
		{"draft empty, sent empty", "", sent(), false, 0, false},
		{"duplicate in sent", key(a09A), sent(a09A, a09A), true, 0, true},
	} {
		ok, n := threadRecipientsAdded(c.draft, c.c)
		if ok != c.ok || n != c.added || threadRecipientsMatch(c.draft, c.c) != c.matchOK {
			t.Errorf("%s: added = %v %d, match = %v", c.name, ok, n, threadRecipientsMatch(c.draft, c.c))
		}
	}
}
