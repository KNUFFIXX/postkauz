package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A11 Teil 2: drafts.discard (discard.go).

func (e *cuEnv) discard(id int64) (DiscardResult, error) {
	return e.svc.Discard(context.Background(), DiscardRequest{ID: id, KeyID: cuKey})
}

// Eigener abgelegter Entwurf: abandoned (discarded), Aktion draft_discard
// mit der Kopie, nach der Ausführung im Papierkorb; nicht mehr unter den
// offenen Entwürfen; Audit ohne Inhalt.
func TestA11DiscardPushed(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	u := e.push(d, "eins", true)
	res, err := e.discard(d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.Draft.Status != string(StatusAbandoned) || res.Copies != 1 || res.ActionID == 0 || res.Trash != "Trash" {
		t.Fatalf("result = %+v", res)
	}
	a, _ := e.st.Action(ctx, res.ActionID)
	if a.Params.Source != SourceDraftDiscard || a.Params.DraftID != d.ID || a.RequestedBy != cuKey || int64(a.Targets[0].UID) != u {
		t.Fatalf("action = %+v", a)
	}
	ev, ok, err := e.st.DraftClosedBy(ctx, d.ID, e.draft(d.ID).ClosedAt)
	if err != nil || !ok || ev.Reason != ReasonDiscarded || ev.Operation != OpDiscard || ev.KeyID != cuKey {
		t.Fatalf("closed by = %+v %v %v", ev, ok, err)
	}
	if strings.Contains(string(ev.Params), "Probe") || strings.Contains(string(ev.Params), "anna") {
		t.Fatalf("audit names content: %s", ev.Params)
	}
	var p map[string]any
	_ = json.Unmarshal(ev.Params, &p)
	if p["copies"] != float64(1) || p["draft_id"] != float64(d.ID) {
		t.Fatalf("params = %v", p)
	}
	open, _ := e.st.ListDrafts(ctx, store.DraftFilter{AccountID: e.acc, Status: []string{string(StatusDraft)}})
	if len(open) != 0 {
		t.Fatalf("open drafts = %d", len(open))
	}
	e.run()
	if e.inDrafts(u) || len(e.remote.moves) != 1 {
		t.Fatalf("copy not moved: %v", e.remote.moves)
	}
	// Zweiter Aufruf: abgelehnt wie bei supersede (draft_closed).
	if _, err := e.discard(d.ID); ReasonOf(err) != ReasonDraftClosed {
		t.Fatalf("second call = %v", err)
	}
}

// Nie abgelegt: geschlossen, keine Aktion, kein späterer Push.
func TestA11DiscardNeverPushed(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	res, err := e.discard(d.ID)
	if err != nil || res.Copies != 0 || res.ActionID != 0 || res.Draft.Status != string(StatusAbandoned) {
		t.Fatalf("result = %+v %v", res, err)
	}
	if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
		t.Fatalf("actions = %d", n)
	}
	q, err := e.st.DraftPushQueue(ctx, e.acc, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range q {
		if it.ID == d.ID {
			t.Fatal("closed draft still in the push queue")
		}
	}
	if Pending(e.draft(d.ID)) {
		t.Fatal("closed draft pending")
	}
}

// remote missing: geschlossen, keine Aktion. Abgelegt, die UID war
// gespiegelt und ist es nicht mehr: copy_missing, der Entwurf bleibt offen
// und ein späterer Versand wird noch erkannt.
func TestA11DiscardMissingAndGone(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		e := newCuEnv(t)
		d := e.newDraft("Probe")
		e.push(d, "eins", true)
		e.exec(`UPDATE drafts SET remote_state = 'missing' WHERE id = ?`, d.ID)
		res, err := e.discard(d.ID)
		if err != nil || res.ActionID != 0 || res.Draft.Status != string(StatusAbandoned) {
			t.Fatalf("result = %+v %v", res, err)
		}
	})
	t.Run("gone from the mirror", func(t *testing.T) {
		e := newCuEnv(t)
		d := e.newDraft("Probe")
		u := e.push(d, "eins", true)
		e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u)
		if _, err := e.discard(d.ID); ReasonOf(err) != ReasonCopyMissing || !strings.Contains(err.Error(), "after the next sync") {
			t.Fatalf("err = %v", err)
		}
		if got := e.draft(d.ID); got.Status != string(StatusDraft) {
			t.Fatalf("status = %s", got.Status)
		}
		// Der Mensch hat gesendet: Die Versanderkennung schließt ihn noch.
		e.m.put("sent", sentMsg{messageID: d.MessageIDHeader, subject: d.Subject, from: "demo@example.org", text: "eins (gesendet)", date: e.now})
		e.check()
		if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) {
			t.Fatalf("after send: %s", got.Status)
		}
	})
}

