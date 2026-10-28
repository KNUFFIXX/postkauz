package mailsync

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Sperre je Konto: Zwei Syncs desselben Kontos (z. B. `sync --watch` und ein
// manueller `sync`) würden dieselben Nachrichten gleichzeitig als neu
// eintragen. Der zweite bricht deshalb mit ops.ErrSyncRunning ab (nach
// wenigen kurzen Wiederholungen, siehe lockAttempts).
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
type accountLock struct {
	l    *flock.Lock
	path string
}

// held merkt sich die Sperrdateien, die dieser Prozess hält (Pfad →
// Anzahl; mit sync --watch --all mehrere). lockedElsewhere zählt sie nicht
// als fremd belegt.
var held = struct {
	sync.Mutex
	m map[string]int
}{m: map[string]int{}}

// lockedElsewhere meldet, ob ein anderer Prozess die Sperre des Kontos
// hält: Sie wird einen Augenblick geholt und sofort freigegeben (wie
// flock.BusyAccounts; ein gleichzeitig startender Sync wiederholt deshalb,
// lockAttempts). Hält dieser Prozess sie selbst, ist sie nicht fremd
// belegt. Ein Fehler zählt als belegt.
func lockedElsewhere(dir string, accountID int64) bool {
	path := filepath.Join(dir, flock.AccountLockName(accountID))
	held.Lock()
	mine := held.m[path] > 0
	held.Unlock()
	if mine {
		return false
	}
	l, err := flock.TryLock(path)
	if err != nil {
		return true
	}
	l.Release()
	return false
}

// Wiederholung beim Holen der Kontosperre (T2 Punkt 7, Prüfsitzung
// DRACO-PK09): doctor und das Abschließen toter Starts eines anderen Laufs
// (closeDeadStarts) prüfen fremde Sperren, indem sie sie einen Augenblick
// selbst halten (flock.BusyAccounts). Ein Sync, der genau dann startet,
// meldete sonst fälschlich sync_running. Deshalb versucht lockAccount es
// lockAttempts-mal im Abstand lockRetryWait (zusammen rund 1,6 s), bevor
// es aufgibt. In Tests ersetzbar.
var (
	lockAttempts  = 5
	lockRetryWait = 400 * time.Millisecond
)

// lockAccount sperrt das Konto für einen schreibenden Sync. Ist es
// gesperrt, versucht es lockAttempts-mal und liefert dann flock.ErrLocked;
// andere Fehler kommen sofort zurück.
func lockAccount(dir string, accountID int64) (*accountLock, error) {
	path := filepath.Join(dir, flock.AccountLockName(accountID))
	var err error
	for i := range max(lockAttempts, 1) {
		if i > 0 {
			time.Sleep(lockRetryWait)
		}
		var l *flock.Lock
		if l, err = flock.TryLock(path); err == nil {
			held.Lock()
			held.m[path]++
			held.Unlock()
			return &accountLock{l: l, path: path}, nil
		}
		if !errors.Is(err, flock.ErrLocked) {
			return nil, err
		}
	}
	return nil, err
}

// release gibt die Sperre frei.
func (l *accountLock) release() {
	if l != nil && l.l != nil {
		held.Lock()
		if held.m[l.path]--; held.m[l.path] <= 0 {
			delete(held.m, l.path)
		}
		held.Unlock()
		l.l.Release()
		l.l = nil
	}
}
