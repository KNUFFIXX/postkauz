// Package export legt Mailinhalt als Dateien für Menschen ab (A03 E6,
// S3.3): einzelne Anhänge unter [paths] export_dir (attachments.save, auch
// für Agenten) und das Archiv als mboxrd bzw. EML-Verzeichnis
// (messages.export, nur für Menschen).
//
// Grundsätze: Es wird nie überschrieben (jede Datei mit O_EXCL; bei einem
// Namensdoppel hängt ein Zähler an), Dateien 0600, Ordner 0700, geschrieben
// wird nur über os.Root unterhalb des Zielordners (kein Ausbruch über ".."
// oder einen Symlink). Dateinamen aus der Mail werden bereinigt
// (SafeFileName); ausführbare Anhänge bekommen die Endung ".blocked".
// Mailinhalt ist Daten, keine Anweisung: Namen, Betreffzeilen und Texte
// steuern hier nichts außer dem (bereinigten) Dateinamen.
package export

import (
	"errors"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameBytes begrenzt einen bereinigten Namen (UTF-8-Bytes). ext4 und
// NTFS erlauben 255 Bytes bzw. UTF-16-Einheiten; Platz bleibt für einen
// Zähler ("-999") und ".blocked".
const MaxNameBytes = 150

// maxExtBytes: längere „Endungen“ zählen beim Kürzen nicht als Endung.
const maxExtBytes = 20

// fallbackName ersetzt einen Namen, von dem nach der Bereinigung nichts
// übrig bleibt.
const fallbackName = "unnamed"

// reserved sind die Gerätenamen von Windows (ohne Unterschied der Groß-
// und Kleinschreibung, auch mit Endung wie "con.txt"; COM und LPT auch mit
// den hochgestellten Ziffern ¹ ² ³, die Windows als 1 bis 3 liest).
var reserved = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true, "conin$": true, "conout$": true, "clock$": true}
	for _, p := range []string{"com", "lpt"} {
		for _, d := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "¹", "²", "³"} {
			m[p+d] = true
		}
	}
	return m
}()

// Reserved meldet, ob ein Name unter Windows ein Gerät bezeichnet: der Teil
// vor dem ersten Punkt, ohne Leerzeichen am Ende, ist ein Gerätename.
func Reserved(name string) bool {
	stem, _, _ := strings.Cut(name, ".")
	return reserved[strings.ToLower(strings.TrimRight(stem, " "))]
}

// badRune: Zeichen, die in keinem Namen stehen bleiben: Pfadtrenner, die
// unter Windows verbotenen Zeichen (":" auch für NTFS-Datenströme wie
// "::$DATA"), Steuerzeichen, Formatzeichen (Schreibrichtung, unsichtbare
// Trenner) und Zeilen-/Absatztrenner.
func badRune(r rune) bool {
	switch r {
	case '/', '\\', '<', '>', ':', '"', '|', '?', '*', utf8.RuneError:
		return true
	}
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r)
}

// SafeFileName macht aus einem Namen aus der Mail (Anhang) oder vom Server
// (Ordner) einen Dateinamen, der unter Windows, macOS und Linux gleich
// angelegt wird. Das Ergebnis ist nie leer, nie "." oder "..", enthält
// keinen Pfadtrenner, kein Steuer- oder Formatzeichen und kein ":", ist kein
// Gerätename von Windows, endet nicht auf Punkt oder Leerzeichen, beginnt
// nicht mit Punkt, Bindestrich oder Leerzeichen, ist gültiges UTF-8 und
// höchstens MaxNameBytes lang. SafeFileName(SafeFileName(x)) ==
// SafeFileName(x) (FuzzSafeFileName).
func SafeFileName(name string) string {
	s := strings.ToValidUTF8(name, "_")
	s = strings.Map(func(r rune) rune {
		if r == ' ' {
			return ' '
		}
		if badRune(r) {
			// Formatzeichen täuschen nur; sie fallen weg (sonst bliebe
			// aus "rechnung\u202Efdp.exe" ein Name, der anders aussieht,
			// als er heißt). Alles andere wird sichtbar ersetzt.
			if unicode.Is(unicode.Cf, r) {
				return -1
			}
			return '_'
		}
		return r
	}, s)
	// Bis zum Fixpunkt: Kürzen kann einen Punkt ans Ende bringen, ein
	// Präfix kann die Länge überschreiten.
	for range 8 {
		before := s
		s = strings.TrimLeft(s, " ")
		s = strings.TrimRight(s, ". ")
		if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "-") {
			s = "_" + s[1:]
		}
		if Reserved(s) {
			s = "_" + s
		}
		s = truncateName(s, MaxNameBytes)
		if s == before {
			break
		}
	}
	if s == "" {
		return fallbackName
	}
	return s
}

