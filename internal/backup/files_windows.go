//go:build windows

package backup

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// Flags von GetFinalPathNameByHandle: Laufwerksbuchstabe statt
// Volume-GUID, normalisierter Name (beide 0, in x/sys nicht benannt).
const (
	volumeNameDOS      = 0x0
	fileNameNormalized = 0x0
)

// finalPath liefert den endgültigen Pfad von p, wie Windows ihn nach allen
// Junctions, Mount Points und symbolischen Links sieht
// (T3-Nachbesserung 2, Punkt 11): Der tiefste vorhandene Vorfahr wird mit
// FILE_FLAG_BACKUP_SEMANTICS geöffnet (auch Ordner, ohne Leserecht auf den
// Inhalt) und über GetFinalPathNameByHandle aufgelöst, der Rest angehängt.
// Seit Go 1.23 löst filepath.EvalSymlinks Junctions nicht mehr auf; eine
// Junction auf einen Unterordner des Datenverzeichnisses fiele sonst nicht
// auf. false, wenn ein vorhandener Pfad sich nicht öffnen lässt.
func finalPath(p string) (string, bool) {
	p = filepath.Clean(p)
	rest := ""
	for {
		fp, err := finalPathOf(p)
		if err == nil {
			return filepath.Join(fp, rest), true
		}
		if _, lerr := os.Lstat(p); lerr == nil {
			return "", false
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		rest = filepath.Join(filepath.Base(p), rest)
		p = parent
	}
}

// finalPathOf öffnet einen vorhandenen Pfad (Datei oder Ordner) und fragt
// seinen endgültigen Pfad ab, ohne Präfix \\?\ (bei Netzpfaden \\server).
func finalPathOf(p string) (string, error) {
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(name, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), volumeNameDOS|fileNameNormalized)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", errors.New("empty final path")
		}
		if n < uint32(len(buf)) {
			buf = buf[:n]
			break
		}
		// Puffer zu klein: n ist die nötige Länge samt Nullzeichen.
		buf = make([]uint16, n)
	}
	s := windows.UTF16ToString(buf)
	switch {
	case strings.HasPrefix(s, `\\?\UNC\`):
		s = `\\` + s[len(`\\?\UNC\`):]
	case strings.HasPrefix(s, `\\?\`):
		s = s[len(`\\?\`):]
	}
	return filepath.Clean(s), nil
}

// isTransientFSErr meldet Fehler, die unter Windows meist nur kurz
// anhalten, weil ein anderes Programm (Virenscanner, Indexdienst,
// Sicherungsprogramm) eine Datei offen hält: ERROR_ACCESS_DENIED und
// ERROR_SHARING_VIOLATION (T3-Nachbesserung 2, Punkt 13; retryFS).
func isTransientFSErr(err error) bool {
	return errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}
