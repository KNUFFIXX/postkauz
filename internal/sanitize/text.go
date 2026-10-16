package sanitize

import (
	"regexp"
	"strings"
	"unicode"
)

// Invisible meldet Zeichen, die nichts anzeigen, aber Text verändern,
// verschleiern oder versteckte Botschaften tragen können: alle
// Formatzeichen der Unicode-Kategorie Cf (Nullbreite, Bidi-Steuerung wie
// U+202E, weiches Trennzeichen, Wortverbinder, BOM, Tag-Zeichen
// U+E0000–E007F, Interlinear-Anker U+FFF9–FFFB), dazu Variantenwähler
// (U+FE00–FE0F, U+E0100–E01EF; Kategorie Mn, mit ihnen lassen sich Bytes
// unsichtbar an ein Zeichen hängen) und das Graphem-Verbindungszeichen
// U+034F. Zentrale Funktion für Sync, CLI-Textausgabe und MCP-Ausgabe.
// Preis: Emoji-Sequenzen verlieren Verbinder (U+200D) und Variantenwähler
// (eine Familien-Emoji wird zu Einzelpersonen); der Inhalt bleibt lesbar.
func Invisible(r rune) bool {
	switch {
	case r < 0x00AD:
		return false
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	case r >= 0xE0000 && r <= 0xE007F, r >= 0xFFF9 && r <= 0xFFFB:
		return true
	case r == 0x034F:
		return true
	}
	return unicode.Is(unicode.Cf, r)
}

// Text entfernt unsichtbare Zeichen (siehe Invisible); Zeilen und
// sichtbarer Inhalt bleiben, wie sie sind.
func Text(s string) string {
	if !strings.ContainsFunc(s, Invisible) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if Invisible(r) {
			return -1
		}
		return r
	}, s)
}

var (
	// Einleitung eines Zitats: „Am 3. März 2026 schrieb Anna <…>:",
	// „On Tue, … Anna wrote:", „Anna schrieb am …:", „Le … a écrit :".
	attributionRe = regexp.MustCompile(`(?i)^(?:am |on |le |el |il |op ).{0,300}(?:schrieb|wrote|a écrit|escribió|ha scritto|schreef)\s*.{0,200}:\s*$|^.{1,200}\s(?:schrieb am|wrote on)\s.{0,200}:\s*$`)
	// Trennlinie vor einer Originalnachricht (Outlook, Thunderbird u. a.).
	originalRe = regexp.MustCompile(`(?i)^\s*(?:-{2,}\s*(?:original message|ursprüngliche nachricht|originalnachricht|message d'origine)\s*-{2,}|_{20,})\s*$`)
	// Kopfblock einer zitierten Nachricht (Outlook ohne Trennlinie).
	fromRe    = regexp.MustCompile(`(?i)^\s*\*?(?:von|from|de|da)\s*:\*?\s+\S`)
	headerRes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*\*?(?:gesendet|sent|date|datum|envoyé|inviato)\s*:\*?\s`),
		regexp.MustCompile(`(?i)^\s*\*?(?:an|to|à|a)\s*:\*?\s`),
		regexp.MustCompile(`(?i)^\s*\*?(?:betreff|subject|objet|oggetto)\s*:\*?\s`),
	}
	// Weiterleitungen sind kein Zitat: Der weitergeleitete Inhalt ist der
	// eigentliche Inhalt und bleibt.
	forwardRe = regexp.MustCompile(`(?i)^\s*-{2,}\s*(?:forwarded message|weitergeleitete nachricht|message transféré)\s*-{2,}\s*$`)
)

// StripQuotes liefert den neuen Text einer Antwort ohne zitierte Teile:
// Zeilen mit „>", die Einleitung davor („Am … schrieb …:"), und alles ab
// einer Originalnachricht im Outlook-Stil („-----Ursprüngliche
// Nachricht-----" oder ein Kopfblock Von:/Gesendet:/An:/Betreff:).
// Weitergeleitete Nachrichten bleiben. Bliebe nichts übrig (nur Zitat),
// kommt der Text unverändert zurück.
func StripQuotes(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	cut := len(lines)
	for i, l := range lines {
		if forwardRe.MatchString(l) {
			break // ab hier Weiterleitung: nichts mehr abschneiden
		}
		if originalRe.MatchString(l) || (fromRe.MatchString(l) && headerBlock(lines[i:])) {
			cut = i
			break
		}
	}
	var out []string
	for i := 0; i < cut; i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimLeft(l, " \t"), ">") {
			continue
		}
		if attributionRe.MatchString(strings.TrimSpace(l)) && quoteFollows(lines[i+1:cut]) {
			continue
		}
		// Einleitung über zwei Zeilen (umbrochen): „Am …, Anna" / „<a@…> schrieb:"
		if i+1 < cut && attributionRe.MatchString(strings.TrimSpace(l)+" "+strings.TrimSpace(lines[i+1])) && quoteFollows(lines[i+2:cut]) {
			i++
			continue
		}
		out = append(out, l)
	}
	res := strings.TrimSpace(tidy(strings.Join(out, "\n")))
	if res == "" {
		return strings.TrimSpace(text)
	}
	return res
}

// quoteFollows: Nach Leerzeilen folgt eine Zitatzeile oder nichts mehr.
func quoteFollows(rest []string) bool {
	for _, l := range rest {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		return strings.HasPrefix(t, ">")
	}
	return true
}

// headerBlock: Ab der Von:-Zeile folgen innerhalb von sechs Zeilen
// mindestens zwei weitere typische Kopfzeilen (Gesendet/An/Betreff).
func headerBlock(lines []string) bool {
	found := 0
	for _, l := range lines[1:min(len(lines), 7)] {
		for _, re := range headerRes {
			if re.MatchString(l) {
				found++
				break
			}
		}
	}
	return found >= 2
}
