package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// A10 Teil 3: Marken [draft] und [scheduled …] in search und thread, Zeile
// scheduled: in show; --json trägt roles und scheduled_for.
func TestA10CLIStateMarks(t *testing.T) {
	var b bytes.Buffer
	o := ops.SearchOutput{Order: "date", Total: 3, Hits: []ops.SearchHitOutput{
		{ID: 1, Subject: "Entwurf", Roles: []string{"all", "drafts"}},
		{ID: 2, Subject: "Geplant", Roles: []string{"all"}, ScheduledFor: "2026-11-21T09:00:00Z"},
		{ID: 3, Subject: "Gesendet", Roles: []string{"all", "sent"}},
	}}
	if err := renderSearch(&b, o); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	lines := strings.Split(s, "\n")
	if !strings.Contains(lines[1], "Entwurf [draft]") || !strings.Contains(lines[2], "Geplant [scheduled 2026-11-") ||
		strings.Contains(lines[3], "[") {
		t.Fatalf("search:\n%s", s)
	}
	j, _ := json.Marshal(o.Hits[1])
	if !strings.Contains(string(j), `"roles":["all"]`) || !strings.Contains(string(j), `"scheduled_for":"2026-11-21T09:00:00Z"`) {
		t.Fatalf("json = %s", j)
	}
	j, _ = json.Marshal(ops.SearchHitOutput{Roles: []string{}})
	if !strings.Contains(string(j), `"roles":[]`) || strings.Contains(string(j), "scheduled_for") {
		t.Fatalf("json without roles = %s", j)
	}
	b.Reset()
	if err := renderThread(&b, ops.ThreadOutput{ID: 4, Messages: []ops.ThreadMessageOutput{
		{ID: 2, Subject: "Geplant", Roles: []string{"all"}, ScheduledFor: "2026-11-21T09:00:00Z"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Geplant [scheduled 2026-11-") {
		t.Fatalf("thread:\n%s", b.String())
	}
	b.Reset()
	if err := renderShow(&b, ops.MessageOutput{ID: 2, ScheduledFor: "2026-11-21T09:00:00Z", To: json.RawMessage("[]"),
		Cc: json.RawMessage("[]")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "2026-11-21T09:00:00Z (scheduled in the mail client; not sent yet)") || !strings.Contains(b.String(), "scheduled:") {
		t.Fatalf("show:\n%s", b.String())
	}
}

// A10 Teil 2/3 (c): draft show und draft list eines geplanten Entwurfs.
func TestA10CLIDraftScheduled(t *testing.T) {
	var b bytes.Buffer
	d := ops.DraftDetail{DraftID: 6, Kind: "reply", Account: "demo", Status: "ready", Remote: drafts.RemoteScheduled,
		RemoteNote: ops.ScheduledNote(mustTime(t, "2026-11-21T09:00:00Z")), ScheduledFor: "2026-11-21T09:00:00Z", ScheduledMessageID: 42}
	if err := renderDraftShow(&b, d); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if !strings.Contains(s, "remote:      scheduled (scheduled in the mail client for 2026-11-21T09:00:00Z; not sent yet)") ||
		!strings.Contains(s, "scheduled:   2026-11-21T09:00:00Z (message 42; not sent yet)") {
		t.Fatalf("draft show:\n%s", s)
	}
	b.Reset()
	if err := renderDraftList(&b, ops.DraftsListOutput{Drafts: []ops.DraftSummary{{DraftID: 6, Account: "demo", Kind: "reply",
		Status: "ready", Remote: "scheduled", ScheduledFor: "2026-11-21T09:00:00Z"}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "draft 6: scheduled in the mail client for 2026-11-21T09:00:00Z; not sent yet") ||
		!strings.Contains(b.String(), " scheduled ") {
		t.Fatalf("draft list:\n%s", b.String())
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}
