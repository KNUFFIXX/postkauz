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

// reservedKeywords sind System-Keywords von IMAP-Servern und Mailclients
// (V16 der Nachbesserung T2, Festlegung T3), klein geschrieben: Spam-
// Kennzeichen von Thunderbird, Apple Mail und Gmail (junk, nonjunk,
// notjunk, auch mit '$'), die $-Keywords aus RFC 5788/9051 und der
// IANA-Liste ($forwarded, $mdnsent, $phishing, $notphishing, $junk,
// $notjunk, $submitpending, $submitted, $important) und Thunderbirds
// Etiketten $label1 bis $label5. Ein Tag mit so einem Namen schriebe am
// Server ein Kennzeichen, das Client oder Server als Spam-Urteil, Versand-
// oder Lesebestätigung deuten. Gültige Tag-Namen enthalten kein '$'; die
// Formen mit '$' stehen hier, damit die Liste vollständig bleibt, falls
// sich die Namensregel einmal ändert.
var reservedKeywords = map[string]bool{
	"junk": true, "nonjunk": true, "notjunk": true,
	"$junk": true, "$nonjunk": true, "$notjunk": true,
	"$forwarded": true, "$mdnsent": true, "$phishing": true, "$notphishing": true,
	"$submitpending": true, "$submitted": true, "$important": true,
	"$label1": true, "$label2": true, "$label3": true, "$label4": true, "$label5": true,
}

// Reserved meldet, ob ein Tag-Name einem System-Keyword entspricht (ohne
// Rücksicht auf Groß- und Kleinschreibung, wie IMAP Keywords vergleicht).
// Solche Tags bleiben lokal: [tags] sync_keywords schreibt sie nie auf den
// Server (V16).
func Reserved(name string) bool { return reservedKeywords[strings.ToLower(name)] }

// SplitReserved teilt Tag-Namen in die, die als Keyword auf den Server
// dürfen, und die reservierten (bleiben lokal).
func SplitReserved(names []string) (sync, local []string) {
	for _, n := range names {
		if Reserved(n) {
			local = append(local, n)
		} else {
			sync = append(sync, n)
		}
	}
	return sync, local
}
