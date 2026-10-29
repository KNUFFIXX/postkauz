package httpmcp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// fakeAS: Autorisierungsserver im Test (S3.6 Akzeptanz 1): Metadaten nach
// RFC 8414, JWKS, signiert Token. Schlüsselwechsel mit neuer kid möglich.
type fakeAS struct {
	srv  *httptest.Server
	mu   sync.Mutex
	key  *rsa.PrivateKey
	kid  string
	down atomic.Bool
	// jwksHits zählt Abrufe der JWKS (Cache).
	jwksHits atomic.Int32
	// metaDelay verzögert die Metadaten (Nanosekunden; langsame Discovery).
	metaDelay atomic.Int64
}

func newFakeAS(t *testing.T) *fakeAS {
	t.Helper()
	as := &fakeAS{kid: "k1"}
	as.key = genKey(t)
	mux := http.NewServeMux()
	as.srv = httptest.NewTLSServer(mux)
	meta := func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Duration(as.metaDelay.Load()))
		if as.down.Load() {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": as.srv.URL, "jwks_uri": as.srv.URL + "/jwks",
			"authorization_endpoint": as.srv.URL + "/authorize", "token_endpoint": as.srv.URL + "/token",
			"response_types_supported": []string{"code"}, "code_challenge_methods_supported": []string{"S256"},
		})
	}
	mux.HandleFunc("/.well-known/oauth-authorization-server", meta)
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		as.jwksHits.Add(1)
		as.mu.Lock()
		defer as.mu.Unlock()
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &as.key.PublicKey, KeyID: as.kid, Algorithm: "RS256", Use: "sig"}}})
	})
	t.Cleanup(as.srv.Close)
	return as
}

func genKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// rotate wechselt den Signaturschlüssel samt kid.
func (as *fakeAS) rotate(t *testing.T) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.key, as.kid = genKey(t), as.kid+"x"
}

// sign signiert claims mit dem aktuellen Schlüssel (key = nil) oder einem
// fremden; typ leer = ohne typ.
func (as *fakeAS) sign(t *testing.T, key *rsa.PrivateKey, typ string, claims map[string]any) string {
	t.Helper()
	as.mu.Lock()
	kid := as.kid
	if key == nil {
		key = as.key
	}
	as.mu.Unlock()
	opts := (&jose.SignerOptions{}).WithHeader("kid", kid)
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

const testResource = "https://node.example.net/mcp"

type oauthEnv struct {
	*testEnv
	as  *fakeAS
	ts  *httptest.Server
	srv *Server
}

func newOAuthEnv(t *testing.T) *oauthEnv {
	t.Helper()
	e := newEnv(t)
	as := newFakeAS(t)
	ts, s := e.start(t, func(h *config.HTTP) {
		h.Auth = config.HTTPAuthOAuth
		h.Resource = testResource
		h.Issuer = as.srv.URL
		h.AllowedHosts = []string{"node.example.net"}
		h.Principals = map[string]string{"user-reader": "reader", "user-assistant": "assistant", "user-gone": "no-such-key"}
	}, func(d *Deps) { d.HTTPClient = as.srv.Client() })
	return &oauthEnv{testEnv: e, as: as, ts: ts, srv: s}
}

func (o *oauthEnv) claims(sub string, mod func(map[string]any)) map[string]any {
	now := time.Now()
	c := map[string]any{"iss": o.as.srv.URL, "sub": sub, "aud": []string{testResource}, "client_id": "claude-code",
		"exp": now.Add(5 * time.Minute).Unix(), "iat": now.Unix(), "scope": "openid offline_access"}
	if mod != nil {
		mod(c)
	}
	return c
}

func (o *oauthEnv) token(t *testing.T, sub string, mod func(map[string]any)) string {
	return o.as.sign(t, nil, "at+jwt", o.claims(sub, mod))
}

// hostRT schreibt den Host wie tailscale serve (Name des Knotens).
type hostRT struct{ base http.RoundTripper }

func (h hostRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Host = "node.example.net"
	return h.base.RoundTrip(r)
}

func (o *oauthEnv) post(t *testing.T, tok string) (int, http.Header, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, o.ts.URL+PathMCP, strings.NewReader(initBody))
	req.Host = "node.example.net"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, res.Header, string(b)
}

