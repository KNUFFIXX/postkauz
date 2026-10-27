package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

func TestRenderSyncSkippedTooLarge(t *testing.T) {
	var b bytes.Buffer
	o := ops.SyncOutput{Account: "demo", Mode: "full", Strategy: "uid-scan", Errors: 423, MessagesMirrored: 7052,
		SkippedTooLarge: 423, MaxMessageBytes: 1 << 20,
		Mailboxes: []ops.SyncMailbox{{Path: "Sent", Role: "sent", Errors: 423, Mirrored: 7052, Total: 7475}}}
	if err := renderSync(&b, o); err != nil {
		t.Fatal(err)
	}
	want := "423 skipped (larger than 1048576 bytes, [imap] max_message_bytes); not fetched, counted as errors; " +
		"raise the limit and run sync --full to fetch them\n7052 messages mirrored\n423 errors\n"
	if !strings.HasSuffix(b.String(), want) {
		t.Fatalf("output:\n%s", b.String())
	}
	b.Reset()
	o.SkippedTooLarge, o.Errors = 0, 0
	_ = renderSync(&b, o)
	if strings.Contains(b.String(), "skipped") {
		t.Fatalf("without skipped:\n%s", b.String())
	}
}
