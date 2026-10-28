package cli

import (
	"strings"
	"testing"
)

// T2: sync --watch --all (Eingaben, Trockenlauf je Konto) und sync request
// über die CLI.
func TestSyncAllAndRequestCLI(t *testing.T) {
	setupEnv(t)
	addr := mailServer(t, canary)
	for _, name := range []string{"privat", "verein"} {
		if code, out, errs := runIn(t, canary+"\n", "account", "add", name, "--imap", addr, "--user", "tester@example.invalid",
			"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
			t.Fatalf("add %s: %d %s %s", name, code, out, errs)
		}
	}
	for _, args := range [][]string{{"sync", "--all"}, {"sync", "--watch", "--all", "-a", "privat"},
		{"sync", "--watch", "--all", "--mailbox", "INBOX"}} {
		if code, _, errs := run(t, args...); code != ExitUsage || !strings.Contains(errs, "--all") {
			t.Fatalf("%v: %d %s", args, code, errs)
		}
	}
	code, out, errs := run(t, "sync", "--watch", "--all", "--dry-run")
	if code != ExitOK || strings.Count(out, "dry run: would sync 3 mailboxes") != 2 || !strings.Contains(out, "account privat") ||
		!strings.Contains(out, "account verein") {
		t.Fatalf("watch --all --dry-run: %d %q %q", code, out, errs)
	}

	// sync request: ohne Konto bei zwei Konten Exit 2; sonst eingereiht,
	// eine zweite Anforderung kommt zum offenen Job.
	if code, _, _ := run(t, "sync", "request"); code != ExitUsage {
		t.Fatalf("sync request without account: %d", code)
	}
	code, out, errs = run(t, "sync", "request", "-a", "privat", "--mailbox", "INBOX")
	if code != ExitOK || !strings.Contains(out, "sync of account privat (mailbox INBOX) queued as job ") ||
		!strings.Contains(out, "note: the sync is queued: a running sync --watch") {
		t.Fatalf("sync request: %d %q %q", code, out, errs)
	}
	code, out, _ = run(t, "sync", "request", "-a", "privat")
	if code != ExitOK || !strings.Contains(out, "sync of account privat (all mailboxes) added to queued job ") {
		t.Fatalf("second sync request: %d %q", code, out)
	}
	code, out, _ = run(t, "sync", "request", "-a", "verein", "--dry-run")
	if code != ExitOK || !strings.Contains(out, "dry run: would queue a sync of account verein (all mailboxes); nothing was queued") {
		t.Fatalf("dry run: %d %q", code, out)
	}
	// Ein einmaliger Sync erledigt den offenen Job.
	if code, out, errs := run(t, "sync", "-a", "privat"); code != ExitOK {
		t.Fatalf("sync: %d %s %s", code, out, errs)
	}
	code, out, _ = run(t, "sync", "request", "-a", "privat")
	if code != ExitOK || !strings.Contains(out, "queued as job ") {
		t.Fatalf("request after sync: %d %q (the sync should have closed the earlier job)", code, out)
	}
}
