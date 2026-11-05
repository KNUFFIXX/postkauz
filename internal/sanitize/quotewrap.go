package sanitize

import "strings"

// Grenzen für JoinWrappedQuotes (A06 Teil 3): Ein Mailprogramm bricht
// Zitatzeilen um, wenn sie länger als etwa 72 bis 78 Bytes würden
// (Thunderbird 72, Gmail im Browser nach Bytes, gemessen 76 bis 81 an der
// Umbruchstelle); eine Zeile über WrapMaxLine Bytes ist nie umbrochen.
const (
	WrapMinWidth = 72
	WrapMaxLine  = 80
)

// JoinWrappedQuotes hängt verwaiste Fortsetzungszeilen umbrochener
// Zitatzeilen an die Zeile davor (A06 Teil 3). Befund aus dem Dauerbetrieb:
// Gmail im Browser baut beim Senden den Klartext neu aus dem HTML; innere
// Zitatebenen stehen mit „>“, ihre Fortsetzungszeilen nach dem Umbruch
// aber ohne „>“. StripQuotes entfernt die „>“-Zeilen, die Fortsetzungen
// blieben im zitatfreien Text stehen und störten den Vergleich der
// Versanderkennung.
//
// Regel: Eine Zeile ist Fortsetzung, wenn sie nicht leer ist, nicht mit
// „>“ beginnt (Leerraum davor zählt nicht) und direkt, ohne Leerzeile, auf
// eine Zeile mit „>“ oder auf eine Fortsetzung folgt, die vorige
// physische Zeile (nie die schon verbundene; Länge mit „> “-Präfix, ohne
// CR) höchstens WrapMaxLine Bytes hat und Bytes(vorige) + 1 +
// Bytes(erstes Wort der Zeile) > WrapMinWidth ist: Hätte das Wort noch in
// die vorige Zeile gepasst, war es kein Umbruch. Gezählt wird in Bytes,
// nicht in Zeichen (Gmail bricht nach Bytes um; mit Zeichen gälte eine
// Zeile mit „ß“ an der Kante nicht als umbrochen).
//
// Nur für den Vergleich der Versanderkennung (internal/drafts, sentText);
// Spiegel, Suche, Snippet und mail_get_message bleiben unverändert.
func JoinWrappedQuotes(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	prev := ""       // vorige physische Zeile
	inQuote := false // vorige Zeile ist eine „>“-Zeile oder eine Fortsetzung
	for _, l := range lines {
		l = strings.TrimSuffix(l, "\r")
		t := strings.TrimLeft(l, " \t")
		quote := strings.HasPrefix(t, ">")
		if !quote && inQuote && strings.TrimSpace(l) != "" && len(prev) <= WrapMaxLine &&
			len(prev)+1+len(firstWord(t)) > WrapMinWidth {
			out[len(out)-1] += " " + strings.TrimSpace(l)
			prev = l
			continue
		}
		out = append(out, l)
		prev, inQuote = l, quote
	}
	return strings.Join(out, "\n")
}

// firstWord ist das erste durch Leerraum begrenzte Wort von s.
func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return ""
}
