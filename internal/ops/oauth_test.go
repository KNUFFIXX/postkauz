package ops

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

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
	for _, op := range []Operation{AccountsAdd(e.st, e.sec, creds.OAuth), AccountsSet(e.st, e.sec), AccountsRemove(e.st, e.sec),
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

// K3 (zweite Nachbesserung T4): insecure_test_server prüft nur den
// IMAP-Host. Mit insecure-file gilt OAuth2 nur, wenn auch die
// Anmeldeadressen lokal sind (Fake-AS); ein Konto mit lokalem IMAP-Host
// und echtem Microsoft-Endpunkt legte sonst ein echtes Refresh-Token in
// insecure-secrets/secrets.json.
func TestOAuth2InsecureFileNeedsLocalSignIn(t *testing.T) {
	e := newOAuthEnv(t)
	lokal := outlookInput()
	lokal["name"], lokal["host"], lokal["tls"], lokal["insecure_test_server"] = "lokal", "127.0.0.1", "none", true
	if _, err := e.run("accounts.add", lokal, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "lokal"}, "", false); err != nil {
		t.Fatal(err)
	}
	e.sec.insecure = true
	before := len(e.sec.m)
	authorize, refreshes := e.as.Count("/authorize"), e.as.Count("refresh_token")
	orig := e.x

	remote := func(string) oauth2.Endpoint {
		return oauth2.Endpoint{AuthURL: "https://login.example.org/authorize", TokenURL: "https://login.example.org/token",
			DeviceAuthURL: "https://login.example.org/devicecode"}
	}
	half := func(tenant string) oauth2.Endpoint {
		ep := e.as.Endpoint(tenant)
		ep.TokenURL = "https://login.example.org/token"
		return ep
	}
	for name, ms := range map[string]oauth.Microsoft{
		"microsoft":         {OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second},
		"remote endpoint":   {Endpoint: remote, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second},
		"remote token only": {Endpoint: half, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second},
	} {
		creds := e.creds
		creds.OAuth = ms
		reg := NewRegistry()
		for _, op := range []Operation{AccountsAdd(e.st, e.sec, ms), AccountsTest(e.st, creds, &tokenProber{}), AccountsLogin(e.st, creds)} {
			if err := reg.Register(op); err != nil {
				t.Fatal(err)
			}
		}
		e.x = &Executor{Registry: reg, Audit: e.au}
		run := func(op string, in map[string]any) error {
			_, err := e.run(op, in, "", false)
			return err
		}
		in := maps.Clone(lokal)
		in["name"] = "lokal2"
		if err := run("accounts.add", in); !errors.Is(err, secrets.ErrUnavailable) {
			t.Errorf("%s: add: %v", name, err)
		}
		if err := run("accounts.login", map[string]any{"name": "lokal"}); !errors.Is(err, secrets.ErrUnavailable) {
			t.Errorf("%s: login: %v", name, err)
		}
		if err := run("accounts.test", map[string]any{"name": "lokal"}); !errors.Is(err, secrets.ErrUnavailable) {
			t.Errorf("%s: test: %v", name, err)
		}
	}
	e.x = orig
	if len(e.sec.m) != before || e.as.Count("/authorize") != authorize || e.as.Count("refresh_token") != refreshes {
		t.Fatal("nothing may be signed in, refreshed or stored with a remote sign-in server")
	}
	// Mit lokalem Fake-AS bleibt der Testweg offen.
	if _, err := e.run("accounts.test", map[string]any{"name": "lokal"}, "", false); err != nil {
		t.Fatalf("test with the local fake AS: %v", err)
	}
}