// S3.6 Akzeptanz 1 (ADR-010 Punkt 5): gültiges Token → Tools nach dem
// Schlüssel; andere aud, abgelaufen, falsche Signatur, fremder iss, kein
// at+jwt, ID-Token → 401; unbekanntes sub → 403 ohne Hinweis.
func TestOAuthTokenChecks(t *testing.T) {
	o := newOAuthEnv(t)
	cs, _ := connect(t, o.ts.URL+PathMCP, hostRT{http.DefaultTransport}, bearer(o.token(t, "user-reader", nil)))
	got := toolNames(t, cs)
	if want := o.stdioToolNames(t, "reader"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("reader over oauth: %v, want %v", got, want)
	}
	cs2, _ := connect(t, o.ts.URL+PathMCP, hostRT{http.DefaultTransport}, bearer(o.token(t, "user-assistant", nil)))
	if n := len(toolNames(t, cs2)); n <= len(got) {
		t.Errorf("assistant has %d tools, reader %d", n, len(got))
	}

	other := genKey(t)
	cases := []struct {
		name string
		tok  string
		want int
	}{
		{"valid", o.token(t, "user-reader", nil), http.StatusOK},
		{"aud with trailing slash", o.token(t, "user-reader", func(c map[string]any) { c["aud"] = testResource + "/" }), http.StatusOK},
		{"expired within the clock skew", o.token(t, "user-reader", func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Second).Unix() }), http.StatusOK},
		{"typ application/at+jwt", o.as.sign(t, nil, "application/at+jwt", o.claims("user-reader", nil)), http.StatusOK},
		{"other audience", o.token(t, "user-reader", func(c map[string]any) { c["aud"] = "https://other.example.net/mcp" }), http.StatusUnauthorized},
		{"no audience", o.token(t, "user-reader", func(c map[string]any) { delete(c, "aud") }), http.StatusUnauthorized},
		{"id token (aud = client)", o.as.sign(t, nil, "JWT", o.claims("user-reader", func(c map[string]any) { c["aud"] = "claude-code" })), http.StatusUnauthorized},
		{"typ JWT with the right aud", o.as.sign(t, nil, "JWT", o.claims("user-reader", nil)), http.StatusUnauthorized},
		{"without typ", o.as.sign(t, nil, "", o.claims("user-reader", nil)), http.StatusUnauthorized},
		{"expired", o.token(t, "user-reader", func(c map[string]any) { c["exp"] = time.Now().Add(-time.Minute).Unix() }), http.StatusUnauthorized},
		{"no exp", o.token(t, "user-reader", func(c map[string]any) { delete(c, "exp") }), http.StatusUnauthorized},
		{"foreign issuer", o.token(t, "user-reader", func(c map[string]any) { c["iss"] = "https://evil.example.org" }), http.StatusUnauthorized},
		{"wrong signature", o.as.sign(t, other, "at+jwt", o.claims("user-reader", nil)), http.StatusUnauthorized},
		{"garbage", "a.b.c", http.StatusUnauthorized},
		{"API key in oauth mode", o.keys["reader"], http.StatusUnauthorized},
		{"no sub", o.token(t, "", nil), http.StatusUnauthorized},
		{"unknown sub", o.token(t, "user-unknown", nil), http.StatusForbidden},
		{"sub mapped to a missing key", o.token(t, "user-gone", nil), http.StatusForbidden},
	}
	for _, c := range cases {
		code, hdr, body := o.post(t, c.tok)
		if code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, code, c.want, body)
			continue
		}
		switch code {
		case http.StatusUnauthorized:
			if !strings.Contains(hdr.Get("WWW-Authenticate"), `resource_metadata="https://node.example.net/.well-known/oauth-protected-resource/mcp"`) {
				t.Errorf("%s: WWW-Authenticate %q", c.name, hdr.Get("WWW-Authenticate"))
			}
		case http.StatusForbidden:
			if strings.TrimSpace(body) != "forbidden" {
				t.Errorf("%s: body %q gives a hint", c.name, body)
			}
		}
	}
	// Unbekanntes Subjekt: im Audit (für den Betreiber), nie im Text an den
	// Client; nie ein Token in Audit oder Log.
	found := false
	for _, r := range o.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "principal_unknown" && strings.Contains(string(r.ParamsRedacted), `"sub":"user-unknown"`) {
			found = true
		}
	}
	if !found {
		t.Error("unknown subject not in the audit")
	}
	for _, c := range cases {
		sig := c.tok[strings.LastIndex(c.tok, ".")+1:]
		if len(sig) < 20 {
			continue
		}
		if strings.Contains(o.log.String(), sig) {
			t.Errorf("%s: token in the log", c.name)
		}
		for _, r := range o.auditAll(t) {
			if strings.Contains(string(r.ParamsRedacted), sig) {
				t.Errorf("%s: token in audit entry %d", c.name, r.ID)
			}
		}
	}
}

