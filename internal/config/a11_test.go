package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A11 Teil 1 (c): [draft] previous_copies nur "trash" (Standard, auch ohne
// Schlüssel) oder "keep"; alles andere ist ein Fehler mit Hinweis.
func TestA11PreviousCopies(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	for _, c := range []struct {
		name, body, want, err string
	}{
		{"key missing", "", PreviousCopiesTrash, ""},
		{"trash", "[draft]\nprevious_copies = \"trash\"\n", PreviousCopiesTrash, ""},
		{"keep", "[draft]\nprevious_copies = \"keep\"\n", PreviousCopiesKeep, ""},
		{"other", "[draft]\nprevious_copies = \"delete\"\n", "", `use "trash" (default) or "keep"`},
		{"number", "[draft]\nprevious_copies = 1\n", "", "previous_copies"},
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
			if cfg.Draft.PreviousCopies != c.want {
				t.Fatalf("previous_copies = %q, want %q", cfg.Draft.PreviousCopies, c.want)
			}
		})
	}
}
