// Command postkauz ist die Kommandozeile des agenten-first E-Mail-Clients.
// „postkauz" ist ein Arbeitstitel; Namen stehen in internal/brand.
package main

import (
	"os"

	"github.com/KNUFFIXX/postkauz/internal/cli"
	// Ausdrücklich: Das init von extract schaltet das Programm in den
	// Extraktor-Modus, wenn es als eigener Kindprozess für die
	// Textextraktion gestartet wird (internal/extract).
	_ "github.com/KNUFFIXX/postkauz/internal/extract"
)

// version wird beim Bauen per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
