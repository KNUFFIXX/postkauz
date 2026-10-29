//go:build !windows

package backup

// isTransientFSErr: Unter macOS und Linux hält kein Virenscanner eine
// Datei so fest, dass Rename oder Remove scheitern; kein Fehler wird
// wiederholt (T3-Nachbesserung 2, Punkt 13).
func isTransientFSErr(error) bool { return false }
