package importer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Source ist eine Quelle des Imports: eine mbox-Datei oder ein Maildir-Ordner
// mit seinem Zielordner.
type Source struct {
	Path   string // absolut
	Rel    string // Anzeige (relativ zum angegebenen Pfad)
	Format string // mbox | maildir
	Folder string // Zielordner (import_folders.path)
	Mode   string // store.ImportContent | store.ImportLabels
}

// Formate.
const (
	FormatMbox    = "mbox"
	FormatMaildir = "maildir"
)

// ErrNoSources: Unter dem Pfad liegt nichts Importierbares.
var ErrNoSources = errors.New("no mbox files or maildir folders found")

// Discover findet die Quellen unter path. mailbox ist der Zielordner (eine
// Datei bzw. die Wurzel eines Maildirs) bzw. das Präfix der Ordner darunter
// (leer: Pfade relativ zum Verzeichnis; eine einzelne Datei heißt wie die
// Datei, die Wurzel eines Maildirs INBOX). Thunderbird-Profile
// werden erkannt: Unterordner liegen in "<ordner>.sbd/", Indexdateien
// (.msf) und andere Dateien werden übersprungen (nur Dateien, die mit einer
// mbox-Trennzeile beginnen, zählen), Ordnernamen in IMAP-UTF-7 (wie
// Thunderbird sie für IMAP-Ordner auf die Platte schreibt, z. B.
// "Entw&APw-rfe") werden dekodiert. ignored zählt übersprungene Dateien.
func Discover(path, format, mailbox, mode string) (srcs []Source, ignored int, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, 0, err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return nil, 0, err
	}
	mailbox = strings.Trim(mailbox, "/")
	switch format {
	case FormatMbox:
		if !fi.IsDir() {
			folder := mailbox
			if folder == "" {
				folder = DecodeFolderName(filepath.Base(abs))
			}
			return []Source{{Path: abs, Rel: filepath.Base(abs), Format: format, Folder: folder, Mode: mode}}, 0, nil
		}
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			ok, err := looksLikeMbox(p)
			if err != nil {
				return err
			}
			if !ok {
				if !emptyFile(d) {
					ignored++
				}
				return nil
			}
			rel, _ := filepath.Rel(abs, p)
			srcs = append(srcs, Source{Path: p, Rel: filepath.ToSlash(rel), Format: format, Folder: folderPath(mailbox, rel), Mode: mode})
			return nil
		})
	case FormatMaildir:
		if !fi.IsDir() {
			return nil, 0, fmt.Errorf("%s: a maildir is a folder (with cur, new and tmp)", path)
		}
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			switch d.Name() {
			case "cur", "new", "tmp":
				if p != abs {
					return fs.SkipDir
				}
			}
			if !isMaildir(p) {
				return nil
			}
			rel, _ := filepath.Rel(abs, p)
			folder := mailbox
			if rel != "." {
				folder = folderPath(mailbox, rel)
			} else if folder == "" {
				// Wurzel eines Maildirs ist (wie bei Dovecot, Maildir++)
				// die INBOX; Unterordner ".Archiv" heißen "Archiv".
				folder = "INBOX"
			}
			srcs = append(srcs, Source{Path: p, Rel: filepath.ToSlash(rel), Format: format, Folder: folder, Mode: mode})
			return nil
		})
	default:
		return nil, 0, fmt.Errorf("unknown format %q (mbox or maildir)", format)
	}
	if err != nil {
		return nil, ignored, err
	}
	if len(srcs) == 0 {
		return nil, ignored, fmt.Errorf("%w under %s", ErrNoSources, path)
	}
	slices.SortFunc(srcs, func(a, b Source) int { return strings.Compare(a.Folder+"\x00"+a.Path, b.Folder+"\x00"+b.Path) })
	return srcs, ignored, nil
}

func emptyFile(d fs.DirEntry) bool {
	fi, err := d.Info()
	return err == nil && fi.Size() == 0
}

// looksLikeMbox: Die Datei beginnt mit "From " (leere Dateien sind leere
// Thunderbird-Ordner und zählen nicht).
func looksLikeMbox(p string) (bool, error) {
	f, err := os.Open(p)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, 5)
	n, err := io.ReadFull(f, buf)
	if err != nil && n < 5 {
		return false, nil
	}
	return string(buf) == "From ", nil
}

func isMaildir(p string) bool {
	for _, sub := range []string{"cur", "new"} {
		if fi, err := os.Stat(filepath.Join(p, sub)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// folderPath bildet aus einem relativen Pfad den Ordnerpfad: ".sbd" der
// Thunderbird-Unterordner weg, Maildir++-Namen (".A.B") zu "A/B", Namen
// dekodiert, Trenner "/", mit Präfix.
func folderPath(prefix, rel string) string {
	var parts []string
	for _, c := range strings.Split(filepath.ToSlash(rel), "/") {
		c = strings.TrimSuffix(c, ".sbd")
		if strings.HasPrefix(c, ".") && len(c) > 1 {
			// Maildir++: ".Archiv.2024" → Archiv/2024
			for _, p := range strings.Split(c[1:], ".") {
				if p != "" {
					parts = append(parts, DecodeFolderName(p))
				}
			}
			continue
		}
		if c != "" && c != "." {
			parts = append(parts, DecodeFolderName(c))
		}
	}
	if prefix != "" {
		parts = append([]string{prefix}, parts...)
	}
	return strings.Join(parts, "/")
}

// DecodeFolderName dekodiert einen Ordnernamen in modifiziertem UTF-7
// (RFC 3501, Abschnitt 5.1.3; Thunderbird benennt IMAP-Ordner auf der Platte
// so, z. B. "Entw&APw-rfe" für „Entwürfe"). Ein Name, der kein gültiges
// IMAP-UTF-7 ist, bleibt, wie er ist.
func DecodeFolderName(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '&' {
			if c < 0x20 || c > 0x7e {
				return s
			}
			b.WriteByte(c)
			i++
			continue
		}
		j := strings.IndexByte(s[i+1:], '-')
		if j < 0 {
			return s
		}
		enc := s[i+1 : i+1+j]
		i += j + 2
		if enc == "" {
			b.WriteByte('&')
			continue
		}
		raw, err := base64.RawStdEncoding.DecodeString(strings.ReplaceAll(enc, ",", "/"))
		if err != nil || len(raw)%2 != 0 {
			return s
		}
		units := make([]uint16, len(raw)/2)
		for k := range units {
			units[k] = uint16(raw[2*k])<<8 | uint16(raw[2*k+1])
		}
		for _, r := range utf16.Decode(units) {
			if r == utf8.RuneError || r < 0x80 {
				return s // ASCII gehört nicht in eine Base64-Folge
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// excludeSources entfernt aus labels die Quellen, die schon als Inhalt
// gelesen werden (gleicher Pfad).
func excludeSources(labels, content []Source) []Source {
	seen := map[string]bool{}
	for _, s := range content {
		seen[s.Path] = true
	}
	out := labels[:0]
	for _, s := range labels {
		if !seen[s.Path] {
			s.Mode = store.ImportLabels
			out = append(out, s)
		}
	}
	return out
}
