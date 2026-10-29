package blob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Nachbesserung 3, Punkt 8: Die Sperre des Blob-Ordners wiederholt nur
// vorübergehende Fehler beim Öffnen von gc.lock (unter Windows Sharing-,
// Lock- und Access-Violation, isTransientLockErr) bis zur Frist; jeder
// andere scheitert sofort mit dem echten Fehler, nicht mit ErrBusy (sonst
// wartete bei einem dauerhaften Fehler jede Nachricht 10 s). Nachgestellt
// mit gc.lock als Ordner (EISDIR, auf allen Plattformen dauerhaft); als
// vorübergehend gilt er nur über den Test-Hook transientLockErr.
// TestT3N3PutPermanentOpenErrorFailsAtOnce ersetzt
// TestT3N2PutPersistentOpenErrorIsBusy, TestT3N3PutRetriesTransientOpenError
// den gleichnamigen T3N2-Test.

// transientDirLock lässt lockWait den Fehler von gc.lock als Ordner wie
// einen vorübergehenden behandeln.
func transientDirLock(t *testing.T) {
	t.Helper()
	orig := transientLockErr
	t.Cleanup(func() { transientLockErr = orig })
	transientLockErr = func(err error) bool { return errors.Is(err, syscall.EISDIR) }
}

func TestT3N3PutPermanentOpenErrorFailsAtOnce(t *testing.T) {
	skipWithoutLocks(t)
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Root(), LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	info, err := s.Put(strings.NewReader("bleibt gesperrt"))
	d := time.Since(start)
	if err == nil || errors.Is(err, ErrBusy) {
		t.Fatalf("Put with a permanent open error = %v, want the open error, not ErrBusy", err)
	}
	if !errors.Is(err, syscall.EISDIR) || !strings.Contains(err.Error(), LockName) {
		t.Fatalf("Put error %v does not carry the open error of %s", err, LockName)
	}
	if d > 2*time.Second {
		t.Fatalf("Put gave up after %v; want at once (wait is %v)", d, putLockWait)
	}
	if info.SHA256 != "" {
		t.Fatalf("Put returned info %+v with an error", info)
	}

	// Ebenso blobs.gc.
	start = time.Now()
	if _, err := s.LockForGC(context.Background(), 10*time.Second); err == nil || errors.Is(err, ErrBusy) {
		t.Fatalf("LockForGC with a permanent open error = %v, want the open error", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("LockForGC gave up after %v; want at once", d)
	}
}

func TestT3N3PutRetriesTransientOpenError(t *testing.T) {
	skipWithoutLocks(t)
	transientDirLock(t)
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(s.Root(), LockName)
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		removed <- os.Remove(lockDir)
	}()
	info, err := s.Put(strings.NewReader("anhang trotz kurz gesperrter Sperrdatei"))
	if rerr := <-removed; rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Fatalf("Put after a transient open error of %s: %v (want a retry)", LockName, err)
	}
	if !s.Has(info.SHA256) {
		t.Fatalf("blob %s not stored", info.SHA256)
	}
}

// Bleibt ein vorübergehender Öffnungsfehler bis zur Frist, kommt ErrBusy
// mit dem letzten Fehler, und es liegt kein Blob.
func TestT3N3PutTransientOpenErrorUntilDeadlineIsBusy(t *testing.T) {
	skipWithoutLocks(t)
	transientDirLock(t)
	s, err := Open(t.TempDir(), Options{LockWait: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Root(), LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	info, err := s.Put(strings.NewReader("bleibt kurz gesperrt"))
	if !errors.Is(err, ErrBusy) || !errors.Is(err, syscall.EISDIR) {
		t.Fatalf("Put with a transient open error until the deadline = %v, want ErrBusy with the open error", err)
	}
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("Put gave up after %v, before the wait of 50ms", d)
	}
	if info.SHA256 != "" {
		t.Fatalf("Put returned info %+v with an error", info)
	}
}
