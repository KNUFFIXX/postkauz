package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// A9: CheckInput entscheidet über die Eingabe (E4). Mensch: nur die
// Obergrenze; Schlüssel: [delete], [move], bulk_confirm_threshold mit
// confirm_count genau gleich der Zahl der Ziele.
func TestCheckInput(t *testing.T) {
	human := Key{ID: "cli", Human: true, Scopes: Roles["admin"]}
	agent := Key{ID: "key:a", Scopes: Roles["operator"]}
	n := func(i int) *int { return &i }
	def := DefaultRules()
	cases := []struct {
		name string
		key  Key
		f    Facts
		r    Rules
		want string // "" = erlaubt
	}{
		{"human many", human, Facts{Action: ActionMove, Targets: 1000}, def, ""},
		{"human too many", human, Facts{Action: ActionMove, Targets: 1001}, def, "too_many_ids"},
		{"agent too many", agent, Facts{Targets: 1001, ConfirmCount: n(1001)}, def, "too_many_ids"},
		{"agent below threshold", agent, Facts{Action: ActionMove, Targets: 50}, def, ""},
		{"agent above threshold", agent, Facts{Action: ActionMove, Targets: 51}, def, "confirm_count_required"},
		{"agent wrong count", agent, Facts{Action: ActionMove, Targets: 51, ConfirmCount: n(50)}, def, "confirm_count_mismatch"},
		{"agent right count", agent, Facts{Action: ActionMove, Targets: 51, ConfirmCount: n(51)}, def, ""},
		{"human above threshold", human, Facts{Action: ActionTrash, Targets: 500}, def, ""},
		{"trash allowed", agent, Facts{Action: ActionTrash, Targets: 1}, def, ""},
		{"trash none", agent, Facts{Action: ActionTrash, Targets: 1}, Rules{DeleteAgentMay: "none"}, "policy_forbids"},
		{"move none", agent, Facts{Action: ActionMove, Targets: 1}, Rules{MoveAgentMay: "none"}, "policy_forbids"},
		{"archive only to inbox", agent, Facts{Action: ActionMove, Targets: 1, TargetRole: "inbox"}, Rules{MoveAgentMay: "archive_only"}, "policy_forbids"},
		{"archive only to all", agent, Facts{Action: ActionMove, Targets: 1, TargetRole: "all"}, Rules{MoveAgentMay: "archive_only"}, ""},
		{"archive only to archive", agent, Facts{Action: ActionMove, Targets: 1, TargetRole: "archive"}, Rules{MoveAgentMay: "archive_only"}, ""},
		{"cross account", agent, Facts{Action: ActionMove, Targets: 1, CrossAccount: true}, def, "cross_account"},
		{"mark below", agent, Facts{Action: ActionMark, Targets: 10}, Rules{BulkConfirmThreshold: 10}, ""},
		{"mark above own threshold", agent, Facts{Action: ActionMark, Targets: 11}, Rules{BulkConfirmThreshold: 10}, "confirm_count_required"},
	}
	for _, c := range cases {
		d := CheckInput(c.key, c.f, c.r)
		if c.want == "" && !d.Allowed() || c.want != "" && (d.Outcome != Deny || d.Reason != c.want) {
			t.Errorf("%s: %+v, want %q", c.name, d, c.want)
		}
	}
}

func TestFileRules(t *testing.T) {
	f := File{Doc: defaults(FileDoc{}, nil)}
	if r := f.Rules(); !reflect.DeepEqual(r, DefaultRules()) {
		t.Fatalf("rules without file = %+v", r)
	}
	f.Doc.Move.AgentMay, f.Doc.Delete.AgentMay, f.Doc.Move.BulkConfirmThreshold = "archive_only", "none", 7
	if r := f.Rules(); r.MoveAgentMay != "archive_only" || r.DeleteAgentMay != "none" || r.BulkConfirmThreshold != 7 {
		t.Fatalf("rules = %+v", r)
	}
}

