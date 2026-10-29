package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// dirNames liefert die Namen im Ordner (sortiert).
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, x := range ents {
		names = append(names, x.Name())
	}
	return names
}

// T3-Nachbesserung 2, Nebenfund N1 (Wegwerf-Test A8b): Scheitert die
// ERSTE Sicherung in einen neuen Ordner (hier: ein Blob verschwindet
// während des Laufs), bleiben nur ENCRYPT-ME.txt und (seit der
// T3-Nachbesserung 3, Punkt 6) die eigene Laufmarke liegen. Der nächste Lauf in
// denselben Ordner muss gehen (betrieb.md 2.3: „Starte die Sicherung
// einfach noch einmal“), statt „not empty and holds no backup“ zu melden.
func TestT3N2FailedFirstBackupLeavesFolderUsable(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	to := filepath.Join(t.TempDir(), "backup")
	sum := e.sums[1]
	if err := e.st.ExecForTest(`INSERT OR IGNORE INTO blobs(sha256, size, created_at) VALUES (?, 12, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE messages SET blob_sha256 = ? WHERE id = 1`, sum); err != nil {
		t.Fatal(err)
	}
	e.r.hook = func(stage string) {
		if stage == "snapshot-taken" {
			if _, err := e.blobs.Remove(sum); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := e.r.Create(ctx, Options{To: to}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("first run: err = %v, want ErrIncomplete", err)
	}
	if names := dirNames(t, to); !slices.Equal(names, []string{runMarkerName, EncryptNoteName}) {
		t.Fatalf("left in the folder after the failed first run: %v", names)
	}
	// Blob wieder da (z. B. aus der Quarantäne zurück): zweiter Lauf.
	e.r.hook = nil
	e.put(t, e.blobs, "zweiter Blob")
	e.now = e.now.Add(60e9)
	res, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatalf("second run into the same folder after the failure: %v", err)
	}
	if m := readManifest(t, to); m.Database != res.Database || m.Counts.Blobs != 3 {
		t.Fatalf("manifest after the second run: %+v", m)
	}
}

// N1, Varianten: Reste eines Laufs, der während des Kopierens abbrach
// (Schnappschuss, blobs/, Temporärdateien neben ENCRYPT-ME.txt und der
// eigenen Laufmarke), gelten ebenfalls als leer; der nächste Lauf räumt sie
// ab. Ein Ordner mit einer fremden Datei oder nur mit blobs/ bzw. einem
// Schnappschuss (ohne Laufmarke) bleibt abgewiesen.
func TestT3N2LeftoversOfInterruptedFirstBackup(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	to := filepath.Join(t.TempDir(), "backup")
	cctx, cancel := context.WithCancel(ctx)
	e.r.hook = func(stage string) {
		if stage == "snapshot" {
			cancel()
		}
	}
	if _, err := e.r.Create(cctx, Options{To: to}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run: err = %v", err)
	}
	e.r.hook = nil
	for _, n := range []string{".tmp-0123456789ab", "." + snapshotName(e.now, 7) + ".tmp"} {
		if err := os.WriteFile(filepath.Join(to, n), []byte("rest"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(to, "blobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(to, ManifestName)); !os.IsNotExist(err) {
		t.Fatalf("manifest after the interrupted run: %v", err)
	}
	e.now = e.now.Add(60e9)
	res, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatalf("run after the interrupted first run: %v", err)
	}
	if res.SnapshotsRemoved != 1 {
		t.Fatalf("snapshot of the interrupted run not removed: %+v", res)
	}
	want := []string{EncryptNoteName, "blobs", ManifestName, res.Database}
	slices.Sort(want)
	if names := dirNames(t, to); !slices.Equal(names, want) {
		t.Fatalf("folder after the run = %v, want %v", names, want)
	}

	for name, files := range map[string][]string{
		"foreign file": {EncryptNoteName, "notizen.txt"},
		"blobs only":   {"blobs/"},
		"db only":      {snapshotName(e.now, 1)},
	} {
		dir := filepath.Join(t.TempDir(), "x")
		for _, f := range files {
			p := filepath.Join(dir, f)
			if f[len(f)-1] == '/' {
				if err := os.MkdirAll(p, 0o700); err != nil {
					t.Fatal(err)
				}
				continue
			}
			if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := e.r.prepareFolder(context.Background(), dir); !errors.Is(err, ErrTarget) {
			t.Errorf("%s: folder accepted: %v", name, err)
		}
	}
}
