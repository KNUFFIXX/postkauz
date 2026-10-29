//go:build !windows && !linux

package backup

// finalPath: Ohne eigene Systemabfrage (macOS und andere) bleibt es bei
// realPath, der Datei-Identität und den aufgelösten Links (insideByID).
func finalPath(string) (string, bool) { return "", false }
