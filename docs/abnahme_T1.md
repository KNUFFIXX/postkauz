# Abnahme T1: Härtung, Audit, Jobs, Policy (Entwurf)

Stand: 27.10.2026 (Nachbesserung T1a, Build 0.3.0-alpha.1b). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Haltepunkt von T1b. **Die Schritte ab 1 schreibt T1b** (Skript `abnahme_T1.ps1` nach Rahmen 13); hier steht vorab nur Schritt 0, weil er schon für jeden Test eines T1a-Builds an einer Kopie gilt und vor dem späteren Update der echten Datenbank Pflicht ist.

## 0. Vor dem Update: alle Prozesse von 0.2.0-rc8 stoppen (Pflicht)

**Warum:** Ab 0.3.0-alpha.1b hält jeder PostKauz-Prozess eine gemeinsame Sperre neben der Datenbank (`postkauz.db.lock`), und eine neue Version migriert nur, wenn kein anderer Prozess die Datenbank offen hat (sonst `migration_blocked`, nichts geändert). **0.2.0-rc8 kennt diese Sperre nicht.** Ein laufender MCP-Server oder Audit-Export von rc8 würde die Migration nicht verhindern und danach mit dem neuen Schema weiterarbeiten. Deshalb vor dem ersten Start der neuen Version von Hand stoppen:

| Nr. | Was | Wie (Windows) | Erwartet |
|---|---|---|---|
| 0.1 | Aufgabe `sync --watch` | Start → „Aufgabenplanung“ öffnen → links „Aufgabenplanungsbibliothek“ → Aufgabe des Watchers anklicken → rechts „Beenden“, dann „Deaktivieren“ | Spalte „Status“: „Deaktiviert“ |
| 0.2 | Aufgabe Audit-Export | dort Aufgabe „PostKauz audit export“ → rechts „Deaktivieren“ | Spalte „Status“: „Deaktiviert“ |
| 0.3 | Claude-App (startet den MCP-Server `postkauz mcp`) | Claude-App schließen, dann im Infobereich unten rechts (Pfeil „^“) Rechtsklick auf das Claude-Symbol → „Beenden“ | kein Claude-Symbol mehr im Infobereich |
| 0.4 | Kontrolle: kein PostKauz-Prozess mehr | PowerShell: `Get-Process postkauz -ErrorAction SilentlyContinue` | keine Ausgabe |

Erst dann die neue Programmdatei einsetzen und starten. Nach dem Update (bzw. nach dem Test an der Kopie) die Aufgaben wieder aktivieren (Aufgabe anklicken → rechts „Aktivieren“) und die Claude-App starten.

Für einen T1a-Build an einer **Kopie** der Datenbank (Rahmen 5: nie die Live-Datenbank) gilt Schritt 0 genauso, wenn die Kopie mit einem eigenen Datenverzeichnis läuft: dann reicht 0.4 für Prozesse, die auf dieses Verzeichnis zeigen. Die echte Datenbank fasst ein Build, der nicht von `main` stammt, nie an.
