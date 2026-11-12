package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// A08 Teil 1: [draft] attach_deny_dirs und attach_deny_files erweitern die
// Anhang-Sperre; ein leerer Eintrag, ein Pfadtrenner, „..“ oder ein
// ungültiges Muster ist ein Konfigurationsfehler mit Hinweis.
func TestA08AttachDenyLists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	for _, c := range []struct {
		name, body  string
		dirs, files []string
		err         string
	}{
		{"keys missing", "", nil, nil, ""},
		{"files xlsm", "[draft]\nattach_deny_files = [\"*.xlsm\"]\n", nil, []string{"*.xlsm"}, ""},
		{"dirs and files", "[draft]\nattach_deny_dirs = [\"Steuer\", \"Privat\"]\nattach_deny_files = [\"*.xlsm\", \"geheim-*.pdf\"]\n",
			[]string{"Steuer", "Privat"}, []string{"*.xlsm", "geheim-*.pdf"}, ""},
		{"empty list", "[draft]\nattach_deny_files = []\n", nil, []string{}, ""},
		{"dotdot file", "[draft]\nattach_deny_files = [\"..\"]\n", nil, nil, `contains ".."`},
		{"dotdot dir", "[draft]\nattach_deny_dirs = [\"a..b\"]\n", nil, nil, `contains ".."`},
		{"slash", "[draft]\nattach_deny_dirs = [\"a/b\"]\n", nil, nil, "path separator"},
		{"backslash", "[draft]\nattach_deny_files = ['a\\b.txt']\n", nil, nil, "path separator"},
		{"empty entry", "[draft]\nattach_deny_dirs = [\"  \"]\n", nil, nil, "is empty"},
		{"bad pattern", "[draft]\nattach_deny_files = [\"[x\"]\n", nil, nil, "not a valid pattern"},
		{"not a list", "[draft]\nattach_deny_files = \"*.xlsm\"\n", nil, nil, "attach_deny_files"},
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
				if !strings.Contains(err.Error(), "draft.attach_deny") && !strings.Contains(err.Error(), "attach_deny_files") {
					t.Fatalf("err %v does not name the key", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(cfg.Draft.AttachDenyDirs, c.dirs) || !slices.Equal(cfg.Draft.AttachDenyFiles, c.files) {
				t.Fatalf("lists = %q %q", cfg.Draft.AttachDenyDirs, cfg.Draft.AttachDenyFiles)
			}
		})
	}
	// Der Hinweis nennt, dass die Liste nur ergänzt.
	if err := os.WriteFile(f, []byte("[draft]\nattach_deny_files = [\"../x\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(); err == nil || !strings.Contains(err.Error(), "only adds to the built-in rules") {
		t.Fatalf("hint missing: %v", err)
	}
}
