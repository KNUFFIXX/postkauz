// Package mcp wird der MCP-Server (stdio zuerst, HTTP ab Schnitt 3).
//
// Stand Phase 0: nur der Plan, kein Code. Maßgeblich ist docs/mcp.md.
//
// Plan (Schnitt 1, lesend):
//
//   - Server auf Basis des offiziellen github.com/modelcontextprotocol/go-sdk
//     (aktuelle Version per `go get` auflösen, nicht hier festschreiben).
//   - Transport stdio; der API-Schlüssel kommt aus der Umgebung
//     (POSTKAUZ_KEY) oder einer Datei mit Rechten 600, nie als Argument.
//   - Tools werden aus dem Operationsregister (internal/ops) gebunden, per
//     ausdrücklicher Tabelle „Operation → Tool-Name" (accounts.list →
//     mail_list_accounts), nicht per Reflection. Die Tool-Liste folgt den
//     Scopes des Schlüssels: Was er nicht darf, sieht er nicht.
//   - Jede Antwort mit Mailinhalt wird in die Untrusted-Hülle verpackt
//     ({"untrusted": true, "source": "email", ...}); Quote-Stripper und
//     HTML→Text laufen davor. Links stehen getrennt mit Domain.
//   - Aufrufe laufen ausschließlich über ops.Executor.Execute; dieses Paket
//     enthält keine Fachlogik und keine Policy.
//   - Resources (mail://...) und Prompts folgen in Schnitt 1 bzw. 2.
//   - Es gibt im MVP kein Versand-Tool; mail_request_send ist „ab Schnitt 4".
//
// Vertragstest (ab dem ersten Tool): Schema des Tools == Schema der
// Operation im Register; jede Operation durchläuft die Kette
// Scope → Policy → Freigabe → Audit → Handler.
package mcp
