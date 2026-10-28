package export

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/KNUFFIXX/postkauz/internal/magic"
)

// BlockedSuffix hängt an einen ausführbaren Anhang (Programm, Skript,
// Verknüpfung): Ein Doppelklick öffnet ihn so nicht.
const BlockedSuffix = ".blocked"

// maxCounter begrenzt die Versuche bei Namensdoppeln.
const maxCounter = 999

// ErrNameTaken: Alle Namen mit Zähler bis maxCounter sind vergeben.
var ErrNameTaken = errors.New("no free file name (counter exhausted)")

// ErrDamaged: Der gelesene Inhalt passt nicht zu seinem SHA-256 (Blob
// beschädigt); die Datei wird nicht abgelegt.
var ErrDamaged = errors.New("stored content does not match its sha256")

// SaveRequest beschreibt einen Anhang, der unter export_dir abgelegt wird.
type SaveRequest struct {
	// ExportDir ist [paths] export_dir (absolut); er wird bei Bedarf mit
	// 0700 angelegt.
	ExportDir string
	// Subdir ist der Unterordner relativ zu ExportDir (SplitSubdir).
	Subdir string
	// Filename ist der Name aus der Mail (unbereinigt; leer = Fallback).
	Filename string
	// Fallback ersetzt einen leeren Dateinamen (z. B. "attachment-12.pdf").
	Fallback string
	// DeclaredType ist der Content-Type aus der Mail; Executable das
	// gespeicherte Ergebnis der Prüfung beim Spiegeln.
	DeclaredType string
	Executable   bool
	// SHA256 des Inhalts laut Datenbank; ein gültiger Hash wird geprüft.
	SHA256 string
	// Open öffnet den Inhalt (Blob-Store); er wird zweimal gelesen: einmal
	// für Typ und Prüfsumme, dann zum Schreiben.
	Open func() (io.ReadCloser, error)
	// DryRun: nur prüfen und den Namen bestimmen, nichts anlegen.
	DryRun bool
}

// SaveResult ist das Ergebnis von SaveAttachment.
type SaveResult struct {
	// Path ist der Pfad relativ zu ExportDir, mit "/" getrennt.
	Path string
	// Name ist der Dateiname (bereinigt, mit Zähler und ".blocked").
	Name    string
	Size    int64
	SHA256  string
	Blocked bool
}

// SaveAttachment legt einen Anhang unter ExportDir/Subdir ab: Name
// bereinigt, ausführbare Typen mit ".blocked", nie überschreibend (O_EXCL,
// bei einem Doppel "name-2.pdf", "-3", …), Datei 0600, Ordner 0700. Der
// Unterordner wird über os.Root geöffnet: ".." und Symlinks führen nie aus
// ExportDir hinaus (ErrOutside).
func SaveAttachment(req SaveRequest) (SaveResult, error) {
	parts, err := SplitSubdir(req.Subdir)
	if err != nil {
		return SaveResult{}, err
	}
	// Erster Durchgang: Inhalt prüfen (Typ an den Magic Bytes, SHA-256).
	sum, size, detected, err := sniff(req.Open)
	if err != nil {
		return SaveResult{}, err
	}
	if validSHA256(req.SHA256) && sum != req.SHA256 {
		return SaveResult{}, ErrDamaged
	}
	raw := req.Filename
	if strings.TrimSpace(raw) == "" {
		raw = req.Fallback
	}
	name := SafeFileName(raw)
	// Ausführbar ist, was beim Spiegeln so erkannt wurde, was Inhalt,
	// Content-Type oder Endung des Originalnamens sagen, und was die
	// Endung des bereinigten Namens sagt (aus "x.exe\u202E" wird "x.exe").
	blocked := req.Executable || magic.Check(req.DeclaredType, req.Filename, detected).Executable || magic.ExecutableName(name)
	res := SaveResult{Size: size, SHA256: sum, Blocked: blocked}

	rel := filepath.Join(parts...)
	root, err := openExportRoot(req.ExportDir, req.DryRun)
	if err != nil {
		return SaveResult{}, err
	}
	if root != nil {
		defer root.Close()
		if err := checkSubdir(root, parts, req.DryRun); err != nil {
			return SaveResult{}, err
		}
	}
	for n := 1; n <= maxCounter; n++ {
		cand := WithCounter(name, n)
		if blocked {
			cand += BlockedSuffix
		}
		target := cand
		if rel != "" {
			target = filepath.Join(rel, cand)
		}
		res.Name, res.Path = cand, filepath.ToSlash(target)
		if req.DryRun {
			if root == nil {
				return res, nil
			}
			if _, err := root.Lstat(target); errors.Is(err, fs.ErrNotExist) {
				return res, nil
			} else if err != nil {
				return SaveResult{}, rootErr(err)
			}
			continue
		}
		f, err := root.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return SaveResult{}, rootErr(err)
		}
		if err := writeContent(f, req.Open, sum, size); err != nil {
			_ = root.Remove(target)
			return SaveResult{}, err
		}
		return res, nil
	}
	return SaveResult{}, ErrNameTaken
}

