//go:build linux

package backup

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mountInfo ist ein Eintrag aus /proc/self/mountinfo: Gerät
// (major:minor), Wurzel des Eintrags innerhalb des Dateisystems und Ort im
// Verzeichnisbaum.
type mountInfo struct {
	dev, root, point string
}

// finalPath liefert für p einen Pfad, der vom Weg dorthin unabhängig ist
// (T3-Nachbesserung 2, Punkt 11): "/<major:minor>/<Pfad im Dateisystem>".
// Ein Bind-Mount eines Unterordners des Datenverzeichnisses an einen
// anderen Ort ergibt so denselben Pfad wie der Unterordner selbst; weder
// EvalSymlinks noch Lstat sehen diesen Umweg. Grundlage ist der
// Mount-Eintrag mit dem längsten passenden Ort (bei gleichem Ort der
// spätere, er liegt oben). false, wenn /proc nicht lesbar ist.
func finalPath(p string) (string, bool) {
	real := realPath(p)
	mounts, err := readMountInfo()
	if err != nil || len(mounts) == 0 {
		return "", false
	}
	best := -1
	for i, m := range mounts {
		if within(real, m.point) && (best < 0 || len(m.point) >= len(mounts[best].point)) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	m := mounts[best]
	rel, err := filepath.Rel(m.point, real)
	if err != nil {
		return "", false
	}
	return filepath.Join("/", m.dev, m.root, rel), true
}

// readMountInfo liest /proc/self/mountinfo (Felder 3 bis 5: Gerät, Wurzel,
// Ort; Leerzeichen und Sonderzeichen darin sind oktal maskiert).
func readMountInfo() ([]mountInfo, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []mountInfo
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 5 {
			continue
		}
		root, point := unescapeMount(fields[3]), unescapeMount(fields[4])
		if !filepath.IsAbs(root) || !filepath.IsAbs(point) {
			continue
		}
		out = append(out, mountInfo{dev: fields[2], root: filepath.Clean(root), point: filepath.Clean(point)})
	}
	return out, sc.Err()
}

// unescapeMount löst die oktalen Masken (\040 für ein Leerzeichen usw.)
// der Felder in /proc/self/mountinfo auf.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
