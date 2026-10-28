//go:build unix

package ops

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Rest aus T1a (T2): Das Öffnen der Stildatei wartet nicht auf eine FIFO,
// die nach der Prüfung per Lstat untergeschoben würde. Hier direkt am
// Öffnen geprüft (der Wettlauf selbst ist nicht zuverlässig nachstellbar):
// mit openNonblock kehrt Open sofort zurück.
func TestStyleOpenDoesNotBlockOnFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), "stil.fifo")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("no mkfifo here:", err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := os.OpenFile(p, os.O_RDONLY|openNonblock, 0)
		if err == nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("open of a FIFO blocked")
	}
	if _, _, err := readStyleFile(p, 100); err == nil {
		t.Fatal("FIFO accepted as style file")
	}
}
