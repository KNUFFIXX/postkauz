package ops

import (
	"errors"
	"fmt"
	"strings"
)

// ErrorKinds beschreibt einen Fehler fürs Log ohne seinen Text (T1b): die
// Typen der Kette (errors.Unwrap, bei errors.Join alle Zweige), dazu die
// bekannten Codes (Policy-Grund, fachliche Ablehnung). Texte von Datenbank,
// Server oder Eingabe gelangen so nie ins Log, die Art des Fehlers schon.
func ErrorKinds(err error) string {
	var parts []string
	var walk func(error, int)
	walk = func(e error, depth int) {
		if e == nil || depth > 8 {
			return
		}
		parts = append(parts, fmt.Sprintf("%T", e))
		switch x := e.(type) {
		case interface{ Unwrap() []error }:
			for _, c := range x.Unwrap() {
				walk(c, depth+1)
			}
		case interface{ Unwrap() error }:
			walk(x.Unwrap(), depth+1)
		}
	}
	walk(err, 0)
	var pe *PolicyError
	var de *DeniedError
	switch {
	case errors.As(err, &pe):
		parts = append(parts, "reason="+pe.Decision.Reason)
	case errors.As(err, &de):
		parts = append(parts, "reason="+de.Reason)
	}
	return strings.Join(parts, " > ")
}
