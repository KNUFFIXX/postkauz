package blob

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// OpenExisting legt nichts an; Scan ordnet Blob-, Temporär- und fremde
// Dateien ein, ohne etwas zu ändern (doctor --blobs, A03 A5.4).
func TestOpenExistingAndScan(t *testing.T) {
	root := filepath.Join(t.TempDir(), "blobs")
	if _, err := OpenExisting(root, Options{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing root: %v", err)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("OpenExisting created the root: %v", err)
	}
	s, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := s.Put(strings.NewReader("klar"))
	if err != nil {
		t.Fatal(err)
	}
	z, err := Open(root, Options{Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	packed, err := z.Put(strings.NewReader("gepackt"))
	if err != nil {
		t.Fatal(err)
	}
	misplaced := strings.Repeat("ab", 32)
	for name, data := range map[string]string{
		"tmp/put-1":                           "x",
		"tmp/fremd":                           "x",
		"README":                              "x",
		"ab/cd/" + misplaced:                  "x", // gültiger Hash, falscher Ort
		"ab/ab/" + strings.ToUpper(misplaced): "x",
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ro, err := OpenExisting(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	blobs := map[string]bool{}
	var temp, unknown int
	if err := ro.Scan(func(e Entry) {
		switch e.Kind {
		case EntryBlob:
			blobs[e.SHA256] = e.Compressed
		case EntryTemp:
			temp++
		default:
			unknown++
		}
	}); err != nil {
		t.Fatal(err)
	}
	if len(blobs) != 2 || blobs[plain.SHA256] || !blobs[packed.SHA256] || temp != 1 || unknown != 4 {
		t.Fatalf("scan: blobs=%v temp=%d unknown=%d", blobs, temp, unknown)
	}
	if err := ro.Verify(packed.SHA256, packed.Size); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tmp", "put-1")); err != nil {
		t.Fatalf("scan removed a temp file: %v", err)
	}
}
