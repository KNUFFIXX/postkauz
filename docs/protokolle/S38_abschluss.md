# S3.8 Abschluss von Schnitt 3 (0.3.0-rc1)

Stand 29.10.2026. Auftrag: Prüfsitzung DRACO-PK10 nach dem Merge von T5 (`main` = `24d70a7`), Umfang nach `auftraege/A03_s3.md` Teil C, Zeile S3.8; laut `auftraege/A03_teile.md` übernimmt der zuletzt zusammengeführte Teil (T5) den Abschluss. Zweig `claude/determined-fermi-1g8w63`. **Kein Tag** (`v0.3.0-rc1` setzt der Betreiber nach D10), kein GitHub-Release, nichts nach `main`.

## 1. Was gemacht wurde

1. **Doku auf Stand S3.8** (Commit `65ac6a9`, kein Code):
   - `architektur.md`: Stand, Komponenten-Diagramm (HTTP-MCP, Dienste und Jobs von Schnitt 3), Paketübersicht (Zeile `internal/flock` neu, Stand-Spalte für `internal/actions`, `internal/tags`, `internal/retention` ergänzt), Abschnitt 7 (Export, Sicherung, Löschkonzept ohne Zeile in `jobs`; Prozesse `sync --watch --all` und `mcp --http`, Selbstschutz unter Linux). Abschnitt 4 (Provider mit `SetFlags`, `Move`, `Trash`, kein Expunge) war schon auf Stand.
   - `sicherheit.md`: Stand; Abschnitt 5 Tabelle um Postfach-Aktionen (T2) und Export/Sicherung/Löschkonzept (T3); Abschnitt 6 Aufbewahrung berichtigt (Rotation umgesetzt); Abschnitt 10 Netz mit `mcp --http`. Abschnitte 2, 3, 4 und 8 waren auf Stand (T2 bis T5).
   - `mcp.md`: Stand (24 Tools, gegen `internal/mcp/tools.go` gezählt), S3-Tabelle „umgesetzt in T2 und T3“, Transport HTTP umgesetzt.
   - `cli.md`: Stand, Zeile `mcp --http` in der Kommandotabelle. Abschnitte 2k bis 2n, 4 (Exit-Codes) und 5 (`[http]`) waren auf Stand.
   - `datenmodell.md`: Stand Schema 22; Plan: T5 ohne Migration.
   - `betrieb.md`: Stand-Zeile (Abschnitte je Teil).
   - README: Status 0.3.0-rc1 mit dem Stand der Abnahmen (englisch und Kurzfassung deutsch), Abschnitte „Tags and mailbox actions“, „Remote access (HTTP-MCP)“ (systemd-Credential, Empfehlung systemd ≥ 257), „Backup“ geprüft, Roadmap-Zeile S3.
   - `entscheidungen.md`: ADR-010 Status (D8 Teile A bis C bestanden, Teil D offen), Nachtrag ADR-003 (Löschkonzept umgesetzt), Nachtrag ADR-004 (Audit `started` in Schnitt 3, Ausnahmen `restore` und `keys.authenticate`).
2. **Roadmap und CHANGELOG** (Commit `502da91`): Stand S3.8, „Als Nächstes“ neu (Gegenprüfung, D10, D8 Teil D mit VPS, D9 zurückgestellt, D7 Teil R über die THV-Sitzung), die acht früheren offenen Fragen mit ihrer Standardantwort geschlossen (Entschiedene Fragen 68), drei neue Fragen mit Default; CHANGELOG-Abschnitt `0.3.0-rc1` mit Kurzfassung (die Einzeleinträge stehen darunter; sie enthalten auch Schnitt 2, der als 0.2.0-rc1 bis rc8 ohne eigenen Abschnitt veröffentlicht wurde).
3. **Abnahme-Dateien geprüft, nicht neu geschrieben:** D7 und D7a (`-ExpectVersion` Standard `0.3.0-alpha.7c`), D8 (`0.3.0-alpha.8c`) passen zu ihren Abnahmeläufen. D9 nennt als Standard noch `0.3.0-ms.1` (Fund 2).

