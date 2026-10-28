package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// T3 (E8, A11): [retention] deleted_remote_after und blob_gc_min_age,
// [audit] retain. Dauern in Tagen ("365d"), wie Go ("24h") oder 0;
// Standards 0 (nie), 24h, 365d; zu kurze oder falsche Werte mit klarer
// Meldung.
func TestRetentionConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retention.DeletedRemoteAfter != 0 || cfg.Retention.BlobGCMinAge.D() != 24*time.Hour || cfg.Audit.Retain.D() != 365*24*time.Hour {
		t.Fatalf("defaults: %+v, retain %s", cfg.Retention, cfg.Audit.Retain.D())
	}

	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(f, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("[retention]\ndeleted_remote_after = \"90d\"\nblob_gc_min_age = \"36h\"\n[audit]\nretain = \"400d\"\n")
	cfg, _, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Retention.DeletedRemoteAfter.D() != 90*24*time.Hour || cfg.Retention.BlobGCMinAge.D() != 36*time.Hour || cfg.Audit.Retain.D() != 400*24*time.Hour {
		t.Fatalf("values: %+v, retain %s", cfg.Retention, cfg.Audit.Retain.D())
	}
	write("[retention]\ndeleted_remote_after = 0\n")
	if cfg, _, err = Load(); err != nil || cfg.Retention.DeletedRemoteAfter != 0 {
		t.Fatalf("plain 0: %v %v", cfg.Retention, err)
	}

	for in, want := range map[string]string{
		"[retention]\ndeleted_remote_after = 30\n":       "needs a unit",
		"[retention]\ndeleted_remote_after = \"-1d\"\n":  "invalid duration",
		"[retention]\ndeleted_remote_after = \"zwei\"\n": "invalid duration",
		"[retention]\nblob_gc_min_age = \"10m\"\n":       "retention.blob_gc_min_age 10m0s is too short",
		"[audit]\nretain = \"12h\"\n":                    "audit.retain 12h0m0s is too short",
		"[audit]\nretain = \"999999999d\"\n":             "invalid duration",
		"[retention]\nkeep = \"1d\"\n":                   "unknown keys: retention.keep",
	} {
		write(in)
		if _, _, err := Load(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", in, err, want)
		}
	}
}
