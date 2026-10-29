package httpmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/KNUFFIXX/postkauz/internal/config"
)

// Nachbesserung T5, Punkt 3: jwks_uri mit http (nicht Loopback) wird
// abgelehnt.
func TestT5NDiscoverRejectsHTTPJWKS(t *testing.T) {
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "jwks_uri": "http://keys.example.net/jwks",
			"authorization_endpoint": srv.URL + "/a", "token_endpoint": srv.URL + "/t", "code_challenge_methods_supported": []string{"S256"}})
	})
	got, err := discoverJWKS(context.Background(), srv.URL, srv.Client())
	if err == nil || !strings.Contains(err.Error(), "must use https") {
		t.Errorf("http jwks_uri: %q %v, want an error", got, err)
	}
}

func TestT5NCheckJWKSURI(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://as.example.net/jwks":    true,
		"HTTPS://as.example.net/jwks":    true,
		"http://localhost:9091/jwks":     true,
		"http://127.0.0.5/jwks":          true,
		"http://[::1]:8080/jwks":         true,
		"http://as.example.net/jwks":     false,
		"http://100.64.0.1/jwks":         false,
		"http://localhost.example.net/k": false,
		"ftp://as.example.net/jwks":      false,
		"/jwks":                          false,
		"":                               false,
	} {
		if err := checkJWKSURI(raw); (err == nil) != ok {
			t.Errorf("%q: %v, want ok=%v", raw, err, ok)
		}
	}
}

// jwksEnv: OAuth-Umgebung mit eigenem AS, dessen JWKS-Handler der Test
// vorgibt.
func jwksEnv(t *testing.T, jwks http.HandlerFunc) (*oauthEnv, string) {
	t.Helper()
	mux := http.NewServeMux()
	as := httptest.NewTLSServer(mux)
	t.Cleanup(as.Close)
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": as.URL, "jwks_uri": as.URL + "/jwks",
			"authorization_endpoint": as.URL + "/a", "token_endpoint": as.URL + "/t",
			"code_challenge_methods_supported": []string{"S256"}})
	})
	mux.HandleFunc("/jwks", jwks)
	e := newEnv(t)
	o := &oauthEnv{testEnv: e}
	o.ts, o.srv = e.start(t, func(h *config.HTTP) {
		h.Auth = config.HTTPAuthOAuth
		h.Resource = testResource
		h.Issuer = as.URL
		h.AllowedHosts = []string{"node.example.net"}
		h.Principals = map[string]string{"user-reader": "reader"}
	}, func(d *Deps) { d.HTTPClient = as.Client() })
	key := genKey(t)
	sig, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("at+jwt"))
	tok, _ := jwt.Signed(sig).Claims(map[string]any{"iss": as.URL, "sub": "user-reader", "aud": testResource,
		"exp": time.Now().Add(time.Minute).Unix()}).Serialize()
	return o, tok
}

// Nachbesserung T5, Punkt 3: Hängt die JWKS (Verbindung offen, keine
// Antwort), antwortet PostKauz binnen 15 s mit 503 und Retry-After (Frist
// des JWKS-Clients 10 s, Frist der Anmeldung 15 s).
func TestT5NJWKSHangAnswers503(t *testing.T) {
	block := make(chan struct{})
	o, tok := jwksEnv(t, func(http.ResponseWriter, *http.Request) { <-block })
	defer close(block) // vor den Cleanups, sonst blockiert as.Close
	req, _ := http.NewRequest(http.MethodPost, o.ts.URL+PathMCP, strings.NewReader(initBody))
	req.Host = "node.example.net"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	start := time.Now()
	res, err := (&http.Client{Timeout: 16 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("no answer after %v while the JWKS hangs: %v", time.Since(start).Round(time.Second), err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable || res.Header.Get("Retry-After") == "" {
		t.Errorf("hanging JWKS: %d (Retry-After %q), want 503", res.StatusCode, res.Header.Get("Retry-After"))
	}
	if d := time.Since(start); d > authTimeout+time.Second {
		t.Errorf("answer after %v", d)
	}
}

// Eine JWKS mit Fehlerstatus, Umleitung oder ohne JSON ist kein schlechtes Token:
// 503, nicht 401.
func TestT5NJWKSErrorIs503(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"status 502": func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusBadGateway) },
		"redirect":   func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/elsewhere", http.StatusFound) },
		"not json":   func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
	} {
		o, tok := jwksEnv(t, h)
		code, hdr, body := o.post(t, tok)
		want := http.StatusServiceUnavailable
		if code != want || (code == http.StatusServiceUnavailable && hdr.Get("Retry-After") == "") {
			t.Errorf("%s: %d %s, want %d", name, code, strings.TrimSpace(body), want)
		}
	}
}
