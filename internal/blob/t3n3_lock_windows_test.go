//go:build windows

package blob

import (
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// Nachbesserung 3, Punkt 8: Unter Windows gelten Sharing-, Lock- und
// Access-Violation beim Öffnen von gc.lock (eingepackt wie von os.OpenFile)
// als vorübergehend, andere Fehler nicht. Nur unter Windows (kompiliert, im
// Container nicht ausgeführt).
func TestT3N3TransientLockErrOnWindows(t *testing.T) {
	for err, want := range map[error]bool{
		&os.PathError{Op: "open", Path: `C:\b\gc.lock`, Err: windows.ERROR_SHARING_VIOLATION}: true,
		&os.PathError{Op: "open", Path: `C:\b\gc.lock`, Err: windows.ERROR_LOCK_VIOLATION}:    true,
		&os.PathError{Op: "open", Path: `C:\b\gc.lock`, Err: windows.ERROR_ACCESS_DENIED}:     true,
		&os.PathError{Op: "open", Path: `C:\b\gc.lock`, Err: windows.ERROR_PATH_NOT_FOUND}:    false,
		&os.PathError{Op: "open", Path: `C:\b\gc.lock`, Err: syscall.EISDIR}:                  false,
	} {
		if got := isTransientLockErr(err); got != want {
			t.Errorf("isTransientLockErr(%v) = %v, want %v", err, got, want)
		}
	}
}
