package ops

import (
	"context"
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// countingRemote zählt Kommandos an den Server (actions.Remote).
type countingRemote struct{ calls int }

func (c *countingRemote) FetchHeaders(context.Context, string, uint32, []provider.UID) ([]provider.Header, error) {
	c.calls++
	return nil, nil
}

func (c *countingRemote) SetFlags(context.Context, string, uint32, []provider.UID, provider.FlagPatch) error {
	c.calls++
	return nil
}

func (c *countingRemote) Move(context.Context, string, uint32, []provider.UID, string) (provider.MoveResult, error) {
	c.calls++
	return provider.MoveResult{}, nil
}

func scopeStrings(ss []policy.Scope) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = string(s)
	}
	return out
}

// V8 der Nachbesserung T2 (Betreiber 28.10.2026): Ein Schlüssel verschiebt
// nie in Spam, Entwürfe oder Gesendet, auch nicht per Ordnerpfad; der
// Mensch ohne Schlüssel darf es weiter.
func TestMoveToJunkDraftsSentIsHumanOnly(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	ctx := context.Background()
	acct, _ := e.st.AccountByName(ctx, "demo")
	boxes, _ := e.st.Mailboxes(ctx, acct.ID)
	var specs []store.MailboxSpec
	for _, b := range boxes {
		specs = append(specs, store.MailboxSpec{Path: b.Path, Name: b.Name, Role: b.Role})
	}
	specs = append(specs, store.MailboxSpec{Path: "Spam", Name: "Spam", Role: "junk"}, store.MailboxSpec{Path: "Gesendet", Name: "Gesendet", Role: "sent"})
	if _, _, err := e.st.SyncMailboxes(ctx, acct.ID, specs); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{`"role":"junk"`, `"role":"drafts"`, `"role":"sent"`, `"mailbox":"Spam"`, `"mailbox":"Entwuerfe"`, `"mailbox":"Gesendet"`} {
		_, err := execJSON(t, x, operatorKey, "messages.move", `{"account":"demo","ids":[1],`+in+`}`, false)
		var pe *PolicyError
		if !errors.As(err, &pe) || pe.Decision.Reason != "target_role_human_only" {
			t.Fatalf("key moves to %s: %v", in, err)
		}
		if _, err := execJSON(t, x, humanKey, "messages.move", `{"account":"demo","ids":[1],`+in+`}`, true); err != nil {
			t.Fatalf("human may not move to %s: %v", in, err)
		}
	}
	if _, err := execJSON(t, x, operatorKey, "messages.move", `{"account":"demo","ids":[1],"role":"archive"}`, true); err != nil {
		t.Fatalf("key may not move to the archive: %v", err)
	}
}

// V3 der Nachbesserung T2: Die Ausführung prüft eine eingereihte Aktion
// eines Schlüssels mit den Regeln der Policy des ausführenden Prozesses.
// Verengt policy.toml [move] agent_may, [delete] agent_may oder
// [keys.<name>] nach dem Antrag, läuft die Aktion nicht (Grund der Policy),
// auch nach einer Freigabe; der Mensch ist nicht betroffen.
func TestActionOriginCheckAppliesCurrentPolicy(t *testing.T) {
	e := newContractEnv(t)
	ctx := context.Background()
	if _, err := e.st.CreateAPIKey(ctx, store.APIKey{Name: "agent", KeyHash: HashKey("y"), Role: "operator",
		Scopes: scopeStrings(policy.Roles["operator"])}); err != nil {
		t.Fatal(err)
	}
	move := actions.Origin{KeyID: "key:agent", Scope: policy.ScopeMailMove,
		Facts: policy.Facts{Action: policy.ActionMove, Targets: 1, TargetRole: "custom"}}
	trash := actions.Origin{KeyID: "key:agent", Scope: policy.ScopeMailTrash, Approved: true,
		Facts: policy.Facts{Action: policy.ActionTrash, Targets: 1, TargetRole: "trash"}}
	open := policy.DefaultRules()
	narrowAll := policy.Rules{MoveAgentMay: "archive_only", DeleteAgentMay: "none", BulkConfirmThreshold: 50}
	narrowKey := policy.DefaultRules()
	narrowKey.Keys = map[string]policy.Rules{"agent": {MoveAgentMay: "none", DeleteAgentMay: "none"}}
	for _, tc := range []struct {
		name  string
		rules *policy.Rules
		o     actions.Origin
		want  string
	}{
		{"move, defaults", &open, move, ""},
		{"move, [move] archive_only", &narrowAll, move, "policy_forbids"},
		{"move, [keys.agent] none", &narrowKey, move, "policy_forbids"},
		{"trash approved, [delete] none", &narrowAll, trash, "policy_forbids"},
		{"trash approved, defaults", &open, trash, ""},
		{"move to junk", &open, actions.Origin{KeyID: "key:agent", Scope: policy.ScopeMailMove,
			Facts: policy.Facts{Action: policy.ActionMove, Targets: 1, TargetRole: "junk"}}, "target_role_human_only"},
		{"bulk without confirm", &open, actions.Origin{KeyID: "key:agent", Scope: policy.ScopeMailMove,
			Facts: policy.Facts{Action: policy.ActionMove, Targets: 51, TargetRole: "archive"}}, "confirm_count_required"},
		{"no rules", nil, move, "policy_rules_missing"},
		{"human, narrowed", &narrowKey, actions.Origin{KeyID: "cli", Scope: policy.ScopeMailMove, Facts: move.Facts}, ""},
	} {
		got, err := ActionOriginCheck(e.st, tc.rules)(ctx, tc.o)
		if err != nil || got != tc.want {
			t.Errorf("%s: reason %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

// V3 Ende zu Ende im Runner: Eine Aktion, die der Schlüssel unter der alten
// Policy angelegt hat, scheitert nach der Verengung mit dem Grund, ohne
// Kommando an den Server.
func TestRunnerRefusesActionAfterPolicyNarrowed(t *testing.T) {
	e := newContractEnv(t)
	x, _ := actionExec(t, e)
	ctx := context.Background()
	if _, err := e.st.CreateAPIKey(ctx, store.APIKey{Name: "op", KeyHash: HashKey("z"), Role: "operator",
		Scopes: scopeStrings(policy.Roles["operator"])}); err != nil {
		t.Fatal(err)
	}
	res, err := execJSON(t, x, operatorKey, "messages.move", `{"account":"demo","ids":[1],"role":"archive"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	id := res.Output.(ActionOutput).ActionID
	// Die Aktion aus der Vertragsumgebung (Papierkorb, key:agent) stört
	// hier nicht: Sie wartet auf die Freigabe.
	narrow := policy.DefaultRules()
	narrow.Keys = map[string]policy.Rules{"op": {MoveAgentMay: "none"}}
	remote := &countingRemote{}
	acct, _ := e.st.AccountByName(ctx, "demo")
	r := &actions.Runner{Store: e.st, Origin: ActionOriginCheck(e.st, &narrow)}
	if _, err := r.Run(ctx, acct.ID, remote); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.Action(ctx, id)
	if a.State != store.ActionFailed || a.Error != "policy_forbids" || remote.calls != 0 {
		t.Fatalf("action = %s/%s, %d commands", a.State, a.Error, remote.calls)
	}
	if a.Targets[0].Result != store.TargetFailed {
		t.Fatalf("targets = %+v", a.Targets)
	}
}
