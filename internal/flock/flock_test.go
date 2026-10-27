package flock

import (
	"errors"
	"path/filepath"
	"runtime"
	"testing"
)

func TestTryLockAndAccountLocks(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no OS file locks on this platform")
	}
	dir := t.TempDir()
	a, err := TryLock(filepath.Join(dir, AccountLockName(1)))
	if err != nil {
		t.Fatal(err)
	}
	// Eine zweite Sperre derselben Datei (anderes Handle) scheitert sofort.
	if _, err := TryLock(filepath.Join(dir, AccountLockName(1))); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock = %v", err)
	}
	b, err := TryLock(filepath.Join(dir, AccountLockName(2)))
	if err != nil {
		t.Fatal(err)
	}
	b.Release()
	if _, err := AccountLocks(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("AccountLocks with a held lock = %v", err)
	}
	// Die freie Datei von Konto 2 ist danach wieder frei.
	if l, err := TryLock(filepath.Join(dir, AccountLockName(2))); err != nil {
		t.Fatalf("lock 2 not released: %v", err)
	} else {
		l.Release()
	}
	a.Release()
	release, err := AccountLocks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(filepath.Join(dir, AccountLockName(1))); !errors.Is(err, ErrLocked) {
		t.Fatalf("AccountLocks does not hold the locks: %v", err)
	}
	release()
	a.Release() // doppelt freigeben ist harmlos
}

// W1: Geteilte Sperren bestehen nebeneinander und schließen die exklusive
// aus; die exklusive schließt geteilte aus.
func TestTryLockShared(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no OS file locks on this platform")
	}
	path := filepath.Join(t.TempDir(), "pk.db.lock")
	a, err := TryLockShared(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := TryLockShared(path)
	if err != nil {
		t.Fatalf("second shared lock = %v", err)
	}
	if _, err := TryLock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("exclusive with shared holders = %v", err)
	}
	a.Release()
	if _, err := TryLock(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("exclusive with one shared holder = %v", err)
	}
	b.Release()
	x, err := TryLock(path)
	if err != nil {
		t.Fatalf("exclusive after release = %v", err)
	}
	if _, err := TryLockShared(path); !errors.Is(err, ErrLocked) {
		t.Fatalf("shared with an exclusive holder = %v", err)
	}
	x.Release()
	c, err := TryLockShared(path)
	if err != nil {
		t.Fatal(err)
	}
	c.Release()
}
