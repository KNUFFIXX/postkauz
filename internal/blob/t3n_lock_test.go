package blob

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Nachbesserung T3 Punkt 6: Hält blobs.gc die Sperre des Blob-Ordners
// (Lösch-Paket), wartet Put, statt zu scheitern, und legt danach ab; die
// Sperrdatei zählt Scan nicht als fremde Datei. Nach putLockWait gibt Put
// mit ErrBusy auf; solange Put ablegt, bekommt GC die Sperre nicht.
func TestT3NPutWaitsForGCLock(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no OS file locks on this platform")
	}
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.LockForGC(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		info Info
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, err := s.Put(strings.NewReader("anhang während blob gc"))
		done <- result{info, err}
	}()
	select {
	case r := <-done:
		t.Fatalf("Put did not wait for the gc lock: %+v %v", r.info, r.err)
	case <-time.After(100 * time.Millisecond):
	}
	lock.Release()
	select {
	case r := <-done:
		if r.err != nil || !s.Has(r.info.SHA256) {
			t.Fatalf("Put after the gc lock: %+v %v", r.info, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Put still waiting after the gc lock was released")
	}

	// Die Sperrdatei liegt im Blob-Ordner, Scan meldet sie nicht.
	if _, err := os.Stat(filepath.Join(s.Root(), LockName)); err != nil {
		t.Fatalf("lock file: %v", err)
	}
	var unknown int
	if err := s.Scan(func(e Entry) {
		if e.Kind == EntryUnknown {
			unknown++
		}
	}); err != nil || unknown != 0 {
		t.Fatalf("scan: %d unknown entries, %v", unknown, err)
	}

	// Grenze: Nach putLockWait gibt Put auf.
	defer func(w time.Duration) { putLockWait = w }(putLockWait)
	putLockWait = 50 * time.Millisecond
	lock, err = s.LockForGC(time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(strings.NewReader("zu lange gewartet")); !errors.Is(err, ErrBusy) {
		t.Fatalf("Put with a held gc lock after the wait = %v, want ErrBusy", err)
	}
	lock.Release()

	// Umgekehrt: Eine geteilte Sperre (ein Put legt gerade ab) hält GC auf.
	shared, err := s.lockWait(false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.LockForGC(20 * time.Millisecond); !errors.Is(err, ErrBusy) {
		t.Fatalf("gc lock while a Put holds the shared lock = %v, want ErrBusy", err)
	}
	shared.Release()
}
