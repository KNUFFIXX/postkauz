package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/crash"
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
				rules := policy.DefaultRules()
				x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: au, Rules: &rules}
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
	def := policy.DefaultRules()
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
		rules := c.rules
		if rules == nil {
			rules = &def
		}
		x := &Executor{Registry: reg, Audit: au, Rules: rules}
		b, _ := json.Marshal(c.in)
		dry := false
		_, err := x.Execute(context.Background(), Request{Key: c.key, Actor: audit.ActorAgent, Operation: "t.move", Input: b, DryRun: &dry})
		checkPolicyInputResult(t, au.Events(), ran, err, c.want)
	}
}

// T1b (A03 Nachtrag 26.10.): Redact sieht nur Eingaben, die das Schema
// bestanden haben. Eine ungültige Eingabe ist genau ein Eintrag error mit
// Grund invalid_input und leeren Parametern; der Handler läuft nicht.
func TestRedactOnlyAfterSchemaCheck(t *testing.T) {
	redacted, ran := 0, 0
	op := newTestOp("t.redact", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { ran++; return map[string]any{}, nil })
	op.InputSchema = json.RawMessage(`{"type":"object","properties":{"id":{"type":"integer"}},"additionalProperties":false}`)
	op.Redact = func(raw json.RawMessage) (json.RawMessage, []string) {
		redacted++
		return raw, []string{"message:x"} // gäbe die Eingabe ungeprüft ins Audit
	}
	reg := NewRegistry()
	if err := reg.Register(op); err != nil {
		t.Fatal(err)
	}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au}
	_, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "key:r", Scopes: policy.Roles["reader"]}, Actor: audit.ActorAgent,
		Operation: "t.redact", Input: json.RawMessage(`{"id":"KANARIE-Betreff"}`)})
	evs := au.Events()
	if err == nil || ran != 0 || redacted != 0 || len(evs) != 1 || evs[0].Result != audit.ResultError || evs[0].Reason != "invalid_input" ||
		string(evs[0].ParamsRedacted) != `{}` || len(evs[0].Targets) != 0 {
		t.Fatalf("err=%v ran=%d redacted=%d events=%+v", err, ran, redacted, evs)
	}
	if _, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "key:r", Scopes: policy.Roles["reader"]}, Actor: audit.ActorAgent,
		Operation: "t.redact", Input: json.RawMessage(`{"id":5}`)}); err != nil || redacted != 1 || ran != 1 {
		t.Fatalf("valid input: err=%v redacted=%d ran=%d", err, redacted, ran)
	}
}

// T2 Punkt 7: Ohne Regeln der Policy-Datei scheitert eine Operation mit
// PolicyInput (fail closed, ein Eintrag error policy_rules_missing), und
// ein Panic in PolicyInput ist ein bereinigter Fehler wie im Handler.
func TestExecutorPolicyInputFailsClosed(t *testing.T) {
	ran := 0
	op := newTestOp("t.mark", policy.ScopeMailTag, policy.R2, func(context.Context, Call) (any, error) { ran++; return map[string]any{}, nil })
	op.PolicyInput = func(context.Context, json.RawMessage) (policy.Facts, error) { return policy.Facts{Targets: 1}, nil }
	boom := newTestOp("t.boom", policy.ScopeMailTag, policy.R2, func(context.Context, Call) (any, error) { ran++; return map[string]any{}, nil })
	boom.PolicyInput = func(context.Context, json.RawMessage) (policy.Facts, error) { panic("KANARIE-Betreff in panic") }
	reg := NewRegistry()
	for _, o := range []Operation{op, boom} {
		if err := reg.Register(o); err != nil {
			t.Fatal(err)
		}
	}
	key := policy.Key{ID: "key:a", Scopes: policy.Roles["assistant"]}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au}
	if _, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "t.mark", Input: json.RawMessage(`{}`)}); err == nil ||
		ran != 0 || len(au.Events()) != 1 || au.Events()[0].Reason != "policy_rules_missing" || au.Events()[0].Result != audit.ResultError {
		t.Fatalf("without rules: err=%v ran=%d events=%+v", err, ran, au.Events())
	}
	rules := policy.DefaultRules()
	au = &audit.Memory{}
	panics := 0
	x = &Executor{Registry: reg, Audit: au, Rules: &rules, OnPanic: func(string, crash.Report) { panics++ }}
	_, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "t.boom", Input: json.RawMessage(`{}`)})
	if !errors.Is(err, ErrInternal) || ran != 0 || panics != 1 || len(au.Events()) != 1 || au.Events()[0].Reason != "panic" ||
		strings.Contains(err.Error(), "KANARIE") {
		t.Fatalf("panic: err=%v ran=%d panics=%d events=%+v", err, ran, panics, au.Events())
	}
}

// E3, A14: Eine R3-Operation mit ApprovalRequest legt bei needs_approval
// nur den Antrag an (der Handler läuft, Abschluss needs_approval, Ausgabe
// ohne Fehler), auch für den Menschen; im Trockenlauf ebenso
// needs_approval, der Handler bekommt DryRun. Ohne ApprovalRequest läuft
// bei R3 kein Handler.
func TestExecutorApprovalRequest(t *testing.T) {
	var calls []bool
	req := newTestOp("t.trash", policy.ScopeMailTrash, policy.R3, func(_ context.Context, c Call) (any, error) {
		calls = append(calls, c.DryRun)
		return map[string]any{"status": "needs_approval"}, nil
	})
	req.ApprovalRequest = true
	plain := newTestOp("t.purge", policy.ScopeMailTrash, policy.R3, func(_ context.Context, c Call) (any, error) {
		calls = append(calls, c.DryRun)
		return map[string]any{}, nil
	})
	reg := NewRegistry()
	for _, o := range []Operation{req, plain} {
		if err := reg.Register(o); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	for _, c := range []struct {
		key policy.Key
		dry *bool
	}{
		{policy.Key{ID: "key:op", Scopes: policy.Roles["operator"]}, &no},
		{policy.Key{ID: "key:op", Scopes: policy.Roles["operator"], DryRunDefault: true}, nil},
		{policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}, &no},
		{policy.Key{ID: "cli", Human: true, Scopes: policy.Roles["admin"]}, &yes},
	} {
		calls = nil
		au := &audit.Memory{}
		x := &Executor{Registry: reg, Audit: au}
		res, err := x.Execute(context.Background(), Request{Key: c.key, Actor: audit.ActorAgent, Operation: "t.trash", DryRun: c.dry})
		wantDry := policy.EffectiveDryRun(c.key, c.dry)
		evs := au.Events()
		if err != nil || res.Decision.Outcome != policy.NeedsApproval || res.Output == nil || len(calls) != 1 || calls[0] != wantDry ||
			len(evs) != 2 || evs[0].Result != audit.ResultStarted || evs[1].Result != audit.ResultNeedsApproval || evs[1].RefID != res.AuditID {
			t.Fatalf("key %s dry %v: res=%+v err=%v calls=%v events=%+v", c.key.ID, c.dry, res, err, calls, evs)
		}
		calls = nil
		au = &audit.Memory{}
		x = &Executor{Registry: reg, Audit: au}
		if _, err := x.Execute(context.Background(), Request{Key: c.key, Actor: audit.ActorAgent, Operation: "t.purge", DryRun: c.dry}); !errors.Is(err, ErrNeedsApproval) ||
			len(calls) != 0 || len(au.Events()) != 1 {
			t.Fatalf("plain R3 ran: err=%v calls=%v", err, calls)
		}
	}
}
