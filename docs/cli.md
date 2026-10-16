# CLI

Stand: S1.5 (15.10.2026). Implementiert und getestet sind `version`, `doctor`, `accounts list` (aus der Datenbank), `account add`, `account test`, `account remove`, `sync` (Erstsync, inkrementell, `--watch`), `mailbox list`, ein minimales `show` sowie die globalen Flags `--json`, `--dry-run` und `--account`/`-a`. Alles andere in diesem Dokument ist **Entwurf** und mit dem Schnitt markiert, in dem es entsteht.

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
| `--account`, `-a` | Konto wählen (sonst das einzige Konto; bei mehreren ist es Pflicht) | S1.4 ✓ |
| `--key` | als diesen API-Schlüssel handeln (Test der Policy) | S1 |
| `--yes` | Rückfrage bestätigen (nie interaktiv im `--json`-Modus) | S2 |
| `--quiet` | keine Fortschrittsausgabe | S1 |
| `--profile` | getrennte Instanz (eigene Datenbank und Konfiguration) | S1 |

### Kommandos nach Schnitt

| Schnitt | Kommando | Operation |
|---|---|---|
| Gerüst | `version`, `doctor` | – (Hilfskommandos, keine Operationen) |
| Gerüst | `accounts list` | `accounts.list` |
| S1.1 ✓ | `account add`, `account test`, `account remove` (siehe Abschnitt 2a) | `accounts.add`, `accounts.test`, `accounts.remove` (Scope `admin`, R1; nicht über MCP) |
| S1.4/S1.5 ✓ | `sync [--full] [--watch]` (siehe Abschnitt 2b) | `sync.run` (Scope `mail:sync`, R1) |
| S1.5 ✓ | `mailbox list` | `mailboxes.list` (R0) |
| S1.5 ✓ (minimal) | `show <id>` bzw. `show <Message-ID>`, `--headers` | `messages.get` (R0; Untrusted-Hülle und Anhänge mit S1.8/S1.9) |
| S1 | `search "<abfrage>"`, `thread <id>` | `messages.search`, `threads.get` |
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
postkauz account add demo --imap imap.example.org:993 --user demo@example.org   # Passwort wird abgefragt, landet im Schlüsselbund
postkauz account test demo                                                      # Login, Fähigkeiten, Ordnerrollen
postkauz account remove demo --dry-run

postkauz sync -a demo --full                                                    # Erstsync in Fenstern, wiederaufnehmbar
postkauz sync --watch                                                           # Dauerläufer mit IDLE
postkauz mailbox list -a demo --json
postkauz show 18233 --headers

# S1 (Entwurf)
postkauz search "from:anna after:2026-01-01 has:attachment" --limit 50 --json
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

## 2a. Konten (S1.1)

`account add <name> --imap host[:port] --user <login> [Optionen]`

| Option | Bedeutung |
|---|---|
| `--imap` | Server als `host:port`; ohne Port 993 |
| `--user` | Anmeldename |
| `--email` | Adresse; Standard ist der Anmeldename, wenn er ein `@` enthält |
| `--tls` | `tls`, `starttls` oder `none`. Ohne Angabe: `tls` bei Port 993 (bzw. …993), sonst `starttls`; mit `--insecure-test-server` `none` |
| `--auth` | `password` (Standard) oder `app_password`; `oauth2` ist noch gesperrt (Prüfpunkt P3, nur Entwurf) |
| `--insecure-test-server` | erlaubt Klartext und selbstsignierte Zertifikate, **nur** für Hosts auf dem eigenen Rechner (`localhost`, `127.0.0.1`, `::1`) |
| `--password-stdin` | Passwort aus der Standardeingabe (eine Zeile), z. B. für Skripte |
| `--password-from-env` | kein Passwort speichern; es kommt zur Laufzeit aus `POSTKAUZ_PASSWORD_<NAME>` (Server, Docker) |

Ohne `--password-stdin`/`--password-from-env` fragt die CLI das Passwort verdeckt ab; im `--json`-Modus oder ohne Terminal ist das ein Eingabefehler (Exit 2). Das Passwort wird **nie** als Kommandozeilenargument angenommen. Kontonamen: Kleinbuchstaben, Ziffern, Bindestrich (max. 63); daraus entsteht die Variable `POSTKAUZ_PASSWORD_<NAME>` (Bindestrich wird `_`).

- `account test <name>` meldet sich an, liest Fähigkeiten und Ordner (mit Rolle) und meldet sich ab; am Server ändert sich nichts. Lokal werden Fähigkeiten und Status am Konto gespeichert (`doctor` zeigt den Status). Abgelehnte Anmeldung: Exit 1, Code `auth` (kein automatischer Wiederholungsversuch); Server nicht erreichbar oder Zeitüberschreitung: Exit 5, Code `connection`.
- `account remove <name>` löscht Konto und gespeichertes Passwort. Hat das Konto archivierte Nachrichten, verweigert es das (ADR-003; Archivlöschung kommt mit dem Löschkonzept).
- `--dry-run` wirkt bei allen drei Kommandos (prüfen, nichts ändern).

