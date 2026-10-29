package httpmcp

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// Nachbesserung T5, Punkt 9 (d): Token-Fälle, die bisher kein Test
// abdeckte; nbf und iat mit höchstens 30 s Uhrabweichung (go-oidc allein:
// nbf mit 5 min, iat gar nicht).
func TestT5NTokenGaps(t *testing.T) {
	o := newOAuthEnv(t)
	// Cache füllen (kid k1 bekannt).
	if code, _, b := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("valid: %d %s", code, b)
	}
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	claims := o.claims("user-reader", nil)
	none := enc(map[string]string{"alg": "none", "typ": "at+jwt", "kid": "k1"}) + "." + enc(claims) + "."

	pubDER, err := x509.MarshalPKIXPublicKey(&o.as.key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	hs, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: pubDER}, (&jose.SignerOptions{}).WithType("at+jwt").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	hsTok, err := jwt.Signed(hs).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	at := func(d time.Duration) int64 { return time.Now().Add(d).Unix() }
	cases := []struct {
		name string
		tok  string
		want int
	}{
		{"alg none", none, http.StatusUnauthorized},
		{"HS256 with the public key as secret", hsTok, http.StatusUnauthorized},
		{"nbf 20 s in the future", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = at(20 * time.Second) }), http.StatusOK},
		{"nbf 40 s in the future", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = at(40 * time.Second) }), http.StatusUnauthorized},
		{"nbf 4 min in the future", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = at(4 * time.Minute) }), http.StatusUnauthorized},
		{"nbf 5 min 20 s in the future", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = at(5*time.Minute + 20*time.Second) }), http.StatusUnauthorized},
		{"nbf as text", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = "now" }), http.StatusUnauthorized},
		{"nbf in the past", o.token(t, "user-reader", func(c map[string]any) { c["nbf"] = at(-time.Minute) }), http.StatusOK},
		{"iat 20 s in the future", o.token(t, "user-reader", func(c map[string]any) { c["iat"] = at(20 * time.Second) }), http.StatusOK},
		{"iat 40 s in the future", o.token(t, "user-reader", func(c map[string]any) { c["iat"] = at(40 * time.Second) }), http.StatusUnauthorized},
		{"iat one day in the future", o.token(t, "user-reader", func(c map[string]any) { c["iat"] = at(24 * time.Hour) }), http.StatusUnauthorized},
		{"iat as text", o.token(t, "user-reader", func(c map[string]any) { c["iat"] = "now" }), http.StatusUnauthorized},
		{"without iat", o.token(t, "user-reader", func(c map[string]any) { delete(c, "iat") }), http.StatusOK},
		{"exp in 10 years", o.token(t, "user-reader", func(c map[string]any) { c["exp"] = time.Now().AddDate(10, 0, 0).Unix() }), http.StatusOK},
	}
	for _, c := range cases {
		if code, _, body := o.post(t, c.tok); code != c.want {
			t.Errorf("%s: %d, want %d (%s)", c.name, code, c.want, strings.TrimSpace(body))
		}
	}
}

// Aussteller in den Metadaten passt nicht → Discovery scheitert
// (TestDiscoverJWKS behauptete das nur im Kommentar).
func TestT5NDiscoverIssuerMismatch(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": "https://evil.example.org", "jwks_uri": srv.URL + "/k",
			"authorization_endpoint": srv.URL + "/a", "token_endpoint": srv.URL + "/t", "code_challenge_methods_supported": []string{"S256"}})
	})
	if got, err := discoverJWKS(context.Background(), srv.URL, srv.Client()); err == nil {
		t.Errorf("foreign issuer in metadata accepted: %q", got)
	}
}
