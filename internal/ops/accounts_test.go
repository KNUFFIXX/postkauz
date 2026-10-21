package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

const canary = "CANARY-pw-51d0e2b4-do-not-leak"

// memSecrets ist ein Schlüsselbund im Speicher (Tests).
type memSecrets struct {
	mu   sync.Mutex
	m    map[string]secrets.Secret
	down bool
}

func newMemSecrets() *memSecrets { return &memSecrets{m: map[string]secrets.Secret{}} }

func (s *memSecrets) Get(ref string) (secrets.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[ref]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (s *memSecrets) Set(ref string, v secrets.Secret) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.down {
		return fmt.Errorf("%w: test", secrets.ErrUnavailable)
	}
	s.m[ref] = v
	return nil
}
func (s *memSecrets) Delete(ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.m[ref]; !ok {
		return secrets.ErrNotFound
	}
	delete(s.m, ref)
	return nil
}
func (s *memSecrets) Status() secrets.Status {
	return secrets.Status{Backend: "keyring", Available: !s.down}
}

type fakeProber struct {
	res  ProbeResult
	err  error
	seen secrets.Secret
}

func (f *fakeProber) Probe(_ context.Context, _ store.Account, pw secrets.Secret) (ProbeResult, error) {
	f.seen = pw
	return f.res, f.err
}

type accountsEnv struct {
	st     *store.SQLite
	sec    *memSecrets
	prober *fakeProber
	au     *audit.Memory
	x      *Executor
}

