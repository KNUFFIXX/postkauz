package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A12 Teil 3 (a): [extract] pdftotext.
func TestA12ExtractPDFToText(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "poppler", "pdftotext")
	for _, c := range []struct {
		name, toml, want, err string
	}{
		{"absent", ``, "", ""},
		{"off", "[extract]\npdftotext = \"off\"\n", "off", ""},
		{"absolute, file missing", "[extract]\npdftotext = '" + abs + "'\n", abs, ""},
		{"relative", "[extract]\npdftotext = 'bin/pdftotext'\n", "", "not an absolute path"},
		{"cmd", "[extract]\npdftotext = '" + abs + ".cmd'\n", "", ".bat or .cmd"},
		{"bat", "[extract]\npdftotext = '" + abs + ".BAT'\n", "", ".bat or .cmd"},
		{"wrong type", "[extract]\npdftotext = 1\n", "", "pdftotext"},
		{"unknown key", "[extract]\npdf = 'x'\n", "", "unknown keys: extract.pdf"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			if err := os.WriteFile(path, []byte("data_dir = '"+filepath.Join(dir, "data")+"'\n"+c.toml), 0o600); err != nil {
				t.Fatal(err)
			}
			cfg, _, err := LoadWith(LoadOptions{ConfigFile: path})
			switch {
			case c.err == "" && err != nil:
				t.Fatalf("err = %v", err)
			case c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)):
				t.Fatalf("err = %v, want %q", err, c.err)
			case c.err == "" && cfg.Extract.PDFToText != c.want:
				t.Fatalf("pdftotext = %q, want %q", cfg.Extract.PDFToText, c.want)
			}
		})
	}
}
