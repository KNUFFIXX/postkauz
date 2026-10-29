//go:build !windows

package blob

// isTransientLockErr: Unter macOS und Linux gibt es kein Öffnen ohne
// Freigabe zum Mitbenutzen; ein Fehler beim Öffnen oder Sperren von
// gc.lock (etwa EACCES, ENOENT, EISDIR, ENOLCK) bleibt, wie er ist, und
// wird nicht wiederholt (Nachbesserung 3, Punkt 8). Eine belegte Sperre
// (flock.ErrLocked) wartet lockWait davon unabhängig ab.
func isTransientLockErr(error) bool { return false }
