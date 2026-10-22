package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.0/A6: `account set --aliases` pflegt die weiteren eigenen Adressen,
// `accounts list` zeigt sie (Text und --json); mit --key (auch admin) ist
// das Kommando verweigert (human_only, Exit 3), und nichts ändert sich.
func TestAccountSetAliasesCLI(t *testing.T) {
	setupEnv(t)
	addDemo(t, canary)
	code, out, errs := run(t, "account", "set", "demo", "--aliases", "Post@Example.net, zweit@example.org")
	if code != ExitOK || !strings.Contains(out, "account demo: aliases post@example.net, zweit@example.org") {
		t.Fatalf("set: %d %q %q", code, out, errs)
	}
	code, out, _ = run(t, "accounts", "list", "--json")
	var list struct {
		Accounts []struct {
			Name    string   `json:"name"`
			Aliases []string `json:"aliases"`
		} `json:"accounts"`
	}
	if code != ExitOK || json.Unmarshal([]byte(out), &list) != nil || len(list.Accounts) != 1 || len(list.Accounts[0].Aliases) != 2 {
		t.Fatalf("list --json: %d %s", code, out)
	}
	if code, out, _ = run(t, "accounts", "list"); code != ExitOK || !strings.Contains(out, "ALIASES") || !strings.Contains(out, "post@example.net, zweit@example.org") {
		t.Fatalf("list: %s", out)
	}
	if code, _, errs := run(t, "account", "set", "demo"); code != ExitUsage || !strings.Contains(errs, "nothing to change") {
		t.Fatalf("without flag: %d %q", code, errs)
	}
	if code, _, errs := run(t, "account", "set", "demo", "--aliases", "Name <x@example.org>"); code != ExitUsage || !strings.Contains(errs, "invalid alias") {
		t.Fatalf("invalid alias: %d %q", code, errs)
	}

	if code, out, errs := run(t, "key", "create", "--name", "adm", "--role", "admin", "--json"); code != ExitOK {
		t.Fatalf("key create: %d %s %s", code, out, errs)
	}
	code, _, errs = run(t, "account", "set", "demo", "--aliases", "", "--key", "adm", "--json")
	if code != ExitDenied || !strings.Contains(errs, "human_only") {
		t.Fatalf("with --key: %d %q", code, errs)
	}
	if _, out, _ = run(t, "accounts", "list", "--json"); !strings.Contains(out, "zweit@example.org") {
		t.Fatalf("aliases changed by a key: %s", out)
	}
	// Entscheid 40 (S2.3): für einen Schlüssel nie unter operations, aber
	// in der eigenen Liste human_only.
	_, out, _ = run(t, "policy", "show", "--key", "adm", "--json")
	var pol ops.PolicyOutput
	if err := json.Unmarshal([]byte(out), &pol); err != nil {
		t.Fatal(err)
	}
	for _, op := range pol.Operations {
		if op.Name == "accounts.set" {
			t.Fatalf("policy show lists a human-only operation for a key: %s", out)
		}
	}
	if !strings.Contains(fmt.Sprint(pol.HumanOnly), "accounts.set") {
		t.Fatalf("policy show --key lacks accounts.set in human_only: %s", out)
	}
	if _, out, _ = run(t, "policy", "show", "--json"); !strings.Contains(out, "accounts.set") {
		t.Fatalf("policy show for the human lacks accounts.set: %s", out)
	}
	// Leeres --aliases entfernt alle.
	if code, out, _ := run(t, "account", "set", "demo", "--aliases", ""); code != ExitOK || !strings.Contains(out, "aliases none") {
		t.Fatalf("clear: %d %s", code, out)
	}
}
