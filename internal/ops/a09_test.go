package ops

import (
	"fmt"
	"testing"
)

// A09 Teil 1: drafts.get nennt bei einem über den Verlauf mit ergänzten
// Empfängern erkannten Entwurf closed_note „recipients added in the mail
// client (n)“, gelesen aus dem Audit; closed_reason bleibt leer.
func TestA09DraftGetNamesRecipientsAdded(t *testing.T) {
	for _, c := range []struct {
		name, params, note string
	}{
		{"added", `{"draft_id":%d,"sent_message_id":5,"recipients_added":2}`, "recipients added in the mail client (2)"},
		{"same recipients", `{"draft_id":%d,"sent_message_id":5}`, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDraftEnv(t, true)
			id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "x",
				"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
			const closed = int64(1793000000000)
			for _, q := range []string{
				fmt.Sprintf(`UPDATE drafts SET status = 'sent_by_human', closed_at = %d WHERE id = %d`, closed, id),
				fmt.Sprintf(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result, reason)
					VALUES (%d, 'system', 'system', 'drafts.sent_detected', '`+c.params+`', '["draft:%d"]', 'ok', 'thread')`, closed, id, id),
			} {
				if err := e.st.ExecForTest(q); err != nil {
					t.Fatal(err)
				}
			}
			got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
			if got.ClosedNote != c.note || got.ClosedReason != "" {
				t.Fatalf("closed_note = %q, closed_reason = %q", got.ClosedNote, got.ClosedReason)
			}
		})
	}
}
