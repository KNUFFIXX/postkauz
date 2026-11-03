package sanitize

import "strings"

// HTML-Quelltext im Klartextteil (A05 Teil 3): Manche Absender-Software
// schreibt in den text/plain-Teil HTML-Quelltext (`<html xmlns=…><head>
// <meta …>`). Ohne Erkennung lasen Agenten, Snippet und Suche rohes HTML,
// und alternative_mismatch stand auf true, obwohl der Mensch denselben
// Inhalt sieht. PlainIsHTML ist die gemeinsame Regel für den Sync
// (mailsync, fillClean) und für Zitat und Weiterleitung von Entwürfen
// (compose, originalText); die Store-Abfrage der gezielten Neuaufbereitung
// sucht mit derselben Regel vor (store.plainHTMLCond).

// PlainHTMLSpace sind die Zeichen, die vor dem Anfang übersprungen werden:
// Leerzeichen, Tabulator, Zeilenvorschub, Wagenrücklauf (in SQL
// ltrim(x, char(32,9,10,13))). Ein BOM entfernt Text schon vorher.
const PlainHTMLSpace = " \t\n\r"

// plainHTMLStarts sind die Anfänge, an denen ein Klartextteil als HTML
// gilt (Groß-/Kleinschreibung egal), jeweils gefolgt von einem Ende des
// Namens (Leerraum, „>", „/" oder Textende).
var plainHTMLStarts = []string{"<!doctype html", "<html", "<head", "<body", "<meta"}

// PlainIsHTML meldet, ob ein Klartextteil HTML-Quelltext ist: Nach
// führendem Leerraum beginnt er mit `<!doctype html`, `<html`, `<head`,
// `<body` oder `<meta`, oder mit einem XML-Prolog `<?xml … ?>`, auf den
// (nach Leerraum) `<!doctype html` oder `<html` folgt. Die Regel ist
// bewusst eng: lieber ein Fall zu wenig als ein echter Klartext, der mit
// „<" beginnt („<Name> schrieb:", „<b>" mitten im Text).
func PlainIsHTML(s string) bool {
	s = strings.TrimLeft(s, PlainHTMLSpace)
	if hasFoldPrefix(s, "<?xml") {
		end := strings.Index(s, "?>")
		if end < 0 {
			return false
		}
		rest := strings.TrimLeft(s[end+2:], PlainHTMLSpace)
		return startsTag(rest, "<!doctype html") || startsTag(rest, "<html")
	}
	for _, p := range plainHTMLStarts {
		if startsTag(s, p) {
			return true
		}
	}
	return false
}

// startsTag: s beginnt mit p (Groß-/Kleinschreibung egal), danach endet
// der Name (Leerraum, „>", „/" oder Textende); „<header>" ist also kein
// „<head", „<metadata>" kein „<meta".
func startsTag(s, p string) bool {
	if !hasFoldPrefix(s, p) {
		return false
	}
	if len(s) == len(p) {
		return true
	}
	switch s[len(p)] {
	case ' ', '\t', '\n', '\r', '>', '/':
		return true
	}
	return false
}

func hasFoldPrefix(s, p string) bool {
	return len(s) >= len(p) && strings.EqualFold(s[:len(p)], p)
}
