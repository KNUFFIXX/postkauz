// Package procguard schützt Prozesse mit Postfachzugangsdaten vor dem
// Auslesen durch andere Prozesse desselben Nutzers (Nachbesserung T5,
// Punkt 6). Auf einem VPS laufen sync --watch (mit Passwort in der
// Umgebung) und mcp --http (ohne) als derselbe Nutzer, weil PostKauz
// Datenbank und Blobs nur für den Eigentümer freigibt. Ohne Schutz könnte
// der HTTP-Dienst /proc/<pid>/environ des Sync-Dienstes lesen oder ihn
// per ptrace untersuchen.
//
// Unter Linux setzt DenyInspection PR_SET_DUMPABLE auf 0: /proc/<pid>/environ,
// /proc/<pid>/mem und ptrace sind dann nur noch für root (CAP_SYS_PTRACE)
// zugänglich, und es entsteht kein Core-Dump mit dem Passwort. Das wirkt
// unabhängig von der systemd-Version. Auf anderen Systemen tut es nichts
// (Windows: Sync und HTTP-MCP laufen unter demselben Benutzer, der Schutz
// liegt dort nur im Code; docs/betrieb.md 4.9).
package procguard

// Supported meldet, ob DenyInspection auf diesem System wirkt.
const Supported = supported

// DenyInspection schützt den laufenden Prozess (siehe Paketkommentar).
// Ein Fehler heißt: Der Schutz ist nicht gesetzt; der Prozess läuft
// trotzdem (der Aufrufer meldet es im Log).
func DenyInspection() error { return denyInspection() }

// Inspectable meldet, ob der laufende Prozess noch auslesbar ist (nur für
// Tests und doctor; auf Systemen ohne Schutz immer true).
func Inspectable() (bool, error) { return inspectable() }
