package blob

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// A14 (T3): Put mit schon vorhandenem Inhalt erneuert das Dateidatum. Sonst
// schützte das Mindestalter von blobs.gc einen Blob nicht, den ein Sync oder
// Entwurf gerade wieder ablegt und gleich verweist.
func TestPutExistingRefreshesModTime(t *testing.T) {
	for _, compress := range []bool{false, true} {
		s, err := Open(t.TempDir(), Options{Compress: compress})
		if err != nil {
			t.Fatal(err)
		}
		info, err := s.Put(strings.NewReader("derselbe Inhalt"))
		if err != nil {
			t.Fatal(err)
		}
		p, _, _ := s.Path(info.SHA256)
		old := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
		if mt, _ := s.ModTime(info.SHA256); !mt.Equal(old) {
			t.Fatalf("mtime = %v, want %v", mt, old)
		}
		again, err := s.Put(strings.NewReader("derselbe Inhalt"))
		if err != nil || !again.Existed {
			t.Fatalf("second put: %+v %v", again, err)
		}
		mt, err := s.ModTime(info.SHA256)
		if err != nil || time.Since(mt) > time.Minute {
			t.Fatalf("compress=%v: mtime after a second put = %v (%v); want now", compress, mt, err)
		}
	}
}

func TestRemoveAndModTime(t *testing.T) {
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Put(strings.NewReader("weg damit"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ModTime("../x"); !errors.Is(err, ErrBadHash) {
		t.Fatalf("bad hash: %v", err)
	}
	if removed, err := s.Remove(info.SHA256); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if s.Has(info.SHA256) {
		t.Fatal("blob still there")
	}
	if _, err := s.ModTime(info.SHA256); !errors.Is(err, ErrNotFound) {
		t.Fatalf("mtime of a removed blob: %v", err)
	}
	if removed, err := s.Remove(info.SHA256); err != nil || removed {
		t.Fatalf("second remove: %v %v", removed, err)
	}
}
