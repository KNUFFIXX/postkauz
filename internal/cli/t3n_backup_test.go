package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
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

// T3-Nachbesserung 3(b): doctor nennt Archivdateien ohne Eintrag in
// audit_archives (eingetragene nicht); backup create warnt bei einem
// eingetragenen Archiv, das nicht mehr zu seiner Prüfsumme passt, und
// sichert trotzdem.
func TestT3NDoctorAndBackupArchiveFiles(t *testing.T) {
	dataDir, _ := setupEnv(t)
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %s", errs)
	}
	registered, orphan := "audit-archive-2025-01-01-2025-01-31.db", "audit-archive-2024-01-01-2024-03-31.db"
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO audit_archives(file, from_day, to_day, first_event_id, last_event_id, events, sha256,
		chain_prev, chain_head, created_at) VALUES (?, '2025-01-01', '2025-01-31', 1, 2, 2, ?, 'p', 'h', 1)`,
		registered, strings.Repeat("0", 64)); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	for _, n := range []string{registered, orphan} {
		if err := os.WriteFile(filepath.Join(dataDir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, out, _ := run(t, "doctor", "--json")
	var r DoctorReport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if !slices.Equal(r.AuditArchivesUnregistered, []string{orphan}) {
		t.Fatalf("audit_archives_unregistered = %v, want [%s]", r.AuditArchivesUnregistered, orphan)
	}
	if !slices.ContainsFunc(r.Warnings, func(w string) bool {
		return strings.Contains(w, orphan) && strings.Contains(w, "interrupted audit rotate")
	}) {
		t.Fatalf("doctor warnings = %v", r.Warnings)
	}

	code, _, errs := run(t, "backup", "create", "--to", filepath.Join(t.TempDir(), "b"))
	if code != ExitOK || !strings.Contains(errs, "warning: audit archive "+registered+" (archive_changed) is not in this backup") {
		t.Fatalf("backup with a changed archive: %d %s", code, errs)
	}
}
