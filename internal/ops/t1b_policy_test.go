package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// policyInputCases: je Operation mit PolicyInput (A9) mindestens ein Fall,
// den die Policy ablehnt, und einer, den sie erlaubt (Eingabe, Schlüssel,
// erwarteter Grund; "" = erlaubt). Eine neue Operation mit Eingabegrenzen
// braucht hier Zeilen, sonst scheitert TestContractPolicyInputCases. Bis
// T2 (move, trash, mark) hat keine Katalog-Operation PolicyInput.
var policyInputCases = map[string][]struct {
	input string
	key   policy.Key
	want  string
}{}

// A9: Jede Operation mit Eingabegrenzen hat einen Policy-Testfall; die
// Ablehnung ist genau ein Eintrag denied mit dem Grund der Policy, vor
// jedem started, und der Handler läuft nicht.
func TestContractPolicyInputCases(t *testing.T) {
	for _, op := range Catalog(Deps{}) {
		if op.PolicyInput == nil {
			if _, ok := policyInputCases[op.Name]; ok {
				t.Errorf("%s has policy input cases but no PolicyInput", op.Name)
			}
			continue
		}
		cases := policyInputCases[op.Name]
		var denied, allowed bool
		for _, c := range cases {
			denied = denied || c.want != ""
			allowed = allowed || c.want == ""
		}
		if !denied || !allowed {
			t.Errorf("%s: PolicyInput needs at least one denied and one allowed case in policyInputCases", op.Name)
			continue
		}
		t.Run(op.Name, func(t *testing.T) {
			for _, c := range cases {
				e := newContractEnv(t)
				ran := map[string]int{}
				au := &audit.Memory{}
				x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: au}
				_, err := x.Execute(context.Background(), Request{Key: c.key, Actor: audit.ActorAgent, Operation: op.Name,
					Input: json.RawMessage(c.input)})
				checkPolicyInputResult(t, au.Events(), ran[op.Name], err, c.want)
			}
		})
	}
}

func checkPolicyInputResult(t *testing.T, evs []audit.Event, ran int, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil || ran != 1 || len(evs) != 2 || evs[0].Result != audit.ResultStarted {
			t.Fatalf("allowed: err=%v ran=%d events=%+v", err, ran, evs)
		}
		return
	}
	if !errors.Is(err, ErrDenied) || ran != 0 || len(evs) != 1 || evs[0].Result != audit.ResultDenied || evs[0].Reason != want {
		t.Fatalf("want denied (%s): err=%v ran=%d events=%+v", want, err, ran, evs)
	}
}

// A9 am Executor mit einer Testoperation (Verschieben vieler Nachrichten):
// Die Grenzen prüft die Policy vor dem Start-Eintrag, nicht der Handler.
func TestExecutorPolicyInput(t *testing.T) {
	ran := 0
	op := newTestOp("t.move", policy.ScopeMailMove, policy.R2, func(context.Context, Call) (any, error) { ran++; return map[string]any{}, nil })
	op.InputSchema = json.RawMessage(`{"type":"object","properties":{"ids":{"type":"array","items":{"type":"integer"}},
		"to_role":{"type":"string"},"confirm_count":{"type":"integer"}},"required":["ids"],"additionalProperties":false}`)
	op.PolicyInput = func(_ context.Context, raw json.RawMessage) (policy.Facts, error) {
		var in struct {
			IDs          []int64 `json:"ids"`
			ToRole       string  `json:"to_role"`
			ConfirmCount *int    `json:"confirm_count"`
		}
		if err := json.Unmarshal(raw, &in); err != nil {
			return policy.Facts{}, err
		}
		return policy.Facts{Action: policy.ActionMove, Targets: len(in.IDs), TargetRole: in.ToRole, ConfirmCount: in.ConfirmCount}, nil
	}
	reg := NewRegistry()
	if err := reg.Register(op); err != nil {
		t.Fatal(err)
	}
	ids := func(n int) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = int64(i + 1)
		}
		return out
	}
	agent := policy.Key{ID: "key:op", Scopes: policy.Roles["operator"]}
	human := policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}
	archive := policy.Rules{MoveAgentMay: "archive_only", DeleteAgentMay: "trash_only", BulkConfirmThreshold: 50}
	for _, c := range []struct {
		key   policy.Key
		in    map[string]any
		rules *policy.Rules
		want  string
	}{
		{agent, map[string]any{"ids": ids(51)}, nil, "confirm_count_required"},
		{agent, map[string]any{"ids": ids(51), "confirm_count": 50}, nil, "confirm_count_mismatch"},
		{agent, map[string]any{"ids": ids(51), "confirm_count": 51}, nil, ""},
		{agent, map[string]any{"ids": ids(1001), "confirm_count": 1001}, nil, "too_many_ids"},
		{human, map[string]any{"ids": ids(1001)}, nil, "too_many_ids"},
		{human, map[string]any{"ids": ids(500)}, nil, ""},
		{agent, map[string]any{"ids": ids(1), "to_role": "inbox"}, &archive, "policy_forbids"},
		{agent, map[string]any{"ids": ids(1), "to_role": "all"}, &archive, ""},
	} {
		ran = 0
		au := &audit.Memory{}
		x := &Executor{Registry: reg, Audit: au, Rules: c.rules}
		b, _ := json.Marshal(c.in)
		dry := false
		_, err := x.Execute(context.Background(), Request{Key: c.key, Actor: audit.ActorAgent, Operation: "t.move", Input: b, DryRun: &dry})
		checkPolicyInputResult(t, au.Events(), ran, err, c.want)
	}
}
