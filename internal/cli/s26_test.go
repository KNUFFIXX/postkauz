package cli

import (
	"bytes"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.6: sync nennt, was die Prüfung nach dem Spiegeln an Entwürfen
// geändert hat (Zähler und Draft-IDs, kein Inhalt); ohne Änderung keine
// Zeile.
func TestRenderDraftCheck(t *testing.T) {
	var b bytes.Buffer
	renderDraftCheck(&b, nil)
	renderDraftCheck(&b, &drafts.CheckReport{})
	if b.Len() != 0 {
		t.Fatalf("empty check printed %q", b.String())
	}
	renderDraftCheck(&b, &drafts.CheckReport{SentByMessageID: 1, SentByFallback: 1, SentDraftIDs: []int64{3, 5}, Expired: 2, Missing: 1})
	want := "drafts: 2 sent in the mail client, now sent_by_human (1 by Message-ID, 1 by recipients, subject and text; draft 3, 5); " +
		"2 approval(s) ran out, back to status draft; 1 copy/copies gone from the Drafts folder without a matching sent message " +
		"(close with draft mark-sent or draft abandon)\n"
	if b.String() != want {
		t.Fatalf("got  %q\nwant %q", b.String(), want)
	}
	// Die Zeile steht vor den beiden Schlusszeilen, die Skripte auswerten.
	b.Reset()
	if err := renderSync(&b, ops.SyncOutput{Account: "demo", Mode: "incremental", DraftCheck: &drafts.CheckReport{TakenOver: 1}}); err != nil {
		t.Fatal(err)
	}
	if got := b.String(); !bytes.HasSuffix([]byte(got), []byte("drafts: 1 edited in the mail client (taken over)\n0 messages mirrored\n0 errors\n")) {
		t.Fatalf("sync output = %q", got)
	}
}
