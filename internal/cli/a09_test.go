package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// A09 Teil 1: draft show nennt bei sent_by_human die im Mailprogramm
// ergänzten Empfänger (closed_note), sonst keine Zeile „closed:“.
func TestA09CLIShowsRecipientsAdded(t *testing.T) {
	var b bytes.Buffer
	d := ops.DraftDetail{DraftID: 7, Kind: "reply", Account: "demo", Status: "sent_by_human", Remote: "missing",
		ClosedAt: "2026-11-17T10:00:00Z", ClosedNote: ops.RecipientsAddedNote(1)}
	if err := renderDraftShow(&b, d); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "closed:      2026-11-17T10:00:00Z, sent: recipients added in the mail client (1)\n") {
		t.Fatalf("draft show:\n%s", b.String())
	}
	b.Reset()
	d.ClosedNote = ""
	if err := renderDraftShow(&b, d); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "closed:") {
		t.Fatalf("draft show without note:\n%s", b.String())
	}
}
