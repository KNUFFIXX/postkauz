//go:build windows

package backup

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// T3-Nachbesserung „Junctions unter Windows“ (G1-10): Ein Sicherungsziel,
// das über eine Junction (mount point) im Datenverzeichnis liegt, muss
// abgelehnt werden. Seit Go 1.23 löst filepath.EvalSymlinks Junctions
// nicht mehr auf (winsymlink=1), realPath sieht den Pfad deshalb nicht im
// Datenverzeichnis; insideByID vergleicht die Datei-Identität.
func TestT3NBackupTargetThroughJunctionIntoDataDir(t *testing.T) {
	e := newEnv(t)
	j := filepath.Join(t.TempDir(), "J")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, e.dir).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	for _, to := range []string{j, filepath.Join(j, "backup")} {
		if _, _, err := e.r.prepareFolder(context.Background(), to); !errors.Is(err, ErrTarget) {
			t.Errorf("target %s (junction into the data directory) accepted: %v", to, err)
		}
	}
	// Restore: neuer Ordner im Datenverzeichnis über die Junction.
	if err := checkRestoreTarget(t.TempDir(), filepath.Join(j, "neu"), []string{e.dir}); !errors.Is(err, ErrTarget) {
		t.Errorf("restore target through a junction into the data directory accepted: %v", err)
	}
}
