// Package tags enthält die Regeln für lokale Etiketten (A03 E5, Schnitt
// 3): gültige Namen, Normalisierung und die Abbildung auf IMAP-Keywords.
// Die Speicherung liegt im Store (Tabellen tags, message_tags), die
// Operationen in internal/ops (messages.tag, tags.list).
package tags

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// MaxLen ist die größte Länge eines Tag-Namens.
const MaxLen = 64

// MaxPerCall begrenzt die Tags je Aufruf (hinzufügen plus entfernen).
const MaxPerCall = 20

var nameRE = regexp.MustCompile(`^[a-z0-9._-]{1,64}$`)

// Valid meldet, ob name ein gültiger Tag-Name ist: Kleinbuchstaben,
// Ziffern, '.', '_', '-', 1 bis 64 Zeichen (wie der CHECK in Migration
// 0020).
func Valid(name string) bool { return nameRE.MatchString(name) }

// Normalize macht aus einer Eingabe einen Tag-Namen: Leerraum außen weg,
// Großbuchstaben (ASCII) klein. Was danach nicht gültig ist, ist ein
// Fehler; der Fehler nennt den Namen nicht (er könnte aus einer Mail
// stammen), nur die Position.
func Normalize(in []string) ([]string, error) {
	out := make([]string, 0, len(in))
	for i, s := range in {
		n := strings.ToLower(strings.TrimSpace(s))
		if !Valid(n) {
			return nil, fmt.Errorf("tag %d is invalid: use lowercase letters, digits, '.', '_' and '-' (1 to %d characters)", i+1, MaxLen)
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out, nil
}

// Keyword ist der Name des IMAP-Keywords für einen Tag (E5: Keyword =
// Tag-Name). Jeder gültige Tag-Name ist ein gültiges IMAP-Atom (keine
// Sonderzeichen, kein führendes '\').
func Keyword(name string) string { return name }
