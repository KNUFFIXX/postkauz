package sanitize

import (
	"regexp"
	"strings"
	"unicode"
)

// Grenzen für AlternativeMismatch (Wortzahl, verschiedene Wörter).
const (
	// altPlainExtra: so viele Wörter des Klartexts dürfen im HTML fehlen.
	// Ein aus dem HTML erzeugter Klartext hat praktisch keine eigenen
	// Wörter; ein eingeschobener Satz hat mehr.
	altPlainExtra = 5
	// altHTMLExtra und altHTMLShare: Das HTML darf mehr zeigen als der
	// Klartext (Alternativtexte, Grußformel, Fußzeilen, „im Browser
	// ansehen"), aber nicht viel mehr: mindestens so viele Wörter und mehr
	// als ein Drittel seiner Wörter.
	altHTMLExtra = 8
	altHTMLShare = 3
	// altNumberDigits: Zahlen ab so vielen Ziffern (IBAN, Beträge,
	// Rechnungsnummern, Telefonnummern) müssen, wenn sie im Klartext stehen,
	// auch im HTML stehen.
	altNumberDigits = 4
)

var (
	// URLs und die Bildmarke aus HTML() („[image: …]") gehören nicht zum
	// Inhalt.
	altURLRe = regexp.MustCompile(`(?i)\bhttps?://[^\s<>()\[\]]+|\[image:`)
	// altNumberRe: eine Zahl samt üblicher Gliederung („3704 0044 0532",
	// „1.250,00", „0211/123-45").
	altNumberRe = regexp.MustCompile(`\d(?:[ .,'/-]?\d)*`)
)

// AlternativeMismatch meldet, ob Klartext und HTML-Text derselben
// multipart/alternative-Nachricht wesentlich voneinander abweichen. Der
// Agent liest den Klartext, der Mensch sieht das HTML; weichen sie ab,
// kann der Klartext eine Anweisung tragen, die der Mensch nie sieht (oder
// eine andere Kontonummer). Verglichen werden ohne URLs:
//
//   - Wörter (Buchstabenfolgen, klein geschrieben) als Mengen; Reihenfolge
//     und Formatierung (Umbrüche, Listenzeichen, Hervorhebungen,
//     Linkschreibweise) spielen keine Rolle. Wesentlich: mindestens
//     altPlainExtra Wörter des Klartexts fehlen im HTML, oder das HTML hat
//     mindestens altHTMLExtra Wörter und mehr als ein Drittel, die im
//     Klartext fehlen.
//   - Zahlen: Jede Zahl des Klartexts mit mindestens altNumberDigits
//     Ziffern muss mit denselben Ziffern in derselben Folge im HTML stehen
//     (Gliederung durch Leerzeichen, Punkte, Striche egal).
//
// Ein leerer Klartext ist keine Abweichung (dann liest auch der Agent den
// HTML-Text). Die Markierung ist ein Hinweis, kein Urteil: Newsletter mit
// eigenem Klartext werden ebenfalls markiert.
func AlternativeMismatch(plain, htmlText string) bool {
	plain, htmlText = altURLRe.ReplaceAllString(plain, " "), altURLRe.ReplaceAllString(htmlText, " ")
	p, h := altWords(plain), altWords(htmlText)
	if len(p) == 0 && strings.TrimSpace(plain) == "" {
		return false
	}
	htmlDigits := onlyDigits(htmlText)
	for _, n := range altNumberRe.FindAllString(plain, -1) {
		if d := onlyDigits(n); len(d) >= altNumberDigits && !strings.Contains(htmlDigits, d) {
			return true
		}
	}
	plainExtra, htmlExtra := 0, 0
	for w := range p {
		if _, ok := h[w]; !ok {
			plainExtra++
		}
	}
	if plainExtra >= altPlainExtra {
		return true
	}
	for w := range h {
		if _, ok := p[w]; !ok {
			htmlExtra++
		}
	}
	return htmlExtra >= altHTMLExtra && htmlExtra*altHTMLShare > len(h)
}

// altWords zerlegt Text in die Menge seiner Wörter (Buchstabenfolgen).
func altWords(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) }) {
		out[f] = struct{}{}
	}
	return out
}

func onlyDigits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}
