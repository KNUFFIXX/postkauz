package rs

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeAS stellt nur JWKS und Discovery bereit und signiert Token im Test.
type fakeAS struct {
	srv *httptest.Server
	key *rsa.PrivateKey
	kid string
}

func newFakeAS(t *testing.T) *fakeAS {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	as := &fakeAS{key: k, kid: "k1"}
	mux := http.NewServeMux()
	as.srv = httptest.NewTLSServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": as.srv.URL, "jwks_uri": as.srv.URL + "/jwks",
			"authorization_endpoint": as.srv.URL + "/auth", "token_endpoint": as.srv.URL + "/token",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &as.key.PublicKey, KeyID: as.kid, Algorithm: "RS256", Use: "sig"}}})
	})
	t.Cleanup(as.srv.Close)
	return as
}

func (as *fakeAS) sign(t *testing.T, key *rsa.PrivateKey, typ string, claims map[string]any) string {
	t.Helper()
	opts := (&jose.SignerOptions{}).WithHeader("kid", as.kid)
	if typ != "" {
		opts = opts.WithType(jose.ContentType(typ))
	}
	s, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, opts)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(s).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type env struct {
	as       *fakeAS
	rs       *httptest.Server
	resource string
}

func setup(t *testing.T, mod func(*Config)) *env {
	t.Helper()
	as := newFakeAS(t)
	rsSrv := httptest.NewUnstartedServer(nil)
	rsSrv.StartTLS()
	t.Cleanup(rsSrv.Close)
	resource := rsSrv.URL + "/mcp"
	cfg := Config{
		Resource:     resource,
		Issuer:       as.srv.URL,
		Principals:   map[string]string{"user-1": "cloud-reader"},
		RequireATJWT: true,
		ClockSkew:    30 * time.Second,
		HTTPClient:   as.srv.Client(),
	}
	if mod != nil {
		mod(&cfg)
	}
	h, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	rsSrv.Config.Handler = h
	return &env{as: as, rs: rsSrv, resource: resource}
}

func (e *env) claims(mod func(map[string]any)) map[string]any {
	now := time.Now()
	c := map[string]any{
		"iss": e.as.srv.URL, "sub": "user-1", "aud": e.resource, "client_id": "claude-code",
		"iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(), "scope": "mcp",
	}
	if mod != nil {
		mod(c)
	}
	return c
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

func (e *env) post(t *testing.T, token, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.resource, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := e.rs.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestNoBearer401WithResourceMetadata(t *testing.T) {
	e := setup(t, nil)
	resp := e.post(t, "", initBody)
	if resp.StatusCode != 401 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	wa := resp.Header.Get("WWW-Authenticate")
	if !strings.Contains(wa, `resource_metadata="`+e.rs.URL+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatalf("WWW-Authenticate %q", wa)
	}
}

func TestPRM(t *testing.T) {
	e := setup(t, nil)
	for _, p := range []string{"/.well-known/oauth-protected-resource/mcp", "/.well-known/oauth-protected-resource"} {
		resp, err := e.rs.Client().Get(e.rs.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&m)
		resp.Body.Close()
		if m["resource"] != e.resource || m["authorization_servers"].([]any)[0] != e.as.srv.URL {
			t.Fatalf("%s: %v", p, m)
		}
	}
}

func TestTokenCases(t *testing.T) {
	e := setup(t, nil)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"valid", e.as.sign(t, e.as.key, "at+jwt", e.claims(nil)), 200},
		{"other audience", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["aud"] = "https://other.example.com/mcp" })), 401},
		{"audience is client id (id token style)", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["aud"] = "claude-code" })), 401},
		{"trailing slash audience", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["aud"] = []string{"x", e.resource + "/"} })), 200},
		{"expired", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() })), 401},
		{"expired within skew", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Second).Unix() })), 200},
		{"no exp", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { delete(c, "exp") })), 401},
		{"bad signature", e.as.sign(t, other, "at+jwt", e.claims(nil)), 401},
		{"other issuer", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["iss"] = "https://evil.example.com" })), 401},
		{"typ JWT (id token)", e.as.sign(t, e.as.key, "JWT", e.claims(nil)), 401},
		{"unknown sub", e.as.sign(t, e.as.key, "at+jwt", e.claims(func(c map[string]any) { c["sub"] = "user-2" })), 403},
		{"garbage", "abc.def.ghi", 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := e.post(t, c.token, initBody)
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.want {
				t.Fatalf("status %d, body %q", resp.StatusCode, body)
			}
			if strings.Contains(string(body), c.token) || strings.Contains(resp.Header.Get("WWW-Authenticate"), c.token) {
				t.Fatal("token echoed")
			}
			if c.want == 403 && strings.Contains(string(body), "user-2") {
				t.Fatal("403 names the subject")
			}
			if c.want == 200 && resp.Header.Get("Mcp-Session-Id") != "" {
				t.Fatal("stateless server set a session id")
			}
		})
	}
}

