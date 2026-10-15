package blob

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func sum(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func readAll(t *testing.T, s *Store, h string) []byte {
	t.Helper()
	rc, err := s.Get(h)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPutGetPlainAndCompressed(t *testing.T) {
	data := []byte(strings.Repeat("Grüße aus dem Erdgeschoß\r\n", 500))
	for _, compress := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "zstd"}[compress], func(t *testing.T) {
			dir := t.TempDir()
			s, err := Open(dir, Options{Compress: compress})
			if err != nil {
				t.Fatal(err)
			}
			info, err := s.Put(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			if info.SHA256 != sum(data) || info.Size != int64(len(data)) || info.Compressed != compress || info.Existed {
				t.Fatalf("info = %+v", info)
			}
			p, comp, err := s.Path(info.SHA256)
			if err != nil || comp != compress {
				t.Fatalf("path %s %v %v", p, comp, err)
			}
			want := filepath.Join(dir, info.SHA256[:2], info.SHA256[2:4], info.SHA256)
			if compress {
				want += ".zst"
			}
			if p != want {
				t.Fatalf("path = %s, want %s", p, want)
			}
			if compress {
				st, _ := os.Stat(p)
				if st.Size() >= int64(len(data)) {
					t.Errorf("compressed file not smaller: %d", st.Size())
				}
			}
			if got := readAll(t, s, info.SHA256); !bytes.Equal(got, data) {
				t.Fatal("content differs")
			}
			if err := s.Verify(info.SHA256, info.Size); err != nil {
				t.Fatal(err)
			}
			// Temporärverzeichnis ist leer.
			if e, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(e) != 0 {
				t.Fatalf("tmp not empty: %v", e)
			}
		})
	}
}

func TestDeduplicationAcrossModes(t *testing.T) {
	dir := t.TempDir()
	plain, _ := Open(dir, Options{})
	zst, _ := Open(dir, Options{Compress: true})
	data := []byte("same content")
	a, err := plain.Put(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	b, err := zst.Put(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if !b.Existed || b.Compressed || b.SHA256 != a.SHA256 {
		t.Fatalf("second put = %+v", b)
	}
	if _, err := os.Stat(filepath.Join(dir, a.SHA256[:2], a.SHA256[2:4], a.SHA256+".zst")); !os.IsNotExist(err) {
		t.Fatal("a compressed duplicate was written")
	}
}

func TestSizeLimit(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, Options{MaxSize: 1000})
	_, err := s.Put(bytes.NewReader(make([]byte, 1001)))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Put(bytes.NewReader(make([]byte, 1000))); err != nil {
		t.Fatalf("exactly at limit: %v", err)
	}
	if e, _ := os.ReadDir(filepath.Join(dir, "tmp")); len(e) != 0 {
		t.Fatalf("temp file left behind: %v", e)
	}
}

// Ein Lesefehler mitten im Schreiben hinterlässt keinen Blob.
func TestFailedWriteLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, Options{})
	r := io.MultiReader(strings.NewReader("teil 1"), errReader{})
	if _, err := s.Put(r); err == nil {
		t.Fatal("want error")
	}
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	if n != 0 {
		t.Fatalf("%d files left", n)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("network gone") }

func TestVerifyDetectsCorruption(t *testing.T) {
	for _, compress := range []bool{false, true} {
		dir := t.TempDir()
		s, _ := Open(dir, Options{Compress: compress})
		info, _ := s.Put(strings.NewReader("original"))
		p, _, _ := s.Path(info.SHA256)
		if compress {
			// Andere, gültige zstd-Daten unter demselben Namen.
			other, _ := Open(t.TempDir(), Options{Compress: true})
			oi, _ := other.Put(strings.NewReader("manipuliert"))
			op, _, _ := other.Path(oi.SHA256)
			b, _ := os.ReadFile(op)
			_ = os.WriteFile(p, b, 0o600)
		} else {
			_ = os.WriteFile(p, []byte("manipuliert"), 0o600)
		}
		if err := s.Verify(info.SHA256, 0); !errors.Is(err, ErrCorrupt) {
			t.Errorf("compress=%v: err = %v", compress, err)
		}
	}
}

func TestBadAndMissingHash(t *testing.T) {
	s, _ := Open(t.TempDir(), Options{})
	if _, err := s.Get("../../etc/passwd"); !errors.Is(err, ErrBadHash) {
		t.Errorf("traversal: %v", err)
	}
	if _, err := s.Get(strings.Repeat("A", 64)); !errors.Is(err, ErrBadHash) {
		t.Errorf("upper case: %v", err)
	}
	if _, err := s.Get(strings.Repeat("a", 64)); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
	if s.Has(strings.Repeat("a", 64)) {
		t.Error("Has on missing blob")
	}
}

func TestConcurrentPutSameContent(t *testing.T) {
	s, _ := Open(t.TempDir(), Options{Compress: true})
	data := []byte(strings.Repeat("x", 1<<16))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Put(bytes.NewReader(data)); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if err := s.Verify(sum(data), int64(len(data))); err != nil {
		t.Fatal(err)
	}
}

func TestCleanTemp(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir, Options{})
	_ = os.WriteFile(filepath.Join(dir, "tmp", "put-123"), []byte("x"), 0o600)
	if n, err := s.CleanTemp(); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
