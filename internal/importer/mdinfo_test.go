package importer

import (
	"runtime"
	"strings"
	"testing"
)

// mdName setzt in einem Maildir-Testnamen das Trennzeichen der Flags: „:2,“,
// unter Windows „!2,“ (ein „:“ legte dort einen NTFS-Datenstrom an statt
// einer Datei; A12.4). Der Importer liest beide.
func mdName(name string) string {
	if runtime.GOOS == "windows" {
		return windowsInfo(name)
	}
	return name
}

func windowsInfo(name string) string {
	out := []byte(name)
	for i := 0; i+2 < len(out); i++ {
		if out[i] == ':' && out[i+1] == '2' && out[i+2] == ',' {
			out[i] = '!'
		}
	}
	return string(out)
}

// Die Windows-Namen der Tests liest der Importer mit denselben Flags.
func TestWindowsMaildirTestNames(t *testing.T) {
	for _, n := range []string{"1.M1P1.x:2,S", "1700000000.M1P1.rc8:2,FT"} {
		w := windowsInfo(n)
		if strings.Contains(w, ":") || maildirFlags(w) != maildirFlags(n) || maildirFlags(n) == "" {
			t.Fatalf("%q -> %q: flags %q vs %q", n, w, maildirFlags(w), maildirFlags(n))
		}
	}
}
