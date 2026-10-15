package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

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

// --- Vertragstest (ADR-004) ----------------------------------------------------
//
// Jede Operation des Katalogs läuft mit jeder Rolle und jeder Trockenlauf-
// Variante durch den Executor. Geprüft wird die Kette Scope → Policy → Audit →
// Handler: Die Policy entscheidet (Erwartung unabhängig von policy.Check
// formuliert), genau ein Audit-Eintrag entsteht, der Handler läuft nur bei
// Erlaubnis, ein Trockenlauf (und jede R0-Operation) verändert nichts, und
// die Ausgabe trägt die Pflichtfelder ihres OutputSchema. Eine neue Operation
// im Katalog ohne Zeile in contractInputs lässt den Test scheitern.

// contractInputs: je Operation eine gültige Eingabe, mit der der Handler bei
// Erlaubnis bis zu seiner Wirkung kommt (Umgebung: newContractEnv).
var contractInputs = map[string]struct {
	input  string
	secret secrets.Secret
}{
	"accounts.list":   {input: `{}`},
	"accounts.add":    {input: `{"name":"neu","host":"127.0.0.1","port":11143,"tls":"none","username":"u@example.invalid","insecure_test_server":true}`, secret: canary},
	"accounts.remove": {input: `{"name":"leer"}`},
	"accounts.test":   {input: `{"name":"demo"}`},
	"sync.run":        {input: `{"account":"demo"}`},
	"mailboxes.list":  {input: `{"account":"demo"}`},
	"messages.get":    {input: `{"message_id":"a@example.org","account":"demo","headers":true}`},
}

// contractRunner zählt Syncs, die schreiben dürften (DryRun=false).
type contractRunner struct{ fakeRunner }

func (c *contractRunner) Sync(ctx context.Context, a store.Account, pw secrets.Secret, o SyncOptions, p func(Progress)) (SyncOutput, error) {
	return c.fakeRunner.Sync(ctx, a, pw, o, p)
}

type contractEnv struct {
	*mirrorEnv
	runner *contractRunner
	deps   Deps
}

// newContractEnv: Konto demo mit Passwort, einem Ordner und einer Nachricht;
// Konto leer ohne Nachrichten (für accounts.remove).
func newContractEnv(t *testing.T) *contractEnv {
	t.Helper()
	m := newMirrorEnv(t)
	m.seed(t)
	in := demoInput()
	in["name"] = "leer"
	if _, err := m.run("accounts.add", in, canary, false); err != nil {
		t.Fatal(err)
	}
	m.prober.res = ProbeResult{Capabilities: []string{"IMAP4rev1"}, SyncStrategy: "uid-scan", CapabilitiesJSON: `{"imap4rev1":true}`}
	r := &contractRunner{}
	return &contractEnv{mirrorEnv: m, runner: r, deps: Deps{Accounts: m.st, Secrets: m.sec, Prober: m.prober,
		Mailboxes: m.st, Messages: m.st, Blobs: mapBlobs{"sha-1": "Message-ID: <a@example.org>\r\nSubject: Hallo\r\n\r\nText"},
		Runner: r}}
}

// fingerprint fasst den lokalen Zustand zusammen: alle Tabellen der
// Datenbank, den Schlüsselbund und schreibende Sync-Aufrufe.
func (e *contractEnv) fingerprint(t *testing.T) string {
	t.Helper()
	db := e.st.DBForTest()
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	_ = rows.Close()
	var b strings.Builder
	for _, tb := range tables {
		r, err := db.Query(`SELECT * FROM "` + tb + `"`)
		if err != nil {
			continue // virtuelle Hilfstabellen ohne SELECT *
		}
		cols, _ := r.Columns()
		for r.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = r.Scan(ptrs...)
			fmt.Fprintf(&b, "%s%v\n", tb, vals)
		}
		_ = r.Close()
	}
	e.sec.mu.Lock()
	keys := make([]string, 0, len(e.sec.m))
	for k, v := range e.sec.m {
		keys = append(keys, k+"="+string(v))
	}
	e.sec.mu.Unlock()
	sort.Strings(keys)
	fmt.Fprintf(&b, "secrets %v\n", keys)
	if e.runner.acct != "" && !e.runner.got.DryRun {
		b.WriteString("sync wrote\n")
	}
	return b.String()
}