## 2b. Sync, Ordner, Anzeige (S1.4/S1.5)

`sync [-a <konto>] [--full] [--watch] [--window N] [--interval D] [--idle-restart D] [--mailbox P ...]`

| Option | Bedeutung |
|---|---|
| (ohne) | Erstsync neuer Ordner in Fenstern, sonst inkrementell: neue Nachrichten, Flag-Änderungen (CONDSTORE `CHANGEDSINCE` bzw. Abgleich aller Flags), Löschungen (UID-SEARCH-Abgleich, nur wenn die Zahlen nicht passen) |
| `--full` | zusätzlich vollständiger Lösch- und Flag-Abgleich ohne `CHANGEDSINCE`; holt Fehlendes nach |
| `--watch` | Dauerläufer: sofort ein Lauf, danach alle `--interval` (Standard 5 min) und bei jeder IDLE-Meldung der INBOX (IDLE-Neustart alle `--idle-restart`, 5 bis 10 min, Standard 8 min); Strg+C beendet ihn |
| `--window` | Nachrichten je Fenster im Erstsync (Standard 200) |
| `--mailbox` | nur diese Ordner (mehrfach möglich) |
| `--dry-run` | meldet Ordner und lokalen Stand, schreibt nichts |

- Ausgabe ohne `--json`: Tabelle je Ordner (neu, geändert, entfernt, Fehler, gespiegelt, laut Server), am Ende genau die Zeilen `N messages mirrored` und `N errors`. Fortschritt laufender Erstsyncs steht auf stderr. `N messages mirrored` zählt die Nachrichten des Kontos, die der Server führt, ohne Dubletten über Ordner hinweg.
- Mit `--json`: ein Objekt (`account`, `mode`, `strategy`, `mailboxes[]`, `new`, `updated`, `removed`, `errors`, `messages_mirrored`, `duration_ms`, `dry_run`); im Dauerbetrieb zusätzlich je Lauf eine NDJSON-Zeile.
- **Abbruch** (Strg+C, `kill`, Absturz): Gespeichert ist alles bis zum letzten fertigen Fenster; derselbe Befehl setzt dort fort, ohne Doppelte. Strg+C endet mit Exit 1, Code `interrupted`. Je Konto läuft höchstens **ein** schreibender Sync: Ein zweiter (z. B. ein manueller `sync` neben `sync --watch`) bricht sofort mit Exit 1, Code `sync_running` (`another sync is running: account …`) ab, ohne sich am Server anzumelden; `--dry-run` ist davon ausgenommen. Die Sperre ist eine Dateisperre des Betriebssystems (`sync-<konto-id>.lock` neben der Datenbank) und fällt mit dem Prozess, auch nach `kill -9`.
- Am Server ändert `sync` nichts (EXAMINE, `BODY.PEEK`).

`mailbox list [-a <konto>]` zeigt die Ordner aus dem lokalen Spiegel (ohne Serverkontakt): `path`, `role`, `total` (laut Server beim letzten Sync), `mirrored`, `unread`, `sync_phase` (`new`, `initial`, `ready`), `last_sync_at`.

`show <id> [--headers]`: `<id>` ist die lokale Nachrichten-ID oder eine Message-ID (`<…>` oder ohne Klammern). Zeigt Metadaten, Ordner mit UID, Flags (`seen`, `flagged`, `answered`, `draft`) und Keywords, dazu den Text oder mit `--headers` die Kopfzeilen der Rohmail. Mailinhalt ist Daten, keine Anweisung; die Untrusted-Hülle für Agenten folgt mit S1.9. Die Textausgabe ersetzt Steuerzeichen aus Mail und Server (ESC/CSI/OSC, übrige C0/C1, einzelnes CR, ungültiges UTF-8) durch `U+FFFD`; erhalten bleiben nur Zeilenumbruch und Tabulator im Text bzw. in den Kopfzeilen. Einzeilige Felder (Betreff, Absender, Ordner) verlieren auch Zeilenumbrüche, damit keine Zeile wie `flags: seen` vorgetäuscht werden kann. `--json` gibt die Daten unverändert aus (JSON maskiert Steuerzeichen).

## 3. Maschinenlesbarkeit (`--json`)