## 2. Funde (kein BLOCKER, nur Roadmap)

1. **`dist/` ohne Compose-Dateien (KLEIN):** Das Kriterium von S3.8 nennt „Compose-Dateien“ im Release-Paket; `make release` legt sie nicht hinein (ebenso nicht die Units `postkauz-audit-export.service`/`.timer`). Ohne Folge für den Betrieb: Der Docker-Weg baut das Image ohnehin aus dem Repository (`betrieb.md` 4.7), der Timer ist in `betrieb.md` 1.8 beschrieben. Laut Auftrag kein neuer Code; Vorschlag für die nächste Runde: im Ziel `release` `deploy/docker/docker-compose*.yml`, `deploy/docker/Dockerfile` und die beiden Audit-Units nach `dist/` kopieren und in `SHA256SUMS` aufnehmen (rund fünf Zeilen im Makefile).
2. **D9-Skript nennt `0.3.0-ms.1` (KLEIN):** beim Nachholen `-ExpectVersion 0.3.0-rc1` übergeben; das Skript erst dann angleichen.

## 3. Prüfläufe

| Lauf | Ergebnis |
|---|---|
| `make check` | grün vor den Commits (`65ac6a9`, `502da91`; nur Doku geändert) |
| `make race` | grün an `502da91`, einmal am Ende, ohne Testcache: 39 Pakete ohne Datenrennen, 24:24 min gesamt; `internal/cli` **1.415 s (23:35 min, Grenze 45 min)** |
| `make integration` | `IT=dovecot,stalwart,greenmail,authelia` an `502da91` (Images über `mirror.gcr.io` vom Lauf vorher): 40 PASS, 1 SKIP (`TestIntegrationMailboxActions/greenmail`, bekannte Reihenfolge-Abhängigkeit), 1 FAIL `TestIntegrationAuthelia`, weil die Setup-Kette vor `make testauth-up` abbrach: `stalwart-setup.sh` fand das Admin-Passwort nicht im Log (das Stalwart-Volume stammte aus dem vorigen Lauf, Stalwart nennt das Passwort nur beim ersten Start; Konto und Domain waren vorhanden, die Stalwart-Tests liefen grün). Nachgeholt: `make testauth-up`, dann `TestIntegrationAuthelia` PASS; `TestIntegrationCapabilitiesAndRoles` und danach `TestIntegrationMailboxActions/greenmail` PASS. Ergebnis wie bei alpha.8c: 41 PASS plus GreenMail einzeln. Der Abbruch von `stalwart-setup.sh` bei einem wiederverwendeten Volume ist eine Eigenheit der Testumgebung (Roadmap: GreenMail-/Testserver-Reihenfolge) |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make vuln` | nicht in der Cloud (`vuln.go.dev` 403); Prüfsitzung |
| `doctor` mit 0.3.0-rc1 | frisches Datenverzeichnis, Konfiguration mit `[paths] export_dir` und `[http]` im Modus `oauth` mit einem Principal: `schema: 22 (program knows 22)`, `export: <tmp>/export [paths.export_dir]`, `http: mcp --http on 127.0.0.1:8787, auth oauth, resource https://pk.example.org/mcp, issuer https://auth.example.org, 1 principal [http]` (das Subjekt der Zuordnung erscheint nicht), Exit 0 |

## 4. Build 0.3.0-rc1

Gebaut an **`502da91`** (enthält den ganzen Code; danach nur dieses Protokoll) in einem frischen Klon ohne Tags (`git clone --no-tags --branch claude/determined-fermi-1g8w63`, 0 Tags, sauberer Arbeitsbaum), Nachbau in einem zweiten Klon ohne Tags: `SHA256SUMS` gleich (ohne die Zeile `VERSION`, die die Bauzeit trägt). `go version -m`: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261029134736-502da91580d3`, `vcs.revision=502da91580d3d32f88a59efc79f38d5ab7d9ea98`, `vcs.modified=false`. Go 1.27.3, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w"`. `postkauz version` → `postkauz 0.3.0-rc1 (go1.27.3 linux/amd64)`.

