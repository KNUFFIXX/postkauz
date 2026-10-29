package httpmcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S3.6 Akzeptanz 2: mcp --http mit [http] auth = "oauth" gegen den echten
// Autorisierungsserver im Docker (Authelia, eingerichtet aus dem Beispiel
// in deploy/docker/auth durch deploy/testauth/setup.sh). Authorization
// Code mit PKCE S256 und Refresh; die Anmeldung „klickt“ ein Ersatz für
// den Browser mit dem Testkonto. Device-Code ist im Beispiel nicht
// eingerichtet (PostKauz ist nur Resource Server; der Fluss ändert am
// Token nichts).
//
//	make testauth-up
//	POSTKAUZ_IT=authelia make integration
func TestIntegrationAuthelia(t *testing.T) {
	v := os.Getenv("POSTKAUZ_IT")
	if v == "" || !(v == "1" || v == "all" || slices.Contains(strings.Split(v, ","), "authelia")) {
		t.Skip("integration test needs the Docker authorization server: make testauth-up, then POSTKAUZ_IT=authelia make integration")
	}
	dir := os.Getenv("POSTKAUZ_TESTAUTH_DIR")
	if dir == "" {
		dir = "/tmp/postkauz-testauth"
	}
	env := readEnvFile(t, filepath.Join(dir, "env"))
	client := testAuthClient(t, filepath.Join(dir, "tls", "ca.crt"), env["TESTAUTH_ISSUER"], env["TESTAUTH_ADDR"])
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	meta, err := oauthex.GetAuthServerMeta(ctx, env["TESTAUTH_ISSUER"]+"/.well-known/oauth-authorization-server", env["TESTAUTH_ISSUER"], client)
	if err != nil || meta == nil {
		t.Fatalf("AS metadata: %v", err)
	}
	b := &fakeBrowser{c: noRedirect(client), user: env["TESTAUTH_USER"], password: env["TESTAUTH_PASSWORD"], callback: env["TESTAUTH_CALLBACK"]}
	resource := env["TESTAUTH_RESOURCE"]
	// Ohne resource-Parameter (wie ein Client, der RFC 8707 nicht nutzt;
	// Befund B4: requested_audience_mode implicit) und mit.
	tok := b.flow(t, ctx, meta, client, "")
	tokRes := b.flow(t, ctx, meta, client, resource)
	sub := claim(t, tok.AccessToken, "sub")
	if sub == "" || claim(t, tokRes.AccessToken, "sub") != sub {
		t.Fatalf("no subject in the access token")
	}

	e := newEnv(t)
	start := func(principals map[string]string) *Server {
		h := httpConfig(t, func(h *config.HTTP) {
			h.Auth, h.Resource, h.Issuer = config.HTTPAuthOAuth, resource, env["TESTAUTH_ISSUER"]
			h.Principals = principals
		})
		s, err := New(Deps{HTTP: h, Executor: e.x, Keys: e.st, Audit: e.st.AuditWriter(), Log: e.logger(), HTTPClient: client})
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	call := func(s *Server, bearerTok string) int {
		req, _ := http.NewRequest(http.MethodPost, "https://node.postkauz.test/mcp", strings.NewReader(initBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+bearerTok)
		rec := &recorder{h: http.Header{}}
		s.Handler().ServeHTTP(rec, req)
		return rec.code
	}

	// Unbekanntes Subjekt → 403; das Audit nennt sub für die Zuordnung.
	if code := call(start(map[string]string{"someone-else": "reader"}), tok.AccessToken); code != http.StatusForbidden {
		t.Errorf("unknown subject: %d, want 403", code)
	}
	s := start(map[string]string{sub: "reader"})
	for name, raw := range map[string]string{"access token without resource": tok.AccessToken, "access token with resource": tokRes.AccessToken} {
		if code := call(s, raw); code != http.StatusOK {
			t.Errorf("%s: %d, want 200", name, code)
		}
	}
	if id, ok := tok.Extra("id_token").(string); !ok || call(s, id) != http.StatusUnauthorized {
		t.Errorf("id token: accepted or missing (%v)", ok)
	}

	// Ende zu Ende über HTTP mit dem SDK-Client: Tools nach dem Schlüssel.
	ts := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = ts.Serve(ln) }()
	defer ts.Close()
	cs, _ := connect(t, "http://"+ln.Addr().String()+PathMCP, hostRewrite{"node.postkauz.test"}, bearer(tok.AccessToken))
	if got, want := toolNames(t, cs), e.stdioToolNames(t, "reader"); !slices.Equal(got, want) {
		t.Errorf("tools: %v, want %v", got, want)
	}
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_list_accounts", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}

	// Refresh: neues Access-Token vom AS, wieder 200.
	cfg := oauthConfig(meta, b.callback)
	expired := *tok
	expired.Expiry = time.Now().Add(-time.Minute)
	fresh, err := cfg.TokenSource(context.WithValue(ctx, oauth2.HTTPClient, client), &expired).Token()
	if err != nil || fresh.AccessToken == tok.AccessToken {
		t.Fatalf("refresh: %v", err)
	}
	if code := call(s, fresh.AccessToken); code != http.StatusOK {
		t.Errorf("refreshed token: %d", code)
	}

	// Audit: transport http, Principal, iss, sub; nie das Token.
	var via, unknown bool
	for _, r := range e.auditAll(t) {
		p := string(r.ParamsRedacted)
		for _, raw := range []string{tok.AccessToken, fresh.AccessToken} {
			if strings.Contains(p, raw[strings.LastIndex(raw, ".")+1:]) {
				t.Errorf("token in audit entry %d", r.ID)
			}
		}
		if r.Operation == "accounts.list" && strings.Contains(p, `"transport":"http"`) && strings.Contains(p, `"principal":"reader"`) &&
			strings.Contains(p, `"sub":"`+sub+`"`) && strings.Contains(p, `"iss":"`+env["TESTAUTH_ISSUER"]+`"`) {
			via = true
		}
		if r.Operation == ops.OpAuthenticate && r.Reason == "principal_unknown" && strings.Contains(p, sub) {
			unknown = true
		}
	}
	if !via || !unknown {
		t.Errorf("audit: via %v, principal_unknown with sub %v", via, unknown)
	}
}

func readEnvFile(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("test authorization server not set up (make testauth-up): %v", err)
	}
	m := map[string]string{}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			m[k] = v
		}
	}
	return m
}

