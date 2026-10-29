package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// T3-Nachbesserung 2, Punkt 4 (Wegwerf-Test A8): Ein Blob, der schon VOR
// der Sicherung fehlte (z. B. Quarantäne des Virenscanners), blockiert
// keine Sicherung mehr. Ordner (zweimal in denselben Ordner), Tar-Strom und
// Trockenlauf laufen durch und nennen ihn in missing_blobs (Ergebnis,
// Manifest, Zähler in backup.done); restore übernimmt den Zustand (Blob
// fehlt auch in der Kopie, Zähler in backup.restore).
func TestT3N2BackupWithBlobMissingBeforehand(t *testing.T) {
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
	if removed, err := e.blobs.Remove(sum); err != nil || !removed {
		t.Fatalf("remove blob: %v %v", removed, err)
	}
	want := []string{sum}

	// Trockenlauf: nennt den Blob, bricht nicht ab.
	dry, err := e.r.Create(ctx, Options{To: to, DryRun: true})
	if err != nil || !slices.Equal(dry.MissingBlobs, want) {
		t.Fatalf("dry run: missing %v, err %v", dry.MissingBlobs, err)
	}

	for i := range 2 {
		e.now = e.now.Add(60e9)
		res, err := e.r.Create(ctx, Options{To: to})
		if err != nil {
			t.Fatalf("backup %d with a blob missing beforehand: %v", i+1, err)
		}
		if !slices.Equal(res.MissingBlobs, want) || res.Blobs != 2 {
			t.Fatalf("backup %d: missing %v, blobs %d", i+1, res.MissingBlobs, res.Blobs)
		}
		if m := readManifest(t, to); !slices.Equal(m.MissingBlobs, want) || m.Database != res.Database {
			t.Fatalf("backup %d: manifest missing_blobs %v", i+1, m.MissingBlobs)
		}
		done := lastAudit(t, e.st, OpDone)
		if !strings.Contains(done, `"missing_blobs":1`) || strings.Contains(done, sum) {
			t.Fatalf("backup %d: backup.done = %s", i+1, done)
		}
		mustNotLeak(t, "audit", done)
		out, _ := json.Marshal(res)
		mustNotLeak(t, "result", string(out))
	}

	// Tar-Strom: dasselbe, Manifest im Strom mit missing_blobs.
	var buf bytes.Buffer
	res, err := e.r.Create(ctx, Options{Tar: &buf})
	if err != nil || !slices.Equal(res.MissingBlobs, want) {
		t.Fatalf("tar: missing %v, err %v", res.MissingBlobs, err)
	}
	unpacked := t.TempDir()
	untar(t, &buf, unpacked)
	if m := readManifest(t, unpacked); !slices.Equal(m.MissingBlobs, want) {
		t.Fatalf("tar: manifest missing_blobs %v", m.MissingBlobs)
	}

	// Restore übernimmt den Zustand: Kopie ohne den Blob, Liste und Zähler.
	for _, from := range []string{to, unpacked} {
		dst := filepath.Join(t.TempDir(), "neu")
		if r, err := Restore(ctx, RestoreOptions{From: from, DataDir: dst, DryRun: true}); err != nil || !slices.Equal(r.MissingBlobs, want) {
			t.Fatalf("restore dry run from %s: missing %v, err %v", from, r.MissingBlobs, err)
		}
		r, err := Restore(ctx, RestoreOptions{From: from, DataDir: dst})
		if err != nil || !slices.Equal(r.MissingBlobs, want) || r.Blobs != 2 {
			t.Fatalf("restore from %s: missing %v, blobs %d, err %v", from, r.MissingBlobs, r.Blobs, err)
		}
		bs, err := blob.OpenExisting(filepath.Join(dst, "blobs"), blob.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if err := bs.Verify(sum, 0); !errors.Is(err, blob.ErrNotFound) && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("restored copy: the missing blob is there: %v", err)
		}
		st, err := store.Open(filepath.Join(dst, brand.Name+".db"))
		if err != nil {
			t.Fatal(err)
		}
		params := lastAudit(t, st, OpRestore)
		_ = st.Close()
		if !strings.Contains(params, `"missing_blobs":1`) || strings.Contains(params, sum) {
			t.Fatalf("backup.restore = %s", params)
		}
	}
	if _, err := os.Stat(filepath.Join(to, filepath.FromSlash(blobRel(sum, false)))); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("backup folder holds the missing blob: %v", err)
	}
}
