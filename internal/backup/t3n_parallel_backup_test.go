package backup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// T3-Nachbesserung 5(a) (G1-7): Zwei Sicherungen gleichzeitig in denselben
// Ordner. Danach muss das Manifest auf eine vorhandene Datenbank zeigen,
// oder ein Lauf muss scheitern (Sperre); nie darf ein erfolgreicher Lauf
// ein Manifest auf einen gelöschten Schnappschuss hinterlassen. Ob sich
// die Läufe überschneiden, hängt vom Zeitplan ab; den Fall erzwingt
// TestT3NBackupDuringBackupIsRefused.
func TestT3NParallelBackupsKeepManifestDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("1000 blobs; the deterministic case is TestT3NBackupDuringBackupIsRefused")
	}
	e := newEnv(t)
	to := filepath.Join(t.TempDir(), "backup")
	ctx := context.Background()
	// Normalfall: Der Ordner hält schon eine Sicherung (inkrementell).
	if _, err := e.r.Create(ctx, Options{To: to}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		e.put(t, e.blobs, fmt.Sprintf("neu %d %s", i, string(make([]byte, 2048))))
	}
	ra := *e.r
	rb := *e.r
	ra.Now = func() time.Time { return time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC) }
	rb.Now = func() time.Time { return time.Date(2026, 10, 28, 10, 0, 5, 0, time.UTC) }
	var wg sync.WaitGroup
	errs := make([]error, 2)
	res := make([]Result, 2)
	wg.Add(2)
	go func() { defer wg.Done(); res[0], errs[0] = ra.Create(ctx, Options{To: to}) }()
	go func() {
		defer wg.Done()
		time.Sleep(20 * time.Millisecond)
		res[1], errs[1] = rb.Create(ctx, Options{To: to})
	}()
	wg.Wait()
	t.Logf("run A: db %s err %v; run B: db %s err %v", res[0].Database, errs[0], res[1].Database, errs[1])
	if errs[0] != nil && errs[1] != nil {
		t.Fatal("both runs failed")
	}
	m := readManifest(t, to)
	if _, err := os.Stat(filepath.Join(to, m.Database)); err != nil {
		t.Fatalf("manifest points to a missing database snapshot %s: %v", m.Database, err)
	}
	for i, r := range res {
		if errs[i] == nil {
			if _, err := os.Stat(filepath.Join(to, r.Database)); err != nil && r.Database == m.Database {
				t.Fatalf("successful run %d left a manifest without its database", i)
			}
		}
	}
}

// T3-Nachbesserung 5(a), deterministisch: Eine zweite Sicherung startet,
// während die erste ihren Schnappschuss abgelegt hat und die Blobs noch
// vor ihr liegen. Ohne Sperre löschte die zweite den Schnappschuss der
// ersten, deren Manifest dann ins Leere zeigte. Mit der Sperre im
// Datenverzeichnis wird die zweite abgelehnt (Ordner- und Tar-Modus, auch
// in einen anderen Ordner); ein Trockenlauf schreibt nichts und darf.
func TestT3NBackupDuringBackupIsRefused(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	to := filepath.Join(t.TempDir(), "backup")
	if _, err := e.r.Create(ctx, Options{To: to}); err != nil {
		t.Fatal(err)
	}
	ra, rb := *e.r, *e.r
	ra.Now = func() time.Time { return time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC) }
	rb.Now = func() time.Time { return time.Date(2026, 10, 28, 10, 0, 5, 0, time.UTC) }
	var second, other, tarErr, dry error
	ra.hook = func(stage string) {
		if stage != "snapshot" {
			return
		}
		_, second = rb.Create(ctx, Options{To: to})
		_, other = rb.Create(ctx, Options{To: filepath.Join(t.TempDir(), "anderer")})
		_, tarErr = rb.Create(ctx, Options{Tar: io.Discard})
		_, dry = rb.Create(ctx, Options{To: to, DryRun: true})
	}
	res, err := ra.Create(ctx, Options{To: to})
	if err != nil {
		t.Fatalf("first backup: %v", err)
	}
	for name, err := range map[string]error{"same folder": second, "other folder": other, "tar": tarErr} {
		if !errors.Is(err, ErrBusy) {
			t.Errorf("second backup (%s) during a backup: err = %v, want ErrBusy", name, err)
		}
	}
	if dry != nil {
		t.Errorf("dry run during a backup: %v", dry)
	}
	m := readManifest(t, to)
	if m.Database != res.Database {
		t.Fatalf("manifest database %s, first run wrote %s", m.Database, res.Database)
	}
	if _, err := os.Stat(filepath.Join(to, m.Database)); err != nil {
		t.Fatalf("manifest points to a missing database snapshot: %v", err)
	}
	// Danach ist die Sperre frei.
	if _, err := rb.Create(ctx, Options{To: to}); err != nil {
		t.Fatalf("backup after the first one: %v", err)
	}
}
