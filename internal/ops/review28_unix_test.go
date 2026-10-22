//go:build unix

package ops

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// K2 (nur Unix, dort gibt es FIFOs): Eine FIFO als Stildatei blockiert
// drafts.style nicht, sondern ist ein Fehler mit Grund.
func TestReviewK2StyleFileFIFORefused(t *testing.T) {
	e := newDraftEnv(t, false)
	fifo := filepath.Join(t.TempDir(), "stil.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := e.st.ExecForTest(`UPDATE identities SET style_file = ? WHERE id = 1`, fifo); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := e.do(t, assistant, "drafts.style", map[string]any{"identity": "1"}, false)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") || strings.Contains(err.Error(), fifo) {
			t.Fatalf("fifo: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("drafts.style blocks on a FIFO")
	}
}
