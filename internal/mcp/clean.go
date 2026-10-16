package mcp

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/KNUFFIXX/postkauz/internal/sanitize"
)

// cleanString entfernt aus einer Zeichenkette alles, was ein Agent oder
// eine Oberfläche als Steuerung lesen könnte: Steuerzeichen (C0, DEL, C1;
// erhalten bleiben Zeilenumbruch und Tabulator, CR fällt weg) und
// unsichtbare Formatzeichen (Nullbreite, Bidi-Steuerung). Ungültiges UTF-8
// wird zu U+FFFD. Teil der Untrusted-Hülle (docs/mcp.md, Abschnitt 5).
func cleanString(s string) string {
	s = strings.ToValidUTF8(s, string(utf8.RuneError))
	s = sanitize.Text(s)
	if !strings.ContainsFunc(s, unwantedControl) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unwantedControl(r) {
			return -1
		}
		return r
	}, s)
}

func unwantedControl(r rune) bool {
	return unicode.IsControl(r) && r != '\n' && r != '\t'
}

// cleanValue wendet cleanString auf alle Zeichenketten eines JSON-Werts an
// (Schlüssel und Werte; aus encoding/json: map[string]any, []any, string …).
func cleanValue(v any) any {
	switch x := v.(type) {
	case string:
		return cleanString(x)
	case []any:
		for i := range x {
			x[i] = cleanValue(x[i])
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[cleanString(k)] = cleanValue(val)
		}
		return out
	}
	return v
}
