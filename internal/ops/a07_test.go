package ops

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A07 Teil 1: Der Hinweis zur verschwundenen Kopie nennt den Fall „Antwort
// ohne Empfänger“ (nur der Weg über den Verlauf schließt automatisch).
func TestA07RemoteNoteWithoutRecipients(t *testing.T) {
	d := store.Draft{Status: string(drafts.StatusDraft), RemoteState: drafts.RemoteMissing, Kind: string(drafts.KindReply), InReplyToMsg: 7}
	if n := remoteNote(d); !strings.Contains(n, "no To or Cc") || !strings.Contains(n, "In-Reply-To") {
		t.Fatalf("note without recipients = %q", n)
	}
	d.To = []store.Addr{{Address: "anna@example.org"}}
	if n := remoteNote(d); strings.Contains(n, "no To or Cc") {
		t.Fatalf("note with recipients = %q", n)
	}
}

// A07 Teil 4: drafts.get nennt closed_reason copy_gone, seit wann die
// Kopie fehlte und den Satz, dass der Versand nicht erkannt wurde.
func TestA07DraftGetNamesCopyGone(t *testing.T) {
	e := newDraftEnv(t, true)
	id := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"a@example.org"}, "subject": "x",
		"body_markdown": "Text"}).(DraftsWriteOutput).DraftID
	const closed = int64(1793000000000)
	for _, q := range []string{
		fmt.Sprintf(`UPDATE drafts SET status = 'abandoned', closed_at = %d WHERE id = %d`, closed, id),
		fmt.Sprintf(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result, reason)
			VALUES (%d, 'system', 'system', 'drafts.copy_gone_detected', '{"draft_id":%d,"missing_since":"2026-11-01T09:00:00Z"}', '["draft:%d"]', 'ok', 'copy_gone')`,
			closed, id, id),
	} {
		if err := e.st.ExecForTest(q); err != nil {
			t.Fatal(err)
		}
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if got.ClosedReason != "copy_gone" || got.CopyMissingSince != "2026-11-01T09:00:00Z" || !strings.Contains(got.ClosedNote, "not known whether it was sent") ||
		got.ReplacedByMessageID != 0 {
		t.Fatalf("copy_gone draft: %q, %q, %q", got.ClosedReason, got.CopyMissingSince, got.ClosedNote)
	}
	b, _ := json.Marshal(got)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	_, props := requiredFields(t, DraftsGet(e.deps).OutputSchema)
	for k := range out {
		if _, ok := props[k]; !ok {
			t.Fatalf("output field %q is not in the OutputSchema of drafts.get", k)
		}
	}
}
