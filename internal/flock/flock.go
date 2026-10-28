// Package flock sperrt Dateien über das Betriebssystem (flock bzw.
// LockFileEx), ohne zu warten. Die Sperre fällt mit dem Prozess, auch nach
// kill -9 oder Absturz; es bleibt nie eine verwaiste Sperre zurück. Die
// Datei selbst bleibt liegen (leer) und wird wiederverwendet.
//
// Genutzt für die Sperre je Konto (Sync, Import; internal/mailsync), seit
// T1a von store (Migration und Kontolöschung warten nicht auf einen
// laufenden Sync, sie verweigern) und seit T1a-Nachbesserung W1 für die
// Prozesssperre neben der Datenbank (geteilt für jeden Prozess, der sie
// öffnet; exklusiv nur zum Migrieren).
package flock

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrLocked: Ein anderer Prozess (oder ein anderes Handle) hält die Sperre.
var ErrLocked = errors.New("flock: locked")

// Lock ist eine gehaltene Sperre.
type Lock struct{ f *os.File }

// TryLock legt path an (0600) und sperrt die Datei exklusiv, ohne zu
// warten. Ist sie gesperrt (exklusiv oder geteilt), kommt ErrLocked.
func TryLock(path string) (*Lock, error) { return tryLock(path, true) }

// TryLockShared legt path an (0600) und sperrt die Datei geteilt, ohne zu
// warten: Beliebig viele geteilte Sperren bestehen nebeneinander, eine
// exklusive schließt sie aus. Hält jemand die exklusive, kommt ErrLocked.
// Eine geteilte Sperre wird nie in eine exklusive umgewandelt (unter Unix
// nicht atomar, unter Windows nicht möglich); wer umsteigen will, gibt sie
// frei und sperrt neu.
func TryLockShared(path string) (*Lock, error) { return tryLock(path, false) }

func tryLock(path string, exclusive bool) (*Lock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	if err := lockFile(f, exclusive); err != nil {
		_ = f.Close()
		if errors.Is(err, ErrLocked) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return &Lock{f: f}, nil
}

// Release gibt die Sperre frei (Schließen der Datei hebt sie auf). Nil ist
// erlaubt.
func (l *Lock) Release() {
	if l != nil && l.f != nil {
		_ = unlockFile(l.f)
		_ = l.f.Close()
		l.f = nil
	}
}

// AccountLockName ist der Name der Sperrdatei eines Kontos.
func AccountLockName(accountID int64) string { return fmt.Sprintf("sync-%d.lock", accountID) }

// AccountLockPattern findet alle Sperrdateien von Konten in einem Ordner
// (filepath.Glob).
const AccountLockPattern = "sync-*.lock"

// BusyAccounts liefert die IDs der Konten in dir, deren Sperrdatei ein
// anderer Prozess hält (ein Sync oder Import läuft). Jede Sperre wird nur
// einen Augenblick lang und einzeln geholt (doctor, A-F9).
func BusyAccounts(dir string) ([]int64, error) {
	paths, err := filepath.Glob(filepath.Join(dir, AccountLockPattern))
	if err != nil {
		return nil, err
	}
	var busy []int64
	for _, p := range paths {
		var id int64
		if _, err := fmt.Sscanf(filepath.Base(p), "sync-%d.lock", &id); err != nil {
			continue
		}
		l, err := TryLock(p)
		if errors.Is(err, ErrLocked) {
			busy = append(busy, id)
			continue
		}
		if err != nil {
			return nil, err
		}
		l.Release()
	}
	return busy, nil
}

// AccountLocks sperrt alle vorhandenen Sperrdateien von Konten in dir.
// Hält ein anderer Prozess eine davon (ein Sync oder Import läuft), gibt
// sie die schon geholten frei und meldet ErrLocked. Der Aufrufer hält die
// Sperren, solange er arbeitet, und gibt sie mit release frei.
func AccountLocks(dir string) (release func(), err error) {
	paths, err := filepath.Glob(filepath.Join(dir, AccountLockPattern))
	if err != nil {
		return func() {}, err
	}
	var held []*Lock
	release = func() {
		for _, l := range held {
			l.Release()
		}
	}
	for _, p := range paths {
		l, err := TryLock(p)
		if err != nil {
			release()
			return func() {}, err
		}
		held = append(held, l)
	}
	return release, nil
}
