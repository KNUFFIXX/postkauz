// Command mailwerk ist die Kommandozeile des agenten-first E-Mail-Clients.
// „mailwerk" ist ein Arbeitstitel; Namen stehen in internal/brand.
package main

import (
	"os"

	"github.com/KNUFFIXX/mailwerk/internal/cli"
)

// version wird beim Bauen per -ldflags "-X main.version=..." gesetzt.
var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
