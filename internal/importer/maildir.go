package importer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Maildir: je Nachricht eine Datei in cur/ (gesehen vom Client) oder new/
// (noch nicht), tmp/ ist eine laufende Zustellung (wird übersprungen). Flags
// stehen im Namen hinter ":2," (unter Windows auch "!2," oder ";2,"):
// S gelesen, R beantwortet, F markiert, D Entwurf, T zum Löschen markiert
// (wird übersprungen wie Thunderbirds gelöscht markierte Nachrichten).

// maildirEntry ist eine Datei eines Maildirs.
type maildirEntry struct {
	Key   string // "cur/<name>" bzw. "new/<name>" (Sortierung, Cursor)
	Path  string
	MTime time.Time
	Size  int64
	Flags string // Buchstaben hinter ":2,"
}

func (e maildirEntry) has(flag byte) bool { return strings.IndexByte(e.Flags, flag) >= 0 }

// maildirFlags liest die Flags aus dem Dateinamen.
func maildirFlags(name string) string {
	for _, sep := range []string{":2,", "!2,", ";2,"} {
		if i := strings.LastIndex(name, sep); i >= 0 {
			return name[i+len(sep):]
		}
	}
	return ""
}

// listMaildir liest cur/ und new/ (sortiert) und zählt tmp/.
func listMaildir(dir string) (entries []maildirEntry, tmp int, stamp int64, err error) {
	for _, sub := range []string{"cur", "new"} {
		d := filepath.Join(dir, sub)
		des, err := os.ReadDir(d)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, 0, 0, err
		}
		if fi, err := os.Stat(d); err == nil {
			stamp = max(stamp, fi.ModTime().UnixMilli())
		}
		for _, de := range des {
			if !de.Type().IsRegular() || strings.HasPrefix(de.Name(), ".") {
				continue
			}
			fi, err := de.Info()
			if err != nil {
				continue // zwischenzeitlich verschwunden
			}
			e := maildirEntry{Key: sub + "/" + de.Name(), Path: filepath.Join(d, de.Name()), MTime: fi.ModTime(), Size: fi.Size()}
			if sub == "cur" {
				e.Flags = maildirFlags(de.Name())
			}
			entries = append(entries, e)
		}
	}
	if des, err := os.ReadDir(filepath.Join(dir, "tmp")); err == nil {
		for _, de := range des {
			if de.Type().IsRegular() {
				tmp++
			}
		}
	}
	slices.SortFunc(entries, func(a, b maildirEntry) int { return strings.Compare(a.Key, b.Key) })
	return entries, tmp, stamp, nil
}
