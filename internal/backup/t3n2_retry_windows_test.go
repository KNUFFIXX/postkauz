//go:build windows

package backup

import (
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

// T3-Nachbesserung 2, Punkt 13: Unter Windows gelten ERROR_ACCESS_DENIED
// und ERROR_SHARING_VIOLATION (auch eingepackt wie von os.Rename und
// os.Remove) als vorübergehend, ERROR_DIR_NOT_EMPTY nicht. Nur unter
// Windows (kompiliert, im Container nicht ausgeführt).
func TestT3N2TransientFSErrOnWindows(t *testing.T) {
	for err, want := range map[error]bool{
		&os.PathError{Op: "remove", Path: `C:\x`, Err: windows.ERROR_SHARING_VIOLATION}:         true,
		&os.LinkError{Op: "rename", Old: `C:\a`, New: `C:\b`, Err: windows.ERROR_ACCESS_DENIED}: true,
		&os.PathError{Op: "remove", Path: `C:\x`, Err: windows.ERROR_DIR_NOT_EMPTY}:             false,
		&os.PathError{Op: "remove", Path: `C:\x`, Err: windows.ERROR_FILE_NOT_FOUND}:            false,
	} {
		if got := isTransientFSErr(err); got != want {
			t.Errorf("isTransientFSErr(%v) = %v, want %v", err, got, want)
		}
	}
}
