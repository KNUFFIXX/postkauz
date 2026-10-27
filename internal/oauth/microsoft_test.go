package oauth_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

var client = oauth.Client{ClientID: oauthtest.ClientID, Tenant: "consumers"}

func TestClientValidate(t *testing.T) {
	for _, c := range []oauth.Client{client, {ClientID: oauthtest.ClientID, Tenant: "common"},
		{ClientID: oauthtest.ClientID, Tenant: "contoso.example.org"}, {ClientID: oauthtest.ClientID, Tenant: oauthtest.ClientID}} {
		if err := c.Validate(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	for _, c := range []oauth.Client{{ClientID: "x", Tenant: "common"}, {ClientID: oauthtest.ClientID, Tenant: "../evil"},
		{ClientID: oauthtest.ClientID, Tenant: ""}} {
		if err := c.Validate(); err == nil {
			t.Errorf("%+v must be invalid", c)
		}
	}
}

func TestMicrosoftEndpointsAndScopes(t *testing.T) {
	cfg := oauth.Microsoft{}.Config(client)
	if cfg.Endpoint.AuthURL != "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize" ||
		cfg.Endpoint.TokenURL != "https://login.microsoftonline.com/consumers/oauth2/v2.0/token" ||
		cfg.Endpoint.DeviceAuthURL != "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode" {
		t.Fatalf("endpoint = %+v", cfg.Endpoint)
	}
	if strings.Join(cfg.Scopes, " ") != "https://outlook.office.com/IMAP.AccessAsUser.All offline_access" || cfg.ClientSecret != "" {
		t.Fatalf("scopes = %v", cfg.Scopes)
	}
}

// Loopback-Flow Ende zu Ende gegen den Fake-AS: PKCE S256, state, kein
// Client-Secret, Refresh-Token zurück.
func TestLoginLoopbackEndToEnd(t *testing.T) {
	as := oauthtest.New(t)
	m := oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second}
	var prompt oauth.Prompt
	tok, err := m.LoginLoopback(context.Background(), client, func(p oauth.Prompt) { prompt = p })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok.AccessToken.Reveal(), "CANARY-tok-access") || !strings.HasPrefix(tok.RefreshToken.Reveal(), "CANARY-tok-refresh") ||
		tok.Expiry.IsZero() {
		t.Fatalf("token = %+v", tok)
	}
	if !prompt.BrowserOpened || !strings.Contains(prompt.URL, "code_challenge_method=S256") || !strings.Contains(prompt.URL, "redirect_uri=http%3A%2F%2Flocalhost%3A") ||
		strings.Contains(prompt.URL, "CANARY") {
		t.Fatalf("prompt = %+v", prompt)
	}
	forms := as.Forms()
	if len(forms) != 1 || forms[0].Get("code_verifier") == "" || forms[0].Has("client_secret") {
		t.Fatalf("token request = %v", forms)
	}
}

func TestLoginLoopbackDeniedAndTimeout(t *testing.T) {
	as := oauthtest.New(t)
	as.Deny = true
	m := oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second}
	if _, err := m.LoginLoopback(context.Background(), client, nil); !errors.Is(err, oauth.ErrLoginDenied) || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("denied: %v", err)
	}
	// Kein Browser, keine Antwort: Abbruch nach der Frist.
	m = oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: func(string) error { return errors.New("no browser") }, LoginTimeout: 200 * time.Millisecond}
	var prompt oauth.Prompt
	if _, err := m.LoginLoopback(context.Background(), client, func(p oauth.Prompt) { prompt = p }); !errors.Is(err, oauth.ErrLoginTimeout) {
		t.Fatalf("timeout: %v", err)
	}
	if prompt.BrowserOpened || prompt.URL == "" {
		t.Fatalf("prompt without browser: %+v", prompt)
	}
}

// Fremde Anfragen an den Loopback-Server (falscher state) beenden den Flow
// nicht.
func TestLoginLoopbackIgnoresWrongState(t *testing.T) {
	as := oauthtest.New(t)
	browser := oauthtest.Browser(t)
	m := oauth.Microsoft{Endpoint: as.Endpoint, LoginTimeout: 10 * time.Second, OpenBrowser: func(u string) error {
		go func() {
			// zuerst ein Angreifer mit falschem state, dann der echte Browser
			if i := strings.Index(u, "redirect_uri="); i >= 0 {
				resp, err := http.Get(strings.Replace(redirectOf(u), "localhost", "127.0.0.1", 1) + "/?code=evil&state=wrong")
				if err == nil {
					_ = resp.Body.Close()
					if resp.StatusCode != http.StatusBadRequest {
						t.Errorf("wrong state answered %d", resp.StatusCode)
					}
				}
			}
			_ = browser(u)
		}()
		return nil
	}}
	if _, err := m.LoginLoopback(context.Background(), client, nil); err != nil {
		t.Fatal(err)
	}
}

func redirectOf(authURL string) string {
	i := strings.Index(authURL, "redirect_uri=")
	rest := authURL[i+len("redirect_uri="):]
	if j := strings.IndexByte(rest, '&'); j >= 0 {
		rest = rest[:j]
	}
	r := strings.NewReplacer("%3A", ":", "%2F", "/").Replace(rest)
	return r
}

// handlerTransport beantwortet Anfragen im Prozess (ohne Netz), damit der
// Device-Code-Flow in einer synctest-Blase mit falscher Uhr läuft.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, r)
	return rec.Result(), nil
}

