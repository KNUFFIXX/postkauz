package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// AuthOAuth2 ist auth_type der Konten mit Microsoft-Anmeldung (S3.7, E11).
const AuthOAuth2 = "oauth2"

// OpOAuthRefresh ist das Systemereignis je Refresh-Versuch (Ergebnis ok
// oder error mit Code; nie ein Token).
const OpOAuthRefresh = "oauth.refresh"

// Credentials löst die Anmeldedaten eines Kontos auf: das Passwort
// (Umgebung vor Speicher) oder für OAuth2-Konten eine Tokenquelle mit dem
// Refresh-Token aus dem Schlüsselbund (S3.7). Nur Prozesse mit Zugangsdaten
// (CLI: sync, account test, account login) rufen For auf, nie der
// MCP-Server.
type Credentials struct {
	Secrets secrets.Store
	// OAuth baut Konfiguration, Refresher und Flows (Nullwert: Microsoft).
	OAuth oauth.Microsoft
	// Audit nimmt die Systemereignisse oauth.refresh auf (nil = keins).
	Audit audit.Writer
}

// credentials liefert die Auflösung aus den Abhängigkeiten des Katalogs.
func (d Deps) credentials() Credentials {
	return Credentials{Secrets: d.Secrets, OAuth: d.OAuth, Audit: d.SystemAudit}
}

// ErrNoToken: Ein OAuth2-Konto hat (noch) kein Refresh-Token im
// Schlüsselbund. Abhilfe: account login.
var ErrNoToken = errors.New("no OAuth2 token stored for this account")

// oauthClient liefert den OAuth-Client eines Kontos.
func oauthClient(a store.Account) (oauth.Client, error) {
	if a.OAuth == nil {
		return oauth.Client{}, fmt.Errorf("account %s: OAuth2 client id missing; add the account again with --client-id", a.Name)
	}
	c := oauth.Client{ClientID: a.OAuth.ClientID, Tenant: a.OAuth.Tenant}
	if c.Tenant == "" {
		c.Tenant = oauth.DefaultTenant
	}
	if err := c.Validate(); err != nil {
		return oauth.Client{}, fmt.Errorf("account %s: %w", a.Name, err)
	}
	return c, nil
}

// For liefert die Anmeldedaten des Kontos. Im Trockenlauf (dryRun)
// erneuert die Tokenquelle eines OAuth2-Kontos zwar das Access-Token (ohne
// Anmeldung kein Test), speichert das erneuerte Refresh-Token aber nicht
// (Gegenprüfung T4, K3): Microsoft widerruft das alte beim Einlösen nicht,
// der gespeicherte Stand bleibt nutzbar. Das Systemereignis oauth.refresh
// entsteht trotzdem (der Token-Endpunkt wurde angesprochen), mit dry_run.
func (c Credentials) For(a store.Account, dryRun bool) (provider.Credential, error) {
	if a.AuthType != AuthOAuth2 {
		pw, err := c.Secrets.Get(a.CredentialRef)
		if errors.Is(err, secrets.ErrNotFound) {
			return provider.Credential{}, fmt.Errorf("%w: %s (keyring entry %s or environment variable %s)", ErrNoSecret, a.Name, a.CredentialRef, secrets.EnvName(a.CredentialRef))
		} else if err != nil {
			return provider.Credential{}, err
		}
		return provider.PasswordCredential(pw), nil
	}
	client, err := oauthClient(a)
	if err != nil {
		return provider.Credential{}, err
	}
	// Auch hier, nicht nur bei add und login: Die Quelle speichert jedes
	// erneuerte Refresh-Token (etwa nach einem Wechsel von secrets.backend).
	if _, err := oauthStoreAllowed(c.Secrets, a.InsecureTestServer, c.OAuth, client); err != nil {
		return provider.Credential{}, fmt.Errorf("account %s: %w", a.Name, err)
	}
	saved, err := oauth.Load(c.Secrets, a.CredentialRef)
	if errors.Is(err, secrets.ErrNotFound) {
		return provider.Credential{}, fmt.Errorf("%w: %s (keyring entry %s); sign in with: account login %s", ErrNoToken, a.Name, a.CredentialRef, a.Name)
	} else if err != nil {
		return provider.Credential{}, fmt.Errorf("account %s: %w", a.Name, err)
	}
	src := oauth.NewSource(oauth.Token{RefreshToken: saved.RefreshToken}, c.OAuth.Refresher(client))
	if !dryRun {
		src.OnRefresh = func(t oauth.Token) error { return oauth.Save(c.Secrets, a.CredentialRef, t, time.Now()) }
	}
	src.Report = func(ok bool, reason string) { c.auditRefresh(a, ok, reason, dryRun) }
	// K7: Ein zweiter Prozess (sync --watch neben account test) kann
	// inzwischen ein neueres Refresh-Token gespeichert haben.
	src.Reload = func() (secrets.Secret, error) {
		s, err := oauth.Load(c.Secrets, a.CredentialRef)
		return s.RefreshToken, err
	}
	return provider.Credential{Token: src}, nil
}

