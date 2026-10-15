# CLI

Stand: Phase 0. Implementiert und getestet sind `version`, `doctor` (Stub), `accounts list` sowie die globalen Flags `--json` und `--dry-run`. Alles andere in diesem Dokument ist **Entwurf** und mit dem Schnitt markiert, in dem es entsteht.

## 1. Befehlsstruktur

```
postkauz <objekt> <verb> [ziel] [optionen]        Alias: pk
```

Jedes Kommando, das eine Operation des Registers ausführt, ist in `internal/cli/bindings.go` ausdrücklich an diese Operation gebunden (Tabelle, keine Ableitung). Ohne Argumente zeigt `postkauz` die Hilfe (ein interaktiver Modus ist nicht geplant).

Der Alias `pk` ist eine Kopie oder Verknüpfung der Binärdatei (`make install` legt sie an); cobra kennt für die Wurzel keine Aliase.

### Globale Optionen

| Option | Bedeutung | Stand |
|---|---|---|
| `--json` | maschinenlesbare Ausgabe; Fehler als JSON auf stderr | Gerüst |
| `--dry-run` | beschreiben, was geschähe, nichts verändern | Gerüst |
| `--account`, `-a` | Konto wählen (sonst das einzige/Standardkonto) | S1 |
| `--key` | als diesen API-Schlüssel handeln (Test der Policy) | S1 |
| `--yes` | Rückfrage bestätigen (nie interaktiv im `--json`-Modus) | S2 |
| `--quiet` | keine Fortschrittsausgabe | S1 |
| `--profile` | getrennte Instanz (eigene Datenbank und Konfiguration) | S1 |

### Kommandos nach Schnitt

| Schnitt | Kommando | Operation |
|---|---|---|
| Gerüst | `version`, `doctor` | – (Hilfskommandos, keine Operationen) |
| Gerüst | `accounts list` | `accounts.list` |
| S1 | `account add`, `account test`, `account remove` (`account add --insecure-test-server` erlaubt Klartext/selbstsignierte Zertifikate, nur für localhost-Testserver) | `accounts.add` … |
| S1 | `sync [--full] [--watch]` | `sync.run` |
| S1 | `mailbox list` | `mailboxes.list` |
| S1 | `search "<abfrage>"`, `show <id>`, `thread <id>` | `messages.search`, `messages.get`, `threads.get` |
| S1 | `attach list`, `attach text` | `attachments.list`, `attachments.text` |
| S1 | `import mbox`, `import maildir` | `import.run` |
| S1 | `key create`, `key list`, `key revoke` | `keys.*` (nur Mensch, Scope `admin`) |
| S1 | `audit [--since]`, `audit verify` | `audit.list`, `audit.verify` |
| S1 | `mcp` | startet den stdio-MCP-Server (kein Registerbefehl) |
| S2 | `draft new`, `draft reply`, `draft forward`, `draft list`, `draft show`, `draft edit` | `drafts.*` |
| S2 | `approve <id>`, `reject <id>` | `approvals.decide` |
| S3 | `tag`, `mark`, `move`, `trash`, `attach save` | `messages.*`, `attachments.save` |
| S3 | `export`, `backup`, `restore` | `export.run`, `backup.run`, `restore.run` |

Es gibt **kein** `send`, kein `rule` und kein `serve --rest` vor Schnitt 4.

## 2. Beispiele

Mit „(Entwurf)" markierte Beispiele laufen erst im genannten Schnitt.

