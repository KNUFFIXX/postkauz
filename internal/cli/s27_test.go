package cli

import (
	"strings"
	"testing"
)

// S2.7: Ein fehlender Scope wird bei jedem Kommando gleich gemeldet, in
// derselben Form wie eine fachliche Ablehnung: "denied (missing_scope): <op>
// needs the scope <scope>, …" (vorher "ops: <op>: deny (missing_scope)",
// Befund der Teilabnahme S2.3), mit und ohne --json.
func TestMissingScopeMessageIsUniform(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	if code, out, errs := run(t, "key", "create", "--name", "leser", "--role", "reader"); code != ExitOK {
		t.Fatalf("key create: %d %s %s", code, out, errs)
	}
	cases := []struct {
		args  []string
		op    string
		scope string
	}{
		{[]string{"draft", "new", "-a", "demo", "--to", "a@example.org"}, "drafts.create", "mail:draft"},
		{[]string{"draft", "edit", "1", "--subject", "x"}, "drafts.update", "mail:draft"},
		{[]string{"draft", "show", "1"}, "drafts.get", "mail:draft"},
		{[]string{"draft", "list"}, "drafts.list", "mail:draft"},
		{[]string{"draft", "request", "1"}, "drafts.request_ready", "mail:draft"},
		{[]string{"approval", "show", "1"}, "approvals.get", "mail:draft"},
		{[]string{"key", "list"}, "keys.list", "admin"},
		{[]string{"sync", "-a", "demo"}, "sync.run", "mail:sync"},
		{[]string{"audit", "export", "--to", t.TempDir()}, "audit.export", "admin"},
	}
	for _, c := range cases {
		for _, asJSON := range []bool{false, true} {
			args := append(append([]string{}, c.args...), "--key", "leser")
			if asJSON {
				args = append(args, "--json")
			}
			code, _, errs := run(t, args...)
			want := "denied (missing_scope): " + c.op + " needs the scope " + c.scope
			if code != ExitDenied || !strings.Contains(errs, want) || strings.Contains(errs, "ops: ") {
				t.Errorf("%v: exit %d, stderr %q; want exit 3 and %q", args, code, errs, want)
			}
			if asJSON && !strings.Contains(errs, `"code":"denied"`) {
				t.Errorf("%v: no JSON error: %q", args, errs)
			}
		}
	}
	// human_only behält seine Meldung (gleiche Form).
	if code, _, errs := run(t, "approve", "1", "--key", "leser"); code != ExitDenied ||
		!strings.Contains(errs, "denied (human_only): drafts.approve is for a human without a key") {
		t.Fatalf("approve --key: %d %q", code, errs)
	}
}