// auditRefresh schreibt das Systemereignis oauth.refresh: Konto, Ergebnis,
// Code; nie ein Token, nie ein Text des Anbieters.
func (c Credentials) auditRefresh(a store.Account, ok bool, reason string, dryRun bool) {
	if c.Audit == nil {
		return
	}
	res := audit.ResultOK
	if !ok {
		res = audit.ResultError
	}
	p := map[string]any{"account": a.Name}
	if dryRun {
		p["dry_run"] = true
	}
	params, _ := json.Marshal(p)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = c.Audit.Append(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorSystem, KeyID: "system", Scope: string(policy.ScopeMailSync),
		Operation: OpOAuthRefresh, ParamsRedacted: params, Targets: []string{"account:" + a.Name}, Result: res, Reason: reason})
}

// --- accounts.login --------------------------------------------------------

// AccountsLoginInput ist die Eingabe von accounts.login.
type AccountsLoginInput struct {
	Name       string `json:"name"`
	DeviceCode bool   `json:"device_code,omitempty"`
}

// AccountsLoginOutput ist die Ausgabe von accounts.login. Sie enthält nie
// ein Token, nur wo es liegt und wann das Access-Token abläuft.
type AccountsLoginOutput struct {
	Account string `json:"account"`
	// Method: loopback | device_code.
	Method string `json:"method"`
	// RefreshToken: stored | would_store (Trockenlauf).
	RefreshToken  string `json:"refresh_token"`
	CredentialRef string `json:"credential_ref"`
	SecretStore   string `json:"secret_store"`
	// TokenExpiry: Ablauf des ausgegebenen Access-Tokens (RFC 3339, UTC;
	// das Access-Token bleibt im Speicher und wird nicht gespeichert).
	TokenExpiry string `json:"token_expiry,omitempty"`
	DryRun      bool   `json:"dry_run"`
}

// promptMessage ist der Hinweis an den Menschen während der Anmeldung.
func promptMessage(p oauth.Prompt) string {
	if p.UserCode != "" {
		return fmt.Sprintf("To sign in, open %s in a browser and enter the code %s (valid until %s).",
			p.URL, p.UserCode, p.Expires.Local().Format("15:04"))
	}
	if p.BrowserOpened {
		return "The browser opens the Microsoft sign-in. If it does not, open this address yourself:\n" + p.URL
	}
	return "Open this address in a browser on this computer to sign in:\n" + p.URL
}

