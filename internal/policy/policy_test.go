package policy

import "testing"

func TestCheck(t *testing.T) {
	reader := Key{ID: "k1", Scopes: Roles["reader"]}
	operator := Key{ID: "k2", Scopes: Roles["operator"]}

	tests := []struct {
		name   string
		key    Key
		op     Requirement
		dryRun bool
		want   Outcome
		wantDR bool
	}{
		{"read with scope", reader, Requirement{"messages.get", ScopeMailRead, R0}, false, Allow, false},
		{"read without scope", Key{ID: "k0"}, Requirement{"messages.get", ScopeMailRead, R0}, false, Deny, false},
		{"local write allowed", operator, Requirement{"drafts.create", ScopeMailDraft, R1}, false, Allow, false},
		{"R1 dry run is honoured", operator, Requirement{"drafts.create", ScopeMailDraft, R1}, true, Allow, true},
		{"R0 ignores dry run", reader, Requirement{"messages.get", ScopeMailRead, R0}, true, Allow, false},
		{"R2 without scope", reader, Requirement{"messages.move", ScopeMailMove, R2}, false, Deny, false},
		{"R2 with scope", operator, Requirement{"messages.move", ScopeMailMove, R2}, false, Allow, false},
		{"R2 dry run", operator, Requirement{"messages.move", ScopeMailMove, R2}, true, Allow, true},
		{"R3 needs approval", operator, Requirement{"messages.trash", ScopeMailTrash, R3}, false, NeedsApproval, false},
		{"R3 dry run is allowed", operator, Requirement{"messages.trash", ScopeMailTrash, R3}, true, Allow, true},
		{"R3 without scope is denied, not approvable", reader, Requirement{"messages.trash", ScopeMailTrash, R3}, false, Deny, false},
		{"admin does not imply mail scopes", Key{Scopes: []Scope{ScopeAdmin}}, Requirement{"messages.get", ScopeMailRead, R0}, false, Deny, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Check(tc.key, tc.op, tc.dryRun)
			if got.Outcome != tc.want || got.DryRun != tc.wantDR {
				t.Fatalf("Check = %+v, want outcome=%s dryRun=%v", got, tc.want, tc.wantDR)
			}
			if got.Reason == "" {
				t.Fatal("Reason must never be empty")
			}
		})
	}
}

func TestEffectiveDryRun(t *testing.T) {
	f, tr := false, true
	def := Key{DryRunDefault: true}
	if !EffectiveDryRun(def, nil) {
		t.Error("key default must apply when caller says nothing")
	}
	if EffectiveDryRun(def, &f) {
		t.Error("explicit dry_run=false must override the key default")
	}
	if !EffectiveDryRun(Key{}, &tr) {
		t.Error("explicit dry_run=true must win")
	}
	if EffectiveDryRun(Key{}, nil) {
		t.Error("no default and no explicit value means a real run")
	}
}

func TestRolesAreSubsetsInOrder(t *testing.T) {
	order := []string{"reader", "assistant", "operator", "admin"}
	for i := 1; i < len(order); i++ {
		lower := Key{Scopes: Roles[order[i]]}
		for _, s := range Roles[order[i-1]] {
			if !lower.Has(s) {
				t.Errorf("role %s lacks scope %s of %s", order[i], s, order[i-1])
			}
		}
	}
	for name, scopes := range Roles {
		if name != "admin" && (Key{Scopes: scopes}).Has(ScopeAdmin) {
			t.Errorf("role %s must not carry admin", name)
		}
	}
}
