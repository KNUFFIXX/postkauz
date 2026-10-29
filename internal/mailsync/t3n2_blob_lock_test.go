package mailsync

// Nachbesserung 2, Punkt 8 (aus B2 der Nachprüfung, Wegwerf-Test
// mailsync__pkvb_b2_test.go.txt): Ein Anhang, dessen Ablage kurz auf die
// Sperre gc.lock warten muss, wurde nicht dauerhaft als "not stored"
// vermerkt; Put wartet bis zur Frist, und der Anhang liegt. Seit der
// dritten Nachbesserung (Punkt 8) wiederholt Put nur noch eine belegte
// Sperre und unter Windows vorübergehende Öffnungsfehler (Test der
// Einstufung in internal/blob); hier deshalb mit kurz belegter Sperre
// statt mit gc.lock als Ordner.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
)

const t3n2Raw = "From: a@example.org\r\nTo: b@example.org\r\nSubject: x\r\nMIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=B\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nHallo\r\n" +
	"--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=r.pdf\r\n\r\n%PDF-1.4 x\r\n--B--\r\n"

func TestT3N2AnalyzeWaitsForBusyLock(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := bs.LockForGC(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		lock.Release()
		close(released)
	}()
	e := &Engine{Blobs: bs}
	_, atts, err := e.analyze(strings.NewReader(t3n2Raw), time.Now())
	<-released
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 {
		t.Fatalf("got %d attachments, want 1", len(atts))
	}
	for _, a := range atts {
		if a.SHA256 == "" {
			t.Fatalf("attachment %s stored as %q after a briefly busy lock (no wait)", a.PartID, a.ExtractError)
		}
		if !bs.Has(a.SHA256) {
			t.Fatalf("attachment %s: blob %s missing", a.PartID, a.SHA256)
		}
	}
}
