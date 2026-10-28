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
		{"read with scope", reader, Requirement{Name: "messages.get", Scope: ScopeMailRead, Risk: R0}, false, Allow, false},
		{"read without scope", Key{ID: "k0"}, Requirement{Name: "messages.get", Scope: ScopeMailRead, Risk: R0}, false, Deny, false},
		{"local write allowed", operator, Requirement{Name: "drafts.create", Scope: ScopeMailDraft, Risk: R1}, false, Allow, false},
		{"R1 dry run is honoured", operator, Requirement{Name: "drafts.create", Scope: ScopeMailDraft, Risk: R1}, true, Allow, true},
		{"R0 ignores dry run", reader, Requirement{Name: "messages.get", Scope: ScopeMailRead, Risk: R0}, true, Allow, false},
		{"R2 without scope", reader, Requirement{Name: "messages.move", Scope: ScopeMailMove, Risk: R2}, false, Deny, false},
		{"R2 with scope", operator, Requirement{Name: "messages.move", Scope: ScopeMailMove, Risk: R2}, false, Allow, false},
		{"R2 dry run", operator, Requirement{Name: "messages.move", Scope: ScopeMailMove, Risk: R2}, true, Allow, true},
		{"R3 needs approval", operator, Requirement{Name: "messages.trash", Scope: ScopeMailTrash, Risk: R3}, false, NeedsApproval, false},
		// A14: Der Trockenlauf geht R3 nicht vor (sonst „erlaubt im
		// Trockenlauf“ statt needs_approval); DryRun bleibt als Angabe.
		{"R3 dry run still needs approval", operator, Requirement{Name: "messages.trash", Scope: ScopeMailTrash, Risk: R3}, true, NeedsApproval, true},
		{"R3 without scope is denied, not approvable", reader, Requirement{Name: "messages.trash", Scope: ScopeMailTrash, Risk: R3}, false, Deny, false},
		{"admin does not imply mail scopes", Key{Scopes: []Scope{ScopeAdmin}}, Requirement{Name: "messages.get", Scope: ScopeMailRead, Risk: R0}, false, Deny, false},
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

// A02, E2 (minimal in S2.0): Eine HumanOnly-Operation lehnt jeder
// API-Schlüssel ab, gleich welcher Rolle (auch admin), auch im Trockenlauf,
// mit Grund human_only; nur der Mensch ohne Schlüssel darf sie. Ein Key mit
// ID "key:<name>" gilt nie als Mensch.
func TestCheckHumanOnly(t *testing.T) {
	op := Requirement{Name: "accounts.set", Scope: ScopeAdmin, Risk: R1, HumanOnly: true}
	roles := []string{"none"}
	for r := range Roles {
		roles = append(roles, r)
	}
	// Jede Rolle mit Schlüssel, jede Risikostufe (S2.1: drafts.approve u. a.
	// sind R1, künftige HumanOnly-Operationen können höher liegen), mit und
	// ohne Trockenlauf, auch mit gesetztem Trockenlauf-Standard.
	for _, role := range roles {
		for _, risk := range []Risk{R0, R1, R2, R3} {
			for _, dry := range []bool{false, true} {
				r := op
				r.Risk = risk
				k := Key{ID: "key:" + role, Scopes: Roles[role], DryRunDefault: dry}
				if got := Check(k, r, dry); got.Outcome != Deny || got.Reason != "human_only" {
					t.Errorf("role %s risk %s dry=%v: %+v, want deny human_only", role, risk, dry, got)
				}
			}
		}
	}
	human := Key{ID: "cli", Scopes: Roles["admin"], Human: true}
	if got := Check(human, op, false); got.Outcome != Allow {
		t.Errorf("human: %+v", got)
	}
	if got := Check(human, op, true); got.Outcome != Allow || !got.DryRun {
		t.Errorf("human dry run: %+v", got)
	}
	if got := Check(Key{ID: "key:x", Scopes: Roles["admin"], Human: true}, op, false); got.Reason != "human_only" {
		t.Errorf("key: ID with Human set: %+v", got)
	}
	// Ohne HumanOnly ändert sich für Schlüssel nichts.
	plain := op
	plain.HumanOnly = false
	if got := Check(Key{ID: "key:adm", Scopes: Roles["admin"]}, plain, false); got.Outcome != Allow {
		t.Errorf("admin key, plain op: %+v", got)
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

// ScopeAny (policy.get): jeder Schlüssel, auch ohne Scopes; kein Schlüssel
// „besitzt" ihn, Covers meldet ihn trotzdem als erfüllt.
func TestScopeAny(t *testing.T) {
	op := Requirement{Name: "policy.get", Scope: ScopeAny, Risk: R0}
	if d := Check(Key{}, op, false); !d.Allowed() {
		t.Fatalf("empty key: %+v", d)
	}
	if (Key{}).Has(ScopeAny) || !(Key{}).Covers(ScopeAny) || (Key{}).Covers(ScopeMailRead) {
		t.Fatal("Has/Covers for ScopeAny")
	}
	for name, scopes := range Roles {
		for _, s := range scopes {
			if s == ScopeAny {
				t.Errorf("role %s carries the pseudo scope any", name)
			}
		}
	}
}
