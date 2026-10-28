package oauth

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Sperre beim Speichern (zweite Nachbesserung T4, K2): Ohne Sperre gewinnt
// beim gleichzeitigen Speichern zweier Prozesse der zuletzt geschriebene
// Haupteintrag; stellt der eine genau zwischen Lesen und Schreiben des
// anderen um, bleiben seine Teile verwaist liegen (im Stresstest mit zwei
// schreibenden Goroutinen in rund jedem dritten Lauf). Der Schlüsselbund
// kennt weder Auflisten noch „schreibe nur, wenn unverändert“; eine
// Sperrdatei neben der Datenbank (flock bzw. LockFileEx) schließt das
// gleichzeitige Speichern desselben Eintrags daher prozessübergreifend aus.

// LockTimeout begrenzt das Warten auf die Sperre (ein Speichern sind
// wenige Aufrufe des Schlüsselbunds, je höchstens 10 s).
const LockTimeout = 60 * time.Second

// lockPoll ist der Abstand der Versuche (flock wartet nicht).
const lockPoll = 20 * time.Millisecond

// ErrLockTimeout: Ein anderer Prozess speichert das Token desselben Kontos
// länger als LockTimeout.
var ErrLockTimeout = errors.New("oauth: another process is storing the token of this account")

// LockName ist der Name der Sperrdatei zum Verweis ref
// ("account/1a2b3c4d/outlook" → "oauth-account_1a2b3c4d_outlook.lock").
func LockName(ref string) string {
	return "oauth-" + strings.ReplaceAll(ref, "/", "_") + ".lock"
}

// lockRef sperrt ref über eine Sperrdatei in dir und wartet höchstens
// timeout. dir leer: keine Sperre (Datenbank im Speicher, Tests).
func lockRef(dir, ref string, timeout time.Duration) (release func(), err error) {
	if dir == "" {
		return func() {}, nil
	}
	if err := secrets.ValidRef(ref); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, LockName(ref))
	deadline := time.Now().Add(timeout)
	for {
		l, err := flock.TryLock(path)
		if err == nil {
			return l.Release, nil
		}
		if !errors.Is(err, flock.ErrLocked) {
			return nil, fmt.Errorf("oauth: token lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("%w (waited %s)", ErrLockTimeout, timeout)
		}
		time.Sleep(lockPoll)
	}
}

// SaveLocked ist Save unter der Sperre des Eintrags in lockDir (leer =
// ohne Sperre). Alle Prozesse eines Datenverzeichnisses speichern so.
func SaveLocked(lockDir string, st secrets.Store, ref string, tok Token, now time.Time) error {
	release, err := lockRef(lockDir, ref, LockTimeout)
	if err != nil {
		return err
	}
	defer release()
	return Save(st, ref, tok, now)
}
