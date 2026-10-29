package httpmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/config"
)

// Nachbesserung T5, Punkt 1 (BLOCKER): Ohne Deps.HTTPClient nahm die
// Discovery den Transport des SDK mit SSRF-Sperre, der 100.64.0.0/10 und
// fd7a:… verweigert. Läuft nur mit T5N_TAILNET_HOST (Name, der auf eine
// Tailnet-Adresse zeigt, z. B. über /etc/hosts, ohne Proxy-Umgebung).
func TestT5NDiscoveryToTailnetName(t *testing.T) {
	host := os.Getenv("T5N_TAILNET_HOST")
	if host == "" {
		t.Skip("T5N_TAILNET_HOST not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := discoverJWKS(ctx, "https://"+host+":8443", nil)
	if err != nil && strings.Contains(err.Error(), "non-public") {
		t.Errorf("discovery to a tailnet AS refused by the SDK SSRF guard: %v", err)
	}
}

// Festlegung: Der Aussteller braucht einen DNS-Namen (tailscale serve
// stellt Zertifikate nur für MagicDNS-Namen aus). Eine IP-Adresse außer
// Loopback scheitert mit einem eigenen, klaren Fehler, nicht an der
// SSRF-Sperre des SDK und ohne Netzverkehr.
func TestT5NDiscoveryToTailnetIP(t *testing.T) {
	for _, iss := range []string{"https://100.101.102.103:8443", "https://[fd7a:115c:a1e0::1]:8443", "https://192.0.2.10"} {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := discoverJWKS(ctx, iss, nil)
		cancel()
		if err == nil || strings.Contains(err.Error(), "non-public") || !strings.Contains(err.Error(), "DNS name") {
			t.Errorf("%s: %v, want the error about a DNS name", iss, err)
		}
	}
}

// Der Standard-Client hat einen eigenen DialContext (damit gilt die
// SSRF-Sperre des SDK nicht, ein Name im Tailnet wird gewählt), Fristen von
// 10 s und folgt keiner Umleitung. New setzt ihn im Modus oauth ein.
func TestT5NDefaultASClient(t *testing.T) {
	c := newASClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatalf("transport %T without its own DialContext", c.Transport)
	}
	if c.Timeout != asTimeout || asTimeout != 10*time.Second {
		t.Errorf("client timeout %v", c.Timeout)
	}
	if c.CheckRedirect == nil || c.CheckRedirect(nil, nil) == nil {
		t.Error("client follows redirects")
	}

	e := newEnv(t)
	_, s := e.start(t, func(h *config.HTTP) {
		h.Auth = config.HTTPAuthOAuth
		h.Resource = testResource
		h.Issuer = "https://as.example.net"
		h.AllowedHosts = []string{"node.example.net"}
		h.Principals = map[string]string{"user-reader": "reader"}
	}, nil)
	ac := s.auth.(*oauthAuth).client
	if ac == nil || ac == http.DefaultClient {
		t.Fatal("oauth mode without its own client")
	}
	if tr, ok := ac.Transport.(*http.Transport); !ok || tr.DialContext == nil || ac.CheckRedirect == nil {
		t.Errorf("oauth mode client: %T", ac.Transport)
	}
}

// Discovery folgt keiner Umleitung (auch nicht auf denselben Host).
func TestT5NDiscoveryNoRedirect(t *testing.T) {
	var target atomic.Int32
	mux := http.NewServeMux()
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()
	mux.HandleFunc("/.well-known/oauth-authorization-server", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, _ *http.Request) {
		target.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "jwks_uri": srv.URL + "/jwks",
			"authorization_endpoint": srv.URL + "/a", "token_endpoint": srv.URL + "/t", "code_challenge_methods_supported": []string{"S256"}})
	})
	c := newASClient()
	c.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig
	_, err := discoverJWKS(context.Background(), srv.URL, c)
	if err == nil || target.Load() != 0 || !strings.Contains(err.Error(), "redirects are not followed") {
		t.Errorf("redirect followed: err %v, hits on the target %d", err, target.Load())
	}
}