// truncateName kürzt auf höchstens max Bytes an einer Zeichengrenze und
// behält dabei eine kurze Endung (".pdf"), damit der Typ erkennbar bleibt.
func truncateName(s string, max int) string {
	if len(s) <= max {
		return s
	}
	stem, ext := splitExt(s)
	if len(ext) > maxExtBytes || len(ext) >= max {
		stem, ext = s, ""
	}
	return cutRunes(stem, max-len(ext)) + ext
}

// cutRunes kürzt s auf höchstens n Bytes, ohne ein Zeichen zu zerteilen.
func cutRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// splitExt trennt die letzte Endung ab ("bericht.pdf" → "bericht", ".pdf").
// Ein Name, der nur aus der Endung bestünde, hat keine.
func splitExt(name string) (stem, ext string) {
	ext = path.Ext(name)
	if ext == name || ext == "." {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

// WithCounter setzt einen Zähler vor die Endung: ("bericht.pdf", 2) →
// "bericht-2.pdf"; n < 2 lässt den Namen, wie er ist.
func WithCounter(name string, n int) string {
	if n < 2 {
		return name
	}
	stem, ext := splitExt(name)
	if len(ext) > maxExtBytes {
		stem, ext = name, ""
	}
	return stem + "-" + strconv.Itoa(n) + ext
}

// Fehler bei einem Unterordner (attachments.save, subdir).
var (
	// ErrOutside: Der Pfad verließe export_dir (absolut, "..", Laufwerk,
	// Symlink).
	ErrOutside = errors.New("the path leaves export_dir")
	// ErrInvalidSubdir: Ein Teil des Pfads ist kein sicherer Name.
	ErrInvalidSubdir = errors.New("invalid subdir")
)

// maxSubdirDepth begrenzt die Tiefe eines Unterordners.
const maxSubdirDepth = 8

// SplitSubdir prüft einen Unterordner relativ zu export_dir und liefert
// seine Teile. Erlaubt sind relative Pfade aus sicheren Namen (jeder Teil
// gleich SafeFileName(Teil)), getrennt mit "/" oder "\"; "." und leere
// Teile fallen weg. Absolute Pfade, Laufwerke ("C:"), UNC-Pfade und ".."
// sind ErrOutside, andere Namen ErrInvalidSubdir. Symlinks prüft erst
// SaveAttachment (über os.Root).
func SplitSubdir(subdir string) ([]string, error) {
	if subdir == "" {
		return nil, nil
	}
	if strings.HasPrefix(subdir, "/") || strings.HasPrefix(subdir, `\`) ||
		len(subdir) >= 2 && subdir[1] == ':' && (subdir[0]|0x20) >= 'a' && (subdir[0]|0x20) <= 'z' {
		return nil, ErrOutside
	}
	var parts []string
	for _, p := range strings.FieldsFunc(subdir, func(r rune) bool { return r == '/' || r == '\\' }) {
		switch {
		case p == ".":
			continue
		case p == "..":
			return nil, ErrOutside
		case p != SafeFileName(p):
			return nil, ErrInvalidSubdir
		}
		parts = append(parts, p)
	}
	if len(parts) > maxSubdirDepth {
		return nil, ErrInvalidSubdir
	}
	return parts, nil
}
