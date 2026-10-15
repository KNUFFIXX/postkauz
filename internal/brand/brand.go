// Package brand bündelt alle Stellen, an denen der Projektname vorkommt.
//
// „PostKauz" ist ein Arbeitstitel (Namensprüfung läuft). Bei einer
// Umbenennung müssen nur geändert werden:
//
//  1. der Modulpfad in go.mod (und die Importe, per `go mod edit -module` + sed),
//  2. die Konstanten in dieser Datei,
//  3. BINARY/ALIAS im Makefile,
//  4. der Verzeichnisname cmd/postkauz.
//
// Alles andere (Doku, Meldungen, Pfade) leitet sich aus diesen Werten ab.
package brand

const (
	// Name ist der Binärname und der Programmname in Hilfetexten.
	Name = "postkauz"
	// Alias ist das Kürzel für die Kommandozeile (`pk`). Das Makefile legt es
	// als Kopie bzw. Verknüpfung an; cobra kennt für die Wurzel keine Aliase.
	Alias = "pk"
	// EnvPrefix ist das Präfix aller Umgebungsvariablen (POSTKAUZ_CONFIG, ...).
	EnvPrefix = "POSTKAUZ"
	// ConfigDir ist der Verzeichnisname unterhalb des Konfigurationsordners
	// des Betriebssystems (os.UserConfigDir).
	ConfigDir = "postkauz"
	// KeyringService ist der Dienstname im Schlüsselbund des Betriebssystems.
	KeyringService = "postkauz"
)
