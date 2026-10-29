package backup

import (
	"os"
	"path/filepath"
	"testing"
)

// T3-Nachbesserung „Junctions unter Windows“: insideByID erkennt ein Ziel
// über einen Umweg ins Datenverzeichnis an der Datei-Identität eines
// vorhandenen Vorfahren (auch wenn das Ziel selbst noch nicht existiert).
// Hier mit einem symbolischen Link, den os.Stat wie eine Junction auflöst;
// die Junction selbst prüft TestT3NBackupTargetThroughJunctionIntoDataDir
// unter Windows.
func TestT3NInsideByID(t *testing.T) {
	base := t.TempDir()
	data := filepath.Join(base, "data")
	other := filepath.Join(base, "anderer")
	for _, d := range []string{data, other} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "L")
	if err := os.Symlink(data, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	for name, c := range map[string]struct {
		child string
		want  bool
	}{
		"same":            {data, true},
		"inside, new":     {filepath.Join(data, "neu", "tiefer"), true},
		"via link":        {link, true},
		"via link, new":   {filepath.Join(link, "backup"), true},
		"sibling":         {other, false},
		"sibling, new":    {filepath.Join(other, "backup"), false},
		"parent":          {base, false},
		"missing anyway":  {filepath.Join(base, "fehlt", "x"), false},
		"name prefix":     {data + "2", false},
		"relative inside": {filepath.Join(data, "a", "..", "b"), true},
	} {
		if got := insideByID(c.child, data); got != c.want {
			t.Errorf("%s: insideByID(%s) = %v, want %v", name, c.child, got, c.want)
		}
	}
	if insideByID(data, filepath.Join(base, "gibt-es-nicht")) {
		t.Error("a missing data directory matched")
	}
}
