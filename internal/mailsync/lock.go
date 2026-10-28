package mailsync

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Sperre je Konto: Zwei Syncs desselben Kontos (z. B. `sync --watch` und ein
// manueller `sync`) würden dieselben Nachrichten gleichzeitig als neu
// eintragen. Der zweite bricht deshalb sofort mit ops.ErrSyncRunning ab.
// Die Sperre ist eine Sperre des Betriebssystems auf einer Datei neben der
// Datenbank (internal/flock: flock bzw. LockFileEx); sie fällt mit dem
// Prozess, auch nach kill -9 oder Absturz. Seit T1a prüft auch store.Migrate
// diese Dateien (keine Migration, solange ein Sync oder Import läuft).

// LockAccount sperrt das Konto für einen schreibenden Lauf (Sync oder
// Import, S1.10) wie Runner.Sync: Sperrdatei in lockDir, sonst neben der
// Datenbank (bei :memory: ohne Sperre, locked=false). Ist das Konto
// gesperrt, kommt ops.ErrSyncRunning.
//
// Mit der Sperre schließt LockAccount die offenen Starts früherer, hart
// beendeter Läufe des Kontos ab (A-F9), außer auditID (der eigene Start).
func LockAccount(ctx context.Context, st *store.SQLite, lockDir string, a store.Account, auditID int64, log *slog.Logger) (release func(), locked bool, err error) {
	r := &Runner{Store: st, LockDir: lockDir, Log: log}
	l, err := r.lock(a)
	if err != nil {
		return func() {}, false, err
	}
	if l != nil {
		r.closeDeadStarts(ctx, a, auditID)
	}
	return l.release, l != nil, nil
}

// accountLock hält die Sperre eines Kontos (internal/flock).
type accountLock struct{ l *flock.Lock }

// lockAccount sperrt das Konto für einen schreibenden Sync, ohne zu warten.
// Ist es gesperrt, kommt flock.ErrLocked.
func lockAccount(dir string, accountID int64) (*accountLock, error) {
	l, err := flock.TryLock(filepath.Join(dir, flock.AccountLockName(accountID)))
	if err != nil {
		return nil, err
	}
	return &accountLock{l: l}, nil
}

// release gibt die Sperre frei.
func (l *accountLock) release() {
	if l != nil {
		l.l.Release()
	}
}
