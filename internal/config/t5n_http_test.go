package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nachbesserung T5, Punkt 9 (h): Ein Fehler nur in [http] lässt das Laden
// gelingen und steht in HTTPErr; ein unbekannter Schlüssel außerhalb von
// [http] hält das Laden weiter auf, auch zusammen mit einem unter [http].
// HTTPErr steht nie in JSON.
func TestT5NHTTPErrorDoesNotStopLoad(t *testing.T) {
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

	write("log_level = \"debug\"\n[http]\nlisten = \"8787\"\n")
	cfg, p, err := Load()
	if err != nil || cfg.HTTPErr == nil || !strings.Contains(cfg.HTTPErr.Error(), "host:port") {
		t.Fatalf("broken listen: load %v, HTTPErr %v", err, cfg.HTTPErr)
	}
	// Der Rest der Konfiguration gilt.
	if cfg.LogLevel != "debug" || p.DataDir != dir || !p.ConfigExists {
		t.Errorf("rest of the configuration: %+v %+v", cfg, p)
	}
	b, err := json.Marshal(cfg)
	var m map[string]json.RawMessage
	if err != nil || json.Unmarshal(b, &m) != nil || strings.Contains(string(b), "host:port") {
		t.Errorf("JSON: %v %s", err, b)
	}
	if _, ok := m["HTTPErr"]; ok {
		t.Errorf("HTTPErr in JSON: %s", b)
	}

	write("[http]\nlisten_typo = 1\nauth_typo = 2\n")
	if cfg, _, err = Load(); err != nil || cfg.HTTPErr == nil ||
		!strings.Contains(cfg.HTTPErr.Error(), "unknown keys: http.auth_typo, http.listen_typo") {
		t.Fatalf("unknown http keys: load %v, HTTPErr %v", err, cfg.HTTPErr)
	}

	write("data_dri = \"x\"\n[http]\nlisten_typo = 1\n")
	if _, _, err = Load(); err == nil || !strings.Contains(err.Error(), "unknown keys: data_dri, http.listen_typo") {
		t.Fatalf("unknown key outside [http]: %v", err)
	}

	// Grenze (bewusst): Ein falscher Typ ist ein Fehler der ganzen Datei.
	write("[http]\nlisten = 8787\n")
	if _, _, err = Load(); err == nil {
		t.Fatal("wrong type in [http] loaded")
	}

	write("[http]\nlisten = \"127.0.0.1:8788\"\n")
	if cfg, _, err = Load(); err != nil || cfg.HTTPErr != nil || cfg.HTTP.Listen != "127.0.0.1:8788" {
		t.Fatalf("valid [http]: %v %v %+v", err, cfg.HTTPErr, cfg.HTTP)
	}
}
