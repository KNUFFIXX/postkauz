package export

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestSafeFileName(t *testing.T) {
	cases := map[string]string{
		"Rechnung 2026.pdf":          "Rechnung 2026.pdf",
		"":                           "unnamed",
		"   ":                        "unnamed",
		".":                          "unnamed",
		"..":                         "unnamed",
		"../../etc/passwd":           "_._.._etc_passwd",
		`..\..\Windows\win.ini`:      `_._.._Windows_win.ini`,
		"CON":                        "_CON",
		"con.txt":                    "_con.txt",
		"Nul.tar.gz":                 "_Nul.tar.gz",
		"COM1":                       "_COM1",
		"lpt9.doc":                   "_lpt9.doc",
		"COM¹.txt":                   "_COM¹.txt",
		"CON .txt":                   "_CON .txt",
		"CONSOLE.txt":                "CONSOLE.txt",
		"x.bat.":                     "x.bat",
		"x.bat . . ":                 "x.bat",
		"x.bat::$DATA":               "x.bat__$DATA",
		"a:b":                        "a_b",
		"a<b>c|d?e*f\"g":             "a_b_c_d_e_f_g",
		"zeile\r\numbruch.txt":       "zeile__umbruch.txt",
		"rechnung\u202efdp.exe":      "rechnungfdp.exe",
		".htaccess":                  "_htaccess",
		"-rf":                        "_rf",
		"ok\x00.pdf":                 "ok_.pdf",
		"Umlaute äöü ß.txt":          "Umlaute äöü ß.txt",
		"kaputt\xff\xfe.txt":         "kaputt_.txt",
		"\u00a0leer\u00a0.txt\u00a0": "leer .txt",
	}
	for in, want := range cases {
		if got := SafeFileName(in); got != want {
			t.Errorf("SafeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ä", 200) + ".pdf"
	got := SafeFileName(long)
	if len(got) > MaxNameBytes || !strings.HasSuffix(got, ".pdf") || !utf8.ValidString(got) {
		t.Fatalf("long name: %d bytes %q", len(got), got)
	}
}

func TestWithCounter(t *testing.T) {
	for _, c := range []struct {
		name string
		n    int
		want string
	}{{"bericht.pdf", 1, "bericht.pdf"}, {"bericht.pdf", 2, "bericht-2.pdf"}, {"x.tar.gz", 3, "x.tar-3.gz"},
		{"ohne", 2, "ohne-2"}, {"_htaccess", 2, "_htaccess-2"}} {
		if got := WithCounter(c.name, c.n); got != c.want {
			t.Errorf("WithCounter(%q, %d) = %q, want %q", c.name, c.n, got, c.want)
		}
	}
}

func TestSplitSubdir(t *testing.T) {
	ok := map[string][]string{"": nil, "belege": {"belege"}, "a/b": {"a", "b"}, `a\b`: {"a", "b"}, "./a//b/.": {"a", "b"},
		"2026 Rechnungen": {"2026 Rechnungen"}}
	for in, want := range ok {
		got, err := SplitSubdir(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("SplitSubdir(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"..", "../x", "a/../../x", `a\..\..`, "/etc", `\\server\share`, `C:\x`, "c:x", "a:b", "/"} {
		if _, err := SplitSubdir(in); !errors.Is(err, ErrOutside) {
			t.Errorf("SplitSubdir(%q): %v, want ErrOutside", in, err)
		}
	}
	for _, in := range []string{"CON", "a/nul.txt", "x.", "ab:c", "a\x00b", ".versteckt", strings.Repeat("a/", 9) + "a"} {
		if _, err := SplitSubdir(in); !errors.Is(err, ErrInvalidSubdir) {
			t.Errorf("SplitSubdir(%q): %v, want ErrInvalidSubdir", in, err)
		}
	}
}

// FuzzSafeFileName (A03_teile.md T3, „Export-Dateinamen gegen
// Windows-Reservierungen“): Das Ergebnis ist nie leer, nie reserviert, nie
// mit Trenner, nie "." oder "..", höchstens MaxNameBytes lang, gültiges
// UTF-8 ohne Steuerzeichen und ":" und idempotent.
func FuzzSafeFileName(f *testing.F) {
	for _, s := range []string{"", ".", "..", "CON", "con.txt", "COM¹", "x.bat.", "x.bat::$DATA", "a/b\\c", "\x00", " . ",
		"rechnung\u202efdp.exe", strings.Repeat("ß", 100) + ".pdf", "CON." + strings.Repeat("x", 200), "-", "\xff", "LPT3 ",
		strings.Repeat(".", 300), "a" + strings.Repeat(" ", 160) + ".txt"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := SafeFileName(in)
		switch {
		case got == "", got == ".", got == "..":
			t.Fatalf("%q → %q", in, got)
		case Reserved(got):
			t.Fatalf("%q → reserved %q", in, got)
		case strings.ContainsAny(got, `/\:<>"|?*`):
			t.Fatalf("%q → separator or forbidden character in %q", in, got)
		case len(got) > MaxNameBytes:
			t.Fatalf("%q → %d bytes", in, len(got))
		case !utf8.ValidString(got):
			t.Fatalf("%q → invalid UTF-8 %q", in, got)
		case strings.HasSuffix(got, ".") || strings.HasSuffix(got, " ") || strings.HasPrefix(got, ".") || strings.HasPrefix(got, " "):
			t.Fatalf("%q → leading or trailing dot or space in %q", in, got)
		case strings.IndexFunc(got, func(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }) >= 0:
			t.Fatalf("%q → control or format character in %q", in, got)
		}
		if again := SafeFileName(got); again != got {
			t.Fatalf("not idempotent: %q → %q → %q", in, got, again)
		}
	})
}