// testAuthClient vertraut der Test-CA und wählt den Namen des AS auf die
// Adresse des Containers, ohne Proxy.
func testAuthClient(t *testing.T, caFile, issuer, addr string) *http.Client {
	t.Helper()
	ca, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(ca)
	u, _ := url.Parse(issuer)
	var d net.Dialer
	tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, a string) (net.Conn, error) {
			if a == u.Host {
				a = addr
			}
			return d.DialContext(ctx, network, a)
		}}
	jar, _ := cookiejar.New(nil)
	return &http.Client{Transport: tr, Jar: jar, Timeout: 30 * time.Second}
}

func noRedirect(c *http.Client) *http.Client {
	n := *c
	n.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &n
}

func oauthConfig(meta *oauthex.AuthServerMeta, callback string) oauth2.Config {
	return oauth2.Config{ClientID: "claude-code", RedirectURL: callback, Scopes: []string{"openid", "offline_access"},
		Endpoint: oauth2.Endpoint{AuthURL: meta.AuthorizationEndpoint, TokenURL: meta.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
}

// fakeBrowser folgt den Umleitungen von Authelia, meldet das Testkonto an
// und stimmt zu (Befund B3: bei offline_access immer).
type fakeBrowser struct {
	c                        *http.Client
	user, password, callback string
}

func (b *fakeBrowser) flow(t *testing.T, ctx context.Context, meta *oauthex.AuthServerMeta, client *http.Client, resource string) *oauth2.Token {
	t.Helper()
	cfg := oauthConfig(meta, b.callback)
	verifier := oauth2.GenerateVerifier()
	opts := []oauth2.AuthCodeOption{oauth2.S256ChallengeOption(verifier)}
	if resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", resource))
	}
	state := oauth2.GenerateVerifier()[:24]
	q, err := b.authorize(cfg.AuthCodeURL(state, opts...))
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if q.Get("state") != state {
		t.Fatal("state mismatch")
	}
	ex := []oauth2.AuthCodeOption{oauth2.VerifierOption(verifier)}
	if resource != "" {
		ex = append(ex, oauth2.SetAuthURLParam("resource", resource))
	}
	tok, err := cfg.Exchange(context.WithValue(ctx, oauth2.HTTPClient, client), q.Get("code"), ex...)
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok.RefreshToken == "" {
		t.Fatal("no refresh token")
	}
	return tok
}

func (b *fakeBrowser) authorize(start string) (url.Values, error) {
	cur := start
	su, _ := url.Parse(start)
	clientID := su.Query().Get("client_id")
	for range 20 {
		res, err := b.c.Get(cur)
		if err != nil {
			return nil, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		_ = res.Body.Close()
		if res.StatusCode >= 300 && res.StatusCode < 400 {
			loc, err := res.Request.URL.Parse(res.Header.Get("Location"))
			if err != nil {
				return nil, err
			}
			if strings.HasPrefix(loc.String(), b.callback) {
				q := loc.Query()
				if q.Get("error") != "" {
					return nil, fmt.Errorf("authorization error %s", q.Get("error"))
				}
				return q, nil
			}
			cur = loc.String()
			continue
		}
		u := res.Request.URL
		fid := u.Query().Get("flow_id")
		switch {
		case res.StatusCode == http.StatusOK && fid != "" && strings.Contains(u.Path, "/consent"):
			next, err := b.api(u, "/api/oidc/consent", map[string]any{"flow_id": fid, "client_id": clientID, "consent": true, "pre_configure": false}, "redirect_uri")
			if err != nil {
				return nil, err
			}
			cur = next
		case res.StatusCode == http.StatusOK && fid != "":
			next, err := b.api(u, "/api/firstfactor", map[string]any{"username": b.user, "password": b.password, "keepMeLoggedIn": false,
				"flow": u.Query().Get("flow"), "flowID": fid}, "redirect")
			if err != nil {
				return nil, err
			}
			cur = next
		default:
			return nil, fmt.Errorf("stuck at %s (status %d)", u.Path, res.StatusCode)
		}
	}
	return nil, errors.New("too many redirects")
}

func (b *fakeBrowser) api(page *url.URL, path string, body map[string]any, field string) (string, error) {
	bs, _ := json.Marshal(body)
	res, err := b.c.Post(page.Scheme+"://"+page.Host+path, "application/json", bytes.NewReader(bs))
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var r struct {
		Status string         `json:"status"`
		Data   map[string]any `json:"data"`
	}
	_ = json.NewDecoder(res.Body).Decode(&r)
	next, _ := r.Data[field].(string)
	if r.Status != "OK" || next == "" {
		return "", fmt.Errorf("%s: status %d %s", path, res.StatusCode, r.Status)
	}
	return next, nil
}

// claim liest eine Angabe aus dem Token ohne Prüfung (nur im Test).
func claim(t *testing.T, raw, name string) string {
	t.Helper()
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		t.Fatal("not a JWT")
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	s, _ := m[name].(string)
	return s
}

// hostRewrite setzt den Host wie tailscale serve.
type hostRewrite struct{ host string }

func (h hostRewrite) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = h.host
	return http.DefaultTransport.RoundTrip(r)
}

// recorder ist ein minimaler ResponseWriter (nur der Status zählt).
type recorder struct {
	h    http.Header
	code int
	buf  bytes.Buffer
}

func (r *recorder) Header() http.Header { return r.h }
func (r *recorder) Write(b []byte) (int, error) {
	if r.code == 0 {
		r.code = http.StatusOK
	}
	return r.buf.Write(b)
}
func (r *recorder) WriteHeader(c int) {
	if r.code == 0 {
		r.code = c
	}
}
