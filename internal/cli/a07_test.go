package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
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
