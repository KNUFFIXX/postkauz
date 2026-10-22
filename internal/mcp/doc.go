// Package mcp ist der MCP-Server von PostKauz (stdio seit S1.9, HTTP ab
// Schnitt 3). Maßgeblich ist docs/mcp.md.
//
//   - SDK: das offizielle github.com/modelcontextprotocol/go-sdk (Version in
//     go.mod, per `go get` aufgelöst).
//   - Der Schlüssel kommt aus der Umgebung (POSTKAUZ_KEY) oder einer Datei
//     mit Rechten 600 (POSTKAUZ_KEY_FILE), nie als Argument; die Anmeldung
//     (ops.AuthenticateKey) macht der Aufrufer (internal/cli, Kommando mcp).
//   - Tools, Resources und Prompts stehen in ausdrücklichen Tabellen
//     (tools.go, resources.go, prompts.go): Operation → Tool-Name, keine
//     Reflection, kein Generator (ADR-004). Die Liste folgt den Scopes des
//     Schlüssels: Was er nicht darf, sieht er nicht.
//   - Jeder Aufruf läuft über ops.Executor.Execute (Scope → Policy → Audit →
//     Eingabeprüfung → Handler) mit Akteur „agent" und dem Schlüssel; dieses
//     Paket enthält keine Fachlogik und keine Policy.
//   - Ausgaben mit Mailinhalt tragen die Untrusted-Hülle der Operation
//     (untrusted, source, notice); zusätzlich entfernt der Adapter aus allen
//     Zeichenketten Steuer- und unsichtbare Formatzeichen (clean.go).
//   - Es gibt im MVP kein Tool zum Senden, Löschen oder Verschieben. Seit
//     S2.8 legt ein Schlüssel mit mail:draft Entwürfe an und legt sie vor
//     (Ablage im Entwürfe-Ordner durch den nächsten Sync, E1); Freigeben
//     und Senden bleiben dem Menschen (notbound.go).
//
// Vertragstest (server_test.go): Schema jedes Tools == Schema der Operation
// (Ausnahme: dokumentierte engere Grenzen), jede gebundene Operation steht
// im Katalog, die Tool-Liste folgt den Scopes, jede Ausgabe mit Mailinhalt
// ist in der Hülle.
package mcp
