package blob

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// OpenExisting öffnet ein vorhandenes Blob-Verzeichnis nur zum Lesen und
// Prüfen: Anders als Open legt es nichts an (für `doctor --blobs`, der
// nichts ändern darf). Fehlt das Verzeichnis, liefert es ErrNotFound.
func OpenExisting(root string, opts Options) (*Store, error) {
	if opts.MaxSize <= 0 {
		opts.MaxSize = DefaultMaxSize
	}
	fi, err := os.Stat(root)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, ErrNotFound
	case err != nil:
		return nil, fmt.Errorf("blob: open store: %w", err)
	case !fi.IsDir():
		return nil, fmt.Errorf("blob: open store: not a directory")
	}
	return &Store{root: root, opts: opts}, nil
}

// ValidHash meldet, ob sum ein gültiger Blob-Hash ist (64 Kleinbuchstaben-Hex).
func ValidHash(sum string) bool { return validHash(sum) }

// EntryKind ordnet eine Datei im Blob-Verzeichnis ein.
type EntryKind int

const (
	// EntryBlob ist eine Blob-Datei am richtigen Ort (ab/cd/<sha256>[.zst]).
	EntryBlob EntryKind = iota
	// EntryTemp ist eine Temporärdatei unter tmp/ (put-*), etwa nach einem
	// Absturz liegen geblieben; unschädlich, CleanTemp räumt sie ab.
	EntryTemp
	// EntryUnknown ist alles andere: falscher Name, falscher Ort, keine
	// reguläre Datei, fremde Dateien unter tmp/.
	EntryUnknown
)

// Entry ist eine Datei, die Scan gefunden hat. Für EntryBlob sind SHA256
// und Compressed gesetzt; einen Pfad oder Inhalt liefert Scan nie.
type Entry struct {
	Kind       EntryKind
	SHA256     string
	Compressed bool
}

// Scan läuft einmal über das Blob-Verzeichnis und meldet jede Datei (nur
// Namen und Ort, es liest keinen Inhalt und ändert nichts). Die Reihenfolge
// ist die von filepath.WalkDir. Liegen beide Formen eines Blobs vor
// (<sha256> und <sha256>.zst), kommen beide.
func (s *Store) Scan(fn func(Entry)) error {
	tmpDir := filepath.Join(s.root, "tmp")
	return filepath.WalkDir(s.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == s.root {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Dir(p) == tmpDir {
			if d.Type().IsRegular() && strings.HasPrefix(d.Name(), "put-") {
				fn(Entry{Kind: EntryTemp})
			} else {
				fn(Entry{Kind: EntryUnknown})
			}
			return nil
		}
		sum, compressed := strings.CutSuffix(d.Name(), zstSuffix)
		if !d.Type().IsRegular() || !validHash(sum) || filepath.Dir(p) != s.dir(sum) {
			fn(Entry{Kind: EntryUnknown})
			return nil
		}
		fn(Entry{Kind: EntryBlob, SHA256: sum, Compressed: compressed})
		return nil
	})
}
