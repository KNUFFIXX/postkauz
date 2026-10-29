package magic

import "testing"

// Nachbesserung T3 2, Punkt 10: Endungen aus der Sperrliste von Outlook
// (Level 1), die ExecutableName bisher nicht kannte (Access, ClickOnce,
// App-Pakete, PowerShell-Konsolen und -Formate, Java Web Start, Windows
// Sandbox, Zertifikate, Internet-Einstellungen, Installer-Transformationen,
// Visual Basic, Shell-Skripte), gelten als ausführbar.
func TestT3N2RiskyExtensionsComplete(t *testing.T) {
	for _, ext := range []string{".ade", ".adp", ".appinstaller", ".appxbundle", ".msixbundle", ".vsto", ".ps2xml", ".psc1", ".jnlp",
		".mdb", ".accdb", ".mda", ".vb", ".wsb", ".cer", ".crt", ".der", ".ins", ".isp", ".mst", ".xnk", ".bas", ".ksh", ".csh"} {
		for _, name := range []string{"rechnung" + ext, "Rechnung" + ext + ".", "rechnung.pdf" + ext} {
			if !ExecutableName(name) {
				t.Errorf("%q: not treated as executable", name)
			}
		}
	}
}
