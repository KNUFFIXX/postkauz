package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/backup"
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// doctorRestoreLeftovers meldet Arbeitsordner abgebrochener
// Wiederherstellungen neben dem Datenverzeichnis
// (<datenverzeichnis>.restoring-<zufall>; T3-Nachbesserung 4). doctor
// löscht nichts.
func doctorRestoreLeftovers(rep *DoctorReport, dataDir string) {
	left := backup.RestoreLeftovers(dataDir)
	if len(left) == 0 {
		return
	}
	rep.RestoreLeftovers = left
	for _, l := range left {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("restore: %s is left from an interrupted restore; "+
			"it is not a usable data directory (it may still carry the identity of the original); delete it and run restore again", l))
	}
}

// doctorArchiveFiles meldet Archivdateien von audit rotate im Ordner der
// Datenbank, die in audit_archives nicht eingetragen sind
// (T3-Nachbesserung 3b): Reste eines hart abgebrochenen audit rotate. Sie
// gehören nicht zum Archiv, eine Sicherung nimmt sie nicht mit; doctor
// ändert nichts. Ohne Tabelle (Schema vor 0022) gibt es nichts zu prüfen.
func doctorArchiveFiles(ctx context.Context, db *store.SQLite, rep *DoctorReport) {
	list, err := db.AuditArchives(ctx)
	if err != nil {
		return
	}
	known := map[string]bool{}
	for _, a := range list {
		known[a.File] = true
	}
	entries, err := os.ReadDir(filepath.Dir(db.Path()))
	if err != nil {
		rep.Warnings = append(rep.Warnings, "audit archives: "+err.Error())
		return
	}
	var orphans []string
	for _, e := range entries {
		n := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(n, store.AuditArchivePrefix) && strings.HasSuffix(n, ".db") && !known[n] {
			orphans = append(orphans, n)
		}
	}
	if len(orphans) == 0 {
		return
	}
	sort.Strings(orphans)
	// Läuft gerade eine Rotation (Zweite Nachbesserung T3, Punkt 14), ist
	// die Datei wohl ihr Archiv, das sie gleich einträgt: kein Rat zum
	// Verschieben.
	if auditRotateRunning(filepath.Dir(db.Path())) {
		rep.AuditRotateRunning = true
		rep.Warnings = append(rep.Warnings, fmt.Sprintf("audit: an audit rotate of this data directory is running right now; "+
			"%s without an entry in the database yet (%s) belong to it; leave them alone and check again later with doctor",
			plural(len(orphans), "archive file", "archive files"), strings.Join(orphans, ", ")))
		return
	}
	rep.AuditArchivesUnregistered = orphans
	rep.Warnings = append(rep.Warnings, fmt.Sprintf("audit: %s in the data directory without an entry in the database (%s): "+
		"left by an interrupted audit rotate; they are not part of the audit archive and no backup takes them; "+
		"move them out of the data directory", plural(len(orphans), "archive file", "archive files"), strings.Join(orphans, ", ")))
}

// auditRotateRunning meldet, ob ein anderer Prozess die Sperre von audit
// rotate neben der Datenbank hält. Die Sperre wird nur einen Augenblick
// lang geholt; fehlt die Sperrdatei, lief nie eine Rotation, und doctor
// legt sie nicht an.
func auditRotateRunning(dir string) bool {
	path := filepath.Join(dir, ops.AuditRotateLockName)
	if _, err := os.Stat(path); err != nil {
		return false
	}
	l, err := flock.TryLock(path)
	if errors.Is(err, flock.ErrLocked) {
		return true
	}
	l.Release()
	return false
}
