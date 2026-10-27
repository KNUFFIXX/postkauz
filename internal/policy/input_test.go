package policy

import "testing"

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
	if r := f.Rules(); r != DefaultRules() {
		t.Fatalf("rules without file = %+v", r)
	}
	f.Doc.Move.AgentMay, f.Doc.Delete.AgentMay, f.Doc.Move.BulkConfirmThreshold = "archive_only", "none", 7
	if r := f.Rules(); r.MoveAgentMay != "archive_only" || r.DeleteAgentMay != "none" || r.BulkConfirmThreshold != 7 {
		t.Fatalf("rules = %+v", r)
	}
}
