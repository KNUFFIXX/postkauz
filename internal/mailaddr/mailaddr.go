// Package mailaddr vergleicht Mailadressen mit den eigenen Adressen eines
// Kontos (Nachbesserung T3, Punkt 15).
//
// Gmail stellt eine Adresse unter vielen Schreibweisen zu: Punkte im
// lokalen Teil zählen nicht, ein Zusatz ab „+“ ebenso wenig, und
// googlemail.com ist dieselbe Domain wie gmail.com. Gmail im Browser setzt
// als Absender die Schreibweise, unter der das Konto angelegt wurde (etwa
// mit Punkten), auch wenn das Konto in PostKauz ohne Punkte eingetragen
// ist. Für den Vergleich mit den eigenen Adressen (Versanderkennung,
// „Allen antworten“, Thread-Rückfall, Dubletten der Aliasse) gilt deshalb
// Key; andere Anbieter vergleicht Key nur ohne Rücksicht auf Groß- und
// Kleinschreibung (dort können Punkte und „+“ verschiedene Postfächer
// sein).
//
// Klein geschrieben wird nur ASCII (zweite Nachbesserung T3, Punkt 9):
// strings.ToLower machte aus dem Kelvin-Zeichen U+212A ein „k“ und aus
// U+0130 ein „i“; eine fremde Adresse bekäme so den Schlüssel einer
// eigenen. Mailserver vergleichen Domains und (bei Gmail) lokale Teile nur
// in ASCII ohne Rücksicht auf die Schreibweise.
package mailaddr

import "strings"

// Key ist der Vergleichsschlüssel einer Adresse (ohne Namen): ASCII klein,
// ohne Leerraum; für gmail.com und googlemail.com ohne Punkte im lokalen Teil,
// ohne Zusatz ab „+“ und mit der Domain gmail.com. Ohne „@“ oder mit
// leerem lokalem Teil nach dem Kürzen bleibt die Adresse nur klein
// geschrieben.
func Key(addr string) string {
	s := Lower(addr)
	i := strings.LastIndexByte(s, '@')
	if i <= 0 {
		return s
	}
	local, domain := s[:i], s[i+1:]
	if domain != "gmail.com" && domain != "googlemail.com" {
		return s
	}
	if j := strings.IndexByte(local, '+'); j >= 0 {
		local = local[:j]
	}
	local = strings.ReplaceAll(local, ".", "")
	if local == "" {
		return s
	}
	return local + "@gmail.com"
}

// Lower ist die Adresse ohne Leerraum am Rand, nur in ASCII klein
// geschrieben (andere Zeichen bleiben, wie sie sind). Zwei Adressen mit
// gleichem Lower sind dieselbe Schreibweise; Key fasst darüber hinaus die
// Gmail-Schreibweisen zusammen.
func Lower(addr string) string {
	s := strings.TrimSpace(addr)
	for i := 0; i < len(s); i++ {
		if c := s[i]; 'A' <= c && c <= 'Z' {
			b := []byte(s)
			for j := i; j < len(b); j++ {
				if 'A' <= b[j] && b[j] <= 'Z' {
					b[j] += 'a' - 'A'
				}
			}
			return string(b)
		}
	}
	return s
}

// Set ist eine Menge von Schlüsseln (Key) eigener Adressen.
type Set map[string]bool

// NewSet bildet die Menge der Schlüssel; leere Adressen fallen weg.
func NewSet(addrs ...string) Set {
	s := Set{}
	for _, a := range addrs {
		if k := Key(a); k != "" {
			s[k] = true
		}
	}
	return s
}

// Has meldet, ob addr (nach Key) zur Menge gehört.
func (s Set) Has(addr string) bool {
	k := Key(addr)
	return k != "" && s[k]
}
