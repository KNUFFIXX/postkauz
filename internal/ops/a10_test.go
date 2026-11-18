package ops

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
)

// A10 Teil 2/3 (c): Ein geplanter Entwurf (Vermerk in meta) zeigt in
// drafts.get und drafts.list remote "scheduled" mit scheduled_for, in
// drafts.get dazu scheduled_message_id und remote_note; Änderung, Vorlage
// und Supersede eines Agenten sind draft_scheduled, auch drafts.update des
// Menschen; drafts.abandon des Menschen bleibt möglich.
func TestA10ScheduledDraftOutputAndDenials(t *testing.T) {
	e := newDraftEnv(t, true)
	id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "x",
		"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
	for _, q := range []string{
		fmt.Sprintf(`UPDATE drafts SET remote_state = 'missing' WHERE id = %d`, id),
		fmt.Sprintf(`INSERT INTO meta(key, value) VALUES ('scheduled/%d', '{"message_id":5,"scheduled_for":"2026-11-25T10:00:00Z","state":"scheduled"}')`, id),
	} {
		if err := e.st.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if got.Remote != drafts.RemoteScheduled || got.ScheduledFor != "2026-11-25T10:00:00Z" || got.ScheduledMessageID != 5 ||
		got.RemoteNote != "scheduled in the mail client for 2026-11-25T10:00:00Z; not sent yet" || got.CopyMissingSince != "" {
		t.Fatalf("get = remote %q, for %q, msg %d, note %q, missing since %q", got.Remote, got.ScheduledFor, got.ScheduledMessageID,
			got.RemoteNote, got.CopyMissingSince)
	}
	list := e.must(t, assistant, "drafts.list", map[string]any{}).(DraftsListOutput)
	for _, d := range list.Drafts {
		if want := d.DraftID == id; (d.Remote == drafts.RemoteScheduled) != want || (d.ScheduledFor == "2026-11-25T10:00:00Z") != want {
			t.Fatalf("list = %+v", d)
		}
	}
	for _, c := range []struct {
		op  string
		in  map[string]any
		key string
	}{
		{"drafts.update", map[string]any{"draft_id": id, "body_markdown": "Neu"}, "agent"},
		{"drafts.update", map[string]any{"draft_id": id, "body_markdown": "Neu"}, "human"},
		{"drafts.request_ready", map[string]any{"draft_id": id}, "agent"},
	} {
		k := assistant
		if c.key == "human" {
			k = humanCLI
		}
		_, err := e.do(t, k, c.op, c.in, false)
		var de *DeniedError
		if !errors.As(err, &de) || de.Reason != drafts.ReasonDraftScheduled || !strings.Contains(de.Msg, "scheduled in the mail client") {
			t.Fatalf("%s (%s) = %v", c.op, c.key, err)
		}
	}
	if _, err := e.do(t, humanCLI, "drafts.abandon", map[string]any{"draft_id": id}, false); err != nil {
		t.Fatalf("abandon = %v", err)
	}
	if keys, _ := e.st.ScheduledKeysForTest(t.Context()); len(keys) != 0 {
		t.Fatalf("vermerk left after abandon: %v", keys)
	}
	got = e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if got.Remote == drafts.RemoteScheduled || got.ScheduledFor != "" {
		t.Fatalf("closed draft still scheduled: %+v", got.Remote)
	}
}
