package ops

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// v3Remote zählt Kommandos an den Server (actions.Remote).
type v3Remote struct{ calls int }

func (c *v3Remote) FetchHeaders(context.Context, string, uint32, []provider.UID) ([]provider.Header, error) {
	c.calls++
	return nil, nil
}

func (c *v3Remote) SetFlags(context.Context, string, uint32, []provider.UID, provider.FlagPatch) error {
	c.calls++
	return nil
}

func (c *v3Remote) Move(context.Context, string, uint32, []provider.UID, string) (provider.MoveResult, error) {
	c.calls++
	return provider.MoveResult{}, nil
}

func v3Scopes(ss []policy.Scope) []string {
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
		Scopes: v3Scopes(policy.Roles["operator"])}); err != nil {
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
		Scopes: v3Scopes(policy.Roles["operator"])}); err != nil {
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
	remote := &v3Remote{}
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

// V16 der Nachbesserung T2: Mit [tags] sync_keywords = true gehen Tags,
// deren Name ein System-Keyword ist (junk, nonjunk, notjunk …), nie als
// Keyword auf den Server; sie bleiben lokal, die Ausgabe nennt sie.
func TestReservedTagsStayLocal(t *testing.T) {
	e := newContractEnv(t)
	e.deps.SyncKeywords = true
	x, _ := actionExec(t, e)
	ctx := context.Background()
	res, err := execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1],"add":["Junk","rechnung"],"remove":["notjunk"]}`, false)
	if err != nil {
		t.Fatal(err)
	}
	kw := res.Output.(TagOutput).Keywords
	if kw.Status != "queued" || kw.ActionID == 0 || !slices.Equal(kw.LocalOnly, []string{"junk", "notjunk"}) {
		t.Fatalf("keywords = %+v", kw)
	}
	a, _ := e.st.Action(ctx, kw.ActionID)
	if !slices.Equal(a.Params.AddKeywords, []string{"rechnung"}) || len(a.Params.RemoveKeywords) != 0 {
		t.Fatalf("keyword action = %+v", a.Params)
	}
	before, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`)
	res, err = execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1],"add":["nonjunk"]}`, false)
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output.(TagOutput)
	if out.Keywords.Status != "local_only" || out.Keywords.ActionID != 0 || out.Added != 1 {
		t.Fatalf("only reserved = %+v", out)
	}
	if after, _ := e.st.QueryIntForTest(`SELECT count(*) FROM actions`); after != before {
		t.Fatalf("an action was queued for a reserved tag: %d -> %d", before, after)
	}
	res, err = execJSON(t, x, assistantKey, "messages.tag", `{"account":"demo","ids":[1],"add":["junk"]}`, true)
	if err != nil || !slices.Equal(res.Output.(TagOutput).Keywords.LocalOnly, []string{"junk"}) {
		t.Fatalf("dry run = %+v %v", res.Output, err)
	}
}
