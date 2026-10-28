package blob

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Hilfen für blobs.gc (S3.4, E8, A11; internal/retention). Die
// Entscheidung, was verwaist ist, trifft der Dienst; hier stehen nur Datum
// und Löschen einzelner Blobs.

// ModTime liefert das Dateidatum eines Blobs; liegen beide Formen vor
// (<sha256> und <sha256>.zst), das neuere. ErrNotFound, wenn keine da ist.
func (s *Store) ModTime(sum string) (time.Time, error) {
	if !validHash(sum) {
		return time.Time{}, ErrBadHash
	}
	base := filepath.Join(s.dir(sum), sum)
	var newest time.Time
	found := false
	for _, p := range []string{base, base + zstSuffix} {
		fi, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return time.Time{}, fmt.Errorf("blob: stat: %w", err)
		}
		found = true
		if fi.ModTime().After(newest) {
			newest = fi.ModTime()
		}
	}
	if !found {
		return time.Time{}, ErrNotFound
	}
	return newest, nil
}

// Remove löscht einen Blob in beiden Formen (nur blobs.gc). removed ist
// true, wenn mindestens eine Datei gelöscht wurde; fehlt der Blob, ist das
// kein Fehler. Leere Verzeichnisse bleiben stehen (Put legt sie ohnehin
// wieder an).
func (s *Store) Remove(sum string) (removed bool, err error) {
	if !validHash(sum) {
		return false, ErrBadHash
	}
	base := filepath.Join(s.dir(sum), sum)
	for _, p := range []string{base, base + zstSuffix} {
		switch e := os.Remove(p); {
		case e == nil:
			removed = true
		case errors.Is(e, fs.ErrNotExist):
		default:
			return removed, fmt.Errorf("blob: remove: %w", e)
		}
	}
	return removed, nil
}