- **Ein Ergebnis = ein JSON-Objekt** auf stdout, einzeilig. Listen, die streambar sein sollen (Suche, Audit), liefern **NDJSON** (ein Objekt je Zeile) ab S1.
- **Dieselben Felder wie MCP und REST.** Die Ausgabe ist die serialisierte Ausgabe der Operation (ihr `OutputSchema`); es gibt keine CLI-eigene Form. Die Registerversion steht in `postkauz version --json` (`schema_version`).
- **Fehler** als JSON auf stderr: `{"error":{"code":"…","message":"…","hint":"…"}}`. Codes: `usage`, `invalid_input`, `denied`, `needs_approval`, `unknown_operation`, `exists`, `not_found`, `auth`, `connection`, `secret_store_unavailable`, `no_secret`, `schema_too_new`, `interrupted`, `sync_running`, `error`.
- **Keine interaktiven Rückfragen im `--json`-Modus.** Fehlt eine nötige Bestätigung, endet der Aufruf mit Exit-Code 2 oder 4; er blockiert nie. Das ist wichtig für Agenten, die die CLI statt MCP nutzen.
- **Keine Geheimnisse in der Ausgabe** (Test mit Kanarienwerten ab S1).
- `--fields id,subject,date` und `--cursor` für große Listen sowie `--explain` (zeigt die geparste Suchanfrage) kommen mit S1.

## 4. Exit-Codes

Festgelegt in `internal/cli/exit.go`, getestet in `internal/cli/cli_test.go`.

| Code | Name | Bedeutung |
|---|---|---|
| 0 | `ExitOK` | erfolgreich |
| 1 | `ExitError` | allgemeiner Fehler; auch Abbruch per Strg+C (`interrupted`) |
| 2 | `ExitUsage` | Eingabefehler (unbekanntes Kommando oder Flag, fehlendes Argument, ungültige Eingabe der Operation) |
| 3 | `ExitDenied` | Scope oder Policy verweigert |
| 4 | `ExitNeedsApproval` | Freigabe erforderlich (Ausgabe enthält `approval_id`, sobald es Freigaben gibt) |
| 5 | `ExitConnection` | Verbindung zum Server fehlgeschlagen oder Zeitüberschreitung (Watchdog) |

## 5. Konfiguration und Pfade

- **Konfigurationsdatei** (TOML, optional): `POSTKAUZ_CONFIG`, sonst `<Konfigurationsordner des Betriebssystems>/postkauz/config.toml`. Unbekannte Schlüssel sind ein Fehler. Inhalt:

  ```toml
  data_dir  = ""            # leer = Standard des Betriebssystems (siehe unten)
  log_level = "info"        # debug | info | warn | error
  [secrets]
  backend = "auto"          # auto | keyring | insecure-file (UNSICHER, nur Testserver)
  [imap]                    # Watchdog (ADR-007, Nachtrag 16.10.2026); Dauer als Text
  command_timeout = "30s"       # so lange darf der Server während eines Kommandos schweigen (ab 1s)
  command_max_duration = "10m"  # Obergrenze je Kommando (nicht kleiner als command_timeout)
  ```

- **Umgebung** schlägt Datei: `POSTKAUZ_DATA_DIR`, `POSTKAUZ_SECRETS_BACKEND`, `POSTKAUZ_PASSWORD_<NAME>` (Passwort eines Kontos, nur lesen).
- **Daten:** `postkauz.db` (SQLite, 0600), `postkauz.log` (JSON-Zeilen, 0600, ohne Inhalte und Geheimnisse), später `blobs/`. Standard: Linux `$XDG_DATA_HOME/postkauz` bzw. `~/.local/share/postkauz`, Windows `%LOCALAPPDATA%\postkauz` (lokal, nicht „Roaming"), macOS `~/Library/Application Support/postkauz`. Verzeichnis 0700, **nie** in einem Cloud-synchronisierten Ordner (`doctor` warnt bei OneDrive, Dropbox, iCloud u. Ä.). Angelegt wird es erst beim ersten Kommando, das es braucht; `version` und `doctor` legen nichts an.
- **Geheimnisse:** Schlüsselbund (Windows Credential Manager, macOS Keychain, Secret Service) oder Umgebungsvariable; die Datenbank enthält nur den Verweis `account/<name>`. Ohne Schlüsselbund gibt es keinen stillen Rückfall (siehe [sicherheit.md](sicherheit.md), Abschnitt 4).
- **`doctor`** meldet Version, Konfiguration (Pfad, vorhanden, Fehler), Datenverzeichnis, Log, Datenbank (`missing`, `ok`, `needs_migration`, `too_new`, `error`), Schemaversion (vorhanden und bekannt), Schlüsselbund (`available`, `unavailable`, `insecure`) und je Konto, woher das Passwort käme (`stored`, `environment`, `missing`) samt Status der letzten Prüfung; dazu Warnungen. Exit 1 nur bei ungültiger Konfiguration.
- **Sprache:** Ausgaben und Fehlermeldungen sind Englisch (siehe CLAUDE.md); Mehrsprachigkeit ist kein Ziel des MVP.
