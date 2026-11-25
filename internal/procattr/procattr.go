// Package procattr setzt die Startattribute der Hilfsprogramme des
// Textauszugs (A12 Teil 3 (e)): unter Windows ohne eigenes Konsolenfenster
// (CREATE_NO_WINDOW, HideWindow) wie der Prüf-Hook (internal/hook,
// proc_windows.go). Indiz aus dem Betrieb: Exit-Code 0xC0000142 tritt auch
// auf, wenn ein Konsolenprogramm aus einer Hülle ohne Konsole startet.
// Unter Unix ändert Prepare nichts.
package procattr