// S3.6 Akzeptanz 3: Das Audit nennt transport http, Principal, iss und sub,
// nie das Token; Rechte nur aus dem Schlüssel (das Token mit mehr Scopes
// erweitert nichts).
func TestOAuthAuditAndScopes(t *testing.T) {
	o := newOAuthEnv(t)
	tok := o.token(t, "user-reader", func(c map[string]any) { c["scope"] = "openid mail:draft admin" })
	cs, _ := connect(t, o.ts.URL+PathMCP, hostRT{http.DefaultTransport}, bearer(tok))
	for _, n := range toolNames(t, cs) {
		if n == "mail_create_draft" {
			t.Error("token scope widened the key")
		}
	}
	ctx := context.Background()
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_list_accounts", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	ok := false
	for _, r := range o.auditAll(t) {
		if r.Operation != "accounts.list" {
			continue
		}
		var p struct {
			Via ops.Origin `json:"via"`
		}
		_ = json.Unmarshal(r.ParamsRedacted, &p)
		if p.Via.Transport == "http" && p.Via.Principal == "reader" && p.Via.Issuer == o.as.srv.URL && p.Via.Subject == "user-reader" && r.KeyID == "key:reader" {
			ok = true
		}
		if strings.Contains(string(r.ParamsRedacted), tok[strings.LastIndex(tok, ".")+1:]) {
			t.Error("token in the audit")
		}
	}
	if !ok {
		t.Error("audit lacks transport, principal, iss or sub")
	}
}

// Widerrufener Schlüssel hinter einem gültigen Token → 403 denied
// (key_revoked), im Audit (D8 Schritt 5).
func TestOAuthRevokedKey(t *testing.T) {
	o := newOAuthEnv(t)
	ctx := context.Background()
	k, _ := o.st.APIKeyByName(ctx, "reader")
	if _, err := o.st.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	code, _, body := o.post(t, o.token(t, "user-reader", nil))
	if code != http.StatusForbidden || !strings.Contains(body, `"key_revoked"`) {
		t.Fatalf("revoked: %d %s", code, body)
	}
	found := false
	for _, r := range o.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "key_revoked" && r.KeyID == "key:reader" {
			found = true
		}
	}
	if !found {
		t.Error("revoked key not in the audit")
	}
}

// JWKS-Cache mit Neuladen bei unbekannter kid (S3.6; Spike, offener Punkt
// 3): Nach einem Schlüsselwechsel im AS gelten neue Token ohne Neustart.
func TestOAuthKeyRotation(t *testing.T) {
	o := newOAuthEnv(t)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("before rotation: %d %s", code, body)
	}
	hits := o.as.jwksHits.Load()
	if code, _, _ := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK || o.as.jwksHits.Load() != hits {
		t.Errorf("second token fetched the JWKS again (%d → %d)", hits, o.as.jwksHits.Load())
	}
	o.as.rotate(t)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("after rotation: %d %s", code, body)
	}
}

// Ist der AS beim Start nicht erreichbar, läuft PostKauz trotzdem: 503,
// später (nach discoveryRetry) wieder 200. PRM nennt den AS und die Scopes.
func TestOAuthDiscoveryAndMetadata(t *testing.T) {
	o := newOAuthEnv(t)
	o.as.down.Store(true)
	now := time.Now()
	o.srv.d.Now = func() time.Time { return now }
	o.srv.auth.(*oauthAuth).now = o.srv.d.Now
	if code, hdr, _ := o.post(t, o.as.sign(t, nil, "at+jwt", o.claims("user-reader", nil))); code != http.StatusServiceUnavailable || hdr.Get("Retry-After") == "" {
		t.Fatalf("AS down: %d", code)
	}
	o.as.down.Store(false)
	if code, _, _ := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusServiceUnavailable {
		t.Errorf("retry before discoveryRetry: %d, want 503", code)
	}
	now = now.Add(discoveryRetry + time.Second)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Errorf("after retry: %d %s", code, body)
	}

	req, _ := http.NewRequest(http.MethodGet, o.ts.URL+prmPath+"/mcp", nil)
	req.Host = "node.example.net"
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var meta struct {
		Resource string   `json:"resource"`
		AS       []string `json:"authorization_servers"`
		Scopes   []string `json:"scopes_supported"`
	}
	_ = json.NewDecoder(res.Body).Decode(&meta)
	if meta.Resource != testResource || len(meta.AS) != 1 || meta.AS[0] != o.as.srv.URL || strings.Join(meta.Scopes, " ") != "openid offline_access" {
		t.Errorf("metadata: %+v", meta)
	}
}

// Discovery: RFC 8414 mit Pfad des Ausstellers, sonst OpenID-Discovery;
// der Aussteller in den Metadaten muss genau passen.
func TestDiscoverJWKS(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	issuer := srv.URL + "/realm"
	mux.HandleFunc("/realm/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer, "jwks_uri": srv.URL + "/keys",
			"authorization_endpoint": srv.URL + "/a", "token_endpoint": srv.URL + "/t", "code_challenge_methods_supported": []string{"S256"}})
	})
	got, err := discoverJWKS(context.Background(), issuer, srv.Client())
	if err != nil || got != srv.URL+"/keys" {
		t.Fatalf("openid fallback: %q %v", got, err)
	}
	if _, err := discoverJWKS(context.Background(), srv.URL+"/other", srv.Client()); err == nil {
		t.Error("no metadata: no error")
	}
}
