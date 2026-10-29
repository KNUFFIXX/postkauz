//go:build !windows

package store

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// Nachbesserung T3, „Wenn Zeit“ (G1-8 der Nachprüfung): Der Schnappschuss
// der Sicherung (SnapshotTo) ist von Anfang an 0600, nicht erst nach dem
// chmod (wie der Migrations-Snapshot seit T3). Ein Beobachter liest die
// Rechte der Zieldatei, während VACUUM INTO sie füllt.
func TestT3NSnapshotToNeverReadableByOthers(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Genug Inhalt, dass VACUUM INTO eine Weile schreibt.
	if err := st.ExecForTest(`WITH RECURSIVE c(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM c WHERE i < 200000)
		INSERT INTO audit_events(ts, actor_type, operation, params_redacted, result) SELECT i, 'system', 'sync.run', '{"x":"` +
		`aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}', 'ok' FROM c`); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "snap.db")
	var seen, polls atomic.Uint32
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if fi, err := os.Stat(dest); err == nil {
				polls.Add(1)
				seen.Store(seen.Load() | uint32(fi.Mode().Perm()&0o077))
			}
			time.Sleep(50 * time.Microsecond)
		}
	}()
	err = st.SnapshotTo(ctx, dest)
	close(stop)
	<-done
	if err != nil {
		t.Fatal(err)
	}
	if m := seen.Load(); m != 0 {
		t.Fatalf("snapshot was readable by group/others while it was written (bits %o, %d polls)", m, polls.Load())
	}
	if fi, err := os.Stat(dest); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, %v", fi.Mode().Perm(), err)
	}
}

// SnapshotTo legt das Ziel exklusiv an: Liegt dort schon eine Datei, bleibt
// sie unverändert und der Aufruf scheitert.
func TestT3NSnapshotToRefusesExistingFile(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "snap.db")
	if err := os.WriteFile(dest, []byte("fremd"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := st.SnapshotTo(ctx, dest); err == nil {
		t.Fatal("snapshot over an existing file succeeded")
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "fremd" {
		t.Fatalf("existing file changed: %q, %v", b, err)
	}
}
