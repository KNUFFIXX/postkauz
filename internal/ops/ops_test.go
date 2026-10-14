package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/KNUFFIXX/mailwerk/internal/audit"
	"github.com/KNUFFIXX/mailwerk/internal/policy"
)

type fakeAccounts struct{ list []AccountInfo }

func (f fakeAccounts) ListAccounts(context.Context) ([]AccountInfo, error) { return f.list, nil }

func newTestOp(name string, scope policy.Scope, risk policy.Risk, h Handler) Operation {
	return Operation{
		Name: name, Scope: scope, Risk: risk, Summary: "test",
		InputSchema:  json.RawMessage(`{"type":"object"}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:      h,
	}
}

func TestRegistryRegisterGetList(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"b.two", "a.one"} {
		if err := r.Register(newTestOp(n, policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Register(newTestOp("a.one", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })); err == nil {
		t.Error("duplicate name must be rejected")
	}
	if _, err := r.Get("nope.nope"); !errors.Is(err, ErrUnknownOperation) {
		t.Errorf("Get unknown = %v", err)
	}
	list := r.List()
	if len(list) != 2 || list[0].Name != "a.one" || list[1].Name != "b.two" {
		t.Errorf("List must be sorted by name, got %v", list)
	}
}

func TestOperationValidate(t *testing.T) {
	ok := newTestOp("a.b", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, nil })
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid op rejected: %v", err)
	}
	bad := map[string]func(o *Operation){
		"name without dot":    func(o *Operation) { o.Name = "list" },
		"upper case name":     func(o *Operation) { o.Name = "Accounts.List" },
		"missing scope":       func(o *Operation) { o.Scope = "" },
		"risk out of range":   func(o *Operation) { o.Risk = 9 },
		"missing handler":     func(o *Operation) { o.Handler = nil },
		"missing summary":     func(o *Operation) { o.Summary = "" },
		"input not an object": func(o *Operation) { o.InputSchema = json.RawMessage(`{"type":"string"}`) },
		"output not json":     func(o *Operation) { o.OutputSchema = json.RawMessage(`nope`) },
	}
	for name, mutate := range bad {
		t.Run(name, func(t *testing.T) {
			o := ok
			mutate(&o)
			if err := o.Validate(); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestExecutorChain(t *testing.T) {
	called := 0
	reg := NewRegistry()
	for _, op := range []Operation{
		newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { called++; return map[string]any{"ok": true}, nil }),
		newTestOp("m.move", policy.ScopeMailMove, policy.R2, func(_ context.Context, c Call) (any, error) { called++; return map[string]any{"dry": c.DryRun}, nil }),
		newTestOp("m.trash", policy.ScopeMailTrash, policy.R3, func(context.Context, Call) (any, error) { called++; return nil, nil }),
		newTestOp("m.fail", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { return nil, errors.New("boom") }),
	} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au, Now: func() time.Time { return time.Unix(0, 0) }}
	operator := policy.Key{ID: "op", Scopes: policy.Roles["operator"]}
	reader := policy.Key{ID: "rd", Scopes: policy.Roles["reader"]}
	ctx := context.Background()

	// 1. erlaubt: Handler läuft, Audit hat genau einen Eintrag "ok"
	if _, err := x.Execute(ctx, Request{Key: reader, Actor: audit.ActorAgent, Operation: "m.read"}); err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(au.Events()) != 1 || au.Events()[0].Result != audit.ResultOK {
		t.Fatalf("allowed path: called=%d events=%+v", called, au.Events())
	}

	// 2. verweigert: Handler läuft nicht, Audit "denied"
	_, err := x.Execute(ctx, Request{Key: reader, Actor: audit.ActorAgent, Operation: "m.move"})
	if !errors.Is(err, ErrDenied) || called != 1 {
		t.Fatalf("deny path: err=%v called=%d", err, called)
	}
	if last := au.Events()[len(au.Events())-1]; last.Result != audit.ResultDenied || last.KeyID != "rd" {
		t.Fatalf("deny audit = %+v", last)
	}

	// 3. R3: Freigabe nötig, Handler läuft nicht
	_, err = x.Execute(ctx, Request{Key: operator, Actor: audit.ActorAgent, Operation: "m.trash"})
	if !errors.Is(err, ErrNeedsApproval) || called != 1 {
		t.Fatalf("approval path: err=%v called=%d", err, called)
	}

	// 4. Trockenlauf aus dem Schlüssel-Standard: Handler sieht DryRun=true
	dryKey := policy.Key{ID: "dk", Scopes: policy.Roles["operator"], DryRunDefault: true}
	res, err := x.Execute(ctx, Request{Key: dryKey, Operation: "m.move"})
	if err != nil || res.Output.(map[string]any)["dry"] != true {
		t.Fatalf("dry-run default: res=%+v err=%v", res, err)
	}
	f := false
	res, err = x.Execute(ctx, Request{Key: dryKey, Operation: "m.move", DryRun: &f})
	if err != nil || res.Output.(map[string]any)["dry"] != false {
		t.Fatalf("explicit dry_run=false: res=%+v err=%v", res, err)
	}

	// 5. Handler-Fehler wird durchgereicht und zusätzlich auditiert
	before := len(au.Events())
	if _, err = x.Execute(ctx, Request{Key: reader, Operation: "m.fail"}); err == nil || err.Error() != "boom" {
		t.Fatalf("handler error = %v", err)
	}
	if evs := au.Events(); len(evs) != before+2 || evs[len(evs)-1].Result != audit.ResultError {
		t.Fatalf("error audit missing: %+v", evs[before:])
	}

	// 6. unbekannte Operation
	if _, err = x.Execute(ctx, Request{Key: reader, Operation: "x.y"}); !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("unknown op err = %v", err)
	}
}

type failingAudit struct{}

func (failingAudit) Append(context.Context, audit.Event) error { return errors.New("disk full") }

func TestExecutorFailsClosedWhenAuditFails(t *testing.T) {
	called := false
	reg := NewRegistry()
	_ = reg.Register(newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) { called = true; return nil, nil }))
	x := &Executor{Registry: reg, Audit: failingAudit{}}
	_, err := x.Execute(context.Background(), Request{Key: policy.Key{Scopes: []policy.Scope{policy.ScopeMailRead}}, Operation: "m.read"})
	if err == nil || called {
		t.Fatalf("handler must not run without audit: err=%v called=%v", err, called)
	}
}

func TestAccountsList(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(AccountsList(EmptyAccounts{})); err != nil {
		t.Fatal(err)
	}
	x := &Executor{Registry: reg, Audit: audit.Nop{}}
	key := policy.Key{ID: "k", Scopes: policy.Roles["reader"]}
	res, err := x.Execute(context.Background(), Request{Key: key, Operation: "accounts.list"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.Output)
	if string(b) != `{"accounts":[]}` {
		t.Errorf("empty list must serialize as [] not null, got %s", b)
	}

	reg2 := NewRegistry()
	_ = reg2.Register(AccountsList(fakeAccounts{[]AccountInfo{{ID: 1, Name: "demo", Email: "demo@example.invalid", Provider: "imap"}}}))
	x2 := &Executor{Registry: reg2, Audit: audit.Nop{}}
	res, err = x2.Execute(context.Background(), Request{Key: key, Operation: "accounts.list"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(res.Output)
	var back struct{ Accounts []AccountInfo }
	if err := json.Unmarshal(b, &back); err != nil || len(back.Accounts) != 1 || back.Accounts[0].Name != "demo" {
		t.Errorf("round trip: %s %v", b, err)
	}

	// Ohne Scope: verweigert
	if _, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "none"}, Operation: "accounts.list"}); !errors.Is(err, ErrDenied) {
		t.Errorf("missing scope must be denied, got %v", err)
	}
}
