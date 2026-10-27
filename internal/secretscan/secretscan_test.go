package secretscan

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

func TestScanFindsValuesAndPatterns(t *testing.T) {
	needles := []Needle{{Name: "pw demo", Value: []byte("Kanarie-123456")}, {Name: "short", Value: []byte("pass")}}
	pats := map[string]*regexp.Regexp{"key": regexp.MustCompile(`pk_key_[A-Za-z0-9_-]{43}`)}
	long := strings.Repeat("x", 3<<20) // länger als der Lesepuffer
	text := "eins\npassword is pass\n" + long + "\nzwei Kanarie-123456\n" + "pk_key_" + strings.Repeat("A", 43) + "\n"
	hits, err := Scan(strings.NewReader(text), needles, pats)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0] != (Hit{"pw demo", 4}) || hits[1] != (Hit{"key", 5}) {
		t.Fatalf("hits = %+v", hits)
	}
	// Ohne Zeilenende am Schluss und mehrfach: ein Fund je Geheimnis.
	hits, _ = Scan(strings.NewReader("Kanarie-123456\nKanarie-123456"), needles, nil)
	if len(hits) != 1 || hits[0].Line != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits, err := ScanFile(filepath.Join(t.TempDir(), "fehlt"), needles, pats); err != nil || hits != nil {
		t.Fatalf("missing file: %v %v", hits, err)
	}
}

func TestPerm(t *testing.T) {
	if runtime.GOOS == "windows" {
		if f := Perm("x", "C:\\", true); f.Status != Skipped {
			t.Fatalf("windows: %+v", f)
		}
		return
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = os.WriteFile(p, nil, 0o600)
	if f := Perm("x", p, true); f.Status != OK || f.Detail != "0600" {
		t.Fatalf("0600: %+v", f)
	}
	_ = os.Chmod(p, 0o640)
	if f := Perm("x", p, false); f.Status != Warn || !strings.Contains(f.Detail, "chmod 600") {
		t.Fatalf("0640: %+v", f)
	}
	_ = os.Chmod(dir, 0o755)
	if f := Perm("x", dir, true); f.Status != Fail || !strings.Contains(f.Detail, "chmod 700") {
		t.Fatalf("dir: %+v", f)
	}
	if f := Perm("x", filepath.Join(dir, "nein"), true); f.Status != OK {
		t.Fatalf("missing: %+v", f)
	}
}

// T1c: Bearer- und OAuth-Tokens (JWT) gelten als Leck, auch ohne Signatur;
// Base64-Text ohne den JSON-Anfang „eyJ" in beiden Teilen nicht.
func TestJWTPattern(t *testing.T) {
	// Synthetisch: {"alg":"HS256","typ":"JWT"} . {"sub":"demo"} . Signatur
	tok := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJkZW1vIn0.c2lnbmF0dXJlLW51ci1mdWVyLXRlc3Rz"
	for _, line := range []string{"Authorization: Bearer " + tok, `{"access_token":"` + tok + `"}`,
		"eyJhbGciOiJub25lIn0.eyJzdWIiOiJkZW1vIn0."} {
		hits, err := Scan(strings.NewReader("x\n"+line+"\n"), nil, map[string]*regexp.Regexp{"jwt": JWTPattern})
		if err != nil || len(hits) != 1 || hits[0].Line != 2 {
			t.Errorf("%q: hits = %+v, %v", line, hits, err)
		}
	}
	for _, line := range []string{"eyJ.eyJ.x", "aGVsbG8gd29ybGQ.aGVsbG8gd29ybGQ.x", "version 1.2.3"} {
		if JWTPattern.MatchString(line) {
			t.Errorf("%q matches", line)
		}
	}
}
