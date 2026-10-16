// Package secretscan sucht Geheimnisse dort, wo sie nie stehen dürfen
// (Log, Audit, Exporte, Konfiguration), und prüft Dateirechte. Grundlage von
// `doctor --secrets` (S1.11, docs/sicherheit.md, Abschnitt 4).
//
// Gesucht wird nach den echten Werten (Passwörter der Konten aus
// Schlüsselbund bzw. Umgebung) und nach dem Muster von API-Schlüsseln, deren
// Klartext das Programm nie speichert. Die Werte selbst erscheinen in keinem
// Ergebnis: Ein Fund nennt nur, wessen Geheimnis wo steht.
package secretscan

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"runtime"
)

// Status eines Prüfpunkts.
const (
	OK      = "ok"
	Warn    = "warn"
	Fail    = "fail"
	Skipped = "skipped"
)

// Finding ist ein Prüfpunkt.
type Finding struct {
	Check  string `json:"check"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Needle ist ein gesuchtes Geheimnis. Name beschreibt es ohne den Wert
// (z. B. "password of account demo").
type Needle struct {
	Name  string
	Value []byte
}

// MinNeedle ist die kürzeste Länge, nach der gesucht wird: Kürzere Werte
// („pass") stünden zufällig überall.
const MinNeedle = 8

// Hit ist ein Fund: wessen Geheimnis, in welcher Zeile (ab 1).
type Hit struct {
	Name string
	Line int
}

// Scan durchsucht Text zeilenweise nach Werten und Mustern. Zeilen dürfen
// lang sein (bis 64 MiB); längere werden in Stücken gelesen, ein Wert über
// eine Stückgrenze hinweg kann dann entgehen.
func Scan(r io.Reader, needles []Needle, patterns map[string]*regexp.Regexp) ([]Hit, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	var hits []Hit
	seen := map[string]bool{}
	line := 0
	for {
		b, err := br.ReadSlice('\n')
		if len(b) > 0 || err == nil {
			line++
			for _, n := range needles {
				if len(n.Value) >= MinNeedle && !seen[n.Name] && bytes.Contains(b, n.Value) {
					seen[n.Name] = true
					hits = append(hits, Hit{Name: n.Name, Line: line})
				}
			}
			for name, re := range patterns {
				if !seen[name] && re.Match(b) {
					seen[name] = true
					hits = append(hits, Hit{Name: name, Line: line})
				}
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, bufio.ErrBufferFull):
			line-- // dieselbe Zeile geht weiter
		case errors.Is(err, io.EOF):
			return hits, nil
		default:
			return hits, err
		}
	}
}

// ScanFile durchsucht eine Datei (fehlt sie, gibt es keinen Fund).
func ScanFile(path string, needles []Needle, patterns map[string]*regexp.Regexp) ([]Hit, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return Scan(f, needles, patterns)
}

// Perm prüft, dass Gruppe und andere keine Rechte haben (Unix). Unter
// Windows gelten die Rechte des Profilordners (ACL); dort ist der
// Prüfpunkt "skipped". Eine fehlende Datei ist ok (noch nicht angelegt).
func Perm(check, path string, failWhenOpen bool) Finding {
	if runtime.GOOS == "windows" {
		return Finding{Check: check, Status: Skipped, Detail: "Windows: the access rights of the user profile apply"}
	}
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Finding{Check: check, Status: OK, Detail: "not created yet"}
	}
	if err != nil {
		return Finding{Check: check, Status: Warn, Detail: err.Error()}
	}
	mode := fi.Mode().Perm()
	if mode&0o077 == 0 {
		return Finding{Check: check, Status: OK, Detail: fmt.Sprintf("%04o", mode)}
	}
	st := Warn
	if failWhenOpen {
		st = Fail
	}
	want := "0600"
	if fi.IsDir() {
		want = "0700"
	}
	return Finding{Check: check, Status: st, Detail: fmt.Sprintf("%04o, readable by others; run: chmod %s %s", mode, want[1:], path)}
}
