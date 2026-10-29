package mailsync

// Nachbesserung 2, Punkt 8 (aus B2 der Nachprüfung, Wegwerf-Test
// mailsync__pkvb_b2_test.go.txt): Ein Fehler beim Öffnen von gc.lock, der
// keine belegte Sperre ist (hier: gc.lock ist kurz ein Ordner; unter
// Windows etwa eine Sharing-Violation durch einen Virenscanner), ließ
// blob.Put sofort scheitern, und analyze vermerkte den Anhang dauerhaft als
// "not stored" (kein SHA256). Jetzt wiederholt Put bis zur Frist, und der
// Anhang liegt.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
)

const t3n2Raw = "From: a@example.org\r\nTo: b@example.org\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nHallo\r\n" +
	"--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=r.pdf\r\n\r\n%PDF-1.4 x\r\n--B--\r\n"

func TestT3N2AnalyzeRetriesTransientLockOpenError(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(root, blob.LockName)
	if err := os.Mkdir(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	removed := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		removed <- os.Remove(lockDir)
	}()
	e := &Engine{Blobs: bs}
	_, atts, err := e.analyze(strings.NewReader(t3n2Raw), time.Now())
	if rerr := <-removed; rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 {
		t.Fatalf("got %d attachments, want 1", len(atts))
	}
	for _, a := range atts {
		if a.SHA256 == "" {
			t.Fatalf("attachment %s stored as %q after a transient lock error (no retry)", a.PartID, a.ExtractError)
		}
		if !bs.Has(a.SHA256) {
			t.Fatalf("attachment %s: blob %s missing", a.PartID, a.SHA256)
		}
	}
}
