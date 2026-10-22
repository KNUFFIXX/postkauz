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

// S2.7 (Kleinkram aus der Gegenprüfung von S2.3): Fehlermeldungen nennen
// nie den eingegebenen Wert (Suche, account add, key create, audit, IDs,
// Pfade), weder als Text noch mit --json. Der Wert könnte Mailinhalt oder
// Personendaten sein und landete sonst in Skript-Protokollen.
func TestErrorsDoNotEchoInput(t *testing.T) {
	setupEnv(t)
	const k = "Zqkanarie7"
	cases := []struct {
		stdin string
		args  []string
	}{
		{"", []string{"search", "has:" + k}},
		{"", []string{"search", "is:" + k}},
		{"", []string{"search", "thread:" + k}},
		{"", []string{"search", "after:" + k}},
		{"", []string{"search", "x", "--json", "--fields", "id," + k}},
		{"pw\n", []string{"account", "add", "demo", "--imap", k + "/x:993", "--user", "u", "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:" + k, "--user", "u", "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:993", "--user", "u", "--tls", k, "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", "imap.example.org:993", "--user", "u", "--auth", k, "--password-stdin"}},
		{"pw\n", []string{"account", "add", "demo", "--imap", k + ".example.org:143", "--user", "u", "--insecure-test-server", "--password-stdin"}},
		{"", []string{"key", "create", "--name", k}},
		{"", []string{"key", "create", "--name", "x", "--role", k}},
		{"", []string{"key", "create", "--name", "x", "--role", "reader", "--scopes", k}},
		{"", []string{"key", "create", "--name", "x", "--expires", k}},
		{"", []string{"audit", "--since", k}},
		{"", []string{"draft", "show", k}},
		{"", []string{"thread", k}},
		{"", []string{"import", k, "."}},
		{"", []string{"version", "--config", "/nonexistent/" + k + ".toml"}},
	}
	for _, c := range cases {
		for _, asJSON := range []bool{false, true} {
			args := append([]string{}, c.args...)
			if asJSON {
				args = append(args, "--json")
			}
			code, out, errs := runIn(t, c.stdin, args...)
			if code != ExitUsage {
				t.Errorf("%v: exit %d, want 2 (stderr %q)", args, code, errs)
			}
			if strings.Contains(strings.ToLower(out+errs), strings.ToLower(k)) {
				t.Errorf("%v: the input value appears in the output: %q %q", args, out, errs)
			}
		}
	}
}
