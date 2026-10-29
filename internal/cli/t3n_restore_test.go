package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// T3-Nachbesserung 4: Ein Arbeitsordner, den ein abgebrochener restore
// neben dem Ziel zurückgelassen hat, nennen restore --dry-run (Zeile
// leftover:), restore (JSON leftovers, Warnung im Bericht von doctor) und
// doctor für das Ziel; gelöscht wird er nicht.
func TestT3NRestoreLeftoversCLI(t *testing.T) {
	setupEnv(t)
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %s", errs)
	}
	backupDir := filepath.Join(t.TempDir(), "b")
	if code, _, errs := run(t, "backup", "create", "--to", backupDir); code != ExitOK {
		t.Fatalf("backup: %s", errs)
	}
	target := filepath.Join(t.TempDir(), "neu")
	left := target + ".restoring-00ff00ff"
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run(t, "restore", "--from", backupDir, "--data-dir", target, "--dry-run")
	if code != ExitOK || !strings.Contains(out, "leftover:  "+left+" is left from an interrupted restore") {
		t.Fatalf("restore --dry-run: %d\n%s\n%s", code, out, errs)
	}
	code, out, errs = run(t, "restore", "--from", backupDir, "--data-dir", target, "--json")
	var rep RestoreReport
	if code != ExitOK || json.Unmarshal([]byte(out), &rep) != nil {
		t.Fatalf("restore: %d\n%s\n%s", code, out, errs)
	}
	if !slices.Equal(rep.Restore.Leftovers, []string{left}) || rep.Doctor == nil || !slices.Equal(rep.Doctor.RestoreLeftovers, []string{left}) {
		t.Fatalf("restore leftovers %v, doctor %+v", rep.Restore.Leftovers, rep.Doctor)
	}
	if !slices.ContainsFunc(rep.Doctor.Warnings, func(w string) bool { return strings.Contains(w, left) }) {
		t.Fatalf("doctor warnings = %v", rep.Doctor.Warnings)
	}
	if _, err := os.Stat(left); err != nil {
		t.Fatalf("leftover removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "postkauz.db")); err != nil {
		t.Fatal(err)
	}
}
