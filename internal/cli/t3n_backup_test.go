package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/flock"
)

// T3-Nachbesserung 5(a): Läuft schon eine Sicherung (Sperre im
// Datenverzeichnis gehalten), lehnt backup create ab: Code backup_running,
// Exit 1, nichts geschrieben; ein Trockenlauf geht weiter.
func TestT3NBackupRunningCLI(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %s", errs)
	}
	lk, err := flock.TryLock(filepath.Join(dataDir, backup.LockName))
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	to := filepath.Join(t.TempDir(), "b")
	code, _, errs := run(t, "backup", "create", "--to", to, "--json")
	if code != ExitError || !strings.Contains(errs, `"code":"backup_running"`) {
		t.Fatalf("backup during a backup: %d %s", code, errs)
	}
	if code, _, errs := run(t, "backup", "create", "--to", to, "--dry-run"); code != ExitOK {
		t.Fatalf("dry run during a backup: %d %s", code, errs)
	}
	lk.Release()
	if code, _, errs := run(t, "backup", "create", "--to", to); code != ExitOK {
		t.Fatalf("backup after the lock is free: %d %s", code, errs)
	}
}
