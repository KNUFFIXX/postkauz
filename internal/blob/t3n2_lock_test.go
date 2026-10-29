package blob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Nachbesserung 2, Punkt 8: Die Sperre des Blob-Ordners wird auch dann bis
// zur Frist erneut versucht, wenn schon das Öffnen der Sperrdatei scheitert
// (unter Windows: Sharing-Violation durch Virenscanner oder
// Sicherungsprogramm). Nachgestellt mit gc.lock als Ordner, der kurz darauf
// verschwindet.

func skipWithoutLocks(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no OS file locks on this platform")
	}
}

func TestT3N2PutRetriesTransientOpenError(t *testing.T) {
	skipWithoutLocks(t)
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(s.Root(), LockName)
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		removed <- os.Remove(lockDir)
	}()
	info, err := s.Put(strings.NewReader("anhang trotz kurz gesperrter Sperrdatei"))
	if rerr := <-removed; rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Fatalf("Put after a transient open error of %s: %v (want a retry)", LockName, err)
	}
	if !s.Has(info.SHA256) {
		t.Fatalf("blob %s not stored", info.SHA256)
	}
}

// Bleibt der Öffnungsfehler bis zur Frist, kommt ErrBusy mit dem letzten
// Fehler, und es liegt kein Blob.
func TestT3N2PutPersistentOpenErrorIsBusy(t *testing.T) {
	skipWithoutLocks(t)
	s, err := Open(t.TempDir(), Options{LockWait: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.Root(), LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	info, err := s.Put(strings.NewReader("bleibt gesperrt"))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("Put with a persistent open error = %v, want ErrBusy", err)
	}
	if !strings.Contains(err.Error(), "last error") {
		t.Fatalf("ErrBusy without the open error: %v", err)
	}
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("Put gave up after %v, before the wait of 50ms", d)
	}
	if info.SHA256 != "" {
		t.Fatalf("Put returned info %+v with an error", info)
	}
}

// Ein beendeter Kontext beendet das Warten von PutContext sofort (nicht
// erst nach 10 s), mit context.Canceled; nichts liegt.
func TestT3N2PutContextCancelStopsWaiting(t *testing.T) {
	skipWithoutLocks(t)
	s, err := Open(t.TempDir(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := s.LockForGC(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	const content = "abgebrochen während blob gc"
	start := time.Now()
	_, err = s.PutContext(ctx, strings.NewReader(content))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PutContext after cancel = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("PutContext waited %v after cancel", d)
	}
	lock.Release()
	// Nichts abgelegt: Ein erneutes Put legt neu an.
	info, err := s.Put(strings.NewReader(content))
	if err != nil || info.Existed {
		t.Fatalf("Put after the canceled PutContext: %+v %v (want a new blob)", info, err)
	}
	entries, err := os.ReadDir(filepath.Join(s.Root(), "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temp files left: %v %v", entries, err)
	}

	// Ebenso GC: Ein beendeter Kontext beendet das Warten auf die exklusive
	// Sperre, solange ein Put die geteilte hält.
	shared, err := s.lockWait(context.Background(), false, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer shared.Release()
	done, cancel2 := context.WithCancel(context.Background())
	cancel2()
	start = time.Now()
	if _, err := s.LockForGC(done, 10*time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("LockForGC with a canceled context = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("LockForGC waited %v with a canceled context", d)
	}
}