// spyRegistry registriert den Katalog mit Handlern, die ihren Aufruf melden.
func spyRegistry(t *testing.T, d Deps, ran map[string]int) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, op := range Catalog(d) {
		h, name := op.Handler, op.Name
		op.Handler = func(ctx context.Context, c Call) (any, error) {
			ran[name]++
			return h(ctx, c)
		}
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// requiredFields liest die Pflichtfelder der obersten Ebene eines Schemas.
func requiredFields(t *testing.T, schema json.RawMessage) (required []string, props map[string]any) {
	t.Helper()
	var s struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		t.Fatal(err)
	}
	return s.Required, s.Properties
}

func TestContractEveryOperationRoleAndDryRun(t *testing.T) {
	ops := Catalog(Deps{})
	names := map[string]bool{}
	for _, op := range ops {
		names[op.Name] = true
		if _, ok := contractInputs[op.Name]; !ok {
			t.Errorf("operation %s has no row in contractInputs; add a valid sample input", op.Name)
		}
	}
	for n := range contractInputs {
		if !names[n] {
			t.Errorf("contractInputs has a row for %s, which is not in the catalog", n)
		}
	}
	if reg, err := NewDefaultRegistry(Deps{}); err != nil || len(reg.List()) != len(ops) {
		t.Fatalf("NewDefaultRegistry: %v", err)
	}

	roles := []string{"none"}
	for r := range policy.Roles {
		roles = append(roles, r)
	}
	sort.Strings(roles)
	yes, no := true, false
	modes := []struct {
		name       string
		flag       *bool
		keyDefault bool
	}{
		{"default", nil, false},
		{"dry-run=false", &no, false},
		{"dry-run", &yes, false},
		{"key-default-dry", nil, true},
		{"key-default-dry+dry-run=false", &no, true},
	}

	for _, op := range ops {
		row := contractInputs[op.Name]
		for _, role := range roles {
			for _, mode := range modes {
				t.Run(op.Name+"/"+role+"/"+mode.name, func(t *testing.T) {
					e := newContractEnv(t)
					ran := map[string]int{}
					au := &audit.Memory{}
					x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: au}
					key := policy.Key{ID: "key-" + role, Scopes: policy.Roles[role], DryRunDefault: mode.keyDefault}

					// Erwartung, unabhängig von policy.Check formuliert
					// (docs/sicherheit.md, Abschnitt 2).
					dry := mode.keyDefault
					if mode.flag != nil {
						dry = *mode.flag
					}
					effDry := dry && op.Risk > policy.R0
					want := policy.Allow
					switch {
					case !slices.Contains(key.Scopes, op.Scope):
						want = policy.Deny
					case op.Risk == policy.R3 && !dry:
						want = policy.NeedsApproval
					}

					before := e.fingerprint(t)
					res, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent,
						Operation: op.Name, Input: json.RawMessage(row.input), DryRun: mode.flag, Secret: row.secret})

					// Policy
					switch want {
					case policy.Deny:
						if !errors.Is(err, ErrDenied) {
							t.Fatalf("want denied, got err=%v", err)
						}
					case policy.NeedsApproval:
						if !errors.Is(err, ErrNeedsApproval) {
							t.Fatalf("want needs_approval, got err=%v", err)
						}
					default:
						if err != nil {
							t.Fatalf("want allowed, got err=%v", err)
						}
						if res.Decision.DryRun != effDry {
							t.Fatalf("decision dry_run=%v, want %v", res.Decision.DryRun, effDry)
						}
					}

					// Handler nur bei Erlaubnis
					if got, wantRan := ran[op.Name], want == policy.Allow; (got == 1) != wantRan || got > 1 {
						t.Fatalf("handler ran %d times, want ran=%v", got, wantRan)
					}

					// Audit: genau ein Eintrag mit passendem Ergebnis, ohne Geheimnis
					evs := au.Events()
					if len(evs) != 1 {
						t.Fatalf("audit events = %d, want 1: %+v", len(evs), evs)
					}
					ev := evs[0]
					wantResult := map[policy.Outcome]audit.Result{policy.Allow: audit.ResultOK,
						policy.Deny: audit.ResultDenied, policy.NeedsApproval: audit.ResultNeedsApproval}[want]
					if ev.Operation != op.Name || ev.Scope != string(op.Scope) || ev.KeyID != key.ID ||
						ev.ActorType != audit.ActorAgent || ev.Result != wantResult {
						t.Fatalf("audit event = %+v, want result %s", ev, wantResult)
					}
					if want == policy.Allow && ev.DryRun != effDry {
						t.Fatalf("audit dry_run=%v, want %v", ev.DryRun, effDry)
					}
					if b, _ := json.Marshal(ev); strings.Contains(string(b), canary) {
						t.Fatalf("secret in audit event: %s", b)
					}

					// Wirkung: Trockenlauf, R0 und Ablehnung verändern nichts;
					// eine erlaubte schreibende Operation muss wirken (sonst
					// prüft die Beispiel-Eingabe den Trockenlauf nicht).
					after := e.fingerprint(t)
					writes := want == policy.Allow && !effDry && op.Risk > policy.R0
					if !writes && after != before {
						t.Fatalf("state changed although nothing may be written (dry=%v, risk %s)", effDry, op.Risk)
					}
					if writes && after == before {
						t.Fatalf("allowed %s operation without dry run changed nothing; fix the sample input", op.Risk)
					}

					// Ausgabe: Pflichtfelder des OutputSchema, dry_run wie entschieden
					if want != policy.Allow {
						return
					}
					b, err := json.Marshal(res.Output)
					if err != nil {
						t.Fatal(err)
					}
					if strings.Contains(string(b), canary) {
						t.Fatalf("secret in output: %s", b)
					}
					var out map[string]any
					if err := json.Unmarshal(b, &out); err != nil {
						t.Fatalf("output is not a JSON object: %s", b)
					}
					req, props := requiredFields(t, op.OutputSchema)
					for _, f := range req {
						if _, ok := out[f]; !ok {
							t.Fatalf("output lacks required field %q: %s", f, b)
						}
					}
					if _, ok := props["dry_run"]; ok && out["dry_run"] != effDry {
						t.Fatalf("output dry_run=%v, want %v", out["dry_run"], effDry)
					}
				})
			}
		}
	}
}

// Fail closed für jede Operation: Ohne Audit läuft kein Handler, nichts ändert sich.
func TestContractEveryOperationFailsClosed(t *testing.T) {
	for _, op := range Catalog(Deps{}) {
		t.Run(op.Name, func(t *testing.T) {
			e := newContractEnv(t)
			ran := map[string]int{}
			x := &Executor{Registry: spyRegistry(t, e.deps, ran), Audit: failingAudit{}}
			row := contractInputs[op.Name]
			before := e.fingerprint(t)
			_, err := x.Execute(context.Background(), Request{Key: policy.Key{ID: "adm", Scopes: policy.Roles["admin"]},
				Operation: op.Name, Input: json.RawMessage(row.input), Secret: row.secret})
			if err == nil || ran[op.Name] != 0 || e.fingerprint(t) != before {
				t.Fatalf("err=%v handler ran=%d changed=%v", err, ran[op.Name], e.fingerprint(t) != before)
			}
		})
	}
}
