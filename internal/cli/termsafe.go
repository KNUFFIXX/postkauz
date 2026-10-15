package cli

import (
	"strings"
	"unicode"
)

// Mailinhalt und Serverantworten sind Daten, keine Anweisungen – auch nicht
// an das Terminal. Steuerzeichen (C0, DEL, C1; darunter ESC für CSI- und
// OSC-Sequenzen) aus Betreff, Absender, Kopfzeilen, Text oder Ordnernamen
// würden sonst vom Terminal ausgeführt: Bildschirm löschen, Zeilen
// überschreiben, Fenstertitel oder Links setzen, Zwischenablage schreiben.
// Die Textausgabe ersetzt sie deshalb durch U+FFFD; ungültiges UTF-8 ebenso.
// Die JSON-Ausgabe bleibt unverändert (encoding/json maskiert Steuerzeichen).

// termLine macht eine einzeilige Angabe terminalsicher: alle Steuerzeichen,
// auch Zeilenumbrüche (sonst ließen sich Zeilen wie „flags: seen" fälschen),
// werden zu U+FFFD; ein Tabulator wird zum Leerzeichen (er würde sonst die
// Spalten der tabwriter-Ausgabe verschieben).
func termLine(s string) string {
	return termFilter(s, false)
}

// termText macht mehrzeiligen Text terminalsicher: wie termLine, aber
// Zeilenumbruch, Tabulator und CR vor LF (CRLF) bleiben erhalten.
func termText(s string) string {
	return termFilter(s, true)
}

func termFilter(s string, multiline bool) string {
	clean := true
	for _, r := range s {
		if r == unicode.ReplacementChar || unicode.IsControl(r) {
			clean = false
			break
		}
	}
	if clean {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		switch {
		case r == '\t' && multiline:
			b.WriteRune(r)
		case r == '\t':
			b.WriteByte(' ')
		case r == '\n' && multiline:
			b.WriteRune(r)
		case r == '\r' && multiline && i+1 < len(s) && s[i+1] == '\n':
			b.WriteRune(r)
		case unicode.IsControl(r):
			b.WriteRune(unicode.ReplacementChar)
		default:
			// Ungültiges UTF-8 liefert range als RuneError; WriteRune schreibt
			// dafür U+FFFD statt des rohen Bytes (ein rohes 0x9B wäre in
			// 8-Bit-Terminals ein CSI).
			b.WriteRune(r)
		}
	}
	return b.String()
}

func termLines(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = termLine(s)
	}
	return out
}
