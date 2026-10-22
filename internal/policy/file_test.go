package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePolicy(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A02 E8: ohne Datei gelten die Standards; ein ausdrücklich gesetzter Pfad
// (POSTKAUZ_POLICY) muss existieren.
func TestLoadFileDefaults(t *testing.T) {
	missing := filepath.Join(t.TempDir(), FileName)
	f, err := LoadFile(missing, false)
	if err != nil || f.Exists || f.SHA256 != "" || f.MaxChars() != DefaultMaxChars ||
		f.Doc.Draft.CheckHookTimeout != DefaultCheckHookTimeout || len(f.Doc.Draft.CheckHook) != 0 ||
		!*f.Doc.Draft.RequireApprovalForReady || f.Doc.Delete.AgentMay != "trash_only" || f.Doc.Move.AgentMay != "within_account" ||
		f.Doc.Move.BulkConfirmThreshold != 50 || !*f.Doc.AgentDefaults.DryRunDefault {
		t.Fatalf("defaults: %+v, %v", f, err)
	}
	if _, err := LoadFile(missing, true); !errors.Is(err, ErrInvalidFile) {
		t.Fatalf("explicit missing path: %v", err)
	}
	// Ein leerer Abschnitt ist dasselbe wie keine Angabe.
	g, err := LoadFile(writePolicy(t, "[draft]\n"), false)
	if err != nil || !g.Exists || g.Document() != f.Document() {
		t.Fatalf("empty section: %+v, %v", g, err)
	}
}

func TestLoadFileValues(t *testing.T) {
	content := `# Richtlinie des Betreibers
[draft]
require_approval_for_ready = true
check_hook = ["lint-mail", "--strict", "{eml}"]
check_hook_timeout = "45s"

[delete]
agent_may = "none"

[move]
agent_may = "archive_only"
bulk_confirm_threshold = 20

[agent_defaults]
dry_run_default = false
max_chars = 5000
`
	p := writePolicy(t, content)
	f, err := LoadFile(p, true)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(content))
	if !f.Exists || f.SHA256 != hex.EncodeToString(sum[:]) || f.Path != p {
		t.Fatalf("file = %+v", f)
	}
	d := f.Doc
	if strings.Join(d.Draft.CheckHook, " ") != "lint-mail --strict {eml}" || d.Draft.CheckHookTimeout != 45*time.Second ||
		d.Delete.AgentMay != "none" || d.Move.AgentMay != "archive_only" || d.Move.BulkConfirmThreshold != 20 ||
		*d.AgentDefaults.DryRunDefault || f.MaxChars() != 5000 {
		t.Fatalf("doc = %+v", d)
	}
	want := `{"draft":{"require_approval_for_ready":true,"check_hook":["lint-mail","--strict","{eml}"],"check_hook_timeout":"45s"},` +
		`"delete":{"agent_may":"none"},"move":{"agent_may":"archive_only","bulk_confirm_threshold":20},` +
		`"agent_defaults":{"dry_run_default":false,"max_chars":5000}}`
	if f.Document() != want {
		t.Fatalf("document = %s", f.Document())
	}
}

// Akzeptanz S2.1: Eine Policy-Datei mit unbekanntem Schlüssel wird
// abgelehnt; ebenso ungültige Werte. Die Meldung nennt Schlüssel, nie Werte.
func TestLoadFileRejects(t *testing.T) {
	cases := map[string]struct{ content, want string }{
		"unknown top-level key":  {"[draft]\ncheck_hook = []\n[send]\nallow = true\n", "unknown keys: send, send.allow"},
		"unknown key in section": {"[agent_defaults]\nmax_chars = 1000\nmax_char = 5\n", "unknown keys: agent_defaults.max_char"},
		"unknown root key":       {"version = 2\n", "unknown keys: version"},
		"approval not required":  {"[draft]\nrequire_approval_for_ready = false\n", "only a human can set a draft ready"},
		"hook as string":         {"[draft]\ncheck_hook = \"lint-mail --strict\"\n", "line 2"},
		"hook empty program":     {"[draft]\ncheck_hook = [\"\", \"x\"]\n", "first entry must be the program"},
		"hook with newline":      {"[draft]\ncheck_hook = [\"a\", \"b\\nc\"]\n", "check_hook[1]"},
		"hook timeout too short": {"[draft]\ncheck_hook_timeout = \"500ms\"\n", "check_hook_timeout"},
		"hook timeout too long":  {"[draft]\ncheck_hook_timeout = \"11m\"\n", "check_hook_timeout"},
		"delete all":             {"[delete]\nagent_may = \"expunge\"\n", "delete.agent_may"},
		"move anywhere":          {"[move]\nagent_may = \"anywhere\"\n", "move.agent_may"},
		"threshold negative":     {"[move]\nbulk_confirm_threshold = -1\n", "bulk_confirm_threshold"},
		"max_chars too small":    {"[agent_defaults]\nmax_chars = 50\n", "agent_defaults.max_chars 50"},
		"max_chars too large":    {"[agent_defaults]\nmax_chars = 2000000\n", "agent_defaults.max_chars"},
		"max_chars not a number": {"[agent_defaults]\nmax_chars = \"GEHEIMWERT\"\n", "line 2"},
		"syntax error":           {"[draft\ncheck_hook = []\n", "to end table name"},
	}
	for name, c := range cases {
		_, err := LoadFile(writePolicy(t, c.content), false)
		if !errors.Is(err, ErrInvalidFile) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", name, err, c.want)
			continue
		}
		if strings.Contains(err.Error(), "GEHEIMWERT") || strings.Contains(err.Error(), "lint-mail --strict") {
			t.Errorf("%s: error contains a value: %v", name, err)
		}
	}
}

func TestFilePath(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "postkauz", "config.toml")
	t.Setenv(EnvPolicy, "")
	if p, explicit := FilePath(cfg); p != filepath.Join(filepath.Dir(cfg), FileName) || explicit {
		t.Fatalf("default path = %s %v", p, explicit)
	}
	other := filepath.Join(t.TempDir(), "andere.toml")
	t.Setenv(EnvPolicy, other)
	if p, explicit := FilePath(cfg); p != other || !explicit {
		t.Fatalf("env path = %s %v", p, explicit)
	}
}