`dist/`: drei Programme, `Register-AuditExport.ps1`, `Register-SyncWatch.ps1`, `Run-SyncWatch.ps1`, `check-example.ps1`, `check-example.sh`, `postkauz-sync@.service`, `postkauz-mcp-http.service`, `VERSION`, `SHA256SUMS` (Compose-Dateien fehlen, Fund 1).

```
06ddbaef1ec9dc0b9d1baa463c27de55ca184b6827e0841f5f58941d353e841b  postkauz-linux-amd64
e307fad66ae345578f7f0bb23e2a738f98099a4f4002af446703dc123fb4cc13  postkauz-windows-amd64.exe
7790391a262cab6aa5ac08afb5b2a23266f115457100e197b69252e874890fb9  postkauz-darwin-arm64
26043f606d050fd1095f31019e1a1e3d3ce68bdde402ab4b2b5ce1fe17abcdef  Register-AuditExport.ps1
8a50ee74c064cc731446ed6f224d1d3ba8247d7abcea6cb9da834f2e40ec91c1  Register-SyncWatch.ps1
6973c8473a8a244172530c9e926d4d2b854b2bbee05bc013bde1c700bcdf3de3  Run-SyncWatch.ps1
9d40eb8aaef6b4c2cb68a8eea02651823ead6ac87c0a4917b29d9d3fc6a785e9  check-example.ps1
4bf639c3a3a5e3dd66c759ac21d8b1f0b1be6e7174bf67072bfcad01ecececc5  check-example.sh
84af4fb2c005c1655cfbb22dd680aa6b286137ac9fe044ff3684b61c4aa63408  postkauz-sync@.service
8139650755ceaaa96172246f199f98eead97a6941986e3d1fa2d850db86946ae  postkauz-mcp-http.service
```

Die Programme unterscheiden sich von 0.3.0-alpha.8c nur durch die Versionskennung und den Commit; seit `a641160` hat sich am Programmcode nichts geändert (nur ein Test, Doku und ein Kommentar in der MCP-Unit).

## 5. Haltepunkt

Gegenprüfung durch die Prüfsitzung (Doku, Nachbau, `make vuln`), danach D10 beim Betreiber (Offene Fragen 1 bis 3 in `docs/roadmap.md`).

## 6. Gegenprüfung (Prüfsitzung DRACO-PK10, 29.10.2026)

1. Diff `24d70a7..dce96df`: nur Doku (11 Dateien, kein Code, keine Unit, kein Skript). Stichproben gegen den Code: 24 Tools in `internal/mcp/tools.go`, `policy.MaxTargets` 1000 und `DefaultBulkConfirmThreshold` 50 (ab 51 Zielen `confirm_count`), `[retention] blob_gc_min_age`, offene Starts älter als 10 Minuten (`AuditOpenStarts`): stimmen. **Kein BLOCKER, kein WICHTIG.** Die Funde 1 und 2 oben bleiben KLEIN; Fund 1 (Compose-Dateien und Audit-Units fehlen in `dist/`) weicht vom Kriterium der Zeile S3.8 ab und geht als Hinweis in die Entscheidung D10.
2. Nachbau `0.3.0-rc1` an `502da91` in einem Klon ohne Tags: alle zehn Zeilen von `SHA256SUMS` (ohne `VERSION`) gleich, Windows `e307fad6…`, Linux `06ddbaef…`, darwin `7790391a…`.
3. `make vuln` an `502da91`: govulncheck v1.9.0 ohne Fund.
4. Abnahme D8 Teil A mit `0.3.0-rc1` (`-ExpectVersion 0.3.0-rc1`) auf dem Büro-PC: **29 OK, 0 FAIL, 0 SKIP**.
