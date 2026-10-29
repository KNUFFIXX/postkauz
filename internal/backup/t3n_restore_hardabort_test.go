package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T3-Nachbesserung 4 (G1-5): Solange PrepareRestored nicht gelaufen ist,
// darf unter dem Ziel keine öffnbare Datenbank mit der Identität des
// Originals liegen (ein harter Abbruch, z. B. Stromausfall, ließe sie sonst
// betriebsfähig zurück: Kennung und Schlüsselbund-Verweise des Originals,
// offene draft_push/mailbox_action-Jobs). Der Haken ist RestoreOptions.Now:
// sein erster Aufruf liegt nach Kopie und Migration, direkt vor
// PrepareRestored.
func TestT3NRestoreLeavesNoUsableCopyBeforePrepare(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	to := filepath.Join(t.TempDir(), "neu", "data")
	orig, _ := e.st.DataDirIDFull(ctx)
	var problem string
	checked := false
	_, err := Restore(ctx, RestoreOptions{From: from, DataDir: to, DBName: brand.Name + ".db", Now: func() time.Time {
		if checked {
			return time.Now()
		}
		checked = true
		// Zeitpunkt: nach Kopie und Migration, unmittelbar vor PrepareRestored.
		p := filepath.Join(to, brand.Name+".db")
		if _, err := os.Stat(p); err != nil {
			return time.Now()
		}
		st, err := store.Open(p)
		if err != nil {
			// gesperrt o. ä.: Datei liegt trotzdem unter dem endgültigen Namen
			problem = "database lies under its final name before PrepareRestored (open: " + err.Error() + ")"
			return time.Now()
		}
		defer st.Close()
		id, _ := st.DataDirIDFull(ctx)
		if id == orig {
			problem = "a usable database with the original's identity lies under the final name before PrepareRestored"
		}
		return time.Now()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("hook did not run")
	}
	if problem != "" {
		t.Fatal(problem)
	}
}

// T3-Nachbesserung 4: Ein Arbeitsordner, den ein harter Abbruch neben dem
// Ziel zurückgelassen hat, nennt der nächste restore (auch im
// Trockenlauf) und löscht ihn nicht; ein Fehler mitten im restore entfernt
// den eigenen Arbeitsordner, ein leeres Ziel bleibt leer stehen, und die
// Migrationskopie zeigt nach dem Umbenennen ins Ziel.
func TestT3NRestoreNamesLeftoverWorkFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	from := backupOf(t, e)
	parent := t.TempDir()
	to := filepath.Join(parent, "data")
	left := to + ".restoring-0123abcd"
	if err := os.Mkdir(left, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(parent, "data.restoring-kein-zufall"), 0o700); err != nil {
		t.Fatal(err)
	}
	dry, err := Restore(ctx, RestoreOptions{From: from, DataDir: to, DryRun: true})
	if err != nil || len(dry.Leftovers) != 1 || dry.Leftovers[0] != left {
		t.Fatalf("dry run: leftovers %v, err %v", dry.Leftovers, err)
	}

	// Fehler mitten im restore (abgebrochener Kontext beim Kopieren):
	// Arbeitsordner weg, leeres Ziel unverändert.
	if err := os.Mkdir(to, 0o755); err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Restore(canceled, RestoreOptions{From: from, DataDir: to}); err == nil {
		t.Fatal("restore with a canceled context succeeded")
	}
	entries, _ := os.ReadDir(to)
	if len(entries) != 0 {
		t.Fatalf("target after a failed restore holds %d entries", len(entries))
	}
	if got := RestoreLeftovers(to); len(got) != 1 || got[0] != left {
		t.Fatalf("leftovers after a failed restore = %v (own work folder must be gone)", got)
	}

	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Leftovers) != 1 || res.Leftovers[0] != left {
		t.Fatalf("restore: leftovers %v", res.Leftovers)
	}
	if _, err := os.Stat(left); err != nil {
		t.Fatalf("restore removed a leftover it did not create: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, brand.Name+".db")); err != nil {
		t.Fatal(err)
	}
	if res.MigrationSnapshot != "" && filepath.Dir(res.MigrationSnapshot) != to {
		t.Fatalf("migration snapshot %s is not in the target", res.MigrationSnapshot)
	}
	if got := RestoreLeftovers(to); len(got) != 1 {
		t.Fatalf("leftovers after restore = %v", got)
	}
}

// T3-Nachbesserung 4: Eine Sicherung mit älterem Schema wird im
// Arbeitsordner migriert; die Migrationskopie liegt danach im Ziel, und
// der Pfad im Ergebnis zeigt dorthin (nicht in den umbenannten
// Arbeitsordner).
func TestT3NRestoreMigrationSnapshotPathAfterRename(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, brand.Name+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.MigrateToForTest(ctx, store.KnownSchemaVersion()-1); err != nil {
		t.Fatal(err)
	}
	bs, err := blob.Open(filepath.Join(dir, "blobs"), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := &Runner{Store: st, Blobs: bs, DataDir: dir, Version: "test"}
	from := filepath.Join(t.TempDir(), "backup")
	if _, err := r.Create(ctx, Options{To: from}); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(t.TempDir(), "neu")
	res, err := Restore(ctx, RestoreOptions{From: from, DataDir: to})
	if err != nil {
		t.Fatal(err)
	}
	if res.MigrationSnapshot == "" || filepath.Dir(res.MigrationSnapshot) != to {
		t.Fatalf("migration snapshot = %q, want a file in %s", res.MigrationSnapshot, to)
	}
	if _, err := os.Stat(res.MigrationSnapshot); err != nil {
		t.Fatal(err)
	}
	if res.SchemaVersion != store.KnownSchemaVersion() {
		t.Fatalf("schema after restore = %d", res.SchemaVersion)
	}
}
