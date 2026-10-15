package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDefaultsWithoutFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "missing.toml"))
	t.Setenv(EnvDataDir, filepath.Join(dir, "data"))
	t.Setenv(EnvSecretsBackend, "")
	cfg, p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigExists || cfg.Secrets.Backend != BackendAuto || cfg.LogLevel != "info" {
		t.Fatalf("cfg=%+v paths=%+v", cfg, p)
	}
	if p.Database != filepath.Join(dir, "data", "postkauz.db") || p.Blobs != filepath.Join(dir, "data", "blobs") {
		t.Fatalf("paths = %+v", p)
	}
}

func TestLoadFileAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgFile, []byte("data_dir = \""+filepath.ToSlash(filepath.Join(dir, "fromfile"))+"\"\nlog_level = \"debug\"\n[secrets]\nbackend = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, cfgFile)
	t.Setenv(EnvDataDir, "")
	t.Setenv(EnvSecretsBackend, "")
	cfg, p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !p.ConfigExists || cfg.Secrets.Backend != BackendKeyring || cfg.LogLevel != "debug" || p.DataDir != filepath.Join(dir, "fromfile") {
		t.Fatalf("cfg=%+v paths=%+v", cfg, p)
	}
	// Umgebung schlägt Datei
	t.Setenv(EnvDataDir, filepath.Join(dir, "fromenv"))
	t.Setenv(EnvSecretsBackend, BackendInsecureFile)
	cfg, p, err = Load()
	if err != nil || p.DataDir != filepath.Join(dir, "fromenv") || cfg.Secrets.Backend != BackendInsecureFile {
		t.Fatalf("env override: cfg=%+v paths=%+v err=%v", cfg, p, err)
	}
}

func TestLoadRejectsUnknownKeysAndBadValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	for name, body := range map[string]string{
		"unknown key":   "data_dri = \"x\"\n",
		"bad backend":   "[secrets]\nbackend = \"plaintext\"\n",
		"bad log level": "log_level = \"loud\"\n",
		"invalid toml":  "data_dir = \n",
	} {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".toml")
			if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(EnvConfig, f)
			if _, _, err := Load(); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestEnsureDataDirIsPrivate(t *testing.T) {
	p := Paths{DataDir: filepath.Join(t.TempDir(), "a", "b")}
	if err := EnsureDataDir(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.DataDir)
	if err != nil || !st.IsDir() {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); os.PathSeparator == '/' && perm != 0o700 {
		t.Errorf("perm = %o, want 700", perm)
	}
}

func TestCloudSyncWarning(t *testing.T) {
	if CloudSyncWarning(filepath.Join("C:", "Users", "x", "OneDrive", "postkauz")) == "" {
		t.Error("OneDrive must warn")
	}
	if CloudSyncWarning(filepath.Join("home", "x", ".local", "share", "postkauz")) != "" {
		t.Error("plain path must not warn")
	}
}
