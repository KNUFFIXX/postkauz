# Changelog

Format nach [Keep a Changelog](https://keepachangelog.com/de/1.1.0/); Versionierung nach [SemVer](https://semver.org/lang/de/) (bis 1.0 ohne Kompatibilitätszusage).

## [Unreleased]

### Added

- Repository-Gerüst: Apache-2.0-Lizenz, `NOTICE`, `.gitignore`, `.editorconfig`, `Makefile`, CI (vet, test, Cross-Compile), PII-Gate (`scripts/pii-check.sh`).
- Dokumentation (Deutsch) unter `docs/`: Architektur, Datenmodell, MCP, CLI, Sicherheit, Roadmap, Entscheidungen (ADR-001 bis ADR-006), Testkorpus; `README.md` (Englisch, mit deutscher Kurzfassung), `CLAUDE.md`, `AGENTS.md`, `CONTRIBUTING.md`, `SECURITY.md`.
- Go-Gerüst (kompiliert, getestet): Operationsregister und Executor mit Beispiel `accounts.list`, Policy-Stub (Scopes, Risiko R0–R3), Audit-Typ, Migration `0001_init.sql` (inkl. FTS5) auf `modernc.org/sqlite`, Provider-Interface (`MailProvider`, `Submission`), CLI (`version`, `doctor`, `accounts list`, `--json`, Exit-Codes).
- Testserver-Compose (`deploy/testmail`): Dovecot, Stalwart, GreenMail.
- Auftrag A01 (Go-Anhebung, Spike-Auflagen, Schnitt 1).
- Spike S0 unter `spike/s0/` als eigenständiges Go-Modul (Code, Testserver-Skripte, Korpus-Generator, Rohdaten, `REPORT.md`): **Gate Go bestanden mit Auflagen** (14.10.2026).
- ADR-007 (go-imap v2 beta.8 mit Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Zielversion Go 1.27).

- **S1.1 Konten:** `account add` (Passwort verdeckt abgefragt, per `--password-stdin` oder aus `POSTKAUZ_PASSWORD_<NAME>`), `account test` (Anmeldung, Fähigkeiten, Ordnerrollen; nur lesend am Server), `account remove`; `accounts list` liest die Datenbank.
- Konfiguration (`internal/config`, TOML, `POSTKAUZ_CONFIG`/`POSTKAUZ_DATA_DIR`/`POSTKAUZ_SECRETS_BACKEND`), Datenverzeichnis (0700) mit Datenbank und Log (0600).
- SQLite-Audit-Writer; Audit-Parameter je Operation geschwärzt (nur Feldnamen und Ziele).
- Zugangsdaten (`internal/secrets`): Schlüsselbund über `github.com/zalando/go-keyring`, Umgebungsvariable, unsicherer Testspeicher `insecure-file`; selbstschwärzender Typ `Secret`; Kanarientests.
- `internal/imapx` (Ausschnitt, ADR-007): Verbindung mit Watchdog und TCP-Keepalive, `WordDecoder` mit Charset-Reader, Login, CAPABILITY, LIST; SASL XOAUTH2/OAUTHBEARER und `internal/oauth` als Entwurf (P3).
- `doctor` prüft Pfade, Schlüsselbund, Datenbank, Schemaversion und je Konto die Passwortquelle.
- `make guard`: go-imap nur in `internal/imapx`.
- Neue Abhängigkeiten (per `go get`): `github.com/BurntSushi/toml` v1.6.0 (MIT), `github.com/zalando/go-keyring` v0.2.8 (MIT), `golang.org/x/term` v0.47.0 (BSD), `github.com/emersion/go-imap/v2` v2.0.0-beta.8 (MIT, gepinnt), `github.com/emersion/go-message` v0.18.2 (MIT), `github.com/emersion/go-sasl` (MIT).

- **S1.2:** Blob-Store `internal/blob` (atomar, SHA-256, Deduplizierung, optional zstd, Größenlimit); MIME-Parser `internal/mimeparse` auf `go-message` mit Charset-Reader, tolerantem Datum (Zone ohne Vorzeichen, Ersatz `INTERNALDATE`) und Limits gegen Zip-Bomben und übergroße Teile; Threading `internal/thread` (Union-Find); Korpus-Generator `internal/corpus` mit Manifest, `make corpus`.
- Neue Abhängigkeit (per `go get`): `github.com/klauspost/compress` v1.20.3 (BSD-3-Clause) für zstd; `golang.org/x/text` jetzt direkt genutzt (v0.14.0, unverändert).

- **S1.3:** `internal/imapx` mit `Session` (Watchdog-Abbruch → Close + Reconnect mit exponentiellem Backoff und Jitter; `ErrAuth`/`ErrRejected` ohne Wiederholung), LIST mit SPECIAL-USE und LIST-STATUS, CREATE, NOOP; IMAP-Provider `Connect`/`ListMailboxes` mit Rollen (SPECIAL-USE, Namen, Gmail-Namen, neue Rolle `all`) und Ordnerzustand; Integrationstests gegen Dovecot, Stalwart, GreenMail (`make integration`, nur mit `POSTKAUZ_IT`); `make testmail-setup` richtet Stalwart ein; `make race` und CI-Job `race`; `make guard` prüft den go-imap-Pin.

- **S1.4/S1.5 Sync:** `internal/mailsync` (Erstsync in Fenstern, wiederaufnehmbar über den Cursor je Ordner, eine Transaktion je Fenster, Idempotenz über `(mailbox_id, uid)`; Abgleich per Message-ID und Größe bzw. `content_hash` ohne erneutes Laden; Löschungen per UID-SEARCH-Abgleich nur bei unpassenden Zahlen; Flags per CONDSTORE `CHANGEDSINCE` oder vollem Abgleich; UIDVALIDITY-Wechsel mit Neuzuordnung; `Watch` mit IDLE auf INBOX und Intervall); IMAP-Provider `Changes`, `FetchHeaders`, `FetchRaw`, `Idle`; `imapx` mit EXAMINE/SELECT, UID SEARCH (ESEARCH), FETCH, STATUS, APPEND, STORE (ohne `\Deleted`), IDLE mit eigenem Neustart (5 bis 10 min).
- Operationen `sync.run` (Scope `mail:sync`, R1), `mailboxes.list` und `messages.get` (R0); CLI `sync [--full] [--watch]`, `mailbox list`, `show <id> [--headers]` (minimal), globales `-a/--account`; Strg+C bricht geordnet ab (`interrupted`).
- Migration `0002_sync_state.sql`: Ordnerrolle `all`, Sync-Phase je Ordner, Flags je Ordnerkopie; vor jeder Migration eines Bestands ein Snapshot per `VACUUM INTO`.
- Werkzeug `corpusload` (`make corpus-load`): Korpus per APPEND in einen lokalen Testserver laden; `-mark-seen` für die Abnahme D1.6.
- Integrationstests des Syncs gegen Dovecot, Stalwart, GreenMail (Absturz und Wiederaufnahme, Änderungen von außen, UIDVALIDITY, IDLE); Unit-Test mit echtem `kill -9` eines Kindprozesses.
- P2: Messung mit 50.000 Nachrichten als Nachtrag in `spike/s0/REPORT.md`; `docs/abnahme_D1.md` mit den Abnahme-Kommandos für Windows.
- `make vuln` (govulncheck v1.9.0, per `go list -m golang.org/x/vuln@latest` aufgelöst) und CI-Job `vuln` (A01 N2); Ergebnis 16.10.2026: keine Schwachstellen.

### Changed

- `golang.org/x/text` v0.14.0 → v0.43.0 (per `go get`, A01 N2); `go mod tidy` ohne weitere Änderung.
- Testserver auf geprüfte Versionen festgelegt: Dovecot 2.4.5, GreenMail 2.1.14 (Stalwart bleibt 0.11.8).
- `imapx`-Watchdog misst Schweigen statt Gesamtdauer (lange FETCHes über langsame Leitungen brechen nicht mehr ab); unlesbare Serverantworten sind `ErrProtocol` (Verbindung verwerfen, nicht wiederholen); der Sync grenzt betroffene Nachrichten durch Halbieren ein.
- Provider-Schnittstelle: `Changes` mit `ChangesOptions` und `Present`, `FetchHeaders`/`FetchRaw` mit UIDVALIDITY-Prüfung, `FetchRaw` für ein Fenster von UIDs.
- CI: `actions/checkout` und `actions/setup-go` auf v7 (ersetzt Dependabot-PRs #1/#2).
- `account test` nutzt den IMAP-Provider (eine Verbindung, ein Versuch); Serverablehnungen (NO/BAD) gelten nicht mehr als Verbindungsfehler.
- `policy.Check`: `--dry-run` gilt auch für R1-Operationen (wurde bisher ignoriert).
- Store verweigert Datenbanken mit neuerer Schemaversion; Migration 0001 um `accounts.insecure_test_server` ergänzt.
- **Go 1.27** (ADR-008, A01 Schritt 0): `go.mod` auf `go 1.27`, `modernc.org/sqlite` v1.45.0 → v1.60.1 (SQLite 3.53.4), `GOTOOLCHAIN=local` im Makefile, CI installiert `1.27.x`; `make cross` meldet die Binärgrößen.
- ADR-001 und ADR-005 an die Spike-Befunde angepasst (`modernc.org/sqlite`: v1.46.1 ist das Maximum für Go 1.24).
- CI: `GOTOOLCHAIN=local`, Go 1.27 ausdrücklich installiert.
- README, `docs/roadmap.md` (S0 erledigt, entschiedene Fragen), `docs/architektur.md`, `docs/datenmodell.md` (ß/ss-Normalisierer), `docs/sicherheit.md` (Rückfall-Abgleich), A01 (Schritt 0, Vorgaben, Prüfliste).

### Fixed

- `content_hash` deckt die ganze Rohmail ab, auch wenn der Parser vorzeitig aufhört (unlesbarer Kopf über 1 MiB); vorher bekamen zwei verschiedene Mails mit gleichem Anfang denselben Hash.
- CLI-Textausgabe (`show`, `sync`, `mailbox list`, Fortschritt) filtert Steuerzeichen aus Mail und Server (ESC/CSI/OSC, C0/C1, einzelnes CR, ungültiges UTF-8 → `U+FFFD`); vorher konnte eine Mail Escape-Sequenzen im Terminal auslösen. `--json` unverändert.
- Vertragstest nach ADR-004 ergänzt: jede Operation aus dem neuen `ops.Catalog` × jede Rolle × fünf Trockenlauf-Varianten durch den Executor (Policy, genau ein Audit-Eintrag, Handler nur bei Erlaubnis, Trockenlauf schreibt nichts, Pflichtfelder der Ausgabe) sowie fail closed je Operation; die CLI registriert über `ops.NewDefaultRegistry`.
- Executor: Das Fehler-Ereignis eines Handlers wird auch nach einem Abbruch (Strg+C) ins Audit geschrieben (`context.WithoutCancel`, Frist 5 s); vorher fehlte es.
- Store: Transaktionen beginnen mit `BEGIN IMMEDIATE`; zwei gleichzeitig schreibende Prozesse warten aufeinander (`busy_timeout`), statt dass einer mit `SQLITE_BUSY` den ganzen Ordner als Fehler meldet.
- Sync: Sperre je Konto (Dateisperre des Betriebssystems neben der Datenbank); ein zweiter schreibender Sync desselben Kontos endet sofort mit `sync_running` („another sync is running"), statt doppelt einzutragen. `golang.org/x/sys` v0.49.0 (BSD) jetzt direkt genutzt (unverändert, war indirekt).
- `docs/abnahme_D1.md`: Dovecot wird mit `--force-recreate --renew-anon-volumes` wirklich geleert (vorher blieb das anonyme Volume mit allen Nachrichten erhalten); D1.5/D1.6 ohne feste Werte (Message-ID aus `corpusload -mark-seen`, leere `flags:`-Zeile vor, `flags: seen` nach dem Sync); Hinweis auf `insecure-file` ohne OS-Schlüsselbund und auf `--password-stdin`.
- Doku nach der Gegenprüfung: `docs/sicherheit.md` auf Stand S1.5, A01 `schema 2`, Roadmap (Windows-`TerminateProcess` als ungetestet markiert, Erstsync-Kriterium an die P2-Messung mit 245 MB angepasst, offene Fragen zum Watchdog nach ADR-007 und zu vier weiteren Befunden).