```sh
# Heute lauffähig
postkauz version --json
postkauz doctor
postkauz accounts list --json

# S1 (Entwurf)
postkauz account add demo --imap imap.example.org:993 --user demo@example.org   # Passwort wird abgefragt, landet im Schlüsselbund
postkauz account test demo                                                      # Login, Fähigkeiten, Ordnerrollen
postkauz sync -a demo --full                                                    # Erstsync in Fenstern, wiederaufnehmbar
postkauz sync --watch                                                           # Dauerläufer mit IDLE
postkauz search "from:anna after:2026-01-01 has:attachment" --limit 50 --json
postkauz show 18233 --headers
postkauz thread 4411 --strip-quotes
postkauz import maildir ./Mail/Archive --account demo --mailbox Archive-2024
postkauz key create --name assistant-1 --role assistant --expires 90d       # Klartext erscheint genau einmal
postkauz audit --since 24h --actor agent --json
postkauz audit verify                                                       # Export-Prüfsummen gegen die Datenbank

# S2 (Entwurf)
postkauz draft reply 18233 --all --body-file antwort.md --quote             # legt den Entwurf in den Entwürfe-Ordner
postkauz draft list --status pending_approval
postkauz approve 17                                                         # Mensch gibt den Entwurf frei (Status ready)
```

Der Mensch sendet den Entwurf in seinem gewohnten Mailclient; PostKauz erkennt den Versand beim nächsten Sync.

## 3. Maschinenlesbarkeit (`--json`)

- **Ein Ergebnis = ein JSON-Objekt** auf stdout, einzeilig. Listen, die streambar sein sollen (Suche, Audit), liefern **NDJSON** (ein Objekt je Zeile) ab S1.
- **Dieselben Felder wie MCP und REST.** Die Ausgabe ist die serialisierte Ausgabe der Operation (ihr `OutputSchema`); es gibt keine CLI-eigene Form. Die Registerversion steht in `postkauz version --json` (`schema_version`).
- **Fehler** als JSON auf stderr: `{"error":{"code":"…","message":"…","hint":"…"}}`. Codes u. a.: `usage`, `denied`, `needs_approval`, `unknown_operation`, `error`.
- **Keine interaktiven Rückfragen im `--json`-Modus.** Fehlt eine nötige Bestätigung, endet der Aufruf mit Exit-Code 2 oder 4; er blockiert nie. Das ist wichtig für Agenten, die die CLI statt MCP nutzen.
- **Keine Geheimnisse in der Ausgabe** (Test mit Kanarienwerten ab S1).
- `--fields id,subject,date` und `--cursor` für große Listen sowie `--explain` (zeigt die geparste Suchanfrage) kommen mit S1.

## 4. Exit-Codes

Festgelegt in `internal/cli/exit.go`, getestet in `internal/cli/cli_test.go`.

| Code | Name | Bedeutung |
|---|---|---|
| 0 | `ExitOK` | erfolgreich |
| 1 | `ExitError` | allgemeiner Fehler |
| 2 | `ExitUsage` | Eingabefehler (unbekanntes Kommando oder Flag, fehlendes Argument) |
| 3 | `ExitDenied` | Scope oder Policy verweigert |
| 4 | `ExitNeedsApproval` | Freigabe erforderlich (Ausgabe enthält `approval_id`, sobald es Freigaben gibt) |
| 5 | `ExitConnection` | Verbindung zum Server fehlgeschlagen (ab S1) |

## 5. Konfiguration und Pfade

- **Konfigurationsdatei** (TOML): `POSTKAUZ_CONFIG`, sonst `<Konfigurationsordner des Betriebssystems>/postkauz/config.toml`. `postkauz doctor` zeigt den Pfad und ob die Datei existiert.
- **Daten** (Datenbank, `blobs/`): Datenverzeichnis des Betriebssystems unterhalb von `postkauz/`, **nie** in einem Cloud-synchronisierten Ordner.
- **Geheimnisse:** nur Schlüsselbund (Windows Credential Manager, macOS Keychain, Secret Service) oder Umgebungsvariablen (`POSTKAUZ_…`); die Konfigurationsdatei enthält nur Verweise.
- **Sprache:** Ausgaben und Fehlermeldungen sind Englisch (siehe CLAUDE.md); Mehrsprachigkeit ist kein Ziel des MVP.
