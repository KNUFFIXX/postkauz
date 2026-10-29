package blob

import (
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
// Put: blobs.gc löscht gerade; für GC: ein Put legt gerade ab).
var ErrBusy = errors.New("blob: store busy (blob gc or another write holds the lock); try again")

// Wartezeiten (Festlegung Nachbesserung T3, Punkt 6): Put wartet höchstens
// putLockWait (10 s) auf ein laufendes Lösch-Paket von blobs.gc. Ein Paket hält
// die Sperre nur Millisekunden (gcBatch in internal/retention, danach eine
// Pause länger als lockPoll), Put scheitert also praktisch nie daran;
// die Grenze fängt nur einen hängenden Datenträger ab.
var (
	putLockWait = 10 * time.Second
	lockPoll    = 5 * time.Millisecond
)

func (s *Store) lockPath() string { return filepath.Join(s.root, LockName) }

// lockWait holt die Sperre (geteilt oder exklusiv) und versucht es bis
// wait erneut; danach ErrBusy.
func (s *Store) lockWait(exclusive bool, wait time.Duration) (*flock.Lock, error) {
	try := flock.TryLockShared
	if exclusive {
		try = flock.TryLock
	}
	deadline := time.Now().Add(wait)
	for {
		l, err := try(s.lockPath())
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, flock.ErrLocked) {
			return nil, fmt.Errorf("blob: %w", err)
		}
		if !time.Now().Before(deadline) {
			return nil, ErrBusy
		}
		time.Sleep(lockPoll)
	}
}

// LockForGC holt die exklusive Sperre des Blob-Ordners (nur blobs.gc, nur
// für ein Lösch-Paket). Solange sie besteht, wartet jedes Put; ein Put, das
// gerade ablegt, hält GC höchstens wait auf (dann ErrBusy). Freigeben mit
// Release.
func (s *Store) LockForGC(wait time.Duration) (*flock.Lock, error) {
	return s.lockWait(true, wait)
}
