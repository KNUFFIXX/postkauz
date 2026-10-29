//go:build windows

package blob

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isTransientLockErr meldet Fehler beim Öffnen oder Sperren von gc.lock,
// die unter Windows meist nur kurz anhalten, weil ein anderes Programm
// (Virenscanner, Indexdienst, Sicherungsprogramm) die Datei ohne Freigabe
// zum Mitbenutzen offen hält: ERROR_SHARING_VIOLATION, ERROR_LOCK_VIOLATION
// und ERROR_ACCESS_DENIED (Nachbesserung 3, Punkt 8). Alle anderen sind
// dauerhaft.
func isTransientLockErr(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) ||
		errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_ACCESS_DENIED)
}
