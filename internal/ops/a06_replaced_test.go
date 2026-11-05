package ops

import (
	"fmt"
	"testing"
)

// A06 Teil 4: drafts.get nennt den Grund, wenn das System den Entwurf als
// durch eine eigene Antwort ersetzt geschlossen hat (aus dem Audit, ohne
// Migration); bei einem vom Menschen verworfenen Entwurf nicht.
func TestA06DraftGetNamesReplaced(t *testing.T) {
	e := newDraftEnv(t, true)
	id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "x",
		"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
	other := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "y",
		"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
	const closed = int64(1793000000000)
	for _, q := range []string{
		fmt.Sprintf(`UPDATE drafts SET status = 'abandoned', closed_at = %d WHERE id IN (%d, %d)`, closed, id, other),
		fmt.Sprintf(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result, reason)
			VALUES (%d, 'system', 'system', 'drafts.replaced_detected', '{"draft_id":%d,"reply_message_id":42}', '["draft:%d"]', 'ok', 'replaced')`,
			closed, id, id),
	} {
		if err := e.st.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if got.ClosedReason != "replaced" || got.ReplacedByMessageID != 42 || got.SentMessageID != 0 {
		t.Fatalf("replaced draft: reason %q, reply %d", got.ClosedReason, got.ReplacedByMessageID)
	}
	if got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": other}).(DraftDetail); got.ClosedReason != "" {
		t.Fatalf("abandoned by the human: reason %q", got.ClosedReason)
	}
}
