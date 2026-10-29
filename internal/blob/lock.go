package blob

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/flock"
)

// Sperre des Blob-Ordners (Nachbesserung T3, Punkt 6; A14 „GC bricht
// offene Entwürfe nicht“): Put hält die Datei <root>/gc.lock geteilt, vom
// Nachsehen „liegt der Inhalt schon da?“ bis die Datei liegt bzw. ihr Datum
// erneuert ist; blobs.gc hält sie exklusiv, aber nur während es Dateien
// löscht, und dort in kurzen Paketen. So löscht GC nie eine Datei, die ein
// Put eines anderen Prozesses (drafts.create im MCP-Server, ohne
// Kontosperre) gerade als vorhanden meldet: Entweder Put war vorher fertig
// (dann sieht GC unter der Sperre das frische Datum), oder Put wartet, bis
// das Paket gelöscht ist (dann legt es die Datei neu an).
//
// Die Sperre fällt mit dem Prozess (internal/flock); eine verwaiste gibt es
// nicht. Die Datei bleibt leer liegen; Scan meldet sie nicht.

// LockName ist der Name der Sperrdatei im Blob-Ordner.
const LockName = "gc.lock"

// ErrBusy: Die Sperre war innerhalb der Wartezeit nicht zu bekommen (für
// Put: blobs.gc löscht gerade; für GC: ein Put legt gerade ab). Scheiterte
// zuletzt schon das Öffnen der Sperrdatei mit einem vorübergehenden Fehler
// (Nachbesserung 2, Punkt 8: unter Windows etwa eine Sharing-Violation
// durch Virenscanner oder Sicherungsprogramm), hängt jener Fehler mit an
// (errors.Is findet beide). Ein dauerhafter Öffnungsfehler ist kein
// ErrBusy (lockWait).
// CLI und MCP melden den Code blob_busy („try again“).
var ErrBusy = errors.New("blob: store busy (blob gc or another write holds the lock); try again")

// Wartezeiten (Festlegung Nachbesserung T3, Punkt 6): Put wartet höchstens
// putLockWait (10 s) auf ein laufendes Lösch-Paket von blobs.gc. Ein Paket hält
// die Sperre nur Millisekunden (gcBatch in internal/retention, danach eine
// Pause länger als lockPoll), Put scheitert also praktisch nie daran;
// die Grenze fängt nur einen hängenden Datenträger ab. Options.LockWait
// ersetzt putLockWait je Store (Tests anderer Pakete).
var (
	putLockWait = 10 * time.Second
	lockPoll    = 5 * time.Millisecond
)

func (s *Store) lockPath() string { return filepath.Join(s.root, LockName) }

// putWait ist die Wartezeit von Put auf die Sperre.
func (s *Store) putWait() time.Duration {
	if s.opts.LockWait > 0 {
		return s.opts.LockWait
	}
	return putLockWait
}

// transientLockErr stuft Fehler beim Öffnen oder Sperren von gc.lock als
// vorübergehend ein (isTransientLockErr); in Tests ersetzbar.
var transientLockErr = isTransientLockErr

// lockWait holt die Sperre (geteilt oder exklusiv) und versucht es bis
// wait erneut; danach ErrBusy. Nachbesserung 2, Punkt 8: Wiederholt wird
// nicht nur bei belegter Sperre, sondern auch, wenn das Öffnen der
// Sperrdatei vorübergehend scheitert (unter Windows hält ein Virenscanner
// oder ein Sicherungsprogramm sie kurz ohne Freigabe zum Mitbenutzen);
// bleibt das bis zur Frist so, kommt ErrBusy mit dem letzten Fehler.
// Nachbesserung 3, Punkt 8: Jeder andere Fehler (transientLockErr false,
// unter macOS und Linux alle) endet sofort mit diesem Fehler, ohne
// ErrBusy; sonst wartete bei einem dauerhaften Fehler jede Nachricht die
// volle Frist. Wird ctx beendet, endet das Warten sofort mit ctx.Err().
func (s *Store) lockWait(ctx context.Context, exclusive bool, wait time.Duration) (*flock.Lock, error) {
	try := flock.TryLockShared
	if exclusive {
		try = flock.TryLock
	}
	deadline := time.Now().Add(wait)
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	for {
		l, err := try(s.lockPath())
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, flock.ErrLocked) && !transientLockErr(err) {
			return nil, fmt.Errorf("blob: %w", err)
		}
		if !time.Now().Before(deadline) {
			if errors.Is(err, flock.ErrLocked) {
				return nil, ErrBusy
			}
			return nil, fmt.Errorf("%w (last error: %w)", ErrBusy, err)
		}
		if timer == nil {
			timer = time.NewTimer(lockPoll)
		} else {
			timer.Reset(lockPoll)
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("blob: waiting for the lock: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// LockForGC holt die exklusive Sperre des Blob-Ordners (nur blobs.gc, nur
// für ein Lösch-Paket). Solange sie besteht, wartet jedes Put; ein Put, das
// gerade ablegt, hält GC höchstens wait auf (dann ErrBusy), ein beendetes
// ctx sofort. Freigeben mit Release.
func (s *Store) LockForGC(ctx context.Context, wait time.Duration) (*flock.Lock, error) {
	return s.lockWait(ctx, true, wait)
}
