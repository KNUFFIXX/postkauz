package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T3-Nachbesserung (Wenn Zeit): restore zeigt je Konto Server und
// Benutzer und die Pfade der Stildateien; das Passwort des Kontos steht
// nirgends in der Ausgabe.
func TestT3NRestoreShowsServersAndStyleFiles(t *testing.T) {
	setupEnv(t)
	const pw = "geheim-T3N-pw-41"
	if code, _, errs := runIn(t, pw+"\n", "account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %s", errs)
	}
	style := filepath.Join(t.TempDir(), "stil.md")
	if err := os.WriteFile(style, []byte("kurz und freundlich"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, "identity", "add", "-a", "demo", "--email", "u@example.invalid", "--name", "Buero", "--style-file", style); code != ExitOK {
		t.Fatalf("identity add: %s", errs)
	}
	backupDir := filepath.Join(t.TempDir(), "b")
	if code, _, errs := run(t, "backup", "create", "--to", backupDir); code != ExitOK {
		t.Fatalf("backup: %s", errs)
	}
	code, out, errs := run(t, "restore", "--from", backupDir, "--data-dir", filepath.Join(t.TempDir(), "neu"))
	if code != ExitOK {
		t.Fatalf("restore: %d\n%s\n%s", code, out, errs)
	}
	for _, want := range []string{
		"accounts:  check each server and user before you store a password",
		"           demo: 127.0.0.1:1 (none), user u@example.invalid, password",
		"style:     files this copy reads as style guide",
		"           Buero (demo): ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("restore output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out+errs, pw) {
		t.Fatal("password in the restore output")
	}
}
