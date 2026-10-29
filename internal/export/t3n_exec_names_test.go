package export

// Nachbesserung T3, Punkt 11a: Der mbox-Export legt einen Ordner wie
// "Rechnung.cmd" nie als gleichnamige Datei ab (ein Doppelklick führte sie
// aus).

import (
	"slices"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/magic"
)

func TestT3NMboxFolderNeverExecutableName(t *testing.T) {
	for _, seg := range []string{"Rechnung.cmd", "Belege.bat", "x.ps1", "amazon.com", "Tools.scr", "a.hta", "Notizen.one",
		"Rechnung.cmd.", "x.PS1"} {
		for _, f := range [][]Folder{{{Segments: []string{seg}}}, {{Segments: []string{seg}}, {Segments: []string{seg, "Unter"}}}} {
			for _, p := range layout(FormatMbox, f) {
				if magic.ExecutableName(p) {
					t.Errorf("mbox file for folder %q is %q: executable name", seg, p)
				}
			}
		}
	}
	// Name und ".sbd" bleiben ein Paar (Thunderbird-Ablage).
	got := layout(FormatMbox, []Folder{{Segments: []string{"Rechnung.cmd"}}, {Segments: []string{"Rechnung.cmd", "Unter"}}})
	if want := []string{"Rechnung.cmd_", "Rechnung.cmd_.sbd/Unter"}; !slices.Equal(got, want) {
		t.Errorf("layout = %q, want %q", got, want)
	}
	// EML: Ordner sind Verzeichnisse, ihr Name bleibt.
	if got := layout(FormatEML, []Folder{{Segments: []string{"Rechnung.cmd"}}}); !slices.Equal(got, []string{"Rechnung.cmd"}) {
		t.Errorf("eml layout = %q", got)
	}
}
