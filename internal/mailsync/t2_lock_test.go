package mailsync

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
)

// T2 Punkt 7 (f): doctor hält jede Kontosperre einen Augenblick
// (flock.BusyAccounts). Ein Sync, der genau dann startet, versucht die
// Sperre mehrmals, statt sofort sync_running zu melden. Hält ein anderer
// sie wirklich, kommt nach den Versuchen flock.ErrLocked.
func TestLockAccountRetriesBriefHolds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, flock.AccountLockName(7))
	defer func(n int, w time.Duration) { lockAttempts, lockRetryWait = n, w }(lockAttempts, lockRetryWait)
	lockAttempts, lockRetryWait = 5, 50*time.Millisecond

	// Kurz gehalten (wie doctor): der zweite Versuch gelingt.
	brief, err := flock.TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(70 * time.Millisecond)
		brief.Release()
	}()
	l, err := lockAccount(dir, 7)
	if err != nil {
		t.Fatalf("lock after a brief hold: %v", err)
	}
	l.release()

	// Dauerhaft gehalten: nach allen Versuchen ErrLocked, nicht früher.
	other, err := flock.TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Release()
	start := time.Now()
	if _, err := lockAccount(dir, 7); !errors.Is(err, flock.ErrLocked) {
		t.Fatalf("err = %v, want ErrLocked", err)
	}
	if d := time.Since(start); d < 4*lockRetryWait {
		t.Fatalf("gave up after %v, want %d attempts", d, lockAttempts)
	}
}
