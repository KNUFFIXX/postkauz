package ops

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// oauthEnv: Kontenoperationen mit Microsoft-Anmeldung gegen den Fake-AS
// (S3.7). sys nimmt die Systemereignisse oauth.refresh auf.
type oauthEnv struct {
	*accountsEnv
	as    *oauthtest.AS
	sys   *audit.Memory
	creds Credentials
}

// tokenProber meldet sich mit der Tokenquelle „an": Er holt ein Token und
// merkt es sich (wie ein IMAP-Server bei XOAUTH2).
type tokenProber struct{ got []string }

func (p *tokenProber) Probe(ctx context.Context, _ store.Account, cred provider.Credential) (ProbeResult, error) {
	if !cred.IsOAuth() {
		return ProbeResult{}, errors.New("want a token source")
	}
	tok, err := cred.Token.Token(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	p.got = append(p.got, tok.Reveal())
	return ProbeResult{Capabilities: []string{"IMAP4rev1", "AUTH=XOAUTH2"}, SyncStrategy: "uid-scan", CapabilitiesJSON: "{}", AuthMechanism: "XOAUTH2"}, nil
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	e := newAccountsEnv(t)
	as := oauthtest.New(t)
	sys := &audit.Memory{}
	creds := Credentials{Secrets: e.sec, Audit: sys,
		OAuth: oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second}}
	reg := NewRegistry()
	for _, op := range []Operation{AccountsAdd(e.st, e.sec), AccountsSet(e.st, e.sec), AccountsRemove(e.st, e.sec),
		AccountsTest(e.st, creds, &tokenProber{}), AccountsLogin(e.st, creds)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	e.x = &Executor{Registry: reg, Audit: e.au}
	return &oauthEnv{accountsEnv: e, as: as, sys: sys, creds: creds}
}

func outlookInput() map[string]any {
	return map[string]any{"name": "outlook", "host": "outlook.office365.com", "port": 993, "tls": "tls",
		"username": "ich@example.org", "auth_type": "oauth2", "client_id": oauthtest.ClientID, "tenant": "consumers"}
}

func TestAccountsAddOAuth2(t *testing.T) {
	e := newOAuthEnv(t)
	res, err := e.run("accounts.add", outlookInput(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	d := res.(AccountsAddOutput).Account
	if d.AuthType != "oauth2" || d.ClientID != oauthtest.ClientID || d.Tenant != "consumers" {
		t.Fatalf("detail = %+v", d)
	}
	a, _ := e.st.AccountByName(context.Background(), "outlook")
	if a.OAuth == nil || a.OAuth.ClientID != oauthtest.ClientID || a.OAuth.Tenant != "consumers" {
		t.Fatalf("stored client = %+v", a.OAuth)
	}
	if len(e.sec.m) != 0 {
		t.Fatalf("add must not store anything in the keyring: %v", e.sec.m)
	}

	in := outlookInput()
	in["name"], in["tenant"] = "ohne-tenant", nil
	delete(in, "tenant")
	if _, err := e.run("accounts.add", in, "", false); err != nil {
		t.Fatalf("default tenant: %v", err)
	}
	if a, _ := e.st.AccountByName(context.Background(), "ohne-tenant"); a.OAuth.Tenant != "common" {
		t.Fatalf("tenant = %q", a.OAuth.Tenant)
	}

	var ie *InputError
	for name, mod := range map[string]func(m map[string]any){
		"no client id":    func(m map[string]any) { delete(m, "client_id") },
		"bad client id":   func(m map[string]any) { m["client_id"] = "meine-app" },
		"bad tenant":      func(m map[string]any) { m["tenant"] = "../x" },
		"password env":    func(m map[string]any) { m["password_from_env"] = true },
		"client password": func(m map[string]any) { m["auth_type"] = "password" },
	} {
		in := outlookInput()
		in["name"] = "x-" + strings.ReplaceAll(name, " ", "-")
		mod(in)
		if _, err := e.run("accounts.add", in, "", false); !errors.As(err, &ie) {
			t.Errorf("%s: want input error, got %v", name, err)
		}
	}
	in = outlookInput()
	in["name"] = "mit-passwort"
	if _, err := e.run("accounts.add", in, canary, false); !errors.As(err, &ie) {
		t.Errorf("oauth2 with password: %v", err)
	}

	// Ohne Schlüsselbund lehnt add ab (Rückfrage R2).
	e.sec.down = true
	in = outlookInput()
	in["name"] = "ohne-keyring"
	if _, err := e.run("accounts.add", in, "", false); !errors.Is(err, secrets.ErrUnavailable) || !strings.Contains(err.Error(), "OS keyring") {
		t.Fatalf("without keyring: %v", err)
	}
}

// Anmeldung Ende zu Ende über den Executor, dann account test mit der
// Tokenquelle: Refresh, Speichern des neuen Refresh-Tokens, Audit
// oauth.refresh; kein Token in Ausgabe, Audit oder Fortschritt.
func TestAccountsLoginTestAndRefreshAudit(t *testing.T) {
	e := newOAuthEnv(t)
	if _, err := e.run("accounts.add", outlookInput(), "", false); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.AccountByName(context.Background(), "outlook")

	// Vor der Anmeldung: kein Token.
	if _, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", false); !errors.Is(err, ErrNoToken) {
		t.Fatalf("test before login: %v", err)
	}
	// Trockenlauf: nichts gespeichert, kein Flow.
	res, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", true)
	if err != nil || res.(AccountsLoginOutput).RefreshToken != "would_store" || len(e.sec.m) != 0 || e.as.Count("/authorize") != 0 {
		t.Fatalf("dry run: %v %+v", err, res)
	}

	var prompts []string
	b, _ := json.Marshal(map[string]any{"name": "outlook"})
	xr, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: "accounts.login", Input: b,
		Progress: func(p Progress) { prompts = append(prompts, p.Event+": "+p.Message) }})
	if err != nil {
		t.Fatal(err)
	}
	out := xr.Output.(AccountsLoginOutput)
	if out.RefreshToken != "stored" || out.Method != "loopback" || out.TokenExpiry == "" || out.CredentialRef != a.CredentialRef {
		t.Fatalf("login = %+v", out)
	}
	if len(prompts) != 1 || !strings.HasPrefix(prompts[0], "oauth_prompt: The browser opens") {
		t.Fatalf("prompts = %v", prompts)
	}
	saved, err := oauth.Load(e.sec, a.CredentialRef)
	if err != nil || !strings.HasPrefix(saved.RefreshToken.Reveal(), "CANARY-tok-refresh") {
		t.Fatalf("saved = %v", err)
	}

	// account test: Quelle erneuert (nur Refresh-Token gespeichert),
	// speichert das neue Refresh-Token und auditiert oauth.refresh ok.
	res, err = e.run("accounts.test", map[string]any{"name": "outlook"}, "", false)
	if err != nil || res.(AccountsTestOutput).AuthMechanism != "XOAUTH2" {
		t.Fatalf("test: %v", err)
	}
	after, _ := oauth.Load(e.sec, a.CredentialRef)
	if after.RefreshToken == saved.RefreshToken || e.as.Count("refresh_token") != 1 {
		t.Fatal("rotated refresh token must be stored")
	}
	evs := e.sys.Events()
	if len(evs) != 1 || evs[0].Operation != OpOAuthRefresh || evs[0].Result != audit.ResultOK || evs[0].ActorType != audit.ActorSystem ||
		evs[0].Targets[0] != "account:outlook" {
		t.Fatalf("system events = %+v", evs)
	}

	// Refresh abgelehnt: Fehler oauth_refresh, Audit error mit Code.
	e.as.FailRefresh = "invalid_grant"
	if _, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", false); !errors.Is(err, oauth.ErrRefresh) {
		t.Fatalf("refused refresh: %v", err)
	}
	evs = e.sys.Events()
	if len(evs) != 2 || evs[1].Result != audit.ResultError || evs[1].Reason != "invalid_grant" {
		t.Fatalf("system events = %+v", evs)
	}

	// Kanarientest: kein Token in Ausgaben, Audit, Fortschritt, Datenbank.
	all, _ := json.Marshal([]any{e.au.Events(), e.sys.Events(), prompts, out})
	if strings.Contains(string(all), "CANARY-tok") {
		t.Fatalf("token leaked: %s", all)
	}
	var n int
	if err := e.st.DBForTest().QueryRow(`SELECT count(*) FROM accounts WHERE sync_policy LIKE '%CANARY%' OR capabilities LIKE '%CANARY%' OR COALESCE(last_error,'') LIKE '%CANARY%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("token in the database: %d %v", n, err)
	}

	// Passwort setzen geht nicht; Entfernen löscht Token samt Teilen.
	var ie *InputError
	if _, err := e.run("accounts.set", map[string]any{"name": "outlook", "replace_password": true}, canary, false); !errors.As(err, &ie) {
		t.Fatalf("set password: %v", err)
	}
	if err := oauth.Save(e.sec, a.CredentialRef, oauth.Token{RefreshToken: secrets.Secret(strings.Repeat("r", 4096))}, time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err = e.run("accounts.remove", map[string]any{"name": "outlook"}, "", false)
	if err != nil || !res.(AccountsRemoveOutput).SecretDeleted || len(e.sec.m) != 0 {
		t.Fatalf("remove: %v %v", err, e.sec.m)
	}
}

func TestAccountsLoginRejectsPasswordAccounts(t *testing.T) {
	e := newOAuthEnv(t)
	if _, err := e.run("accounts.add", demoInput(), canary, false); err != nil {
		t.Fatal(err)
	}
	var ie *InputError
	if _, err := e.run("accounts.login", map[string]any{"name": "demo"}, "", false); !errors.As(err, &ie) {
		t.Fatalf("login on a password account: %v", err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "demo"}, "", false); e.as.Count("/authorize") != 0 {
		t.Fatalf("no flow may start: %v", err)
	}
}

// Scheitert das Speichern nach dem Refresh, endet account test laut mit
// token_not_stored (T4 Punkt 2).
func TestTokenNotStoredStopsLoudly(t *testing.T) {
	e := newOAuthEnv(t)
	if _, err := e.run("accounts.add", outlookInput(), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", false); err != nil {
		t.Fatal(err)
	}
	e.sec.failSet = true
	_, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", false)
	if !errors.Is(err, oauth.ErrTokenNotStored) {
		t.Fatalf("err = %v", err)
	}
	if evs := e.sys.Events(); len(evs) != 1 || evs[0].Reason != oauth.ReasonNotStored {
		t.Fatalf("events = %+v", evs)
	}
}

// W1 (Gegenprüfung T4): Der unsichere Testspeicher insecure-file meldet sich
// als verfügbar; ein echtes Refresh-Token darf dort trotzdem nie landen.
// add, login und die Tokenquelle (account test, sync) lehnen ihn ab, außer
// für Konten mit insecure_test_server (lokaler Testserver).
func TestOAuth2RejectsInsecureFileStore(t *testing.T) {
	e := newOAuthEnv(t)
	if _, err := e.run("accounts.add", outlookInput(), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", false); err != nil {
		t.Fatal(err)
	}
	e.sec.insecure = true
	before := len(e.sec.m)
	refreshes := e.as.Count("refresh_token")

	in := outlookInput()
	in["name"] = "outlook2"
	if _, err := e.run("accounts.add", in, "", false); !errors.Is(err, secrets.ErrUnavailable) || !strings.Contains(err.Error(), "insecure-test-server") {
		t.Fatalf("add with insecure-file: %v", err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", false); !errors.Is(err, secrets.ErrUnavailable) {
		t.Fatalf("login with insecure-file: %v", err)
	}
	if e.as.Count("/authorize") != 1 {
		t.Fatal("no second flow may start")
	}
	if _, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", false); !errors.Is(err, secrets.ErrUnavailable) {
		t.Fatalf("test with insecure-file: %v", err)
	}
	if len(e.sec.m) != before || e.as.Count("refresh_token") != refreshes {
		t.Fatal("nothing may be refreshed or stored in the insecure store")
	}

	// Lokaler Testserver: erlaubt.
	in = outlookInput()
	in["name"], in["host"], in["tls"], in["insecure_test_server"] = "lokal", "127.0.0.1", "none", true
	if _, err := e.run("accounts.add", in, "", false); err != nil {
		t.Fatalf("add for a test server: %v", err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "lokal"}, "", false); err != nil {
		t.Fatalf("login for a test server: %v", err)
	}
	if _, err := e.run("accounts.test", map[string]any{"name": "lokal"}, "", false); err != nil {
		t.Fatalf("test for a test server: %v", err)
	}
}
