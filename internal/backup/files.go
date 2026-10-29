package backup

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Rechte im Sicherungsordner und im wiederhergestellten Datenverzeichnis
// (docs/sicherheit.md, Abschnitt 10).
const (
	dirMode  fs.FileMode = 0o700
	fileMode fs.FileMode = 0o600
)

// PathHash ist der Ausweis eines Pfads im Audit: die ersten 16 Hex des
// SHA-256 des absoluten, bereinigten Pfads. So lässt sich ein Ziel
// wiedererkennen, ohne dass Ordnernamen (oft mit Benutzernamen) im Audit
// stehen.
func PathHash(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	sum := sha256.Sum256([]byte(filepath.Clean(p)))
	return hex.EncodeToString(sum[:8])
}

// realPath löst Symlinks auf, soweit der Pfad existiert; der Rest wird
// angehängt (ein Ziel, das es noch nicht gibt, liegt dort, wohin sein
// vorhandener Elternordner wirklich zeigt).
func realPath(p string) string {
	p = filepath.Clean(p)
	rest := ""
	for {
		if r, err := filepath.EvalSymlinks(p); err == nil {
			return filepath.Join(r, rest)
		}
		parent := filepath.Dir(p)
		if parent == p {
			return filepath.Join(p, rest)
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// insideByID meldet, ob child oder einer seiner vorhandenen Vorfahren
// dieselbe Datei ist wie dir (os.SameFile: Laufwerk und Dateinummer). Das
// ergänzt realPath dort, wo EvalSymlinks einen Umweg nicht auflöst: Unter
// Windows löst es seit Go 1.23 Junctions (Mount Points, mklink /J) nicht
// mehr auf; unter Linux sieht es keine Bind-Mounts. os.Stat folgt beiden.
func insideByID(child, dir string) bool {
	want, err := os.Stat(dir)
	if err != nil {
		return false
	}
	p := filepath.Clean(child)
	for {
		if fi, err := os.Stat(p); err == nil && os.SameFile(fi, want) {
			return true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return false
		}
		p = parent
	}
}

// within meldet, ob child gleich parent ist oder darin liegt (beide
// absolut; unter Windows ohne Unterschied der Groß- und Kleinschreibung,
// wie filepath.Rel).
func within(child, parent string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// samePath vergleicht zwei absolute Pfade (Windows: ohne Groß/klein).
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// hashFile liest eine Datei und liefert SHA-256 und Größe.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// randHex liefert n zufällige Bytes als Hex.
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// copyAtomic kopiert src nach dst über eine Temporärdatei im Zielordner
// (fsync, rename; Rechte 0600) und liefert SHA-256 und Größe der Kopie.
// mismatch (optional) prüft SHA-256 und Größe vor dem rename; meldet es
// einen Fehler, bleibt dst unberührt und der Fehler kommt zurück.
func copyAtomic(src, dst string, mismatch func(sum string, n int64) error) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return "", 0, err
	}
	tag, err := randHex(6)
	if err != nil {
		return "", 0, err
	}
	tmpName := filepath.Join(filepath.Dir(dst), ".tmp-"+tag)
	tmp, err := os.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return "", 0, err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), in)
	if err != nil {
		return "", 0, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if mismatch != nil {
		if err := mismatch(sum, n); err != nil {
			return "", 0, err
		}
	}
	if err := tmp.Sync(); err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return "", 0, err
	}
	done = true
	return sum, n, nil
}

// writeFileAtomic schreibt b über eine Temporärdatei nach path (0600).
func writeFileAtomic(path string, b []byte) error {
	tag, err := randHex(6)
	if err != nil {
		return err
	}
	tmpName := filepath.Join(filepath.Dir(path), ".tmp-"+tag)
	f, err := os.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// regularFile prüft, dass path eine gewöhnliche Datei ist (kein Symlink,
// kein Gerät), und liefert ihre Größe.
func regularFile(path string) (int64, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	if !fi.Mode().IsRegular() {
		return 0, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	return fi.Size(), nil
}

// isNotExist: Datei fehlt.
func isNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
