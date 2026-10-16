package mailsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Sperre je Konto: Zwei Syncs desselben Kontos (z. B. `sync --watch` und ein
// manueller `sync`) würden dieselben Nachrichten gleichzeitig als neu
// eintragen. Der zweite bricht deshalb sofort mit ops.ErrSyncRunning ab.
// Die Sperre ist eine Sperre des Betriebssystems auf einer Datei neben der
// Datenbank (flock bzw. LockFileEx); sie fällt mit dem Prozess, auch nach
// kill -9 oder Absturz, es bleibt also nie eine verwaiste Sperre zurück. Die
// Datei selbst bleibt liegen (leer) und wird beim nächsten Lauf wiederverwendet.

// LockAccount sperrt das Konto für einen schreibenden Lauf (Sync oder
// Import, S1.10) wie Runner.Sync: Sperrdatei in lockDir, sonst neben der
// Datenbank (bei :memory: ohne Sperre, locked=false). Ist das Konto
// gesperrt, kommt ops.ErrSyncRunning.
func LockAccount(st *store.SQLite, lockDir string, a store.Account) (release func(), locked bool, err error) {
	r := &Runner{Store: st, LockDir: lockDir}
	l, err := r.lock(a)
	if err != nil {
		return func() {}, false, err
	}
	return l.release, l != nil, nil
}

// errLocked meldet, dass ein anderer Prozess (oder Handle) die Datei sperrt.
var errLocked = errors.New("locked")

// accountLock hält die Sperre eines Kontos.
type accountLock struct{ f *os.File }

// lockAccount sperrt das Konto für einen schreibenden Sync, ohne zu warten.
func lockAccount(dir string, accountID int64) (*accountLock, error) {
	path := filepath.Join(dir, fmt.Sprintf("sync-%d.lock", accountID))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("sync lock %s: %w", path, err)
	}
	if err := lockFile(f); err != nil {
		_ = f.Close()
		if errors.Is(err, errLocked) {
			return nil, errLocked
		}
		return nil, fmt.Errorf("sync lock %s: %w", path, err)
	}
	return &accountLock{f: f}, nil
}

// release gibt die Sperre frei (Schließen der Datei hebt sie auf).
func (l *accountLock) release() {
	if l != nil && l.f != nil {
		_ = unlockFile(l.f)
		_ = l.f.Close()
	}
}
