// Command postkauz ist die Kommandozeile des agenten-first E-Mail-Clients.
// „postkauz" ist ein Arbeitstitel; Namen stehen in internal/brand.
package main

import (
	"os"

	"github.com/KNUFFIXX/postkauz/internal/cli"
	"github.com/KNUFFIXX/postkauz/internal/procguard"
	// Ausdrücklich: Das init von extract schaltet das Programm in den
	// Extraktor-Modus, wenn es als eigener Kindprozess für die
	// Textextraktion gestartet wird (internal/extract).
	_ "github.com/KNUFFIXX/postkauz/internal/extract"
)

// version wird beim Bauen per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

func main() {
	// Erste Anweisung, für alle Kommandos (zweite Nachbesserung T5,
	// Punkt 4a): Unter Linux ist der Prozess ab hier für andere Prozesse
	// desselben Nutzers nicht mehr auslesbar (/proc/<pid>/environ, /mem,
	// ptrace). Vorher kam der Schutz erst nach Datenbank, Migration und
	// Policy, und ein Passwort aus der Umgebung war bis dahin lesbar. Ein
	// Fehler wird hier nicht gemeldet; cli setzt den Schutz beim Öffnen
	// erneut und schreibt ihn ins Log. Unter Windows und macOS ohne Wirkung.
	_ = procguard.DenyInspection()
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
