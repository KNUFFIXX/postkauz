package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.6, Entscheid 43: Ein geänderter Entwurf (er hat schon eine Kopie im
// Entwürfe-Ordner) wird erst abgelegt, wenn er 30 s unverändert ist,
// gemessen an updated_at mit der Uhr des Pushs. Die erste Ablage eines
// neuen Entwurfs wartet nicht.
func TestChangedDraftWaitsForSettle(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	id := e.newDraftFor(t, "Ruhe")
	setUpdated := func(at time.Time) {
		t.Helper()
		if err := e.st.ExecForTest(`UPDATE drafts SET updated_at = ? WHERE id = ?`, at.UnixMilli(), id); err != nil {
			t.Fatal(err)
		}
	}
	// Neu: sofort, auch wenn die Uhr des Pushs genau auf updated_at steht.
	setUpdated(e.now)
	rep := e.push(t)
	if rep.Waiting != 0 || !containsID(rep.DraftIDs, id) {
		t.Fatalf("first push = %+v", rep)
	}

	// Geändert: Die neue Kopie wartet 30 s ab der Änderung.
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "body_markdown": "Hallo neu"})
	d := e.draft(t, id)
	if d.RemoteState != drafts.RemotePending || d.PushedContentHash == "" {
		t.Fatalf("after update = %+v", d)
	}
	e.mirror(t, []store.NewMessage{mirrored(d)}, nil, uint32(d.RemoteUID)+1)
	changed := e.now
	setUpdated(changed)
	for _, after := range []time.Duration{0, 10 * time.Second, DefaultChangeSettle - time.Millisecond} {
		e.now = changed.Add(after)
		if n, _, err := e.pusher.Pending(ctx, e.acct, false); err != nil || n != 0 {
			t.Fatalf("%s after the change: pending = %d, %v", after, n, err)
		}
		if w, err := e.pusher.Waiting(ctx, e.acct); err != nil || w != 1 {
			t.Fatalf("%s after the change: waiting = %d, %v", after, w, err)
		}
		if rep := e.push(t); rep.Pushed != 0 || rep.Waiting != 1 {
			t.Fatalf("%s after the change: push = %+v", after, rep)
		}
		if d := e.draft(t, id); d.RemoteState != drafts.RemotePending {
			t.Fatalf("remote = %s", d.RemoteState)
		}
	}
	// Genau 30 s: fällig.
	e.now = changed.Add(DefaultChangeSettle)
	if n, withCopy, err := e.pusher.Pending(ctx, e.acct, false); err != nil || n != 1 || withCopy != 1 {
		t.Fatalf("pending after 30 s = %d (%d with copy), %v", n, withCopy, err)
	}
	rep = e.push(t)
	if rep.Pushed != 1 || rep.Waiting != 0 || !containsID(rep.DraftIDs, id) {
		t.Fatalf("push after 30 s = %+v", rep)
	}
	if d := e.draft(t, id); d.RemoteState != drafts.RemotePushed || len(d.RemotePrevious) != 1 {
		t.Fatalf("after push = %+v", d)
	}
	// Der Hinweis in draft show nennt die Wartezeit, nicht mehr „within 10 s".
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "body_markdown": "Hallo noch einmal"})
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if !strings.Contains(got.RemoteNote, "unchanged for 30 s") || strings.Contains(got.RemoteNote, "10 s") {
		t.Fatalf("remote note = %q", got.RemoteNote)
	}
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// Wartende zählen im Dauerbetrieb nicht zusammen (derselbe Entwurf wartet
// über mehrere Prüfungen).
func TestDraftPushReportAddKeepsLatestWaiting(t *testing.T) {
	var r DraftPushReport
	r.Add(DraftPushReport{Waiting: 1})
	r.Add(DraftPushReport{Waiting: 1, Pushed: 2})
	r.Add(DraftPushReport{Pushed: 1})
	if r.Waiting != 0 || r.Pushed != 3 {
		t.Fatalf("report = %+v", r)
	}
}
