//go:build unix

package compose

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A12.4: Eine FIFO in attach_dir wird abgelehnt, ohne dass das Öffnen
// blockiert (vorher wartete root.Open auf einen Schreiber).
func TestLoadFileRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe.pdf"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("hallo"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := LoadFile("attachments[0]", dir, "pipe.pdf", 1<<20)
		done <- err
	}()
	select {
	case err := <-done:
		var de *Error
		if !errors.As(err, &de) || de.Reason != ReasonAttachNotFound {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("LoadFile blocked on a FIFO")
	}
	if b, _, err := LoadFile("attachments[1]", dir, "ok.txt", 1<<20); err != nil || string(b) != "hallo" {
		t.Fatalf("regular file: %q %v", b, err)
	}
}
