package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// A07 Teil 3: Sync-Zeile und --json nennen den Weg über den Betreff.
func TestA07RenderSentBySubject(t *testing.T) {
	var b bytes.Buffer
	rep := &drafts.CheckReport{SentBySubject: 1, SentDraftIDs: []int64{9}}
	renderDraftCheck(&b, rep)
	want := "drafts: 1 sent in the mail client, now sent_by_human (1 by subject (new email, same recipients and subject, similar text); draft 9)\n"
	if b.String() != want {
		t.Fatalf("line = %q\nwant  %q", b.String(), want)
	}
	j, _ := json.Marshal(rep)
	if !strings.Contains(string(j), `"sent_by_subject":1`) {
		t.Fatalf("json = %s", j)
	}
	if rep.Empty() {
		t.Fatal("report with sent_by_subject is empty")
	}
	var sum drafts.CheckReport
	sum.Add(*rep)
	sum.Add(*rep)
	if sum.SentBySubject != 2 {
		t.Fatalf("Add = %+v", sum)
	}
}

// A07 Teil 4: Sync-Zeile und draft show bei copy_gone.
func TestA07RenderCopyGone(t *testing.T) {
	var b bytes.Buffer
	renderDraftCheck(&b, &drafts.CheckReport{CopyGone: 1, CopyGoneDraftIDs: []int64{5}})
	want := "drafts: 1 with the copy gone from the Drafts folder for longer than [draft] copy_gone_after and no sent message detected, " +
		"now abandoned (copy_gone; draft 5)\n"
	if b.String() != want {
		t.Fatalf("line = %q\nwant  %q", b.String(), want)
	}
	j, _ := json.Marshal(drafts.CheckReport{CopyGone: 1, CopyGoneDraftIDs: []int64{5}})
	if string(j) != `{"copy_gone":1,"copy_gone_draft_ids":[5]}` {
		t.Fatalf("json = %s", j)
	}
	b.Reset()
	if err := renderDraftShow(&b, ops.DraftDetail{DraftID: 5, Kind: "new", Account: "demo", Status: "abandoned", Remote: "missing",
		ClosedAt: "2026-11-05T10:00:00Z", ClosedReason: drafts.ReasonCopyGone, CopyMissingSince: "2026-11-04T09:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "closed:      2026-11-05T10:00:00Z, abandoned: copy gone from the Drafts folder since 2026-11-04T09:00:00Z "+
		"(copy_gone); no sent message was detected, it may or may not have been sent\n") {
		t.Fatalf("draft show:\n%s", b.String())
	}
}

// Nachtrag 1, B8: draft show sagt nach supersede, dass die Kopie für den
// Papierkorb vorgemerkt ist, mit der Aktion; nicht „went to the trash“.
func TestA07N1RenderSuperseded(t *testing.T) {
	var b bytes.Buffer
	if err := renderDraftShow(&b, ops.DraftDetail{DraftID: 3, Kind: "reply", Account: "demo", Status: "abandoned", Remote: "pushed",
		ClosedAt: "2026-11-10T10:00:00Z", ClosedReason: drafts.ReasonSupersededByDraft, SupersededByDraftID: 5, SupersedeActionID: 12}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "superseded by draft 5; its unchanged copy is queued for the trash (check with: postkauz action show 12)") ||
		strings.Contains(b.String(), "went to the trash") {
		t.Fatalf("draft show:\n%s", b.String())
	}
	b.Reset()
	if err := renderSupersede(&b, ops.SupersedeOutput{DraftID: 3, ByDraftID: 5, Status: "abandoned", Copies: 1, Skipped: 1, Trash: "Papierkorb",
		ActionID: 12, NextStep: "x"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "1 own copy/copies queued for the trash (Papierkorb), 1 earlier copy/copies outside the Drafts folders left alone") {
		t.Fatalf("supersede:\n%s", b.String())
	}
}
