package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// interruptedRun startet eine Sicherung von e nach to und bricht sie ab,
// sobald der Schnappschuss liegt (wie Strg+C mitten im Kopieren). Zurück
// kommt der Name des Schnappschusses.
func interruptedRun(t *testing.T, e *env, to string) string {
	t.Helper()
	cctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.r.hook = func(stage string) {
		if stage == "snapshot" {
			cancel()
		}
	}
	defer func() { e.r.hook = nil }()
	if _, err := e.r.Create(cctx, Options{To: to}); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted run: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, ManifestName)); !os.IsNotExist(err) {
		t.Fatalf("manifest after the interrupted run: %v", err)
	}
	return snapshotName(e.now, 1)
}

// T3-Nachbesserung 3, Punkt 6a (Wegwerf-Test C5a): Ein Ordner mit der
// Sicherung eines ANDEREN Datenverzeichnisses ohne Manifest (verloren, halb
// entpacktes Tar) oder mit dessen abgebrochenem Lauf galt als Rest einer
// eigenen abgebrochenen Sicherung: Der Lauf schrieb hinein und löschte den
// fremden Schnappschuss. Jetzt nimmt prepareFolder Schnappschuss und blobs/
// nur mit der eigenen Laufmarke an.
func TestT3N3ForeignBackupWithoutManifest(t *testing.T) {
	ctx := context.Background()
	t.Run("complete backup, manifest lost", func(t *testing.T) {
		other := newEnv(t)
		to := filepath.Join(t.TempDir(), "backup")
		res, err := other.r.Create(ctx, Options{To: to})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(to, ManifestName)); err != nil {
			t.Fatal(err)
		}
		e := newEnv(t)
		e.now = e.now.Add(3600e9)
		res2, err := e.r.Create(ctx, Options{To: to})
		if !errors.Is(err, ErrTarget) {
			t.Errorf("second data directory into that folder: err = %v (snapshots removed %d), want ErrTarget",
				err, res2.SnapshotsRemoved)
		}
		if _, serr := os.Stat(filepath.Join(to, res.Database)); serr != nil {
			t.Errorf("the snapshot %s of the other backup is gone: %v", res.Database, serr)
		}
	})
	t.Run("interrupted backup of another data directory", func(t *testing.T) {
		other := newEnv(t)
		to := filepath.Join(t.TempDir(), "backup")
		snap := interruptedRun(t, other, to)
		e := newEnv(t)
		e.now = e.now.Add(3600e9)
		res2, err := e.r.Create(ctx, Options{To: to})
		if !errors.Is(err, ErrTarget) {
			t.Errorf("second data directory into the other's leftovers: err = %v (snapshots removed %d), want ErrTarget",
				err, res2.SnapshotsRemoved)
		}
		if _, serr := os.Stat(filepath.Join(to, snap)); serr != nil {
			t.Errorf("the snapshot %s of the other run is gone: %v", snap, serr)
		}
		// Das andere Datenverzeichnis erkennt seine Reste und setzt fort.
		other.now = other.now.Add(60e9)
		if res, err := other.r.Create(ctx, Options{To: to}); err != nil || res.SnapshotsRemoved != 1 {
			t.Errorf("the other data directory resumes its own run: %+v %v", res, err)
		}
	})
}

// Punkt 6b (Wegwerf-Test C5b): Bricht die erste Sicherung ab, nachdem ein
// Audit-Archiv kopiert ist (hier: das Manifest lässt sich nicht schreiben),
// liegt audit-archive-*.db unter den Resten; der nächste Lauf wies den Ordner
// ab. Mit der eigenen Laufmarke zählt das Archiv als Rest; ohne Marke, mit
// fremder Marke oder neben einer fremden Datei bleibt der Ordner abgewiesen.
func TestT3N3LeftoverWithAuditArchive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	const arch = "audit-archive-2026-01-01_2026-03-31.db"
	e.archive(t, arch, "archiv eins")
	to := filepath.Join(t.TempDir(), "backup")
	// Ein Ordner unter dem Namen des Manifests: das Umbenennen auf ihn
	// scheitert, nachdem Blobs und Archiv kopiert sind.
	e.r.hook = func(stage string) {
		if stage == "snapshot" {
			if err := os.MkdirAll(filepath.Join(to, ManifestName, "x"), 0o700); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := e.r.Create(ctx, Options{To: to}); err == nil {
		t.Fatal("run with a folder in place of the manifest succeeded")
	}
	e.r.hook = nil
	if err := os.RemoveAll(filepath.Join(to, ManifestName)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(to, arch)); err != nil {
		t.Fatalf("the audit archive was not copied before the failure: %v", err)
	}
	if _, _, err := e.r.prepareFolder(ctx, to); err != nil {
		t.Fatalf("leftovers of an interrupted first backup with a copied audit archive: %v", err)
	}
	e.now = e.now.Add(60e9)
	res, err := e.r.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatalf("run after the interrupted first run: %v", err)
	}
	if res.AuditArchives != 1 || res.SnapshotsRemoved != 1 {
		t.Fatalf("after the interrupted run: archives %d, snapshots removed %d", res.AuditArchives, res.SnapshotsRemoved)
	}
	if _, err := os.Lstat(filepath.Join(to, runMarkerName)); !os.IsNotExist(err) {
		t.Fatalf("run marker after the manifest: %v", err)
	}

	// Abgewiesene Fälle: Reste ohne eigene Marke.
	ownLeft := filepath.Join(t.TempDir(), "eigen")
	interruptedRun(t, e, ownLeft)
	own, err := os.ReadFile(filepath.Join(ownLeft, runMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	foreign := []byte("datadir_id:00000000000000000000000000000000\n")
	for name, files := range map[string]map[string][]byte{
		"archive without marker": {EncryptNoteName: nil, snapshotName(e.now, 1): nil, arch: nil, "blobs/": nil},
		"foreign marker":         {runMarkerName: foreign, EncryptNoteName: nil, snapshotName(e.now, 1): nil, arch: nil},
		"foreign marker only":    {runMarkerName: foreign, EncryptNoteName: nil},
		"own marker, foreign file": {runMarkerName: own, EncryptNoteName: nil, snapshotName(e.now, 1): nil,
			"notizen.txt": nil},
	} {
		dir := filepath.Join(t.TempDir(), "x")
		writeTree(t, dir, files)
		if _, _, err := e.r.prepareFolder(ctx, dir); !errors.Is(err, ErrTarget) {
			t.Errorf("%s: folder accepted: %v", name, err)
		}
	}
	// Zulässig bleiben: ENCRYPT-ME.txt und Temporärdateien allein, die eigene
	// Marke mit Schnappschuss, Archiv und blobs/.
	for name, files := range map[string]map[string][]byte{
		"note and temp files": {EncryptNoteName: nil, ".tmp-0123456789ab": nil},
		"own marker with archive": {runMarkerName: own, EncryptNoteName: nil, snapshotName(e.now, 1): nil, arch: nil,
			"blobs/": nil},
	} {
		dir := filepath.Join(t.TempDir(), "x")
		writeTree(t, dir, files)
		if _, _, err := e.r.prepareFolder(ctx, dir); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// writeTree legt Dateien (Inhalt oder "x") und Ordner (Name mit "/") an.
func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for f, b := range files {
		p := filepath.Join(dir, f)
		if f[len(f)-1] == '/' {
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if b == nil {
			b = []byte("x")
		}
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
