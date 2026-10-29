package export

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
)

// markFile setzt die Herkunft einer abgelegten Datei (markFromInternet,
// je System eigene Datei); Tests ersetzen es.
var markFile = markFromInternet

// markWarning ist die Warnung, wenn die Herkunft nicht gesetzt werden
// konnte (Nachbesserung T3, Punkt 11c).
const markWarning = "saved files not marked as downloaded from the internet (no Zone.Identifier); Windows will not warn before opening them"

// mark gibt der abgelegten Datei full (voller Pfad, nach dem Schreiben) die
// Herkunft „aus dem Internet“. Ein Fehler kommt ohne Pfad zurück (er geht
// ins Log, der Pfad kann den Benutzernamen und Namen aus der Mail
// enthalten); die Datei bleibt liegen.
func mark(full string) error {
	err := markFile(full)
	if pe := (*fs.PathError)(nil); errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// logger liefert l oder einen Logger ohne Ausgabe.
func logger(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return l
}
