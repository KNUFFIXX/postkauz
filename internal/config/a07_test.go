package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A07 Teil 4: [draft] copy_gone_after nur als Zeichenkette; fehlt der
// Schlüssel, gilt 24h; "off" und "0" schalten ab; eine Zahl ist ein Fehler
// mit Hinweis.
func TestA07CopyGoneAfter(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	for _, c := range []struct {
		name, body string
		want       time.Duration // Effective()
		err        string
	}{
		{"key missing", "", 24 * time.Hour, ""},
		{"no file section", "[draft]\nmailbox = \"Drafts\"\n", 24 * time.Hour, ""},
		{"off", "[draft]\ncopy_gone_after = \"off\"\n", -1, ""},
		{"OFF", "[draft]\ncopy_gone_after = \" OFF \"\n", -1, ""},
		{"zero string", "[draft]\ncopy_gone_after = \"0\"\n", -1, ""},
		{"0s", "[draft]\ncopy_gone_after = \"0s\"\n", -1, ""},
		{"48h", "[draft]\ncopy_gone_after = \"48h\"\n", 48 * time.Hour, ""},
		{"3d", "[draft]\ncopy_gone_after = \"3d\"\n", 72 * time.Hour, ""},
		{"number", "[draft]\ncopy_gone_after = 24\n", 0, "must be a string"},
		{"number zero", "[draft]\ncopy_gone_after = 0\n", 0, "must be a string"},
		{"negative", "[draft]\ncopy_gone_after = \"-1h\"\n", 0, "invalid duration"},
		{"garbage", "[draft]\ncopy_gone_after = \"soon\"\n", 0, "invalid duration"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(f, []byte(c.body), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, _, err := Load()
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Draft.CopyGoneAfter.Effective(); got != c.want {
				t.Fatalf("effective = %s, want %s", got, c.want)
			}
		})
	}
	// Ohne Datei: Standard.
	t.Setenv(EnvConfig, filepath.Join(dir, "fehlt.toml"))
	cfg, _, err := Load()
	if err != nil || cfg.Draft.CopyGoneAfter.Effective() != DefaultCopyGoneAfter || cfg.Draft.CopyGoneAfter.String() != "24h0m0s" {
		t.Fatalf("no file: %v, %v", cfg.Draft.CopyGoneAfter, err)
	}
}
