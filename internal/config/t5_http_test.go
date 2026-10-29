package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T5 (ADR-010): [http] mit Standards und Prüfung beim Laden.
func TestLoadHTTP(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	h := cfg.HTTP
	if h.Listen != DefaultHTTPListen || h.Auth != HTTPAuthKey || h.Resource != "http://127.0.0.1:8787/mcp" ||
		h.MaxRequestsPerMinute != DefaultMaxRequestsPerMin || len(h.Scopes) != 2 {
		t.Fatalf("defaults: %+v", h)
	}
	// --listen bildet die Standard-Resource neu, eine eigene bleibt.
	h.Listen = "127.0.0.1:9999"
	if err := CheckHTTP(&h); err != nil || h.Resource != "http://127.0.0.1:9999/mcp" {
		t.Fatalf("new listen: %v %s", err, h.Resource)
	}

	write(`[http]
auth = "oauth"
resource = "https://node.example.net/mcp"
issuer = "https://auth.example.net"
max_requests_per_minute = 60
[http.principals]
"0b6f-uuid" = "cloud-reader"
`)
	if cfg, _, err = Load(); err != nil || cfg.HTTP.Principals["0b6f-uuid"] != "cloud-reader" || cfg.HTTP.MaxRequestsPerMinute != 60 {
		t.Fatalf("oauth: %v %+v", err, cfg.HTTP)
	}
	h = cfg.HTTP
	h.Listen = "127.0.0.1:9999"
	if err := CheckHTTP(&h); err != nil || h.Resource != "https://node.example.net/mcp" {
		t.Fatalf("own resource changed: %v %s", err, h.Resource)
	}

	for _, bad := range []struct{ toml, want string }{
		{"[http]\nlisten = \"8787\"", "host:port"},
		{"[http]\nlisten = \"node.example.net:8787\"", "IP address"},
		{"[http]\nauth = \"basic\"", "http.auth"},
		{"[http]\nauth = \"oauth\"\nissuer = \"https://auth.example.net\"", "http.resource is required"},
		{"[http]\nauth = \"oauth\"\nresource = \"https://n.example.net/mcp\"", "http.issuer is required"},
		{"[http]\nauth = \"oauth\"\nresource = \"https://n.example.net/mcp\"\nissuer = \"http://auth.example.net\"\n[http.principals]\na = \"b\"", "https"},
		{"[http]\nauth = \"oauth\"\nresource = \"https://n.example.net/mcp\"\nissuer = \"https://auth.example.net\"", "principals"},
		{"[http]\nresource = \"https://u:p@n.example.net/mcp\"", "user info"},
		{"[http]\nmax_requests_per_minute = 100000", "max_requests_per_minute"},
		{"[http.principals]\nsub = \"Not A Key\"", "invalid key name"},
		{"[http]\nallowed_hosts = [\"a b\"]", "allowed_hosts"},
		{"[http]\nlisten_typo = 1", "unknown keys"},
	} {
		// Seit der Nachbesserung T5 (Punkt 9h) lädt die Konfiguration
		// trotzdem; der Fehler steht in HTTPErr (nur mcp --http und doctor).
		write(bad.toml)
		cfg, _, err := Load()
		if err != nil || cfg.HTTPErr == nil || !strings.Contains(cfg.HTTPErr.Error(), bad.want) {
			t.Errorf("%q: load %v, HTTPErr %v, want %q", bad.toml, err, cfg.HTTPErr, bad.want)
		}
	}
}
