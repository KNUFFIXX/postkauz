// Package retention ist das Löschkonzept (A03 S3.4, E8; docs/betrieb.md,
// „Löschkonzept“): retention.purge entfernt Nachrichten, die auf dem Server
// gelöscht wurden, samt Index und Zuordnungen; blobs.gc entfernt Dateien
// im Blob-Store, auf die nichts mehr verweist. Beides läuft nur
// ausdrücklich, als HumanOnly-Operation über den Executor (internal/ops),
// mit Trockenlauf, nie nebenbei (ADR-003). audit.rotate steht wegen der
// Hash-Kette bei den übrigen Audit-Operationen (internal/ops).
//
// Beide Läufe halten die Sperren aller Konten (internal/flock, dieselben
// Dateien wie Sync und Import): Solange ein Sync oder Import läuft, lehnen
// sie ab, und während sie laufen, beginnt keiner (A11).
package retention

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// ErrBusy: Ein Sync oder Import hält die Sperre eines Kontos.
var ErrBusy = errors.New("retention: a sync or import is running")

// Service führt purge und blob gc aus.
type Service struct {
	Store *store.SQLite
	// Blobs ist der Blob-Store (nur blob gc).
	Blobs *blob.Store
	// LockDir ist der Ordner der Kontosperren (neben der Datenbank); leer =
	// ohne Sperren (nur Tests mit :memory:).
	LockDir string
	// Now ist in Tests ersetzbar.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// lockAll holt die Sperren aller Konten der Datenbank, ohne zu warten.
// Hält ein anderer Prozess eine davon, kommt ErrBusy (geholte werden
// freigegeben).
func (s *Service) lockAll(ctx context.Context) (release func(), err error) {
	release = func() {}
	if s.LockDir == "" {
		return release, nil
	}
	ids, err := s.Store.AccountIDs(ctx)
	if err != nil {
		return release, err
	}
	var held []*flock.Lock
	release = func() {
		for _, l := range held {
			l.Release()
		}
	}
	for _, id := range ids {
		l, err := flock.TryLock(filepath.Join(s.LockDir, flock.AccountLockName(id)))
		if err != nil {
			release()
			if errors.Is(err, flock.ErrLocked) {
				return func() {}, fmt.Errorf("%w (account id %d)", ErrBusy, id)
			}
			return func() {}, err
		}
		held = append(held, l)
	}
	return release, nil
}

// PurgeOptions steuert retention.purge.
type PurgeOptions struct {
	// Cutoff: nur Nachrichten, deren Löschung auf dem Server vorher bemerkt
	// wurde (deleted_remote_at < Cutoff).
	Cutoff time.Time
	// AccountID: nur dieses Konto (0 = alle).
	AccountID int64
	DryRun    bool
}

// PurgeReport ist das Ergebnis von Purge (nur Zahlen).
type PurgeReport struct {
	// GoneFromServer: ohne Serverkopie, Löschung vor dem Stichtag bemerkt.
	GoneFromServer int
	// KeptImported: davon im importierten Archiv (bleiben, A13.2).
	KeptImported int
	// KeptDrafts: davon von einem Entwurf verwiesen (bleiben, A14).
	KeptDrafts int
	// Eligible: davon zu löschen (im Trockenlauf: würden gelöscht).
	Eligible int
	// Purged: gelöscht (Trockenlauf: 0); Attachments: ihre Anhangszeilen;
	// ThreadsRemoved: Threads, die dabei leer wurden.
	Purged         int
	Attachments    int
	ThreadsRemoved int
}

// Purge entfernt die Nachrichten, die store.PurgeCandidates nennt (Regel
// in store/retention.go: vom Server gelöscht, nicht importiert, ohne
// Verweis eines Entwurfs). Blobs bleiben liegen; die räumt blob gc ab.
func (s *Service) Purge(ctx context.Context, o PurgeOptions) (PurgeReport, error) {
	var rep PurgeReport
	release := func() {}
	if !o.DryRun {
		var err error
		if release, err = s.lockAll(ctx); err != nil {
			return rep, err
		}
	}
	defer release()
	ids, c, err := s.Store.PurgeCandidates(ctx, o.Cutoff, o.AccountID)
	if err != nil {
		return rep, err
	}
	rep.GoneFromServer, rep.KeptImported, rep.KeptDrafts, rep.Eligible = c.Gone, c.KeptImported, c.KeptDrafts, len(ids)
	if o.DryRun || len(ids) == 0 {
		return rep, nil
	}
	res, err := s.Store.PurgeMessages(ctx, o.Cutoff, ids)
	rep.Purged, rep.Attachments, rep.ThreadsRemoved = res.Messages, res.Attachments, res.ThreadsRemoved
	return rep, err
}

// GCReport ist das Ergebnis von GC (nur Zahlen).
type GCReport struct {
	Files      int // Blob-Dateien (verschiedene Hashes)
	Rows       int // Zeilen in blobs
	Referenced int // verwiesene Hashes (Nachrichten, Anhänge, Entwürfe)
	// Unreferenced: Hashes mit Datei oder Zeile, aber ohne Verweis;
	// davon KeptYoung jünger als das Mindestalter (bleiben).
	Unreferenced int
	KeptYoung    int
	// RemovedFiles, RemovedRows: gelöscht (im Trockenlauf: würden gelöscht);
	// Bytes: Größe der gelöschten Dateien auf dem Datenträger.
	RemovedFiles int
	RemovedRows  int
	Bytes        int64
	// Skipped: Kandidaten, auf die beim Löschen der Zeile doch etwas
	// verwies (die Datei bleibt dann).
	Skipped int
}

// GC ist Mark-and-Sweep über den Blob-Store (E8, A11, A13.3): Verwiesen
// ist, was store.BlobReferences nennt. Gelöscht werden Dateien und Zeilen
// ohne Verweis, deren Datei (ohne Datei: deren Zeile) älter als minAge
// ist; blob.Put erneuert das Dateidatum eines wieder abgelegten Inhalts
// (A14). Zuerst die Zeile (die Anweisung prüft die Verweise erneut), dann
// die Datei; ist die Zeile doch verwiesen, bleibt die Datei. Fremde und
// Temporärdateien bleiben, wie sie sind.
func (s *Service) GC(ctx context.Context, minAge time.Duration, dryRun bool) (GCReport, error) {
	var rep GCReport
	if s.Blobs == nil {
		return rep, errors.New("retention: no blob store")
	}
	release := func() {}
	if !dryRun {
		var err error
		if release, err = s.lockAll(ctx); err != nil {
			return rep, err
		}
	}
	defer release()
	// Erst die Verweise, dann Zeilen und Dateien: Was danach entsteht, ist
	// frisch und bleibt durch das Mindestalter stehen.
	refs, err := s.Store.BlobReferences(ctx)
	if err != nil {
		return rep, err
	}
	rows, err := s.Store.BlobRowAges(ctx)
	if err != nil {
		return rep, err
	}
	files := map[string]bool{}
	if err := s.Blobs.Scan(func(e blob.Entry) {
		if e.Kind == blob.EntryBlob {
			files[e.SHA256] = true
		}
	}); err != nil {
		return rep, err
	}
	rep.Files, rep.Rows, rep.Referenced = len(files), len(rows), len(refs)

	rowAt := map[string]time.Time{}
	for _, r := range rows {
		rowAt[r.SHA256] = r.CreatedAt
	}
	var cands []string
	for h := range files {
		if !refs[h] {
			cands = append(cands, h)
		}
	}
	for h := range rowAt {
		if !refs[h] && !files[h] {
			cands = append(cands, h)
		}
	}
	slices.Sort(cands)
	rep.Unreferenced = len(cands)

	limit := s.now().Add(-minAge)
	var oldRows, oldFiles []string
	sizes := map[string]int64{}
	for _, h := range cands {
		if err := ctx.Err(); err != nil {
			return rep, err
		}
		at, hasRow := rowAt[h]
		if files[h] {
			mt, err := s.Blobs.ModTime(h)
			if errors.Is(err, blob.ErrNotFound) {
				// Zwischen Lauf und Prüfung verschwunden: nur die Zeile zählt.
				files[h] = false
			} else if err != nil {
				return rep, err
			} else {
				at = mt
				sizes[h] = s.blobSize(h)
			}
		}
		if at.After(limit) {
			rep.KeptYoung++
			continue
		}
		if hasRow {
			oldRows = append(oldRows, h)
		}
		if files[h] {
			oldFiles = append(oldFiles, h)
		}
	}
	if dryRun {
		rep.RemovedRows, rep.RemovedFiles = len(oldRows), len(oldFiles)
		for _, h := range oldFiles {
			rep.Bytes += sizes[h]
		}
		return rep, nil
	}
	deleted, err := s.Store.DeleteUnreferencedBlobRows(ctx, oldRows)
	rep.RemovedRows = len(deleted)
	if err != nil {
		return rep, err
	}
	gone := map[string]bool{}
	for _, h := range deleted {
		gone[h] = true
	}
	for _, h := range oldFiles {
		if _, hasRow := rowAt[h]; hasRow && !gone[h] {
			rep.Skipped++ // Zeile inzwischen verwiesen: Datei bleibt
			continue
		}
		removed, err := s.Blobs.Remove(h)
		if err != nil {
			return rep, err
		}
		if removed {
			rep.RemovedFiles++
			rep.Bytes += sizes[h]
		}
	}
	return rep, nil
}

// blobSize liefert die Größe der Datei(en) eines Blobs auf dem Datenträger.
func (s *Service) blobSize(h string) int64 {
	p, _, err := s.Blobs.Path(h)
	if err != nil {
		return 0
	}
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}