// E4 (T2): [keys.<name>] verengt die Regeln für genau diesen Schlüssel;
// weiter als der Standard ist ungültig.
func TestKeyNarrowing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.toml")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("[move]\nagent_may = \"archive_only\"\nbulk_confirm_threshold = 20\n[keys.nacht]\nmax_chars = 500\n[keys.nacht.move]\nagent_may = \"none\"\n" +
		"bulk_confirm_threshold = 5\n[keys.nacht.delete]\nagent_may = \"none\"\n")
	f, err := LoadFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	rules := f.Rules()
	nacht := Key{ID: "key:nacht", Name: "nacht", Scopes: Roles["operator"]}
	other := Key{ID: "key:tag", Name: "tag", Scopes: Roles["operator"]}
	if d := CheckInput(nacht, Facts{Action: ActionMove, Targets: 1, TargetRole: "archive"}, rules); d.Reason != "policy_forbids" {
		t.Fatalf("nacht move = %+v", d)
	}
	if d := CheckInput(other, Facts{Action: ActionMove, Targets: 1, TargetRole: "archive"}, rules); !d.Allowed() {
		t.Fatalf("other move = %+v", d)
	}
	if d := CheckInput(nacht, Facts{Action: ActionTrash, Targets: 1}, rules); d.Reason != "policy_forbids" {
		t.Fatalf("nacht trash = %+v", d)
	}
	if d := CheckInput(nacht, Facts{Action: ActionMark, Targets: 6}, rules); d.Reason != "confirm_count_required" {
		t.Fatalf("nacht mark 6 = %+v", d)
	}
	if d := CheckInput(other, Facts{Action: ActionMark, Targets: 6}, rules); !d.Allowed() {
		t.Fatalf("other mark 6 = %+v", d)
	}
	if f.MaxCharsFor("nacht") != 500 || f.MaxCharsFor("tag") != DefaultMaxChars {
		t.Fatalf("max chars %d %d", f.MaxCharsFor("nacht"), f.MaxCharsFor("tag"))
	}
	for name, doc := range map[string]string{
		"wider move":      "[move]\nagent_may = \"archive_only\"\n[keys.x.move]\nagent_may = \"within_account\"\n",
		"wider delete":    "[delete]\nagent_may = \"none\"\n[keys.x.delete]\nagent_may = \"trash_only\"\n",
		"wider threshold": "[move]\nbulk_confirm_threshold = 20\n[keys.x.move]\nbulk_confirm_threshold = 21\n",
		"wider max_chars": "[agent_defaults]\nmax_chars = 1000\n[keys.x]\nmax_chars = 1001\n",
		"bad name":        "[keys.Gross]\nmax_chars = 500\n",
		"unknown field":   "[keys.x]\nscopes = [\"admin\"]\n",
	} {
		write(doc)
		if _, err := LoadFile(path, true); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestDryRunDefaultFromFile(t *testing.T) {
	if !(File{Doc: defaults(FileDoc{}, nil)}).DryRunDefault() {
		t.Fatal("default must be true")
	}
	no := false
	f := File{Doc: defaults(FileDoc{AgentDefaults: AgentDefaults{DryRunDefault: &no}}, nil)}
	if f.DryRunDefault() {
		t.Fatal("dry_run_default = false ignored")
	}
}

// E3: Verschieben in den Papierkorb ist kein Weg um die Freigabe herum.
func TestCheckInputMoveToTrash(t *testing.T) {
	for _, k := range []Key{{ID: "cli", Human: true, Scopes: Roles["admin"]}, {ID: "key:a", Scopes: Roles["operator"]}} {
		if d := CheckInput(k, Facts{Action: ActionMove, Targets: 1, TargetRole: "trash"}, DefaultRules()); d.Reason != "target_is_trash" {
			t.Fatalf("%s: %+v", k.ID, d)
		}
	}
}
