package backup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// runCmd führt ein Kommando aus und liefert seine Ausgabe (für mount).
func runCmd(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// T3-Nachbesserung 2, Punkt 11 (Wegwerf-Test A9): Ein Umweg (Junction
// unter Windows; hier ein symbolischer Link, den os.Stat genauso auflöst)
// auf einen UNTERORDNER des Datenverzeichnisses. Kein Vorfahr des Ziels ist
// das Datenverzeichnis selbst; insideByID löst den Link deshalb auf und
// prüft den Rest erneut. Relative Links und Ketten zählen mit.
func TestT3N2InsideByIDLinkToSubfolder(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	sub := filepath.Join(data, "blobs")
	other := filepath.Join(base, "anderer")
	for _, d := range []string{sub, other} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "J")
	if err := os.Symlink(sub, link); err != nil {
		t.Skip(err)
	}
	rel := filepath.Join(other, "R")
	if err := os.Symlink(filepath.Join("..", "data", "blobs"), rel); err != nil {
		t.Fatal(err)
	}
	chain := filepath.Join(base, "K")
	if err := os.Symlink(link, chain); err != nil {
		t.Fatal(err)
	}
	away := filepath.Join(base, "W")
	if err := os.Symlink(other, away); err != nil {
		t.Fatal(err)
	}
	for c, want := range map[string]bool{
		link:                               true,
		filepath.Join(link, "backup"):      true,
		filepath.Join(link, "neu", "tief"): true,
		rel:                                true,
		filepath.Join(rel, "backup"):       true,
		filepath.Join(chain, "backup"):     true,
		away:                               false,
		filepath.Join(away, "backup"):      false,
	} {
		if got := insideByID(c, data); got != want {
			t.Errorf("insideByID(%s) = %v, want %v", c, got, want)
		}
	}
	// Eine Schleife endet (Budget), ohne Treffer.
	loop := filepath.Join(base, "S")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if insideByID(filepath.Join(loop, "x"), data) {
		t.Error("a symlink loop matched")
	}
}

// T3-Nachbesserung 2, Punkt 11 Ende-zu-Ende mit Bind-Mount (Linux-
// Gegenstück der Junction: weder EvalSymlinks noch Lstat sehen den Umweg,
// os.Stat zeigt den Unterordner, nicht das Datenverzeichnis). Abgelehnt
// über /proc/self/mountinfo (finalPath). Nur, wenn mount erlaubt ist
// (sonst übersprungen).
func TestT3N2BindMountSubfolderAsTarget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("bind mounts only on Linux")
	}
	e := newEnv(t)
	sub := filepath.Join(e.dir, "sicherung-hier")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	j := filepath.Join(t.TempDir(), "J")
	if err := os.Mkdir(j, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := runCmd("mount", "--bind", sub, j); err != nil {
		t.Skipf("mount --bind: %v %s", err, out)
	}
	defer func() {
		if out, err := runCmd("umount", j); err != nil {
			t.Errorf("umount %s: %v %s", j, err, out)
		}
	}()
	for _, to := range []string{j, filepath.Join(j, "backup")} {
		if _, _, err := e.r.prepareFolder(to); !errors.Is(err, ErrTarget) {
			t.Errorf("target %s (bind mount of %s) accepted: %v", to, sub, err)
		}
	}
	if err := checkRestoreTarget(t.TempDir(), filepath.Join(j, "neu"), []string{e.dir}); !errors.Is(err, ErrTarget) {
		t.Errorf("restore target through a bind mount into the data directory accepted: %v", err)
	}
	// Gegenprobe: Ein Bind-Mount eines fremden Ordners bleibt erlaubt.
	foreign := t.TempDir()
	k := filepath.Join(t.TempDir(), "K")
	if err := os.Mkdir(k, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := runCmd("mount", "--bind", foreign, k); err != nil {
		t.Fatalf("mount --bind: %v %s", err, out)
	}
	defer func() { _, _ = runCmd("umount", k) }()
	if _, _, err := e.r.prepareFolder(filepath.Join(k, "backup")); err != nil {
		t.Errorf("target in a bind mount of a foreign folder refused: %v", err)
	}
}