// K3 (Gegenprüfung T4): account test und sync im Trockenlauf melden sich
// mit einem erneuerten Access-Token an, speichern das neue Refresh-Token
// aber nicht. oauth.refresh steht im Audit, mit dry_run.
func TestDryRunRefreshStoresNothing(t *testing.T) {
	e := newOAuthEnv(t)
	runner := &tokenRunner{}
	if err := e.x.Registry.Register(SyncRun(e.st, e.creds, runner)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.add", outlookInput(), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", false); err != nil {
		t.Fatal(err)
	}
	a, _ := e.st.AccountByName(context.Background(), "outlook")
	before, _ := oauth.Load(e.sec, a.CredentialRef)
	e.as.KeepUsedRefresh = true // wie Microsoft: das gespeicherte bleibt gültig

	res, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", true)
	if err != nil || !res.(AccountsTestOutput).DryRun {
		t.Fatalf("test --dry-run: %v", err)
	}
	if _, err := e.run("sync.run", map[string]any{"account": "outlook"}, "", true); err != nil || len(runner.got) != 1 {
		t.Fatalf("sync --dry-run: %v", err)
	}
	after, err := oauth.Load(e.sec, a.CredentialRef)
	if err != nil || after.RefreshToken != before.RefreshToken || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("a dry run must not store the renewed token")
	}
	if e.as.Count("refresh_token") != 2 {
		t.Fatalf("refreshes = %d", e.as.Count("refresh_token"))
	}
	evs := e.sys.Events()
	if len(evs) != 2 {
		t.Fatalf("system events = %+v", evs)
	}
	for _, ev := range evs {
		if ev.Operation != OpOAuthRefresh || ev.Result != audit.ResultOK || !strings.Contains(string(ev.ParamsRedacted), `"dry_run":true`) {
			t.Fatalf("event = %+v", ev)
		}
	}

	// Ohne Trockenlauf wird gespeichert.
	if _, err := e.run("accounts.test", map[string]any{"name": "outlook"}, "", false); err != nil {
		t.Fatal(err)
	}
	if stored, _ := oauth.Load(e.sec, a.CredentialRef); stored.RefreshToken == before.RefreshToken {
		t.Fatal("a real run must store the renewed token")
	}
	if ev := e.sys.Events()[2]; strings.Contains(string(ev.ParamsRedacted), "dry_run") {
		t.Fatalf("event = %+v", ev)
	}
}

// tokenRunner ist ein Sync, der sich mit der Tokenquelle „anmeldet".
type tokenRunner struct{ got []string }

func (r *tokenRunner) Sync(ctx context.Context, a store.Account, cred provider.Credential, o SyncOptions, _ func(Progress)) (SyncOutput, error) {
	tok, err := cred.Token.Token(ctx)
	if err != nil {
		return SyncOutput{}, err
	}
	r.got = append(r.got, tok.Reveal())
	return SyncOutput{Account: a.Name, Mode: "incremental"}, nil
}

// K7 (Gegenprüfung T4): zwei Prozesse mit eigener Tokenquelle auf demselben
// Schlüsselbund (sync --watch und account test). Der Fake-AS lässt jedes
// Refresh-Token nur einmal zu; ohne Neulesen scheiterte der zweite mit
// invalid_grant, weil der erste das gemeinsame Token schon eingelöst hat.
func TestTwoProcessesShareRotatedToken(t *testing.T) {
	e := newOAuthEnv(t)
	if _, err := e.run("accounts.add", outlookInput(), "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run("accounts.login", map[string]any{"name": "outlook"}, "", false); err != nil {
		t.Fatal(err)
	}
	e.as.ExpiresIn = 60 // kürzer als die 2 Minuten Vorlauf: jeder Aufruf erneuert
	a, _ := e.st.AccountByName(context.Background(), "outlook")
	p1, err := e.creds.For(a, false)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := e.creds.For(a, false)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i, p := range []provider.Credential{p1, p2, p1, p2} {
		if _, err := p.Token.Token(ctx); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if n := e.as.Count("refresh_token"); n != 4 {
		t.Fatalf("refreshes = %d", n)
	}
	for _, ev := range e.sys.Events() {
		if ev.Result != audit.ResultOK {
			t.Fatalf("event = %+v", ev)
		}
	}
}