func newAccountsEnv(t *testing.T) *accountsEnv {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	e := &accountsEnv{st: st, sec: newMemSecrets(), prober: &fakeProber{}, au: &audit.Memory{}}
	reg := NewRegistry()
	for _, op := range []Operation{AccountsList(st), AccountsAdd(st, e.sec), AccountsRemove(st, e.sec), AccountsTest(st, e.sec, e.prober)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	e.x = &Executor{Registry: reg, Audit: e.au}
	return e
}

// ref liefert den Schlüsselbund-Verweis eines Kontos in dieser Datenbank
// (account/<datadir-id>/<name>, S2.0/A2).
func (e *accountsEnv) ref(t *testing.T, name string) string {
	t.Helper()
	id, err := e.st.DataDirID(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return secrets.AccountRef(id, name)
}

// adminKey ist der Mensch an der CLI ohne --key (wie cli.humanKey).
var adminKey = policy.Key{ID: "cli", Scopes: policy.Roles["admin"], Human: true}

func (e *accountsEnv) run(op string, input any, secret secrets.Secret, dry bool) (any, error) {
	b, _ := json.Marshal(input)
	req := Request{Key: adminKey, Actor: audit.ActorHuman, Operation: op, Input: b, Secret: secret}
	if dry {
		req.DryRun = &dry
	}
	res, err := e.x.Execute(context.Background(), req)
	return res.Output, err
}

func demoInput() map[string]any {
	return map[string]any{"name": "demo", "host": "127.0.0.1", "port": 11143, "tls": "none",
		"username": "tester@example.invalid", "insecure_test_server": true}
}

func TestAccountsAddListRemove(t *testing.T) {
	e := newAccountsEnv(t)
	out, err := e.run("accounts.list", map[string]any{}, "", false)
	if b, _ := json.Marshal(out); err != nil || string(b) != `{"accounts":[]}` {
		t.Fatalf("empty list: %s %v", b, err)
	}

	// Trockenlauf: nichts gespeichert.
	out, err = e.run("accounts.add", demoInput(), canary, true)
	if err != nil || !out.(AccountsAddOutput).DryRun {
		t.Fatalf("dry run: %+v %v", out, err)
	}
	if len(e.sec.m) != 0 {
		t.Fatal("dry run stored a secret")
	}
	if l, _ := e.st.ListAccounts(context.Background()); len(l) != 0 {
		t.Fatal("dry run created an account")
	}

	out, err = e.run("accounts.add", demoInput(), canary, false)
	if err != nil {
		t.Fatal(err)
	}
	add := out.(AccountsAddOutput)
	if add.Account.ID == 0 || add.Account.CredentialRef != e.ref(t, "demo") || add.Account.SecretSource != "keyring" || add.Account.Email != "tester@example.invalid" {
		t.Fatalf("add output: %+v", add)
	}
	if v, err := e.sec.Get(e.ref(t, "demo")); err != nil || v.Reveal() != canary {
		t.Fatalf("secret not stored: %v", err)
	}
	if _, err := e.run("accounts.add", demoInput(), "other", false); !errors.Is(err, store.ErrAccountExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if v, _ := e.sec.Get(e.ref(t, "demo")); v.Reveal() != canary {
		t.Fatal("duplicate add overwrote the secret")
	}

	out, err = e.run("accounts.list", map[string]any{}, "", false)
	if l := out.(AccountsListOutput).Accounts; err != nil || len(l) != 1 || l[0].Name != "demo" || l[0].Provider != "imap" {
		t.Fatalf("list: %+v %v", out, err)
	}

	// Entfernen: Trockenlauf, dann echt.
	out, err = e.run("accounts.remove", map[string]any{"name": "demo"}, "", true)
	if err != nil || !out.(AccountsRemoveOutput).DryRun {
		t.Fatalf("remove dry: %+v %v", out, err)
	}
	out, err = e.run("accounts.remove", map[string]any{"name": "demo"}, "", false)
	if err != nil || !out.(AccountsRemoveOutput).SecretDeleted {
		t.Fatalf("remove: %+v %v", out, err)
	}
	if _, err := e.sec.Get(e.ref(t, "demo")); !errors.Is(err, secrets.ErrNotFound) {
		t.Fatal("secret still stored after remove")
	}
	if _, err := e.run("accounts.remove", map[string]any{"name": "demo"}, "", false); !errors.Is(err, store.ErrAccountNotFound) {
		t.Fatalf("remove missing: %v", err)
	}

	// Kanarienwert: nie im Audit (Parameter nur Feldnamen).
	for _, ev := range e.au.Events() {
		b, _ := json.Marshal(ev)
		if strings.Contains(string(b), canary) || strings.Contains(string(ev.ParamsRedacted), "127.0.0.1") {
			t.Fatalf("audit leaks values: %s", b)
		}
	}
	if ev := e.au.Events()[2]; ev.Operation != "accounts.add" || len(ev.Targets) != 1 || ev.Targets[0] != "account:demo" || !strings.Contains(string(ev.ParamsRedacted), `"host"`) {
		t.Fatalf("add audit: %+v", ev)
	}
}

func TestAccountsAddValidation(t *testing.T) {
	e := newAccountsEnv(t)
	cases := map[string]func(m map[string]any){
		"bad name":                 func(m map[string]any) { m["name"] = "Demo_1" },
		"plaintext without flag":   func(m map[string]any) { delete(m, "insecure_test_server") },
		"insecure on remote host":  func(m map[string]any) { m["host"] = "imap.example.org"; m["tls"] = "tls" },
		"oauth2 not yet":           func(m map[string]any) { m["auth_type"] = "oauth2" },
		"bad tls":                  func(m map[string]any) { m["tls"] = "ssl" },
		"bad port":                 func(m map[string]any) { m["port"] = 0 },
		"missing user":             func(m map[string]any) { m["username"] = " " },
		"unknown field":            func(m map[string]any) { m["password"] = canary },
		"env without env variable": func(m map[string]any) { m["password_from_env"] = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := demoInput()
			mutate(in)
			pw := secrets.Secret(canary)
			if in["password_from_env"] == true {
				pw = ""
			}
			_, err := e.run("accounts.add", in, pw, false)
			var ie *InputError
			if !errors.As(err, &ie) {
				t.Fatalf("want InputError, got %v", err)
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatal("error leaks the secret")
			}
		})
	}
	if _, err := e.run("accounts.add", demoInput(), "", false); err == nil {
		t.Fatal("missing password must fail")
	}
	if l, _ := e.st.ListAccounts(context.Background()); len(l) != 0 || len(e.sec.m) != 0 {
		t.Fatal("failed adds left state behind")
	}
}

func TestAccountsAddPasswordFromEnv(t *testing.T) {
	e := newAccountsEnv(t)
	// Umgebung über den Speicher legen wie im CLI (secrets.WithEnv).
	reg := NewRegistry()
	sec := secrets.WithEnv(e.sec)
	_ = reg.Register(AccountsAdd(e.st, sec))
	x := &Executor{Registry: reg, Audit: e.au}
	t.Setenv(secrets.EnvName(e.ref(t, "demo")), canary)
	in := demoInput()
	in["password_from_env"] = true
	b, _ := json.Marshal(in)
	res, err := x.Execute(context.Background(), Request{Key: adminKey, Operation: "accounts.add", Input: b})
	if err != nil || res.Output.(AccountsAddOutput).Account.SecretSource != "environment" {
		t.Fatalf("env add: %+v %v", res.Output, err)
	}
	if len(e.sec.m) != 0 {
		t.Fatal("password from env must not be stored")
	}
}

func TestAccountsAddKeyringUnavailable(t *testing.T) {
	e := newAccountsEnv(t)
	e.sec.down = true
	if _, err := e.run("accounts.add", demoInput(), canary, false); !errors.Is(err, secrets.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if l, _ := e.st.ListAccounts(context.Background()); len(l) != 0 {
		t.Fatal("account created without a stored password")
	}
}

func TestAccountsRemoveRefusesWithArchive(t *testing.T) {
	e := newAccountsEnv(t)
	if _, err := e.run("accounts.add", demoInput(), canary, false); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.AccountByName(context.Background(), "demo")
	if err := insertMessage(e.st, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.remove", map[string]any{"name": "demo"}, "", false); err == nil || !strings.Contains(err.Error(), "archived messages") {
		t.Fatalf("remove with archive: %v", err)
	}
	if _, err := e.sec.Get(e.ref(t, "demo")); err != nil {
		t.Fatal("secret deleted although remove was refused")
	}
}

func TestAccountsTest(t *testing.T) {
	e := newAccountsEnv(t)
	if _, err := e.run("accounts.test", map[string]any{"name": "demo"}, "", false); !errors.Is(err, store.ErrAccountNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
	if _, err := e.run("accounts.add", demoInput(), canary, false); err != nil {
		t.Fatal(err)
	}
	e.prober.res = ProbeResult{Capabilities: []string{"IDLE", "IMAP4rev1"}, SyncStrategy: "uid-scan",
		Mailboxes: []ProbeMailbox{{Path: "INBOX", Role: "inbox"}}, CapabilitiesJSON: `{"Idle":true}`}
	out, err := e.run("accounts.test", map[string]any{"name": "demo"}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	res := out.(AccountsTestOutput)
	if res.Login != "ok" || len(res.Mailboxes) != 1 || e.prober.seen.Reveal() != canary {
		t.Fatalf("test output: %+v", res)
	}
	a, _ := e.st.AccountByName(context.Background(), "demo")
	if a.Status != "ok" || a.Capabilities != `{"Idle":true}` {
		t.Fatalf("probe not stored: %+v", a)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), canary) || strings.Contains(string(b), "Idle\":true") {
		t.Fatalf("output: %s", b)
	}

	// Serverfehler mit (absurderweise) dem Passwort im Text: geschwärzt.
	e.prober.err = fmt.Errorf("login rejected for %s: %w", canary, errors.New("NO"))
	_, err = e.run("accounts.test", map[string]any{"name": "demo"}, "", false)
	if err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("probe error: %v", err)
	}
	a, _ = e.st.AccountByName(context.Background(), "demo")
	if a.Status != "error" || strings.Contains(a.LastError, canary) {
		t.Fatalf("error status: %+v", a)
	}

	// Ohne gespeichertes Passwort: klarer Fehler mit Verweis und Variable.
	_ = e.sec.Delete(e.ref(t, "demo"))
	_, err = e.run("accounts.test", map[string]any{"name": "demo"}, "", false)
	if !errors.Is(err, ErrNoSecret) || !strings.Contains(err.Error(), "POSTKAUZ_PASSWORD_DEMO") {
		t.Fatalf("no secret: %v", err)
	}
}

// Kontenverwaltung verlangt admin; ein Agenten-Schlüssel (assistant)
// erreicht den Handler nie.
func TestAccountAdminOpsDeniedForAgents(t *testing.T) {
	e := newAccountsEnv(t)
	agent := policy.Key{ID: "agent", Scopes: policy.Roles["operator"]}
	for _, op := range []string{"accounts.add", "accounts.remove", "accounts.test"} {
		b, _ := json.Marshal(demoInput())
		_, err := e.x.Execute(context.Background(), Request{Key: agent, Actor: audit.ActorAgent, Operation: op, Input: b, Secret: canary})
		if !errors.Is(err, ErrDenied) {
			t.Errorf("%s: want denied, got %v", op, err)
		}
	}
	if len(e.sec.m) != 0 {
		t.Fatal("denied call changed state")
	}
	// accounts.list ist lesend und für reader erlaubt.
	if _, err := e.x.Execute(context.Background(), Request{Key: policy.Key{Scopes: policy.Roles["reader"]}, Operation: "accounts.list"}); err != nil {
		t.Fatal(err)
	}
}

func insertMessage(st *store.SQLite, accountID int64) error {
	return st.ExecForTest(`INSERT INTO messages(account_id, subject, created_at) VALUES (?, 'x', 1)`, accountID)
}