// openExportRoot legt export_dir an (0700) und öffnet ihn als os.Root. Im
// Trockenlauf wird nichts angelegt; fehlt der Ordner, ist root nil (alle
// Namen sind frei).
func openExportRoot(dir string, dryRun bool) (*os.Root, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("export: export_dir must be an absolute path")
	}
	if dryRun {
		if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
	} else if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("export: create export_dir: %w", err)
	}
	return os.OpenRoot(dir)
}

// checkSubdir geht den Unterordner Teil für Teil durch: Ein Symlink ist
// ErrOutside (auch einer, der innerhalb bliebe: einfacher und strenger),
// eine Datei an seiner Stelle ErrInvalidSubdir. Fehlende Teile legt es an
// (außer im Trockenlauf), über os.Root und damit nie außerhalb.
func checkSubdir(root *os.Root, parts []string, dryRun bool) error {
	cur := ""
	for _, p := range parts {
		cur = filepath.Join(cur, p)
		fi, err := root.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			if dryRun {
				return nil
			}
			if err := root.Mkdir(cur, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
				return rootErr(err)
			}
			continue
		case err != nil:
			return rootErr(err)
		case fi.Mode()&fs.ModeSymlink != 0:
			return ErrOutside
		case !fi.IsDir():
			return ErrInvalidSubdir
		}
	}
	return nil
}

// rootErr übersetzt die Ablehnung von os.Root (Pfad verließe die Wurzel)
// in ErrOutside; andere Fehler bleiben.
func rootErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "escapes") {
		return fmt.Errorf("%w: %v", ErrOutside, err)
	}
	return err
}

// sniff liest den Inhalt einmal ganz: SHA-256, Größe, erkannter Typ.
func sniff(open func() (io.ReadCloser, error)) (sum string, size int64, t magic.Type, err error) {
	rc, err := open()
	if err != nil {
		return "", 0, t, err
	}
	defer rc.Close()
	h := sha256.New()
	var sn magic.Sniffer
	n, err := io.Copy(io.MultiWriter(h, &sn), rc)
	if err != nil {
		return "", 0, t, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, sn.Detect(), nil
}

// writeContent schreibt den Inhalt in f (zweiter Durchgang) und prüft, dass
// es derselbe ist wie im ersten.
func writeContent(f *os.File, open func() (io.ReadCloser, error), sum string, size int64) error {
	rc, err := open()
	if err != nil {
		_ = f.Close()
		return err
	}
	defer rc.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), rc)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n != size || hex.EncodeToString(h.Sum(nil)) != sum {
		return ErrDamaged
	}
	return nil
}

// validSHA256: 64 Hex-Zeichen in Kleinschrift (so führt der Blob-Store
// seine Schlüssel).
func validSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