func TestATJWTOptional(t *testing.T) {
	e := setup(t, func(c *Config) { c.RequireATJWT = false })
	if r := e.post(t, e.as.sign(t, e.as.key, "JWT", e.claims(nil)), initBody); r.StatusCode != 200 {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestGetDelete405(t *testing.T) {
	e := setup(t, nil)
	tok := e.as.sign(t, e.as.key, "at+jwt", e.claims(nil))
	for _, m := range []string{http.MethodGet, http.MethodDelete} {
		req, _ := http.NewRequest(m, e.resource, nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Accept", "text/event-stream")
		resp, err := e.rs.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 405 {
			t.Fatalf("%s: %d", m, resp.StatusCode)
		}
	}
}

type bearerRT struct {
	tok string
	rt  http.RoundTripper
}

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return b.rt.RoundTrip(r)
}

// Ende zu Ende mit dem SDK-Client: initialize, tools/list, tools/call.
func TestSDKClientStateless(t *testing.T) {
	e := setup(t, nil)
	tok := e.as.sign(t, e.as.key, "at+jwt", e.claims(nil))
	hc := &http.Client{Transport: bearerRT{tok, e.rs.Client().Transport}}
	c := mcp.NewClient(&mcp.Implementation{Name: "probe", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: e.resource, HTTPClient: hc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 1 {
		t.Fatalf("tools %v %v", tools, err)
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "whoami"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(b), `"key":"cloud-reader"`) || !strings.Contains(string(b), `"transport":"http"`) {
		t.Fatalf("whoami %s", b)
	}
	t.Logf("negotiated protocol: %s", cs.InitializeResult().ProtocolVersion)
}

// Hinter einem Reverse-Proxy (tailscale serve, Caddy) kommt die Anfrage über
// Loopback mit fremdem Host-Kopf an. Der DNS-Rebinding-Schutz des SDK lehnt
// das ab; gemessen wird hier, was passiert.
func TestBehindReverseProxyHostHeader(t *testing.T) {
	for _, c := range []struct {
		name string
		mod  func(*Config)
		want int
	}{
		{"sdk protection", func(c *Config) {
			c.SDKLocalhostProtection = true
			c.AllowedHosts = []string{"postkauz.tailnet-example.ts.net"}
		}, http.StatusForbidden},
		{"host not allowed", nil, http.StatusMisdirectedRequest},
		{"host allowed", func(c *Config) { c.AllowedHosts = []string{"postkauz.tailnet-example.ts.net"} }, http.StatusOK},
	} {
		t.Run(c.name, func(t *testing.T) { proxyCase(t, c.mod, c.want) })
	}
}

func proxyCase(t *testing.T, mod func(*Config), want int) {
	e := setup(t, mod)
	tok := e.as.sign(t, e.as.key, "at+jwt", e.claims(nil))
	target, _ := url.Parse(e.rs.URL)
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.Transport = e.rs.Client().Transport
	orig := rp.Director
	rp.Director = func(r *http.Request) { orig(r); r.Host = "postkauz.tailnet-example.ts.net" }
	front := httptest.NewTLSServer(rp)
	defer front.Close()
	req, _ := http.NewRequest(http.MethodPost, front.URL+"/mcp", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := front.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	t.Logf("behind proxy with foreign Host: status %d body %q", resp.StatusCode, strings.TrimSpace(string(b)))
	if resp.StatusCode != want {
		t.Fatalf("want %d, got %d", want, resp.StatusCode)
	}
}
