package magic

import "testing"

// Nachbesserung T3, Punkt 11b: Endungen, die Windows ausführt oder die in
// Angriffsketten dienen (OneNote, RDP, ClickOnce, Access, Suchverknüpfungen,
// Diagnosepakete, XBAP, Hilfedateien, Monad-Skripte), gelten als ausführbar.
func TestT3NRiskyExtensionsComplete(t *testing.T) {
	for _, ext := range []string{".one", ".rdp", ".application", ".accde", ".mde", ".website", ".library-ms", ".search-ms",
		".diagcab", ".xbap", ".hlp", ".msh", ".msh1", ".msh2", ".mshxml", ".msh1xml", ".msh2xml"} {
		for _, name := range []string{"anhang" + ext, "Anhang" + ext + ".", "rechnung.pdf" + ext} {
			if !ExecutableName(name) {
				t.Errorf("%q: not treated as executable", name)
			}
		}
	}
}
