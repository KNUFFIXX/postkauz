package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zweite Nachbesserung T5, Punkt 5 (a): Ein falscher Typ in [http] hält
// nicht mehr jedes Kommando auf, sondern steht nur in HTTPErr; der Rest
// der Konfiguration gilt. Syntaxfehler der Datei und falsche Typen
// außerhalb von [http] halten das Laden weiter auf.
func TestT5N2HTTPTypeErrorDoesNotStopLoad(t *testing.T) {
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
	for _, c := range []struct{ toml, want string }{
		{"log_level = \"debug\"\n[http]\nmax_requests_per_minute = \"60\"\n", "incompatible types"},
		{"log_level = \"debug\"\n[http]\nlisten = 8787\n", "incompatible types"},
		{"log_level = \"debug\"\nhttp = 5\n", "[http]"},
		{"log_level = \"debug\"\n[http]\nlisten = \"127.0.0.1:8787\"\nmax_requests_per_minute = \"60\"\nlisten_typo = 1\n", "incompatible types"},
	} {
		write(c.toml)
		cfg, p, err := Load()
		if err != nil {
			t.Errorf("%q: load failed: %v", c.toml, err)
			continue
		}
		if cfg.HTTPErr == nil || !strings.Contains(cfg.HTTPErr.Error(), c.want) || !strings.Contains(cfg.HTTPErr.Error(), "[http]") {
			t.Errorf("%q: HTTPErr %v, want it to name [http] and %q", c.toml, cfg.HTTPErr, c.want)
		}
		if cfg.LogLevel != "debug" || !p.ConfigExists || cfg.HTTP.Listen != "" {
			t.Errorf("%q: rest of the configuration %+v %+v", c.toml, cfg, p)
		}
	}

	// Global bleiben: Syntaxfehler (auch in [http]) und falsche Typen
	// außerhalb von [http].
	for _, c := range []string{
		"[http]\nlisten = \"127.0.0.1:8787\n",
		"[http\nlisten = \"127.0.0.1:8787\"\n",
		"log_level = 5\n[http]\nlisten = \"127.0.0.1:8787\"\n",
	} {
		write(c)
		if _, _, err := Load(); err == nil {
			t.Errorf("%q: loaded, want an error for the whole file", c)
		}
	}
}
