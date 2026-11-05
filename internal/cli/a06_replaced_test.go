package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// A06 Teil 4: Sync-Zeile, --json und draft show nennen ersetzte Entwürfe.
func TestA06ReplacedOutput(t *testing.T) {
	var b strings.Builder
	renderDraftCheck(&b, &drafts.CheckReport{SentByThread: 1, SentDraftIDs: []int64{3}, ReplacedInThread: 2, ReplacedDraftIDs: []int64{4, 6}})
	want := "drafts: 1 sent in the mail client, now sent_by_human (1 by thread (reply to the same message, same recipients, similar text); " +
		"draft 3); 2 replaced by an own reply in the thread (copy gone from the Drafts folder, same recipients, other text), " +
		"now abandoned (draft 4, 6)\n"
	if b.String() != want {
		t.Fatalf("got  %q\nwant %q", b.String(), want)
	}
	j, _ := json.Marshal(drafts.CheckReport{ReplacedInThread: 1, ReplacedDraftIDs: []int64{4}})
	if string(j) != `{"replaced_in_thread":1,"replaced_draft_ids":[4]}` {
		t.Fatalf("json = %s", j)
	}
	b.Reset()
	if err := renderDraftShow(&b, ops.DraftDetail{DraftID: 4, Kind: "reply", Account: "demo", Status: "abandoned", Remote: "missing",
		ClosedAt: "2026-11-05T10:00:00Z", ClosedReason: drafts.ReasonReplaced, ReplacedByMessageID: 77}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "closed:      2026-11-05T10:00:00Z, abandoned: replaced by an own reply in the thread (message 77); this text was not sent\n") {
		t.Fatalf("draft show:\n%s", b.String())
	}
}