// AccountsLogin führt die Microsoft-Anmeldung eines OAuth2-Kontos aus
// (Loopback-Flow im Browser oder Device-Code) und legt das Refresh-Token im
// Schlüsselbund ab (S3.7, E11). Nur für Menschen (HumanOnly, admin): Die
// Anmeldung braucht den Menschen am Browser, und kein Schlüssel darf
// Zugangsdaten setzen. R1 (lokal schreibend: Schlüsselbund).
func AccountsLogin(repo AccountRepo, creds Credentials) Operation {
	return Operation{
		Name:      "accounts.login",
		Scope:     policy.ScopeAdmin,
		Risk:      policy.R1,
		HumanOnly: true,
		Summary:   "Sign in to an OAuth2 (Microsoft) account in the browser or with a device code and store its refresh token in the OS keyring. Human only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "name": {"type": "string", "pattern": "^[a-z0-9][a-z0-9-]{0,62}$"},
    "device_code": {"type": "boolean", "description": "sign in with a code on another device instead of the browser on this computer"}
  },
  "required": ["name"],
  "additionalProperties": false
}`),
		OutputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "account": {"type": "string"},
    "method": {"type": "string", "enum": ["loopback", "device_code"]},
    "refresh_token": {"type": "string", "enum": ["stored", "would_store"]},
    "credential_ref": {"type": "string"},
    "secret_store": {"type": "string"},
    "token_expiry": {"type": "string", "format": "date-time"},
    "dry_run": {"type": "boolean"}
  },
  "required": ["account", "method", "refresh_token", "credential_ref", "secret_store", "dry_run"]
}`),
		Redact: redactAccountInput,
		Handler: func(ctx context.Context, call Call) (any, error) {
			var in AccountsLoginInput
			if err := decodeInput(call.Input, &in); err != nil {
				return nil, err
			}
			if err := validAccountName(in.Name); err != nil {
				return nil, err
			}
			a, err := repo.AccountByName(ctx, in.Name)
			if err != nil {
				return nil, err
			}
			if a.AuthType != AuthOAuth2 {
				return nil, inputErr("account %s signs in with a password (auth_type %s); account login is only for accounts added with --auth oauth2", a.Name, a.AuthType)
			}
			client, err := oauthClient(a)
			if err != nil {
				return nil, err
			}
			st, err := oauthStoreAllowed(creds.Secrets, a.InsecureTestServer, creds.OAuth, client)
			if err != nil {
				return nil, err
			}
			out := AccountsLoginOutput{Account: a.Name, Method: "loopback", RefreshToken: "would_store",
				CredentialRef: a.CredentialRef, SecretStore: st.Backend, DryRun: call.DryRun}
			if in.DeviceCode {
				out.Method = "device_code"
			}
			if call.DryRun {
				return out, nil
			}
			show := func(p oauth.Prompt) { call.progress(Progress{Event: "oauth_prompt", Message: promptMessage(p)}) }
			var tok oauth.Token
			if in.DeviceCode {
				tok, err = creds.OAuth.LoginDeviceCode(ctx, client, show)
			} else {
				tok, err = creds.OAuth.LoginLoopback(ctx, client, show)
			}
			if err != nil {
				return nil, err
			}
			if err := oauth.Save(creds.Secrets, a.CredentialRef, tok, time.Now()); err != nil {
				return nil, fmt.Errorf("%w: %s", oauth.ErrTokenNotStored, secrets.RedactTokens(err.Error()))
			}
			out.RefreshToken = "stored"
			if !tok.Expiry.IsZero() {
				out.TokenExpiry = tok.Expiry.UTC().Format(time.RFC3339)
			}
			return out, nil
		},
	}
}

// oauthStoreAllowed prüft, ob der Speicher ein Refresh-Token aufnehmen darf
// (Rückfrage R2): nur ein erreichbarer Schlüsselbund. Der unsichere
// Testspeicher insecure-file meldet sich zwar als verfügbar, gilt aber nur
// für Konten mit insecure_test_server (IMAP auf localhost) und nur, wenn
// auch die Anmeldeadressen des Clients lokal sind (Fake-AS; Gegenprüfung
// T4, K3): Ein Konto mit lokalem IMAP-Host, aber echtem Microsoft-Endpunkt
// legte sonst ein echtes Refresh-Token in insecure-secrets/secrets.json.
func oauthStoreAllowed(sec secrets.Store, insecureTestServer bool, ms oauth.Microsoft, c oauth.Client) (secrets.Status, error) {
	st := secrets.Stored(sec).Status()
	if !st.Available || (st.Insecure && !(insecureTestServer && oauthEndpointLocal(ms, c))) {
		return st, oauthNeedsKeyring(st)
	}
	return st, nil
}

// oauthEndpointLocal: Alle Anmeldeadressen (Anmeldeseite, Token-Endpunkt,
// Device-Code) zeigen auf localhost. Microsoft selbst ist nie lokal; nur
// ein Fake-AS in Tests (oauth.Microsoft.Endpoint).
func oauthEndpointLocal(ms oauth.Microsoft, c oauth.Client) bool {
	ep := ms.Config(c).Endpoint
	for _, raw := range []string{ep.AuthURL, ep.TokenURL, ep.DeviceAuthURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || !IsLoopbackHost(u.Hostname()) {
			return false
		}
	}
	return true
}

// oauthNeedsKeyring ist die Ablehnung ohne Schlüsselbund (Rückfrage R2:
// OAuth2-Konten nur auf Rechnern mit Schlüsselbund; das Refresh-Token
// ändert sich bei jedem Refresh und lässt sich nicht über die Umgebung
// setzen).
func oauthNeedsKeyring(st secrets.Status) error {
	if st.Available && st.Insecure {
		return fmt.Errorf("%w (%s is the insecure test store): OAuth2 accounts need the OS keyring; "+
			"secrets.backend = \"insecure-file\" is only allowed for accounts with --insecure-test-server against a local test sign-in server", secrets.ErrUnavailable, st.Backend)
	}
	return fmt.Errorf("%w (%s): OAuth2 accounts need the OS keyring, because the refresh token changes with every renewal; "+
		"on servers without a keyring use an account with an app password instead", secrets.ErrUnavailable, st.Detail)
}
