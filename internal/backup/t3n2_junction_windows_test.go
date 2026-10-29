//go:build windows

package backup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// T3-Nachbesserung 2, Punkt 11: Eine Junction (mklink /J) auf einen
// UNTERORDNER des Datenverzeichnisses. os.Stat der Junction zeigt den
// Unterordner, kein Vorfahr ist das Datenverzeichnis; abgelehnt wird über
// os.Readlink (Mount Point, ModeIrregular) und GetFinalPathNameByHandle
// (finalPath). Nur unter Windows (kompiliert, im Container nicht
// ausgeführt).
func TestT3N2BackupTargetThroughJunctionToSubfolder(t *testing.T) {
	e := newEnv(t)
	sub := filepath.Join(e.dir, "blobs")
	j := filepath.Join(t.TempDir(), "J")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, sub).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	for _, to := range []string{j, filepath.Join(j, "backup"), filepath.Join(j, "neu", "tief")} {
		if !insideByID(to, e.dir) {
			t.Errorf("insideByID(%s -> data\\blobs) = false", to)
		}
		if _, _, err := e.r.prepareFolder(to); !errors.Is(err, ErrTarget) {
			t.Errorf("target %s (junction to a subfolder of the data directory) accepted: %v", to, err)
		}
	}
	if err := checkRestoreTarget(t.TempDir(), filepath.Join(j, "neu"), []string{e.dir}); !errors.Is(err, ErrTarget) {
		t.Errorf("restore target through a junction to a subfolder accepted: %v", err)
	}
	// finalPath: der Pfad hinter der Junction liegt im Datenverzeichnis.
	fj, ok1 := finalPath(filepath.Join(j, "backup"))
	fd, ok2 := finalPath(e.dir)
	if !ok1 || !ok2 || !within(fj, fd) {
		t.Errorf("finalPath: %q (%v) not inside %q (%v)", fj, ok1, fd, ok2)
	}
	// Gegenprobe: Junction auf einen fremden Ordner bleibt erlaubt.
	foreign := t.TempDir()
	k := filepath.Join(t.TempDir(), "K")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", k, foreign).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
	defer os.Remove(k)
	if _, _, err := e.r.prepareFolder(filepath.Join(k, "backup")); err != nil {
		t.Errorf("target through a junction to a foreign folder refused: %v", err)
	}
}
