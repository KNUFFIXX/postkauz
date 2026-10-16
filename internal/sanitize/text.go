package sanitize

import (
	"regexp"
	"strings"
)

// invisible: Zeichen, die nichts anzeigen, aber Text verändern oder
// verschleiern können (Nullbreite, Bidi-Steuerung wie U+202E, weiches
// Trennzeichen, Wortverbinder, BOM).
func invisible(r rune) bool {
	switch {
	case r == 0x00AD, r == 0x034F, r == 0x061C, r == 0x180E, r == 0xFEFF:
		return true
	case r >= 0x200B && r <= 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2060 && r <= 0x2069:
		return true
	}
	return false
}

// Text entfernt unsichtbare Formatzeichen (siehe invisible); Zeilen und
// sichtbarer Inhalt bleiben, wie sie sind.
func Text(s string) string {
	if !strings.ContainsFunc(s, invisible) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if invisible(r) {
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
