package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zusatz zu S2.4: [draft] quote_line kennt {date_numeric}; ein
// verschriebener Platzhalter bleibt ein Fehler, die Meldung nennt alle drei.
func TestQuoteLineDateNumeric(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	if err := os.WriteFile(f, []byte("[draft]\nquote_line = \"Am {date_numeric} schrieb {from}:\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil || cfg.Draft.QuoteLine != "Am {date_numeric} schrieb {from}:" {
		t.Fatalf("quote_line = %q, %v", cfg.Draft.QuoteLine, err)
	}
	if err := os.WriteFile(f, []byte("[draft]\nquote_line = \"Am {date_num} schrieb {from}:\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil || !strings.Contains(err.Error(), "{date}, {date_numeric} and {from}") || strings.Contains(err.Error(), "Am ") {
		t.Fatalf("typo: %v", err)
	}
}
