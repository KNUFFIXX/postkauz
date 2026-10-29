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
// zur Frist erneut versucht, wenn schon das Öffnen der Sperrdatei
// vorübergehend scheitert (unter Windows: Sharing-Violation durch
// Virenscanner oder Sicherungsprogramm). Seit der dritten Nachbesserung
// (Punkt 8) nur noch vorübergehende Fehler: Die Tests dazu stehen in
// t3n3_lock_test.go.

func skipWithoutLocks(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("no OS file locks on this platform")
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
