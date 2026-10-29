package backup

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// T3-Nachbesserung 5(b) (G1-6): Die Sicherung darf nicht als vollständig
// gelten, wenn ein Blob, auf den der Datenbank-Schnappschuss verweist, im
// Manifest fehlt (z. B. purge + blob gc zwischen Schnappschuss und
// listBlobs). Seit der T3-Nachbesserung 2 (Punkt 4) gilt das nur für einen
// Blob, der während der Sicherung verschwindet (hier über den Test-Haken
// direkt nach dem Schnappschuss); ein schon vorher fehlender Blob ist eine
// Warnung (TestT3N2BackupWithBlobMissingBeforehand). Ordner- und Tar-Modus
// brechen mit ErrIncomplete ab; eine frühere Sicherung im Ordner bleibt
// gültig, der Tar-Strom bleibt leer.
func TestT3NBackupFailsWhenReferencedBlobIsMissing(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	to := filepath.Join(t.TempDir(), "backup")
	if _, err := e.r.Create(ctx, Options{To: to}); err != nil {
		t.Fatal(err)
	}
	before := readManifest(t, to)

	sum := e.sums[1]
	if err := e.st.ExecForTest(`INSERT OR IGNORE INTO blobs(sha256, size, created_at) VALUES (?, 12, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE messages SET blob_sha256 = ? WHERE id = 1`, sum); err != nil {
		t.Fatal(err)
	}
	vanish := func(stage string) {
		if stage != "snapshot-taken" {
			return
		}
		if removed, err := e.blobs.Remove(sum); err != nil || !removed {
			t.Fatalf("remove blob: %v %v", removed, err)
		}
	}
	e.r.hook = vanish
	e.now = e.now.Add(60e9)
	res, err := e.r.Create(ctx, Options{To: to})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("backup without a referenced blob: err = %v (%d blobs, manifest %s)", err, res.Blobs, res.ManifestSHA256)
	}
	mustNotLeak(t, "error", err.Error())
	after := readManifest(t, to)
	if after.Database != before.Database || len(after.Files) != len(before.Files) {
		t.Fatalf("the failed run changed the earlier backup: %s -> %s", before.Database, after.Database)
	}
	if _, err := regularFile(filepath.Join(to, after.Database)); err != nil {
		t.Fatalf("snapshot of the earlier backup: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(to, ".*.tmp")); len(left) > 0 {
		t.Fatalf("temporary snapshot left: %v", left)
	}

	// Tar: Der Blob ist wieder da und verschwindet erneut während des Laufs.
	e.put(t, e.blobs, "zweiter Blob")
	var buf bytes.Buffer
	if _, err := e.r.Create(ctx, Options{Tar: &buf}); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("tar without a referenced blob: err = %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("tar stream got %d bytes before the check", buf.Len())
	}
}