// Ablehnungen, je der eigene Grund; nichts ändert sich.
func TestA11DiscardDenied(t *testing.T) {
	for _, c := range []struct {
		name, want string
		human      bool
		prep       func(e *cuEnv, d *store.Draft)
	}{
		{"other key", ReasonNotOwnDraft, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE drafts SET created_by = 'key:assi', updated_by = 'key:assi' WHERE id = ?`, d.ID)
		}},
		{"changed by another key", ReasonChangedByOther, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE drafts SET updated_by = 'cli' WHERE id = ?`, d.ID)
		}},
		{"ready (agent)", ReasonApprovedByHuman, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE drafts SET status = 'ready' WHERE id = ?`, d.ID)
		}},
		{"ready (human)", ReasonApprovedByHuman, true, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE drafts SET status = 'ready' WHERE id = ?`, d.ID)
		}},
		{"scheduled", ReasonDraftScheduled, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`INSERT INTO meta(key, value) VALUES (?, ?)`, "scheduled/"+itoa(d.ID),
				`{"message_id":1,"scheduled_for":"2026-12-01T00:00:00Z","state":"scheduled"}`)
		}},
		{"taken over", ReasonCopyChanged, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE drafts SET remote_state = 'taken_over' WHERE id = ?`, d.ID)
		}},
		{"copy changed", ReasonCopyChanged, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`UPDATE messages SET content_hash = 'anders' WHERE message_id_header = ?`, d.MessageIDHeader)
		}},
		{"not mirrored", ReasonCopyNotMirrored, false, func(e *cuEnv, d *store.Draft) {
			e.exec(`DELETE FROM message_mailboxes WHERE mailbox_id = ?`, e.m.boxes["drafts"])
			e.exec(`UPDATE mailboxes SET uidnext = 1 WHERE id = ?`, e.m.boxes["drafts"])
		}},
		{"changed, in the settle time with an earlier copy", ReasonPushInProgress, false, func(e *cuEnv, d *store.Draft) {
			*d = e.update(*d, "Probe neu")
		}},
		{"push rejected", ReasonPushInProgress, false, func(e *cuEnv, d *store.Draft) {
			*d = e.update(*d, "Probe neu")
			e.exec(`UPDATE drafts SET push_note = 'push_rejected' WHERE id = ?`, d.ID)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newCuEnv(t)
			d := e.newDraft("Probe")
			e.push(d, "eins", true)
			d = e.draft(d.ID)
			c.prep(e, &d)
			before := e.draft(d.ID)
			_, err := e.svc.Discard(context.Background(), DiscardRequest{ID: d.ID, KeyID: cuKey, Human: c.human})
			if ReasonOf(err) != c.want {
				t.Fatalf("err = %v, want %s", err, c.want)
			}
			if c.name == "push rejected" && !strings.Contains(err.Error(), "refused") {
				t.Fatalf("push rejected text: %v", err)
			}
			if got := e.draft(d.ID); got.Status != before.Status || !got.ClosedAt.IsZero() {
				t.Fatalf("draft changed: %s", got.Status)
			}
			if n := e.count(`SELECT count(*) FROM actions`); n != 0 {
				t.Fatalf("actions = %d", n)
			}
		})
	}
}

// Trockenlauf ändert nichts und nennt die Zahl der Kopien; der Mensch ohne
// Schlüssel darf jeden Entwurf.
func TestA11DiscardDryRunAndHuman(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	e.push(d, "eins", true)
	dry := e.svc.As(Who{Actor: "agent", KeyID: cuKey, Scope: "mail:draft", Operation: OpDiscard}, true)
	res, err := dry.Discard(ctx, DiscardRequest{ID: d.ID, KeyID: cuKey})
	if err != nil || res.Copies != 1 || res.ActionID != 0 {
		t.Fatalf("dry run = %+v %v", res, err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) || e.count(`SELECT count(*) FROM actions`) != 0 {
		t.Fatalf("dry run changed something: %s", got.Status)
	}
	e.exec(`UPDATE drafts SET created_by = 'key:assi', updated_by = 'key:assi' WHERE id = ?`, d.ID)
	res, err = e.svc.Discard(ctx, DiscardRequest{ID: d.ID, KeyID: "cli", Human: true})
	if err != nil || res.Copies != 1 || res.ActionID == 0 {
		t.Fatalf("human = %+v %v", res, err)
	}
	if a, _ := e.st.Action(ctx, res.ActionID); a.RequestedBy != "cli" {
		t.Fatalf("requested_by = %s", a.RequestedBy)
	}
}

// Beim Schließen wird der Vermerk „geplant“ (hier abgebrochen) in derselben
// Transaktion gelöscht, der Zähler je Kopie nicht; endet die Aktion stale,
// räumt der Nachlauf (ii) auf.
func TestA11DiscardCountersAndRetry(t *testing.T) {
	e := newCuEnv(t)
	ctx := context.Background()
	d := e.newDraft("Probe")
	u := e.push(d, "eins", true)
	e.exec(`INSERT INTO meta(key, value) VALUES (?, ?)`, "scheduled/"+itoa(d.ID),
		`{"message_id":1,"scheduled_for":"2026-12-01T00:00:00Z","state":"gone"}`)
	if _, err := e.discard(d.ID); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key = ?`, "scheduled/"+itoa(d.ID)); n != 0 {
		t.Fatalf("scheduled note left: %d", n)
	}
	if n := e.count(`SELECT count(*) FROM meta WHERE key LIKE 'draft_copy_cleanup/%'`); n != 1 {
		t.Fatalf("counter = %d", n)
	}
	e.exec(`UPDATE message_mailboxes SET content_loaded = 0 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u)
	e.run() // stale
	if !e.inDrafts(u) {
		t.Fatal("moved by a stale action")
	}
	e.exec(`UPDATE message_mailboxes SET content_loaded = 1 WHERE mailbox_id = ? AND uid = ?`, e.m.boxes["drafts"], u)
	e.now = e.now.Add(time.Minute)
	rep, err := e.svc.RetryCleanup(ctx, e.acc)
	if err != nil || rep.Actions != 1 {
		t.Fatalf("retry = %+v %v", rep, err)
	}
	acts := e.actionsOf(d.ID)
	if len(acts) != 2 || acts[0].Params.Source != SourceDraftDiscard {
		t.Fatalf("actions = %+v", acts)
	}
	e.run()
	if e.inDrafts(u) {
		t.Fatal("copy still in the Drafts folder")
	}
}
