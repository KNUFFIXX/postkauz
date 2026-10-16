// Package search enthält die Volltextsuche ohne Datenbankzugriff: den
// app-seitigen Normalisierer, den deutschen Stemmer, den Aufbau der
// Index-Spalten und die Abfragesprache (Parser, Übersetzung in
// FTS5-Ausdrücke und Filter, Cursor). Die SQL-Ausführung liegt im Store
// (internal/store), der Index-Job in internal/jobs.
//
// Grundsatz (docs/datenmodell.md, „Volltextsuche"): Index und Anfrage
// laufen durch **dieselben** Funktionen. Was hier für den Index gefaltet
// wird, wird für die Anfrage genauso gefaltet; sonst finden sich
// „prüfen" und „pruefen" nicht gegenseitig.
package search

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// FTSVersion ist der Stand von Normalisierer, Stemmer und Spaltenaufbau.
// Ändert sich einer davon, wird die Zahl erhöht; der Index-Job baut den
// Index dann neu auf (meta.fts_version).
//
//	1  S1.7
//	2  Trigram-Index ohne die kontextabhängige Regel ue→u (FoldTrigram)
const FTSVersion = 2

// Fold normalisiert Text für Index und Anfrage:
//
//  1. Kompatibilitätszerlegung (NFKD): Ligaturen, Vollbreitenzeichen und
//     zusammengesetzte Zeichen werden zerlegt.
//  2. Diakritische Zeichen entfallen (ä→a, é→e, ë→e), Kleinschreibung.
//  3. ß (auch ẞ) → ss.
//  4. Deutsche Umschreibungen wie im Snowball-Verfahren „german2":
//     ae→a, oe→o, ue→u; „ue" bleibt nach q (Quelle) und wenn das u
//     zwischen zwei Vokalen steht (Steuer, Bauer, neue).
//
// So ergeben „prüfen", „pruefen" und „prufen" dasselbe Wort („prufen"),
// ebenso „Erdgeschoß" und „Erdgeschoss". Steuerzeichen werden zu
// Leerzeichen. Die Funktion ist deterministisch und zustandslos.
func Fold(s string) string { return fold(s, true, false) }

// FoldTrigram faltet für den Trigram-Index (Teilwortsuche) wie Fold, aber
// ohne die Regel ue→u: Sie hängt vom Zusammenhang ab (Steuer, Quelle
// behalten „ue"), ein Teilwort wie „uer" oder „uelle" sähe ohne diesen
// Zusammenhang anders aus als im Index und fände nichts. Damit „prüf" und
// „pruef" sich trotzdem gegenseitig finden, wird ü hier zu „ue" (statt zu
// u). ä/ö/ß wie bei Fold (ae→a und oe→o gelten ohne Ausnahme).
func FoldTrigram(s string) string { return fold(s, false, true) }

// fold: ueRule wendet ue→u an (Fold), expandU macht ü zu „ue"
// (FoldTrigram); ohne beides bleibt ue und ü wird u (nur für Längen).
func fold(s string, ueRule, expandU bool) string {
	if !isASCII(s) {
		s = norm.NFKD.String(s)
	}
	var b strings.Builder
	b.Grow(len(s))
	// p1, p2: die beiden zuletzt gelesenen (gefalteten) Zeichen der Eingabe.
	var p1, p2 rune
	for _, r := range s {
		if expandU && r == '\u0308' && p1 == 'u' {
			b.WriteByte('e') // ü → ue
			p2, p1 = p1, 'e'
			continue
		}
		if unicode.Is(unicode.Mn, r) {
			continue // diakritisches Zeichen nach der Zerlegung
		}
		r = unicode.ToLower(r)
		switch {
		case r == 'ß':
			b.WriteString("ss")
			p2, p1 = 's', 's'
			continue
		case unicode.IsControl(r) || r == utf8.RuneError:
			r = ' '
		case r == 'e' && (p1 == 'a' || p1 == 'o'):
			p2, p1 = p1, r
			continue // ae → a, oe → o
		case r == 'e' && p1 == 'u' && ueRule && p2 != 'q' && !isVowel(p2):
			p2, p1 = p1, r
			continue // ue → u (nicht nach q, nicht u zwischen Vokalen)
		}
		b.WriteRune(r)
		p2, p1 = p1, r
	}
	return b.String()
}

// FoldText faltet wie Fold und fasst Leerraum zu je einem Leerzeichen
// zusammen (ohne Rand): Ein Teilwort aus mehreren Wörtern („Erdgeschoss
// links") findet den Text unabhängig von Zeilenumbrüchen und doppelten
// Leerzeichen.
func FoldText(s string) string { return collapseSpace(Fold(s)) }

// FoldTrigramText faltet wie FoldTrigram und fasst Leerraum zusammen wie
// FoldText. Grundlage des Trigram-Index und der Teilwortsuche.
func FoldTrigramText(s string) string { return collapseSpace(FoldTrigram(s)) }

// trigramLen ist die Länge eines Suchbegriffs für die Grenze der
// Teilwortsuche (3 Zeichen): wie getippt, ohne Umschreibung von ue und ü.
func trigramLen(s string) int { return runeLen(collapseSpace(fold(s, false, false))) }

func collapseSpace(f string) string {
	var b strings.Builder
	b.Grow(len(f))
	space := false
	for _, r := range f {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Tokens zerlegt gefalteten Text in Wörter: Folgen von Buchstaben und
// Ziffern (wie der FTS5-Tokenizer unicode61; Satzzeichen, Bindestriche,
// @ und Punkte trennen).
func Tokens(folded string) []string {
	return strings.FieldsFunc(folded, func(r rune) bool { return !isTokenRune(r) })
}

func isTokenRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }

func isVowel(r rune) bool {
	switch r {
	case 'a', 'e', 'i', 'o', 'u', 'y':
		return true
	}
	return false
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// runeLen zählt Zeichen.
func runeLen(s string) int { return utf8.RuneCountInString(s) }
