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
