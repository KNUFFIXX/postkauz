package magic

import "testing"

// A14 (S3.3): Die Endungsprüfung lässt sich nicht mit Zeichen umgehen, die
// Windows beim Anlegen entfernt (Punkte und Leerzeichen am Ende), mit
// NTFS-Datenströmen, Groß-/Kleinschreibung, Steuer- und Formatzeichen oder
// einer zweiten Endung.
func TestExecutableNameTricks(t *testing.T) {
	for _, name := range []string{"x.bat", "x.bat.", "x.bat...", "x.bat ", "x.bat . .", "x.bat::$DATA", "x.bat:stream",
		"x.BaT", "X.EXE", "rechnung.pdf.exe", "rechnung.pdf.Exe.", "x.exe\u202e", "rechnung\u202efdp.exe", "x.e\u200bxe",
		`C:\Users\x\start.cmd`, "../tmp/run.sh", "x.ps1\t", "x.lnk::$DATA ", "x.Command", "x.js\x00"} {
		if !ExecutableName(name) {
			t.Errorf("%q: not executable (ext %q)", name, EffectiveExt(name))
		}
		if r := Check("text/plain", name, Type{MIME: TextPlain, Text: true}); !r.Executable {
			t.Errorf("Check %q: not executable", name)
		}
	}
	for _, name := range []string{"x.pdf", "x.bat.pdf", "x.exe.txt", "bat", "x.", "", "x:bat", "x.batch", "exe"} {
		if ExecutableName(name) {
			t.Errorf("%q: executable (ext %q)", name, EffectiveExt(name))
		}
	}
}
