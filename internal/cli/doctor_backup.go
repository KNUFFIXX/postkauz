package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

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
	rep.AuditArchivesUnregistered = orphans
	rep.Warnings = append(rep.Warnings, fmt.Sprintf("audit: %s in the data directory without an entry in the database (%s): "+
		"left by an interrupted audit rotate (or one running right now); they are not part of the audit archive and no backup takes them; "+
		"move them out of the data directory", plural(len(orphans), "archive file", "archive files"), strings.Join(orphans, ", ")))
}
