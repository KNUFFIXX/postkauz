package config

import (
	"os"
	"path/filepath"
	"testing"
)

// S3.3 (E6): [paths] export_dir, Standard <Datenverzeichnis>/export,
// relativ zum Datenverzeichnis, absolut wie angegeben.
func TestLoadExportDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	t.Setenv(EnvConfig, filepath.Join(dir, "fehlt.toml"))
	cfg, p, err := Load()
	if err != nil || p.ExportDir != filepath.Join(dir, "export") || cfg.Paths.ExportDir != p.ExportDir {
		t.Fatalf("default: %s %v", p.ExportDir, err)
	}
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	if err := os.WriteFile(f, []byte("[paths]\nexport_dir = \"ablage\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, p, err = Load(); err != nil || p.ExportDir != filepath.Join(dir, "ablage") {
		t.Fatalf("relative: %s %v", p.ExportDir, err)
	}
	abs := filepath.Join(t.TempDir(), "ablage")
	if err := os.WriteFile(f, []byte("[paths]\nexport_dir = '"+abs+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, p, err = Load(); err != nil || p.ExportDir != abs {
		t.Fatalf("absolute: %s %v", p.ExportDir, err)
	}
}
