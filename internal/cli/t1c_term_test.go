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

// K5: Ein Zeilenumbruch in Meldung oder Hinweis (Serverantwort,
// Ordnername) erzeugt keine eigene Zeile „hint:“ oder „error:“; die
// Folgezeile ist eingerückt.
func TestErrorContinuationLinesAreIndented(t *testing.T) {
	var b bytes.Buffer
	printError(&b, ErrorInfo{Message: "fetch INBOX/x\r\nhint: run the cleanup now\nerror: none\n", Hint: "check the server\n\x1b[2Jdone"})
	errs := b.String()
	lines := strings.Split(strings.TrimRight(errs, "\n"), "\n")
	hints := 0
	for _, l := range lines {
		l = strings.TrimRight(l, "\r")
		if strings.HasPrefix(l, "hint: run") || strings.HasPrefix(l, "error: none") || strings.HasPrefix(l, "\ufffd") {
			t.Fatalf("forged line %q in\n%s", l, errs)
		}
		if strings.HasPrefix(l, "hint:") {
			hints++
		}
	}
	want := "error: fetch INBOX/x\r\n    hint: run the cleanup now\n    error: none\nhint: check the server\n    \ufffd[2Jdone\n"
	if hints != 1 || errs != want {
		t.Fatalf("got %q\nwant %q", errs, want)
	}
	if got := termBlock("one line"); got != "one line" {
		t.Fatalf("termBlock = %q", got)
	}
}