// Device-Code-Flow mit authorization_pending und slow_down (+5 s je
// slow_down, RFC 8628), in falscher Zeit.
func TestLoginDeviceCodePendingAndSlowDown(t *testing.T) {
	as := oauthtest.New(t)
	as.DevicePolls = []string{"authorization_pending", "slow_down", "authorization_pending"}
	as.DeviceInterval = 5
	synctest.Test(t, func(t *testing.T) {
		m := oauth.Microsoft{Endpoint: as.Endpoint, HTTPClient: &http.Client{Transport: handlerTransport{as.Config.Handler}}}
		var prompt oauth.Prompt
		start := time.Now()
		tok, err := m.LoginDeviceCode(context.Background(), client, func(p oauth.Prompt) { prompt = p })
		if err != nil {
			t.Fatal(err)
		}
		// 5 s (pending) + 5 s (slow_down) + 10 s (pending) + 10 s (Token)
		if d := time.Since(start); d != 30*time.Second {
			t.Fatalf("polling took %s, want 30s", d)
		}
		if prompt.UserCode != "ABCD-EFGH" || prompt.URL != "https://microsoft.com/devicelogin" || tok.RefreshToken.IsZero() {
			t.Fatalf("prompt=%+v", prompt)
		}
	})
	if n := as.Count("urn:ietf:params:oauth:grant-type:device_code"); n != 4 {
		t.Fatalf("polls = %d", n)
	}
}

func TestLoginDeviceCodeDenied(t *testing.T) {
	as := oauthtest.New(t)
	as.DevicePolls = []string{"access_denied"}
	synctest.Test(t, func(t *testing.T) {
		m := oauth.Microsoft{Endpoint: as.Endpoint, HTTPClient: &http.Client{Transport: handlerTransport{as.Config.Handler}}}
		if _, err := m.LoginDeviceCode(context.Background(), client, nil); !errors.Is(err, oauth.ErrLoginDenied) {
			t.Fatalf("err = %v", err)
		}
	})
}

// HTTP-Refresher mit der Quelle: Refresh 2 min vor Ablauf, Microsoft ersetzt
// das Refresh-Token, OnRefresh speichert es; ein Fehler des Endpunkts führt
// zu Sperrzeit und genau einem Versuch, ohne Anbietertext im Fehler.
func TestRefresherWithSource(t *testing.T) {
	as := oauthtest.New(t)
	m := oauth.Microsoft{Endpoint: as.Endpoint, OpenBrowser: oauthtest.Browser(t), LoginTimeout: 10 * time.Second}
	first, err := m.LoginLoopback(context.Background(), client, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	src := oauth.NewSource(oauth.Token{AccessToken: first.AccessToken, RefreshToken: first.RefreshToken, Expiry: now.Add(10 * time.Minute)}, m.Refresher(client))
	src.Now = func() time.Time { return now }
	var stored []oauth.Token
	src.OnRefresh = func(t oauth.Token) error { stored = append(stored, t); return nil }
	var reports []string
	src.Report = func(ok bool, r string) { reports = append(reports, r) }
	ctx := context.Background()
	if tok, _ := src.Token(ctx); tok != first.AccessToken || as.Count("refresh_token") != 0 {
		t.Fatal("valid token must not refresh")
	}
	now = now.Add(8*time.Minute + time.Second) // weniger als 2 min vor Ablauf
	tok, err := src.Token(ctx)
	if err != nil || tok == first.AccessToken || as.Count("refresh_token") != 1 {
		t.Fatalf("refresh: %v", err)
	}
	if len(stored) != 1 || stored[0].RefreshToken == first.RefreshToken {
		t.Fatal("rotated refresh token must be stored")
	}

	as.FailRefresh = "invalid_grant"
	now = now.Add(2 * time.Hour)
	_, err = src.Token(ctx)
	if !errors.Is(err, oauth.ErrRefresh) || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("refresh failure: %v", err)
	}
	for i := 0; i < 3; i++ {
		_, _ = src.Token(ctx)
	}
	now = now.Add(4 * time.Minute)
	_, _ = src.Token(ctx)
	if n := as.Count("refresh_token"); n != 2 {
		t.Fatalf("refresh attempts within the cooldown = %d, want 2", n)
	}
	now = now.Add(2 * time.Minute)
	_, _ = src.Token(ctx)
	if n := as.Count("refresh_token"); n != 3 {
		t.Fatalf("after the cooldown: %d", n)
	}
	if strings.Join(reports, ",") != "ok,invalid_grant,invalid_grant" {
		t.Fatalf("reports = %v", reports)
	}
}

// Der Antwortkörper des Token-Endpunkts (kann Token enthalten) erscheint in
// keinem Fehler; nur Code und AADSTS-Nummer.
func TestEndpointErrorsCarryNoProviderText(t *testing.T) {
	as := oauthtest.New(t)
	as.FailRefresh = "invalid_grant"
	_, err := oauth.Microsoft{Endpoint: as.Endpoint}.Refresher(client).Refresh(context.Background(), secrets.Secret("CANARY-rt"))
	var re *oauth.RefreshError
	if !errors.As(err, &re) || re.Code != "invalid_grant" || re.AADSTS != "AADSTS70000" || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("err = %v", err)
	}
	if got := secrets.RedactTokens(`{"access_token":"eyJhbGciOi.eyJzdWIiOi.c2lnbmF0dXJl","refresh_token": "M.R3_BAY-x","error_code":"invalid_grant"} code=abc&state=s`); strings.Contains(got, "eyJ") ||
		strings.Contains(got, "M.R3") || strings.Contains(got, "abc") || !strings.Contains(got, "invalid_grant") {
		t.Fatalf("RedactTokens = %s", got)
	}
}
