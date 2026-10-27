package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

func TestAccountTestAndErrorsAreTerminalSafe(t *testing.T) {
	var b bytes.Buffer
	out := ops.AccountsTestOutput{Account: "demo", Login: "ok\x1b[2J", ProbeResult: ops.ProbeResult{
		Capabilities: []string{"IMAP4rev1", "X\x1b]52;c;Zm9v\x07"}, SyncStrategy: "uid-scan",
		Mailboxes: []ops.ProbeMailbox{{Path: "INBOX/\x1b[31mrot\u202e", Role: "custom"}}}}
	if err := renderAccountTest(&b, out); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(b.String(), "\x1b\x07\u202e") || !strings.Contains(b.String(), "INBOX/\ufffd[31mrot") {
		t.Fatalf("account test:\n%q", b.String())
	}
	setupEnv(t)
	code, _, errs := run(t, "\x1b]52;c;Zm9v\x07")
	if code != ExitUsage || strings.ContainsAny(errs, "\x1b\x07") || !strings.Contains(errs, "error: ") {
		t.Fatalf("unknown command: %d %q", code, errs)
	}
}
