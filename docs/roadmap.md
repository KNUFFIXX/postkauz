# Roadmap

Stand: **Schnitt 1 komplett, Abnahmen D2/D3 offen** (16.10.2026). **S1.11 erledigt** (Abschluss Schnitt 1: täglicher Audit-Export mit SHA-256-Kette und Manifest als Windows-Aufgabe und systemd-Timer, `doctor --secrets`, Panics ohne Inhalte, Kanarientest über alle Ausgabekanäle, Installationsanleitung im README, `make release`; Abschnitt „Stand S1.11" unten). Release-Kandidat **0.1.0-rc1** liegt in `dist/` (Git-Tag setzt der Betreiber nach D3). **S1.1 bis S1.10 erledigt** (15./16.10.2026); **D0 auf Windows und D1 bestanden** (16.10.2026). Als Nächstes: **Abnahme D2** ([abnahme_D2.md](abnahme_D2.md)) und **Gesamtabnahme D3** ([abnahme_D3.md](abnahme_D3.md)) durch den Betreiber, dann seine Entscheidung (D3.5: S1 angenommen / Nachbesserung / S2 starten). Offene Fragen gesammelt und neu nummeriert: Abschnitt „Offene Fragen". Am Ende jeder Arbeitssitzung wird dieser „Stand" und `CHANGELOG.md` gepflegt.

| Phase / Schnitt | Inhalt in einem Satz | Status |
|---|---|---|
| Phase 0 | Gerüst, Architektur, ADRs, Testserver, Spike mit `go-imap/v2`, Gate Go vs. Python | **S0 (Spike und Gate) erledigt 14.10.2026**: Gate Go bestanden mit Auflagen, Bericht `spike/s0/REPORT.md`, ADR-007/008. **Schritt 0 (Go 1.27) erledigt 15.10.2026** |
| S1 | Konto, Sync, Spiegel, Threads, Suche, CLI lesend, stdio-MCP lesend, Audit, Import | **komplett (S1.1 bis S1.11) 16.10.2026**, Release-Kandidat 0.1.0-rc1; D0/D1 bestanden 16.10.2026; **Abnahmen D2 und D3 offen** |
| S2 | Entwürfe als draft-only, Policy-Datei, Freigabe-Zustände, Prüf-Hook | offen |
| S3 | Tags und Postfach-Aktionen, Export/Backup/Restore, HTTP-MCP mit OAuth 2.1 | offen |
| S4+ | SMTP-Versand mit Freigabe, Regeln, REST, JMAP, später GUI | nicht begonnen |

Aufwandsschätzung für S1–S3: **40–80 Sitzungen, drei bis sechs Monate** (Code-Anteil schnell, Tests gegen echte Server und Entscheidungen des Betreibers bestimmen das Tempo). Die Schätzung ist grob und wird nach S1 neu gesetzt.

## Phase 0: Gerüst und Gate

**Erledigt:** Repository, Lizenz, ADR-001 bis ADR-006, Architektur-/Datenmodell-/MCP-/CLI-/Sicherheitsdokumente, Go-Gerüst (Register, Executor, Policy-Stub, Audit-Typ, Migrationen mit FTS5, Provider-Interface, CLI mit `version`/`doctor`/`accounts list`), CI, Testserver-Compose.

**S0 erledigt (14.10.2026): Spike und Gate.** `go-imap/v2` v2.0.0-beta.8 besteht gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail: Erstsync in Fenstern (rund 1.000 bis 1.250 Nachrichten/s lokal), Inkrementalsync (CONDSTORE bzw. Vollvergleich), Löschungserkennung, Draft-APPEND mit identischen Bytes, IDLE, Reconnect. **Urteil: Gate Go bestanden mit Auflagen**, Bericht und Rohdaten in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md). Auflagen und Entscheidungen: ADR-007 (go-imap-Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Go-Version). Der Spike-Code ist Wegwerfcode in einem eigenen Go-Modul (`spike/s0`) und nicht Teil von `./...`.

**Schritt 0 erledigt (15.10.2026):** `go.mod` auf `go 1.27` (Toolchain go1.27.2, `GOTOOLCHAIN=local` in Makefile und CI), `modernc.org/sqlite` v1.45.0 → v1.60.1 (SQLite 3.53.4; mitgezogen: `modernc.org/libc` v1.77.1, `golang.org/x/sys` v0.48.0). `make check` und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 4,9 MB, windows/amd64 5,1 MB, darwin/arm64 4,8 MB (die CLI bindet SQLite noch nicht ein; mit Store-Anbindung in S1.1 wachsen sie, Spike: rund 11 MB mit `-s -w`).

**Offen (Auftrag A01):** Abnahme D2 ([abnahme_D2.md](abnahme_D2.md)) und Gesamtabnahme D3 ([abnahme_D3.md](abnahme_D3.md)) durch den Betreiber; alle Schritte S1.1 bis S1.11 sind erledigt (Abnahme D1 bestanden 16.10.2026). Die Spike-Befunde sind in A01 als Vorgaben und Prüfliste festgehalten.

## S1: Lesen, Spiegeln, Suchen

### Stand S1.11 (16.10.2026, erledigt): Abschluss Schnitt 1

**Akzeptanz S1.11 erfüllt** („`audit verify` erkennt eine geänderte Zeile; Kanarientest grün; Installation in unter 15 Minuten nach README; Release-Binärdateien"):

- **`audit verify` erkennt eine geänderte Zeile:** `TestAuditExportChainAndManifest` (Zeile 2 von Tag 1 geändert → `file_changed` und `chain_broken` je mit Zeile 2; passend nachgerechnete Kette → `file_changed`; fehlendes und verändertes Manifest; geänderte Datenbankzeile → `database_changed` nur für ihren Tag), `TestAuditVerifyLegacyFormat` (Exporte aus S1.8 bis S1.10), `TestAuditExportVerify` (CLI) und im Kanarientest über die gebaute Programmdatei: unverändert `ok`, Exit 0; eine Zeile verändert → `mismatch`, Exit 1; wiederhergestellt → `ok`.
- **Täglicher Export:** Operation `audit.export` (R1, `admin`, im Register seit S1.8) schreibt jetzt Format 2 (JSONL mit `prev`/`chain` über alle Tage, dazu `postkauz-audit-manifest.json`); `deploy/windows/Register-AuditExport.ps1` (Aufgabenplanung, täglich 12:30, `StartWhenAvailable`, angemeldete Sitzung wegen Google-Drive-Laufwerk, `conhost.exe --headless`, Selbstprüfung über `doctor --json`; PowerShell 5.1, UTF-8 mit BOM, nur ASCII, keine Aliase; geprüft mit PSScriptAnalyzer 1.23 einschließlich Syntax-Kompatibilität 5.1, **auf Windows nicht ausgeführt**: Abnahme D3.3c) und `deploy/systemd/postkauz-audit-export.{service,timer}` (`systemd-analyze verify`); Ziel `[audit] export_dir` oder `--to`. `doctor` meldet den letzten Export und warnt bei Tagen älter als zwei Tage ohne Export.
- **Kanarientest über alle Ausgabekanäle grün:** `internal/cli/canary_test.go` baut `cmd/postkauz` und ruft die Programmdatei auf (rund 50 Aufrufe und zwei MCP-Sitzungen über stdio): Kanarien-Passwort, ein zweites (falsches) Passwort, der echte API-Schlüssel, ein falscher Schlüssel und ein Kanarien-Mailtext (Text und Anhang) gegen Audit, Log, `--json` der Admin-Kommandos (`account add/test`, `accounts list`, `sync`, `mailbox list`, `key create/list`, `policy show`, `import`, `audit`, `audit export/verify`, `doctor`, `version`), Fehlermeldungen (falsches Passwort, abgelehnte Eingaben, unbekannter/falscher Schlüssel), Absturzausgaben (sieben eingeschleuste Panics mit Inhalt im Panic-Wert, in CLI und Handlern), Audit-Export, `doctor --secrets` und das Datenverzeichnis. Mailtext erscheint nur in `show`, `search`, `thread`, `attach text`, `mailbox overview` und in MCP-Ergebnissen in der Hülle (dort auch positiv geprüft). Gegenprobe: Ohne die Erlaubnis für lesende Kommandos meldet der Test fünf Funde.
- **`doctor --secrets` und `doctor --json`:** Dateirechte und Suche nach Passwörtern, `POSTKAUZ_KEY` und Schlüsselmustern in Log, Konfiguration, Tabellen ohne Mailinhalt und Audit-Exporten (`TestDoctorSecrets`, `internal/secretscan`); `doctor --json` mit den neuen Feldern (`TestDoctorReportsAuditExport`).
- **Installation in unter 15 Minuten nach README:** Abschnitt „Install in under 15 minutes" (Windows nur mit der exe, Linux, macOS: Download und Prüfsumme, `doctor`, `account add`, `sync --full`, `search`, Claude Code), Fehlersuche, Abschnitt „Security". Die Zeitmessung mit einem echten Postfach macht der Betreiber (Kriterium 10, optional in D3).
- **Release:** `make release VERSION=0.1.0-rc1` (`-trimpath -ldflags "-s -w"`, `dist/VERSION`, `dist/SHA256SUMS`); Größen und Hashes in [abnahme_D3.md](abnahme_D3.md), Abschnitt D3.2. `CHANGELOG.md`: Abschnitt `[0.1.0-rc1]`.
- **Abnahme D3 vorbereitet:** [abnahme_D3.md](abnahme_D3.md) für Windows ohne Docker und Go; Protokolle der Container-Läufe in `docs/protokolle/` (D3.1 Stalwart und GreenMail mit `scripts/abnahme-d3-server.sh`, D3.2 `make check && make cross`, D3.4 Import mit `scripts/abnahme-d3-import.sh`); Testbestand `dist/abnahme-d3-import.zip` (Thunderbird-Profil, reproduzierbar).

**Umsetzung:**

- `internal/ops/auditexport.go`: Export und Prüfung neu gefasst (Format 2, Kette, Manifest ohne Zeitangabe und daher aus den Nachweisen reproduzierbar, Zeilennummern, Format 1 weiter prüfbar, keine Folgefehler nach einer geänderten Datenbankzeile).
- `internal/crash` (bereinigter Panic-Bericht), Executor mit `recover` um den Handler (`ops.ErrInternal`, `PanicError`, `OnPanic`, Audit-Grund `panic`), CLI mit `recover` im Hauptablauf, Fehlercode `internal` (CLI und MCP); Testschalter `POSTKAUZ_INTERNAL_TEST_PANIC`.
- `internal/secretscan`, `store.TableText`/`store.ScanTables`, `doctor --secrets`.
- Kanarientest-Fund behoben: `search --json` trug das Snippet ohne Kennzeichnung der Hülle; jede NDJSON-Zeile endet jetzt mit `untrusted`/`source`.
- `make guard` prüft Kodierung (BOM) und Zeichensatz (ASCII) der PowerShell-Skripte; `.gitattributes` (`*.ps1` CRLF), `.editorconfig`.

**Befunde:**

- **Panics in Nebengoroutinen** (Hilfsgoroutinen des Syncs, des MCP-SDK) fängt weder der Executor noch die CLI ab; sie enden weiter mit der Standardausgabe von Go (Panic-Wert und Stack mit Argumenten). Handler, in denen Mailinhalt verarbeitet wird, sind abgedeckt (Frage 15 unten).
- **Windows-Aufgabe:** Das Skript ist geprüft (Parser, PSScriptAnalyzer, 5.1-Kompatibilität), aber nicht auf Windows gelaufen; ob `conhost.exe --headless` den Exit-Code durchreicht, ist offen. Deshalb prüft das Skript den Erfolg über `doctor --json` (letzter Export) statt über das Ergebnis der Aufgabe, und es gibt `-ShowWindow` als Rückfall.
- Die Aufgabe läuft nur in der angemeldeten Sitzung (Laufwerksbuchstaben von Google Drive und Netzlaufwerken gibt es nur dort); verpasste Läufe holt Windows nach (`StartWhenAvailable`).
- `doctor --secrets` sucht Passwörter unter 8 Zeichen nicht (z. B. `pass` der Testserver), sie stünden zufällig überall; es meldet das als `warn`.
- Eine Datenbankdatei mit zu weiten Rechten setzt `store.Open` selbst auf 0600; Datenverzeichnis und Log nicht (Frage 6 c unten), `doctor --secrets` meldet sie als `fail`.
- Testserver: Nach `docker compose up --force-recreate` fehlt Stalwarts Verwaltungspasswort in den Logs (das benannte Volume bleibt); frisch nur mit `make testmail-down testmail-up testmail-setup`.

**Prüfläufe (16.10.2026, Stand S1.11):** `make check` nach jedem Commit grün (rund 2,5 min; der Kanarientest baut das Programm und braucht rund 10 s); `make race` grün (rund 25 min; `internal/importer` 737 s, `internal/mailsync` 702 s, `internal/ops` 700 s, `internal/cli` 214 s; Grenze 30 min je Paket); `make integration` grün (17 Tests gegen Dovecot, Stalwart, GreenMail, keiner übersprungen); `make vuln` (govulncheck v1.9.0): keine Schwachstellen; `make cross` grün (ohne `-s -w`: linux/amd64 24,2 MB, windows/amd64 24,0 MB, darwin/arm64 22,8 MB); `make release VERSION=0.1.0-rc1` aus Commit `64e516c` (mit `-s -w`: windows/amd64 17.424.896 Bytes, linux/amd64 17.506.464, darwin/arm64 16.399.794; Hashes in [abnahme_D3.md](abnahme_D3.md), D3.2). Container-Läufe für die Abnahme: D3.1 gegen frisch gestartete Stalwart- und GreenMail-Server und D3.4 (`new: 5, existing: 1100, skipped: 3, errors: 0`, zweiter Lauf `new: 0`), Protokolle in `docs/protokolle/`. PowerShell-Skript: Parser und PSScriptAnalyzer 1.23 (Syntax 5.1) ohne Befund, Ablauf mit nachgebildeten Aufgaben-Cmdlets unter PowerShell 7.4 (Erfolg, fehlender Export, `-Unregister`); auf Windows nicht ausgeführt (D3.3c).

**Nicht in S1.11 (bewusst verschoben):** Code-Signatur der Programme (Frage 13), Aufbewahrung und Rotation des Audits (Frage 14), `launchd`-Vorlage für macOS (README nennt `cron`/`launchd`), `doctor --blobs` (Prüfung des Blob-Stores gegen die Datenbank, aus S1.2; Blob-Store hat `Verify`, ein Kommando fehlt noch).

**Erfüllung der Akzeptanzkriterien S1** (Abschnitt „S1" unten), Stand 0.1.0-rc1:

| # | Kriterium | Nachweis | Stand |
|---|---|---|---|
| 1 | Zwei Serverfamilien in der Testmatrix; dritter Anbieter manuell | `make integration` (Dovecot, Stalwart, GreenMail); D3.1-Protokolle | erfüllt; echter Anbieter: Betreiber (P1/P9) |
| 2 | Erstsync 50.000 in unter 60 min, Leerlauf unter 5 s, `kill -9` ohne Doppelte | P2 (78 s lokal), `TestKill9DuringInitialSync` | erfüllt lokal; WAN-Messung Betreiber (P2) |
| 3 | Änderungen von außen, IDLE, UIDVALIDITY | S1.5-Tests, Integrationstests, D1 | erfüllt (D1 bestanden) |
| 4 | Suche unter 300 ms bei 100.000; Wortformen, Umlaute, Teilwörter | [suche_messung.md](suche_messung.md), Suchtestsatz | erfüllt (Median); Windows-Nachmessung offen (Frage 7) |
| 5 | Import mit Überlappung ohne Doppelte, Zählung | S1.10-Tests, D3.4 | erfüllt; Abnahme D3.4 offen |
| 6 | MCP: Tool-Liste nach Scopes, Hülle, versteckter Text | S1.9-Tests, D2.7/D2.8 | erfüllt; Abnahme D2 offen; zweiter MCP-Client: SDK-Client im Test und JSON-RPC in D3.1 |
| 7 | Audit ohne Inhalt, append-only, täglicher Export mit Prüfsumme, `verify` erkennt Änderung | S1.8/S1.11-Tests, D2.9, D3.3c | erfüllt; Windows-Aufgabe: Abnahme D3.3c |
| 8 | Kanarienwerte in keinem Kanal | Kanarientest S1.11 | erfüllt |
| 9 | Jede Operation getestet, vet/test/cross grün | Vertragstests, `make check`, `make cross` | erfüllt |
| 10 | Installation in unter 15 Minuten nach README (Windows 11, Linux) | README, D3.3 | Anleitung fertig; Zeitmessung mit echtem Postfach: Betreiber |

### Stand S1.10 (16.10.2026, erledigt): Import aus mbox und Maildir

**Akzeptanz S1.10 erfüllt** („Import eines Bestands mit 30 % Überlappung zu einem gesyncten Konto: keine Doppelten, Zählung ausgegeben"; alles in `make check` ohne Docker, dazu ein Integrationstest):

- **Überlappung mit einem gesyncten Konto:** `TestImportOverlapWithSyncedAccount` (Testserver im Prozess: Korpus ab Index 300 gesynct, Thunderbird-Profil der Indizes unter 430 importiert; 435 Nachrichten, davon 128 = 29 % schon gesynct, 4 davon ohne Message-ID und nur über `content_hash` erkannt) und `TestIntegrationImportOverlapDovecot` (frisches Dovecot-Konto mit 700 Nachrichten: `new 305, existing 173, skipped 3, errors 0`, 30 % Überlappung): keine doppelte Message-ID, überlappende Nachrichten behalten Serverordner und Flags und bekommen den Import-Ordner, die Zahl der Nachrichten „auf dem Server" bleibt gleich; zweiter Lauf `new: 0`; ein Sync danach findet nichts Neues und entfernt nichts. Umgekehrt (Import zuerst, Sync danach, `TestSyncAfterImport`): keine Doppelten.
- **Zählung `new: N, existing: M, skipped: S, errors: E`** als letzte Zeile (D3.4), zweiter Lauf `new: 0` (`TestImportCommand`, CLI; `TestImportThunderbirdCorpus`).
- **Thunderbird-Variante** (synthetisches Profil, `corpus.WriteThunderbird`): `From - `-Trenner, `X-Mozilla-Status` mit allen Bits → Flags (gelesen, beantwortet, markiert; Bits ohne Bedeutung ignoriert), gelöscht markierte (0x0008) übersprungen, `X-Mozilla-*` entfernt (keine Rohmail enthält sie), `>From` zurückgenommen (auch eine Zeile, die wie eine Trennzeile aussieht), `.sbd`-Unterordner → `Archiv/2024`, IMAP-UTF-7-Name `Entw&APw-rfe` → „Entwürfe", `.msf` und `msgFilterRules.dat` ignoriert, leere Ordner ohne Quelle, Gmail-artige Kopien in „Wichtig" nur zugeordnet (Inhalt einmal), Nachricht nur aus Kopf, überlange Zeile (300 KiB), letzte Nachricht ohne Zeilenende. Jede Korpus-Nachricht ist nach dem Import wieder das Original (`content_hash`).
- **Maildir** (`corpus.WriteMaildir`): `cur/new/tmp`, Flag-Suffixe S/R/F/D, T und `tmp/` übersprungen, Maildir++-Unterordner; eine neue Datei wird beim nächsten Lauf nachgeholt.
- **Wiederaufnahme nach hartem Abbruch:** `TestImportResumesAfterKill9` (Kindprozess per SIGKILL nach 160 Nachrichten beendet; der nächste Lauf setzt an der gespeicherten Byte-Position fort, Bestand vollständig, keine Doppelten), Maildir-Cursor nach Abbruch (`TestImportResumesMaildirAfterAbort`).
- **Großer Datei-Stream:** `TestImportLargeFileStreams` (210 MB in einer Datei, darunter ein Anhang mit 30 MB): Heap vorher 0,7 MB, Spitze 5,1 MB (+4,4 MB); Grenze im Test 64 MB.
- **Trockenlauf:** gleiche Zählung, keine Zeile, keine Datei im Blob-Store (`TestImportDryRun`).
- **Policy/Audit über das Register:** Operation `import.run` (Scope `admin`, R1, nicht über MCP), Vertragstest über alle Rollen und Trockenlauf-Varianten, Audit mit Feldnamen und Konto, ohne Pfad (`TestImportRun`).
- **Messung:** [import_messung.md](import_messung.md).

**Umsetzung:**

- Migration 0007: `import_folders`, `message_import_folders` (importierte Ordner getrennt von den Ordnern des Servers, ADR-003), `import_sources` (Stand je Quelle). Nur importierte Nachrichten tragen `deleted_remote_at`; `show` nennt sie „not on the server (imported only)" und importierte Ordner „(imported)"; `in:<pfad>` und `in:import` finden sie.
- `internal/importer`: Quellen finden, mbox/Maildir streamend lesen, Abgleich (Message-ID, dann `content_hash` mit Varianten am Zeilenende), Aufbereitung über `mailsync.Prepare` (gleicher Parser, Bereinigung, Anhänge, Threads wie beim Sync), Stapel zu 200 Nachrichten bzw. 32 MiB mit Stand je Quelle, Labels-Modus (`--labels-from`), Kontosperre wie der Sync, danach die Jobs `extract` und `index`.
- `mailsync.ContentHasher`: `content_hash` plus Varianten mit 0, 1, 2 Zeilenenden am Schluss; der Sync sucht seit S1.10 ebenfalls mit allen Varianten (sonst lud er importierte Nachrichten mit anderem Zeilenende doppelt).
- CLI `import mbox|maildir` mit `--mailbox`, `--labels-from`, `--rescan`, `--dry-run`, Fortschritt je Datei; Doku [cli.md](cli.md) Abschnitt 2i. Korpus-Generator: [testkorpus.md](testkorpus.md) Abschnitt 1b.

**Prüfläufe (16.10.2026, Stand S1.10):** `make check` (rund 2,5 min; `internal/importer` rund 80 s), `make race` (rund 26 min ohne Cache; `internal/importer` 12 min, `internal/ops` 11,5 min, nebeneinander; Grenze 30 min je Paket), `make integration` (Dovecot, Stalwart, GreenMail; neu: Import gegen Dovecot), `make vuln` (keine Schwachstellen) und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 24,1 MB, windows/amd64 23,9 MB, darwin/arm64 22,7 MB.

**Befunde:**

- **Thunderbird quotet nach mboxo:** Eine Zeile, die im Original mit `>From ` beginnt, steht in der Datei genauso da wie eine gequotete `From `-Zeile; nach dem Import heißt sie `From `. Der Inhalt weicht dann vom Server ab, der Abgleich läuft über die Message-ID. Selten (Zitate von Zitaten).
- **Thunderbird kennt kein Entwurf-Bit:** Aus mbox kommt das Flag `draft` nie (aus Maildir schon, `D`).
- **Teilweise geladene Nachrichten** (Thunderbird-Bit 0x0400, große Mails bei „nur Kopf laden"): werden mit Hinweis importiert. Liegt die vollständige Nachricht schon im Konto, wird sie per Message-ID nur zugeordnet; kommt sie erst später per Sync, entsteht eine zweite Nachricht mit gleicher Message-ID (Inhalt verschieden; Frage 15 unten).
- **Ordnernamen, die Thunderbird als Hash ablegt** (unzulässige Zeichen, sehr lange Namen), lassen sich ohne `.msf`/`folderTree.json` nicht zurückführen; der Ordner heißt dann wie die Datei.
- **Durchsatz** im Container (2 Kerne ohne SHA-Befehlssatz): rund 23 MB/s beim Import, 130 MB/s im Trockenlauf, 1 GB/s für einen zweiten Lauf über vorhandene Nachrichten; SHA-256 (Blob, `content_hash`, Anhänge) und base64 dominieren. Für 20,7 GB rund 15 Minuten (Frage 14 unten).
- `mailbox list` zeigt nur Ordner des Servers; importierte Ordner sieht man in `show` und über `in:`.

**Nicht in S1.10 (bewusst verschoben):** Import als Eintrag in der Tabelle `jobs` (läuft im Vordergrund wie `sync`); EML-Verzeichnisse, PST, Google-Takeout-Labels aus `X-Gmail-Labels`; `mailbox list` für importierte Ordner; Import über MCP (bewusst nicht); paralleles Aufbereiten mehrerer Nachrichten.

### Nachbesserung nach der Gegenprüfung (16.10.2026, Stand `04a9ab8` geprüft)

Je Befund ein Commit mit Test (der ohne die Änderung scheitert), `make check` nach jedem Commit grün.

**Wichtig:**

1. **Unsichtbare Zeichen:** eine zentrale Funktion `sanitize.Invisible` (ganze Kategorie Cf: Bidi, Nullbreite, Tag-Zeichen U+E0000–E007F, U+FFF9–FFFB; dazu Variantenwähler U+FE00–FE0F, U+E0100–E01EF) für Sync, CLI-Textausgabe und MCP; Test je Klasse.
2. **Klartext ≠ HTML** (multipart/alternative): Vergleich als Text (Wortmengen, Zahlen ab 4 Ziffern, ohne URLs und Formatierung), Markierung `alternative_mismatch` (Migration 0006) in `show`, `thread`, `--json` und MCP. Im Korpus keine Fehlmarkierung.
3. **Schlüssel-Widerruf in laufender Sitzung:** `ops.KeyGuard` im Executor prüft vor jedem Aufruf Widerruf und Ablauf (Zwischenspeicher ≤ 2 s); Test: `key revoke` während einer MCP-Sitzung → nächster Aufruf `denied`/`key_revoked`.
4. **Threads:** Der Betreff-Rückfall verlangt gemeinsame Beteiligte (From/To/Cc, ohne die eigenen Adressen des Kontos); Zeitfenster bleibt. Bestehende Zuordnungen werden nicht neu berechnet.
5. **`make race`** mit `-timeout 30m` (gemessen: `internal/ops` 612 s, `internal/mailsync` 514 s, gesamt rund 13 min; der Standard von 600 s je Paket wäre gerissen). Schwere Korpus- und Kindprozess-Tests überspringen sich mit `-short`; `make check`, `make race` und CI setzen es nicht (CLAUDE.md).

**Klein:**

6. Versteckter Text zusätzlich: `transform: scale(0)`, `hsl()`/`hsla()`, Schrift ≤ 2 px, `clip`/`clip-path` ohne Fläche; Grenzen in `docs/sicherheit.md`.
7. Betreff, Absender-/Empfängernamen, Dateinamen beim Sync durch `sanitize.Text`; `CleanVersion` 2 (jetzt 3 mit Punkt 2) zieht den Bestand nach, einschließlich der Kopffelder in `messages`.
8. DOCX: `<w:vanish/>`-Text fehlt, `hidden_text` gesetzt.
9. Trigram-Index ohne ue→u, ü → „ue" (`FTSVersion` 2; „uer" in „Steuer", „uelle" in „Quelle" treffen).
10. Cursor sind kein Schnappschuss: Absatz in `docs/cli.md` und `docs/mcp.md`.
11. Neue Operation `mailboxes.overview`; `attachments.text` (`message`, `account`) und `threads.get` (`account`) prüfen die Zugehörigkeit selbst; MCP-Resources rufen je URI genau eine Operation.
12. `scripts/abnahme-d2-db.sh`: `D2_USER` statt `USER`, Prüfung von `zip`/`sha256sum`/`pdftotext` und `doctor --json` (neu: `extract_pending`) vor dem Packen.
13. Extraktor-Timeout-Test misst ab dem laufenden Kindprozess.
14. `docs/abnahme_D2.md`: Schlüssel steht im Klartext in `%USERPROFILE%\.claude.json`, Aufräumen entfernt ihn.

**Prüfläufe:** `make check`, `make race` (rund 13 min, siehe 5), `make integration` (Dovecot, Stalwart, GreenMail), `make vuln` (keine Schwachstellen), `make cross` grün (linux/amd64 23,9 MB, windows/amd64 23,6 MB, darwin/arm64 22,5 MB). Abnahme-Datenbank neu erzeugt: `dist/abnahme-d2-daten.zip`, 3.614.993 Bytes, SHA-256 `329f6d9e0c28cce98a387bf0f42dea81dc836995204939d9def362b43ac03ad4` (in [abnahme_D2.md](abnahme_D2.md) eingetragen; D2.1 bis D2.5 liefern dieselben Zahlen wie zuvor).

**Offen bzw. neue Fragen an den Betreiber:**

- Emoji-Sequenzen verlieren durch Punkt 1 Verbinder und Variantenwähler (eine Familien-Emoji erscheint als Einzelpersonen). Default: so lassen (Sicherheit vor Darstellung), einverstanden?
- Threads (Punkt 4): Weitere eigene Adressen (Aliasse), die nicht im Konto stehen, zählen als gemeinsamer Beteiligter. Default: später ein Feld „weitere eigene Adressen" am Konto (S1.10/S2), einverstanden?
- Teilwortsuche (Punkt 9): Ein ohne Umlaut getipptes „pruf" findet „Prüfung" nur noch als Wort/Präfix, nicht mitten im Kompositum. Default: hinnehmen.
- Die Erkennung `alternative_mismatch` vergleicht Wortmengen; umgestellte Sätze oder ein einzelnes vertauschtes Wort („nicht") fallen nicht auf (Zahlen schon).

### Stand S1.9 (16.10.2026, erledigt): MCP über stdio, lesend

**Akzeptanz S1.9 erfüllt** (alles in `make check`, ohne Docker; Abnahme D2.7/D2.8 in Claude Code durch den Betreiber steht aus):

- **Tool-Liste folgt den Scopes:** `TestToolListFollowsScopes` (über eine echte MCP-Sitzung: `reader` 9 Tools, 9 Resources, 4 Prompts; verengt auf `mail:search` nur `mail_search` und `mail_get_policy`, keine Message-Resource; ohne Scopes nur `mail_get_policy`; auch `admin` kein schreibendes Tool) und `TestMCPSearchOnlyKey` (verstecktes Tool nicht aufrufbar, Message-Resource verweigert).
- **Vertragstest Register ↔ MCP:** `TestEveryOperationIsBoundOrExcluded`, `TestToolSchemasMatchRegistry` (Schemas wie im Register; einzige Abweichung `mail_search.limit` ≤ 100, im Executor geprüft).
- **Ausgabe jeder Mail in der Hülle:** `TestMCPReaderSession` ruft jedes Tool und jede Resource mit Mailinhalt auf und prüft `untrusted`, `source`, `notice`; versteckter Text fehlt; Steuer- und Formatzeichen werden entfernt (`TestCleanStringRemovesControls`).
- **Smoke-Test über stdio:** `TestMCPStdioSmoke` startet das Programm als Kindprozess (`CommandTransport` des SDK): initialize, tools/list (9 Tools), tools/call (`mail_get_message` in der Hülle).
- **Audit je Aufruf mit Schlüsselname, ohne Mailtext:** `TestMCPReaderSession` prüft danach `audit --key-id key:leser` (Akteur `agent`; keine Suchbegriffe, kein Mailtext, kein Schlüssel).
- **README-Abschnitt** „Set up MCP in Claude Code" (Schlüssel, `claude mcp add`, `.mcp.json`, Claude-App).

**Umsetzung:**

- SDK `github.com/modelcontextprotocol/go-sdk` **v1.8.0** (per `go get` aufgelöst, gepinnt; Lizenz Apache-2.0 bzw. MIT für ältere Beiträge; mitgezogen: `segmentio/encoding` und `segmentio/asm` (MIT), `yosida95/uritemplate/v3` (BSD-3), `golang.org/x/oauth2`, `x/time`, `x/sync` (BSD)). Kein neuer UUID-Import (A01 N4; die Standardbibliothek von Go 1.27 hätte ein Paket `uuid`, gebraucht wurde keins). N5 (HTTP, Spezifikation 2026-07-28) betrifft erst Schnitt 3.
- `internal/mcp`: Tabellen für Tools (9), Resources (2 fest, 1 Audit fest, 6 Vorlagen) und Prompts (4); Standardwerte für Budgets und Quote-Stripper; engeres Schema für `mail_search.limit` (über `Request.Narrow` im Executor geprüft, also auch abgelehnt auditiert); Bereinigung aller Zeichenketten; Fehler als Ergebnis mit `status`/`code`/`reason`, `_meta.audit_id` bei Erfolg.
- `postkauz mcp`: Schlüssel aus `POSTKAUZ_KEY` oder `POSTKAUZ_KEY_FILE`, Anmeldung mit `last_used_at`, ohne gültigen Schlüssel kein Server (Exit 3), `--key` verboten.

**Befunde:**

- **`mail_sync_now` fehlt bewusst** (docs/mcp.md listet es unter S1): Laut Architektur synct der MCP-Prozess nicht selbst, sondern reiht Jobs ein; den Job-Typ `sync` gibt es noch nicht. Bis dahin hält `sync --watch` die Daten aktuell (Frage 13 unten).
- `audit export`/`audit verify` (geplant für S1.11) sind mit S1.8 vorgezogen, weil die Abnahme D2.9 sie braucht; offen bleibt der tägliche Export als Aufgabe.
- Die Untrusted-Felder stehen einmal oben in der Ausgabe statt je Nachricht (Planung in mcp.md); Inhalt bleibt Feld für Feld getrennt.
- Claude Code und die Claude-App speichern den Schlüssel im Klartext in ihrer Konfiguration (Nutzerprofil). Empfehlung im README: kurze Laufzeit, engste Rolle, widerrufen, wenn nicht gebraucht.

**Prüfläufe (16.10.2026, Stand S1.9):** `make check`, `make race` (rund 9 min; `internal/mailsync` und `internal/ops` mit dem Vertragstest über alle Rollen dominieren), `make integration` (Dovecot, Stalwart, GreenMail), `make vuln` (keine Schwachstellen) und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 23,8 MB, windows/amd64 23,6 MB, darwin/arm64 22,4 MB (MCP-SDK rund +3 MB). Abnahme-Datenbank für D2: `dist/abnahme-d2-daten.zip` (3,6 MB, erzeugt mit `sh scripts/abnahme-d2-db.sh`, Anleitung [abnahme_D2.md](abnahme_D2.md)).

**Nicht in S1.9 (bewusst verschoben):** `mail_sync_now` (Job-Typ `sync`), `fields`/`include_links`/`page_range`/`format: html_sanitized` der Planung, Resource-Abos und Änderungsmeldungen, HTTP-Transport (S3), Kanarientest über alle Kanäle (S1.11).

### Stand S1.8 (16.10.2026, erledigt): CLI lesend vollständig

**Akzeptanz S1.8 erfüllt** (alles in `make check`, ohne Docker):

- **Je Kommando ein Test:** bestehende Tests für `accounts list`, `account add/test/remove`, `sync`, `mailbox list`, `show`, `thread`, `attach list/text`, `search`; neu `TestKeyCreateListRevoke` (Klartext genau einmal, in keiner Datei des Datenverzeichnisses, `key list` ohne Klartext und Hash, Widerruf), `TestKeyFlagExplainAndAudit` (`--key`, `--explain` mit Audit-ID, `policy show`, `audit` ohne Suchbegriffe und Mailtext), `TestShowFull` (Absenderprüfung, Text, Warnzeile, Links, Anhangsliste, `--max-chars`, Hülle in `--json`, `thread --max-messages`, `search --fields/--mailbox`), `TestAuditExportVerify` (Export, `ok`, veränderte Datei → `mismatch` und Exit 1, kein Überschreiben), `TestProfileAndQuiet`.
- **Vertragstest Register ↔ CLI:** `TestBindingsCoverCatalogAndSchemas` (jede Operation des Katalogs hat genau ein Kommando und umgekehrt; die Felder, die ein Kommando setzen kann, sind genau die Eigenschaften des `InputSchema`) und `TestCLIContractRegistry` (22 echte Aufrufe gegen einen Testserver im Prozess: jede gebaute Eingabe gegen das `InputSchema`, jede `--json`-Ausgabe bzw. NDJSON-Zeile gegen das `OutputSchema`, jedes Feld mindestens einmal gesetzt).
- **Exit-Codes wie `docs/cli.md`:** `TestExitCodesMatchDocs` liest die Tabelle und die Liste der Fehlercodes aus der Doku und vergleicht mit dem Code (19 Fehlerarten); `TestExitCodesEndToEnd` löst 0, 1, 2, 3 und 5 mit echten Aufrufen aus (4 erst mit Freigaben in S2).

**Umsetzung:**

- Operationen `keys.create/list/revoke` (Scope `admin`), `audit.list` (`audit:read`), `audit.export` (`admin`, R1), `audit.verify` (`audit:read`), `policy.get` (Pseudo-Scope `any`); alle im Vertragstest über alle Rollen und Trockenlauf-Varianten.
- Executor: Eingabe gegen das `InputSchema` (über `github.com/google/jsonschema-go` v0.4.3, MIT, dieselbe Version wie im MCP-SDK) nach dem Audit-Eintrag und vor dem Handler; Audit-ID im Ergebnis; `Call.Key` für `policy.get`.
- Untrusted-Hülle (`untrusted`, `source`, `notice`) und Budgets (`max_chars` …, `truncated`) in den Ausgaben mit Mailinhalt; `messages.get` mit `auth` und `attachments`; `messages.search` mit `mailbox`.
- CLI: globale Optionen `--key`, `--quiet`, `--profile`, `--explain`; `show` voll; `key`, `audit`, `policy show`; `search --fields`.

**Befunde:**

- Ein Agent, der die CLI statt MCP nutzt, handelt als Mensch mit allen Rechten (es gibt dort keine Anmeldung per Schlüssel; `--key` ist eine Probe des Menschen). Agenten gehören an MCP mit `POSTKAUZ_KEY` (S1.9).
- `audit verify` erzeugt selbst einen Eintrag; der laufende Tag erscheint deshalb nach jedem `verify` unter „not exported yet". Das ist gewollt (das Audit bleibt vollständig) und kein Fehler.
- Schlüsselbund-Einträge hängen am Kontonamen, nicht am Profil (offene Frage 7); `--profile` teilt sie deshalb bei gleichem Kontonamen.

**Nicht in S1.8 (bewusst verschoben):** Audit-Export als tägliche Aufgabe und Kanarientest über alle Kanäle (S1.11); `--yes` (S2); `--fields` für andere Kommandos als `search`.

### Stand S1.7 (16.10.2026, erledigt): Volltextsuche

**Akzeptanz S1.7 erfüllt** (Suchtestsatz in `make check`, ohne Docker):

- **Suchtestsatz aus dem Korpus grün:** 22 Fälle im Manifest (Version 3, Feld `search`), geprüft nach einem Erstsync mit Jobs (`TestCorpusSearchSet`): „Rechnung" findet „Rechnungen" und umgekehrt (aber „Rechnungen" nicht „Abrechnung"), „prüfen" = „pruefen" = „prufen" (gleiche Treffermenge), „Vollstreck" findet „Zwangsvollstreckung", „Erdgeschoss" = „Erdgeschoß" (gleiche Treffermenge), „steuer" in Grundsteuer/Kfz-Steuer/Umsatzsteuer, Phrasen über ß/ss, Ausschluss, `from:`, `subject:`, `after:`/`before:`, `in:`, `has:attachment` (alle Treffer mit Anhang), Text aus einem Anhang, versteckter Text nie auffindbar. Dazu Einzeltests für Normalisierer, Stemmer, Parser (FTS5-Syntax in der Eingabe bleibt Suchtext), Store (Index, Änderungen, Löschen, Neuaufbau, Cursor-Seiten), Jobs (Budget, Abbruch, Übernahme nach Absturz) und CLI (NDJSON, Cursor, `--explain`, Exit-Codes).
- **Messung mit 100.000 Nachrichten dokumentiert** ([suche_messung.md](suche_messung.md)): Index 104 s (959 Nachrichten/s); Median aller 18 Anfrage-Typen unter 300 ms (schlechtester 291 ms), einzelne Läufe bei Wörtern in 25–50 % aller Nachrichten bis 345 ms; über die CLI (neuer Prozess je Anfrage) typische Anfragen 30–120 ms, häufige Einzelwörter 260–310 ms, zwei häufige Wörter 290–390 ms. Der synthetische Korpus ist dafür ein Extremfall (Frage 11 unten).

**Umsetzung:**

- `internal/search`: Normalisierer `Fold` (NFKD, Diakritika, ß → ss, ae/oe/ue wie Snowball „german2"), deutscher Stemmer über `github.com/blevesearch/snowballstem` v0.9.0 (BSD-3; Entscheidung in [datenmodell.md](datenmodell.md), „Stemmer"), Index-Spalten, Abfragesprache nach [cli.md](cli.md) Abschnitt 2d, Cursor.
- Migration 0004: FTS-Tabellen neu (gleiche fünf Spalten), `index_queue` mit Triggern, ein offener Job je Art und Konto, Bestand eingereiht; Migration 0005: zwei schmale Indizes für die Suche. Snapshot per `VACUUM INTO` wie bisher (500-MB-Datenbank: 3,5 s).
- `internal/jobs`: `extract` (60 s je Lauf, mindestens ein Anhang) und `index` (200 Nachrichten je Transaktion) über die Tabelle `jobs`, am Ende jedes Syncs; Sperre mit Ablauf, Übernahme nach Abbruch, Wiederholung nach Fehlern, Neuaufbau bei neuer `FTSVersion`; PDFs, die ohne `pdftotext` übersprungen wurden, kommen wieder dran, sobald es gefunden wird (Betreiber-Entscheid zu Frage 10: PDF nur über das optionale Werkzeug, README).
- Ausführung der Suche: FTS-Abfragen je Begriff nebeneinander, Mengenarbeit in Go, Kandidaten über den schmalen Index, Memory-Mapped I/O (`mmap_size` 1 GiB) für alle Datenbankdateien.
- Operation `messages.search` (R0, `mail:search`; Audit ohne Suchtext) und CLI `search` mit `--limit`, `--cursor`, `--explain`, `--json` (NDJSON); `sync` meldet den Index, `doctor` meldet `pdftotext` und wartende Nachrichten.
- `make check`, `make race` (rund 9 min), `make integration` (Dovecot, Stalwart, GreenMail; neu: Index-Job über die vom Server gelieferten 1.000 Nachrichten in rund 0,3 s und zwei Suchfälle je Server), `make vuln` (keine Schwachstellen) und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 20,9 MB, windows/amd64 20,6 MB, darwin/arm64 19,6 MB.

**Befunde:**

- Teilwortsuche heißt auch: „Rechnung" findet „Abrechnung", „steuer" findet „Grundsteuer". Treffer als ganzes Wort stehen vorn, reine Teilwort-Treffer dahinter (nach Datum).
- Ein Teilwort, das mitten im Wort mit „ue" beginnt (z. B. „uer" aus „Steuer"), wird anders gefaltet als das Wort und findet es nicht (Folge der ue-Regel; selten).
- Dovecot nahm im Container per APPEND ab rund 30.000 Nachrichten nur noch etwa 10 Nachrichten/s an; die 100.000er-Datenbank entstand deshalb über den Testserver im Prozess.
- Ein CLI-Test wählte bei einem zufälligen Port auf …993 TLS (Standardregel von `account add`) und scheiterte selten; die Tests legen `--tls none` jetzt fest.

**Nicht in S1.7 (bewusst verschoben):** `OR` und Klammern in der Abfragesprache; Snippets mit hervorgehobenen Treffern (die FTS-Tabellen speichern keinen Text; angezeigt wird das gespeicherte `snippet`); Suche über mehrere Konten mit eigener Gewichtung; Text aus XLSX/PPTX/ODT und OCR (wie S1.6); `optimize` der FTS-Tabellen; Konfiguration von Budget und Grenzen.

### Stand S1.6 (16.10.2026, erledigt): Threads, Body-Bereinigung, Anhänge

**Akzeptanz S1.6 erfüllt** (alles in `make check`, ohne Docker):

- **Threads des Korpus stimmen mit dem Manifest:** 60 Threads exakt, keine Einzelnachricht angehängt, in drei Reihenfolgen (Manifest, umgekehrt, wie der Sync) über `thread.Group` (Korpus-Test in `internal/mimeparse`) und nach einem Erstsync gegen den Testserver im Prozess in der Datenbank, dazu nach Löschen aller Threads über das Nachziehen des Bestands (`TestInitialSyncMirrorsCorpus`). Der Korpus enthält dafür sieben Antworten ganz ohne `References`/`In-Reply-To` (Betreff-Rückfall).
- **Mail mit verstecktem Text liefert `hidden_text`:** Die drei Korpus-Mails (`display:none`, weiß auf weiß, `font-size:0` über eine Klassenregel) tragen `bodies.hidden_text = 1` und verlieren die Anweisung; keine der übrigen rund 330 HTML-Mails wird markiert. `show` meldet `warning: hidden text removed`, `--json` `hidden_text_removed: true`. 17 Verstecktechniken und 10 Fehlalarm-Fälle einzeln getestet; Fuzz-Test ohne Absturz.
- **Extraktor-Timeout wird gehalten:** Ein hängender Fake-Extraktor wird nach 0,5 s beendet, der Aufruf kehrt zurück, der Prozess lebt nicht mehr (`TestTimeoutKillsHangingExtractor`, PID-Prüfung unter Unix); Ausgabegrenze und Umgebung ohne Geheimnisse ebenfalls getestet.
- **Magic-Bytes-Mismatch im Test:** Programm (`MZ`) als `Rechnung_2026-031.pdf` und PNG als `.jpg` werden erkannt (`type_mismatch` mit Grund, `executable`); keiner der übrigen rund 200 Korpus-Anhänge (PDF, DOCX, XLSX, Bilder, ZIP, Zip-Bombe, Text, weitergeleitete Mails, ohne Namen) gilt als Widerspruch; dazu eine Tabelle mit Erkennungs- und Vergleichsfällen.

**Umsetzung:**

- `internal/thread`: Zuordnung je Nachricht gegen einen Index (`Assign`; im Speicher und in SQLite): Verweise verbinden und vereinigen Threads, Antworten ohne Verweise kommen über den normalisierten Betreff (Präfixe Re/AW/WG/Fwd …, Listenmarken, nur Buchstaben und Ziffern) in den Thread im 30-Tage-Fenster; wachsende Threads sammeln solche Waisen ein (reihenfolgeunabhängig). Migration 0003: `thread_refs`, `threads.root_orphan`; der Sync ordnet in derselben Transaktion zu, Bestand ohne Thread wird nachgezogen.
- `internal/sanitize`: HTML → Text mit Allowlist, versteckter Text, Links sichtbar mit Warnungen, unsichtbare Formatzeichen, Quote-Stripper. Neue Abhängigkeit `golang.org/x/net` v0.61.0 (HTML-Parser, IDNA).
- `internal/magic`: Typ aus Magic Bytes (ZIP-Formate nur über Eintragsnamen), Vergleich mit Content-Type und Endung.
- `internal/extract`: PDF über `pdftotext` (wenn installiert), DOCX im eigenen Kindprozess, Text/HTML/Mail im Prozess; Grenzen für Zeit, Ein- und Ausgabe, weiches Speicherlimit, knappe Umgebung.
- `internal/mailsync`: speichert je Nachricht bereinigten Text, Links, `hidden_text`, Text ohne Zitate (auch für das `snippet`) und jeden Anhang dekodiert im Blob-Store mit Typprüfung; extrahiert am Ende jedes Laufs wartende Anhänge (Zeitbudget 60 s); bereitet Bestand mit älterer `clean_version` aus der Rohmail neu auf (die Datenbank der Abnahme D1 bekommt so beim nächsten Sync Threads, Texte und Anhänge).
- Operationen `threads.get`, `attachments.list`, `attachments.text` (R0, `mail:read`) und CLI `thread`, `attach list`, `attach text` (minimal), `show` mit bereinigtem Text, Links, Warnzeile und `--strip-quotes`.
- Von Hand gegen Dovecot (Korpus mit `corpusload`, frisches Konto): `sync --full` spiegelt 1.000 Nachrichten mit 0 Fehlern in rund 6 s einschließlich Aufbereitung und Extraktion; `show` der versteckten Mail, `attach list` (alle vier Extraktionen `ok`, HTML-Anhang mit `hidden text removed`) und `thread --message` wie erwartet.
- `make check`, `make race` (rund 5 min, der Sync-Test mit Extraktion im Kindprozess dominiert), `make integration` (Dovecot, Stalwart, GreenMail) und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 20,6 MB, windows/amd64 20,4 MB, darwin/arm64 19,3 MB.

**Befunde:**

- Im Korpus liegen Thread-Nachrichten zeitlich nicht immer in Antwortreihenfolge (eine Antwort kann vor dem Original datiert sein); Threading und Anzeige kommen damit zurecht, `thread` sortiert nach Datum.
- Newsletter mit verstecktem Vorschautext (`display:none`) werden ebenfalls als `hidden_text` markiert. Die Markierung ist ein Hinweis, kein Urteil (Frage 8 unten).
- `pdftotext` ist nicht Teil des Programms: Ohne Poppler (typisch unter Windows) bleiben PDFs `skipped` mit Grund; der PDF-Test wird dann übersprungen (Frage 10 unten).
- Der Speicher von Kindprozessen ist nur weich begrenzt (`GOMEMLIMIT`); für `pdftotext` gelten nur Zeit- und Eingabegrenze.

**Nicht in S1.6 (bewusst verschoben):** Text aus XLSX, PPTX, ODT und Bildern (OCR); Anhänge innerhalb weitergeleiteter Mails (deren Text wird extrahiert, ihre Anhänge nicht); Tabelle `jobs` (Extraktion läuft im Sync mit Zeitbudget; mit dem Index in S1.7); Zeitbudget und Grenzen der Extraktion in der Konfiguration; CSS mit verschachtelten Selektoren und `@media`; Untrusted-Hülle und MCP-Tools (S1.8/S1.9); `attach save` (S3).

### Stand S1.6 Schritt 0 (16.10.2026, erledigt): Auflagen N1–N3 und Watchdog

Auflagen aus dem Nachtrag 16.10.2026 zu A01 (Programmierstand des Betreibers):

- **N1 (Go 1.27, synchrone Timer-Kanäle, `asynctimerchan` entfernt):** Alle Timer-Stellen geprüft; keine verlässt sich auf einen gepufferten Wert oder leert `t.C` von Hand, keine Änderung am Verhalten nötig. Belegt mit `testing/synctest` (künstliche Uhr, exakte Zeitpunkte):

  | Stelle | Timer | Befund nach dem Go-1.27-Modell | Beleg |
  |---|---|---|---|
  | `internal/imapx/imapx.go`, `guard` (Watchdog) | `time.NewTimer`, `Reset` nur nach Empfang aus `t.C`, `Stop` per defer | unverändert korrekt; kein Leeren nötig | `TestWatchdogTimersSynctest`: Schweigen exakt 30 s, Rinnsal exakt 10 min (Obergrenze), Rückruf ausgenommen, kein Nachzünden nach Ende |
  | `internal/imapx/idle.go`, `Idle` (IDLE-Neustart) | ein `time.NewTimer(maxWait)`, `Stop` per defer | korrekt | `TestIdleRestartSynctest`: Rückkehr exakt nach 8 min, neue Nachricht nach exakt 90 s gemeldet (go-imap-Server über `net.Pipe` in der Blase) |
  | `internal/imapx/session.go`, `sleepCtx` (Backoff) | ein Timer je Wartezeit, `Stop` per defer | korrekt | `TestSessionBackoffSynctest`: 1 + 2 + 4 s = exakt 7 s; Abbruch im Warten endet sofort |
  | `internal/mailsync/watch.go`, `Watch` (Intervall) | `time.NewTicker`, `Stop` per defer | korrekt; ein Lauf länger als das Intervall verschiebt den nächsten auf sein Ende, weitere verpasste Ticks verfallen (wie vor Go 1.23) | `TestWatchTickerSynctest`: Läufe bei 0, 5, 12, 15 min (Lauf ab 5 min dauert 7 min) |
  | `internal/mailsync/watch.go`, `idleLoop` (Pause nach IDLE-Fehler) | vorher `time.After` in der Schleife, jetzt Timer mit `Stop` | beides korrekt (seit Go 1.23 sammelt die Laufzeit nicht gestoppte Timer ein); umgestellt der Deutlichkeit halber | `TestWatchIdleRetrySynctest`: Wiederaufbau exakt 30 s nach dem Fehler |
  | `internal/secrets/keyring.go` (Zeitgrenze Schlüsselbund) | `time.After` in einem `select` | korrekt (siehe oben) | bestehende Tests mit simulierter Zeitüberschreitung |
  | go-imap v2.0.0-beta.8 (Bibliothek) | `imapclient` IDLE-Neustart 28 min (`Reset` nach Empfang), Warten auf CAPABILITY (`NewTimer` + `Stop`) | korrekt; der IDLE-Neustart der Bibliothek greift nie, weil PostKauz nach 5 bis 10 min selbst neu startet | Quelltext geprüft |

  Nebenbei gefunden und behoben: Beendete ein Anmeldefehler eines Laufs `Watch`, lief die IDLE-Schleife mit eigener Verbindung weiter (`TestWatchStopsIdleLoopOnRoundAuthError`).
- **N2:** `make vuln` (govulncheck v1.9.0, Version per `go list -m golang.org/x/vuln@latest` aufgelöst, im Makefile gepinnt, Aufruf per `go run`, deshalb keine Werkzeug-Abhängigkeit in `go.mod`) und CI-Job `vuln`; Ergebnis: keine Schwachstellen. `golang.org/x/text` v0.14.0 → v0.43.0; `go mod tidy` änderte darüber hinaus nichts (die erwartete Zusammenfassung der require-Blöcke blieb aus, weil sie schon getrennt sauber waren).
- **N3:** Sync-Abläufe laufen ohne Docker in `make check` gegen den go-imap-Testserver im Prozess (`imapmemserver`, ausgebaut seit S1.4): Erstsync des Korpus, Wiederaufnahme nach Abbruch und `kill -9`, Änderungen von außen (Flags, Löschung, neue Nachricht), UIDVALIDITY-Wechsel, Dauerbetrieb mit IDLE. Neu: **Leck-Tests** für Erstsync und Dauerbetrieb (`internal/mailsync/leak_test.go`): Profil `goroutineleak` (Go 1.27) ohne Zeile aus diesem Modul und Vergleich aller Goroutinen vor/nach (offene go-imap-Verbindungen fallen so auch auf; von Hand geprüft: ein nicht geschlossener Provider wird gemeldet). Grenze: Der Testserver kann kein CONDSTORE; die Strategie `condstore` prüfen weiterhin nur die Integrationstests (Dovecot, Stalwart).
- **N6** (vor Go 1.28): Makefile, Skripte und CI rufen `go list` nicht mit `...` im Module-Mode auf (`grep`, 16.10.2026); nichts zu ändern.
- **N4** (vor S1.9) und **N5** (vor Schnitt 3) stehen aus.
- **Watchdog (ADR-007 Nachtrag 1, Betreiber-Entscheid zu Frage 6):** Schweigen bleibt die Messgröße (Standard 30 s), dazu Obergrenze je Kommando (Standard 10 min), beides konfigurierbar (`[imap]` in `config.toml`); Rückrufe zählen nicht als Schweigen; Rohmails in Gruppen bis 16 MiB je FETCH.
- `make check`, `make race`, `make integration` (Dovecot, Stalwart, GreenMail) und `make vuln` grün.

### Stand S1.4/S1.5 (15.10.2026, erledigt; Abnahme D1 bestanden 16.10.2026)

**Akzeptanz S1.4 erfüllt:** Die 1.000 Korpus-Nachrichten werden in Fenstern gespiegelt (Rohmail als Blob, Metadaten und geparste Felder in SQLite, Text in `bodies`), `sync --full -a <konto>` endet mit `1000 messages mirrored` und `0 errors` (Dovecot, Stalwart, GreenMail; Integrationstest und von Hand mit der Binärdatei). Abbruch mitten im Fenster, nach einem Fenster und **echtes `kill -9` eines Kindprozesses** (`TestKill9DuringInitialSync`; unter Windows wäre das `TerminateProcess`, dort aber **nicht ausgeführt, ungetestet**: nur kompiliert) setzen beim nächsten Lauf fort: dieselbe Anzahl, keine Doppelten (Nachrichten, Zuordnungen, Message-IDs geprüft). Von Hand gegen Dovecot: `kill -9` und Strg+C, danach `1000 messages mirrored`.

**Akzeptanz S1.5 erfüllt:**

- **Test je Strategie:** `condstore` gegen Dovecot und Stalwart, `uid-scan` gegen GreenMail und den In-Prozess-Server; je Server Flag-Änderung, Löschung und neue Nachricht von außen nach einem Lauf sichtbar (`TestIntegrationSync`). Leerlauf-Inkrementallauf bei 1.000 Nachrichten: 7 ms (Stalwart), 20 ms (Dovecot), 390 ms (GreenMail).
- **Blackhole:** Verstummt der Server während IDLE, fällt das beim fälligen Neustart am unbeantworteten `DONE` auf (Test mit 200 ms Neustart, erkannt nach rund 0,5 s), schweigt er schon vorher, beim IDLE-Start; danach Reconnect mit Backoff (S1.3).
- **IDLE:** neue Nachricht in der INBOX nach 60 bis 570 ms lokal sichtbar (Stalwart, GreenMail, Dovecot mit Hibernate); IDLE startet sich alle 5 bis 10 Minuten selbst neu (Standard 8, `--idle-restart`).
- **Flag von außen → `show <id> --headers` zeigt `seen`** (CLI-Test `TestSyncMailboxListShow`; für D1.6 `corpusload -mark-seen`).
- **UIDVALIDITY-Wechsel (P5):** Ordner gelöscht, neu angelegt, gleiche Nachrichten: Reset, Abgleich per Message-ID und Größe, keine Doppelten, **kein Inhalt erneut geladen** (alle drei Server); Nachrichten ohne Message-ID per `content_hash`.
- **P2** (50.000 Nachrichten lokal, Dovecot): Erstsync 78 s (638/s), Leerlauf-Abgleich 0,15 bis 0,19 s, `UID SEARCH ALL` über 50.000 UIDs 60 ms (ESEARCH) bzw. 72 ms, voller Flag-Abgleich 198 ms. Einzelheiten und WAN-Hochrechnung: [`spike/s0/REPORT.md`](../spike/s0/REPORT.md), Nachtrag P2. WAN-Messung durch den Betreiber offen.
- `mailbox list -a <konto> --json` mit `total` (laut Server), `mirrored` und `unread` je Ordner (D1.5, vorgezogen aus S1.8).

**Umsetzung:** `internal/mailsync` (Engine, Watch, Runner), IMAP-Provider `Changes`/`FetchHeaders`/`FetchRaw`/`Idle`, Store-Spiegel mit einer Transaktion je Fenster, Operationen `sync.run`, `mailboxes.list`, `messages.get`; Ablauf in [architektur.md](architektur.md), „Änderungserkennung". Migration 0002 (Rolle `all`, Sync-Phase, Flags je Ordnerkopie) mit Snapshot per `VACUUM INTO` vor der Migration. Testserver auf Dovecot 2.4.5 und GreenMail 2.1.14 festgelegt. `make race` grün (rund 55 s, der Korpus-Test dominiert).

**Befunde:**

- **Stalwart 0.11.8:** Nach vielen schnellen Schreibvorgängen kann die zwischengespeicherte Ordnerliste veralten, bis zur nächsten Änderung am Konto; neue Sitzungen sehen eben angelegte Ordner nicht. Der Korpus-Lader macht deshalb nach einer Sekunde eine kleine Änderung. Im Betrieb heißt das höchstens: Ein neuer Ordner erscheint einen Sync später.
- **GreenMail 2.1.14:** liefert für `BODY.PEEK[HEADER.FIELDS ("Message-ID")]` nichts (go-imap setzt Feldnamen in Anführungszeichen), daher holt der Sync den ganzen Kopf; sendet bei manchen rohen 8-Bit-Kopfzeilen eine falsche Literal-Länge (go-imap verliert den Takt; jetzt `ErrProtocol`, die Nachricht wird eingegrenzt und als Fehler übersprungen); einzelne `UID FETCH` dauern bei 18.000 Nachrichten im Ordner 12 bis 13 s (für Lastmessungen ungeeignet).
- **go-imap-Testserver** (In-Prozess): liefert für Nachrichten mit einer Kopfzeile ohne Doppelpunkt einen leeren `BODY[]`; der Sync zählt das als Fehler (Server meldet Größe, liefert nichts) statt eine leere Nachricht zu speichern. Die Unit-Tests laden diese zwei Korpus-Nachrichten deshalb nicht.
- **Kopien derselben Nachricht** (Gmail-Labels, Kopien, verschobene Nachrichten) werden eine Zeile in `messages` mit mehreren Zuordnungen; Flags stehen je Kopie, die Nachricht zeigt ein Flag, wenn eine Kopie es trägt (Frage an den Betreiber unten).

**Nicht in S1.4/S1.5 (bewusst verschoben):** Tabelle `jobs` (der Dauerläufer arbeitet ohne sie; mit Index/Extraktion S1.6/S1.7); `sync --watch` für mehrere Konten in einem Prozess (je Konto ein Prozess); Threads, Anhänge, HTML-Text (S1.6); volles `show` mit Untrusted-Hülle (S1.8/S1.9); `--quiet` (S1.8); Gmail `X-GM-EXT-1` (P1, manuell; bis dahin Abgleich der „Alle Nachrichten"-Dubletten per Message-ID); Speicherbedarf beim Erstsync messen (mit der WAN-Messung).

### Stand S1.3 (15.10.2026, erledigt)

**Akzeptanz S1.3 erfüllt:** Integrationstest gegen Dovecot 2.4, Stalwart 0.11.8 und GreenMail (`make testmail-up testmail-setup integration`): erwartete Fähigkeiten, Strategie (`condstore`, `condstore`, `uid-scan`), Rollen und `ErrAuth` bei falschem Passwort, je Server grün. Ohne `POSTKAUZ_IT` werden die Tests übersprungen; `make check` läuft ohne Docker. Watchdog-Tests mit stummem Server: Blackhole-Listener ohne Gruß und Blackhole-Proxy mitten in der Sitzung werden in der konfigurierten Deadline (300 ms) erkannt; danach baut die Session mit Backoff neu auf. `make race` grün (lokal, auch mit den Integrationstests) und als CI-Job.

- **`internal/imapx`:** `Session` (Reconnect mit exponentiellem Backoff und Jitter, Standard 1 s bis 5 min, höchstens 5 Versuche je Aufruf; `ErrAuth` und Ablehnungen `ErrRejected` nie wiederholt; Kontextabbruch schließt ein hängendes Kommando), LIST mit `RETURN (SPECIAL-USE)` und LIST-STATUS, CREATE (idempotent), NOOP. `make guard` prüft zusätzlich den Pin auf v2.0.0-beta.8.
- **`internal/provider/imap`:** `Connect`, `Capabilities`, `Strategy`, `ListMailboxes` (Rollen: SPECIAL-USE vor Namen, jede Rolle einmal, Gmail-Namen unter `[Gmail]`/`[Google Mail]` als Rückfall; neue Rolle `all`). `account test` nutzt denselben Weg. `MailProvider` vollständig erst mit S1.4/S1.5.
- **Testserver:** `deploy/testmail/stalwart-setup.sh` (`make testmail-setup`) richtet Stalwart ein (IPv4, Domain, Konto `test`).
- **Befunde:** Stalwart nennt SPECIAL-USE nur auf `RETURN (SPECIAL-USE)` und liefert in LIST-STATUS `HIGHESTMODSEQ 0` trotz CONDSTORE (S1.4: Wert aus SELECT nehmen); GreenMail kann weder SPECIAL-USE noch LIST-STATUS (Zustand erst mit SELECT) und antwortet auf CREATE eines vorhandenen Ordners ohne `ALREADYEXISTS`; Dovecot meldet zusätzlich `COMPRESS=DEFLATE` und `BINARY` (P6).
- **Beifang:** CI auf `actions/checkout@v7` und `actions/setup-go@v7` (Dependabot-PRs #1/#2 damit überholt).

**Nicht in S1.3 (bewusst verschoben):** IDLE und dessen Neustart alle 5 bis 10 Minuten (S1.5; braucht eine eigene Verbindung, weil `Session.Do` Aufrufe nacheinander ausführt); SELECT/FETCH/Changes (S1.4/S1.5); Rolle `all` in der Datenbank (Migration 0002, S1.4).

### Stand S1.2 (15.10.2026, erledigt)

**Akzeptanz S1.2 erfüllt:** Der Parser besteht den Standardkorpus (1.000 Nachrichten, Samen 42) gegen das Manifest: Betreff, Absender, Empfänger, Message-ID, Datum samt Quelle, Anhangsnamen, Inline-Teile, Textprobe, List-Id; die 60 Threads werden exakt gebildet, keine Einzelnachricht wird angehängt (`TestParserAgainstCorpusManifest`). Eine Zip-Bombe (256 MiB entpackt) wird nicht entpackt, Größe und Hash sind die der gepackten Bytes; zu große Teile werden nur bis zur Grenze dekodiert (`TestZipBombIsNotExtracted`, `TestOversizedPartIsTruncated`, `TestNoDecompressorImports`).

- **Blob-Store** `internal/blob`: SHA-256 der unkomprimierten Bytes, `blobs/ab/cd/<sha256>`, atomar über `blobs/tmp` + fsync + rename, Deduplizierung, optional zstd (`.zst`), Größenlimit 256 MiB, `Verify`. Neue Abhängigkeit `github.com/klauspost/compress` v1.20.3 (BSD-3; Go 1.27 hat keinen zstd-Encoder). Die Zeile in `blobs` schreibt erst der Sync (S1.4).
- **Parser** `internal/mimeparse` auf `go-message` mit `charset.Reader` (ADR-007): RFC 2047 B/Q in allen Zeichensätzen, rohe 8-Bit-Köpfe als Windows-1252, kaputte Kopfzeilen und NUL-Bytes entfernt, Message-IDs tolerant (auch ohne Klammern), Datum tolerant (Zone ohne Vorzeichen; fehlend/unlesbar → `INTERNALDATE`, ohne beides Nullzeit mit `date_source = none`); Limits für Kopf, Rohgröße, Teile, Tiefe, Teilgröße, Text. Abweichungen stehen in `Problems`, nicht als Fehler.
- **Threading** `internal/thread`: Union-Find über Message-ID, In-Reply-To, References (Spike-Verfahren). Betreff-Rückfall im Zeitfenster folgt mit S1.6.
- **Korpus** `internal/corpus`, `make corpus` (Inhalt und Lücken: [testkorpus.md](testkorpus.md), Abschnitt 1a).

**Nicht in S1.2 (bewusst verschoben):** Magic-Bytes, HTML-Bereinigung, versteckter Text, `message/rfc822` rekursiv lesen (S1.6); Betreff-Threading (S1.6); Blobs in der Datenbank verbuchen (S1.4); `doctor --blobs` (S1.11).

### Stand S1.1 (15.10.2026, erledigt)

**Akzeptanz S1.1 erfüllt:** Konto anlegen und auflisten (aus der Datenbank); Passwort nur im Schlüsselbund bzw. in der Umgebung, Kanarientest grün (Datenbank samt WAL, Log, Audit, stdout/stderr; auch über eine echte IMAP-Anmeldung an einen Testserver im Prozess); `doctor` meldet Pfade, Schlüsselbund, Datenbank und Schemaversion.

- **Konfiguration** `internal/config`: TOML (`POSTKAUZ_CONFIG`), unbekannte Schlüssel sind Fehler; `POSTKAUZ_DATA_DIR`, `POSTKAUZ_SECRETS_BACKEND`; Datenverzeichnis je Betriebssystem (Windows `%LOCALAPPDATA%`, nicht Roaming), 0700, Warnung bei Cloud-Ordnern.
- **Store:** Konten-Repository, SQLite-Audit-Writer (ersetzt `audit.Nop` im CLI), Schema-Schutz gegen neuere Datenbanken, Datenbankdatei 0600. `0001_init.sql` um `accounts.insecure_test_server` ergänzt (letzte Änderung an 0001; ab jetzt nur neue Migrationen).
- **Executor:** Schwärzung je Operation (`Redact`: nur Feldnamen und Ziele wie `account:demo`), Geheimnisse getrennt von der Eingabe (`Call.Secret`). **Policy-Korrektur:** `--dry-run` gilt jetzt auch für R1 (lokal schreibende Operationen); vorher wurde es bei R1 stillschweigend ignoriert.
- **Operationen** `accounts.add/test/remove` (Scope `admin`, R1, nicht über MCP), `accounts.list` aus der Datenbank. `account remove` verweigert bei archivierten Nachrichten (ADR-003).
- **P4 erledigt:** Schlüsselbund über `go-keyring` mit Zeitgrenze; ohne Schlüsselbund kein stiller Rückfall, sondern Hinweis auf `POSTKAUZ_PASSWORD_<NAME>`/`--password-from-env`; unsicherer Dateispeicher nur ausdrücklich (`insecure-file`, für Testserver). Getestet mit simuliertem D-Bus-Fehler, Zeitüberschreitung und dem echten Schlüsselbund dieses Linux-Containers (kein Secret Service → `unavailable`). **Windows und macOS** sind nur kompiliert, nicht ausgeführt: Abnahme D0 Punkt 5 und D1 Punkt 1 auf dem Windows-Rechner des Betreibers.
- **P3 als Entwurf:** SASL XOAUTH2 (eigen) und OAUTHBEARER (go-sasl) in `internal/imapx`, Token-Quelle mit Refresh und Sperrzeit in `internal/oauth`; Tests gegen einen Fake-IMAP-Server und einen Fake-Refresher. Offen: Autorisierungs-Flow (Loopback/Device Code), HTTP-Refresher, Kontenanlage mit `oauth2`.
- **`internal/imapx` vorgezogen (Ausschnitt):** Dial mit TCP-Keepalive, Watchdog je Kommando (Blackhole-Test grün), `WordDecoder` mit Charset-Reader (Test), Login, CAPABILITY, LIST; `internal/provider/imap` bestimmt daraus Fähigkeiten, Ordnerrollen (SPECIAL-USE, sonst Namen auf Deutsch/Englisch) und die Strategie (QRESYNC maskiert, ADR-007). `make guard` prüft, dass nur `imapx` go-imap importiert. Handgeprüft gegen Dovecot (`condstore`, Rollen INBOX/Sent/Drafts/Trash/Junk) und GreenMail (`uid-scan`); TLS mit selbstsigniertem Zertifikat (Dovecot 11993) erreicht die Anmeldung.
- **Binärgrößen** (`make cross`, ohne `-s -w`): linux/amd64 17,6 MB, windows/amd64 17,1 MB, darwin/arm64 16,4 MB; mit `-s -w` 12,0 / 11,7 / 11,1 MB (Spike: rund 11 MB).
- `go test -race ./...` lokal grün (braucht CGO; seit S1.3 `make race` und CI-Job, P7).

**Nicht in S1.1 (bewusst verschoben):** globale Optionen `--account`, `--key`, `--quiet`, `--profile` (mit S1.8); `doctor --secrets` und `doctor --blobs` (S1.11); `VACUUM INTO` vor Migrationen (mit der ersten Migration 0002); gemountete Secret-Dateien.

**Umfang:** Konto anlegen und testen (`account add/test`); Erstsync in Fenstern, inkrementell mit CONDSTORE wo vorhanden, Löschungen per UID-SEARCH-Abgleich (QRESYNC fehlt in `go-imap`, ADR-007), IDLE auf INBOX, Reconnect mit Backoff und Watchdog; Spiegel (Rohmail im Blob-Store, Metadaten in SQLite); MIME-Parser und Threading; Textextraktion für PDF/DOCX/TXT (abschaltbar); Volltextsuche mit deutscher Stemmung und Trigram; CLI mit `--json`; stdio-MCP **lesend** (Tools, Resources, Prompts aus [mcp.md](mcp.md)); Audit; Import aus mbox/Maildir mit Message-ID-Abgleich und Überlappung; Schlüsselverwaltung.

**Nicht enthalten:** Entwürfe, Tags, Verschieben, Papierkorb, HTTP, REST, Regeln, Versand.

**Akzeptanzkriterien:**

1. **Zwei Serverfamilien bestehen die Testmatrix** (Dovecot, Stalwart; je per Docker), nachts und auf Abruf. Ein dritter Anbieter (z. B. ein gehostetes Postfach) wird vom Betreiber **manuell** gegengeprüft; er ist nie Teil der CI.
2. **Erstsync:** Ein synthetisches Postfach mit 50.000 Nachrichten (Testkorpus rund 245 MB, rund 4,9 KB je Nachricht; lokal gemessen 78 s, P2) wird auf einem normalen PC in unter 60 Minuten gespiegelt; echte Postfächer mit Anhängen sind deutlich größer (Hochrechnung mit 5 GB über WAN in `spike/s0/REPORT.md`, Messung durch den Betreiber offen); im Leerlauf dauert ein inkrementeller Abgleich unter 5 Sekunden. Ein Abbruch mitten im Erstsync (`kill -9`) setzt ohne Doppelte fort (Test).
3. **Änderungen von außen:** Eine Flag-Änderung oder Löschung auf dem Server ist nach spätestens einem Sync-Intervall lokal sichtbar; in der INBOX nach wenigen Sekunden (IDLE). Ein `UIDVALIDITY`-Wechsel führt zu einem Abgleich per Message-ID, nicht zu Verlust oder Doppelten (Test).
4. **Suche:** Antwort unter 300 ms bei 100.000 Nachrichten; deutsche Flexionen, Umlaute und Teilwörter werden gefunden (Testsatz aus [testkorpus.md](testkorpus.md)).
5. **Import:** Ein mbox- bzw. Maildir-Bestand mit Überlappung zu einem bereits gesyncten Konto erzeugt keine Doppelten; die Zählung (neu/vorhanden/übersprungen) wird ausgegeben.
6. **MCP:** Claude Code und ein zweiter MCP-Client sehen die lesenden Tools; Tool-Liste folgt den Scopes; jede Ausgabe mit Mailinhalt ist in der Untrusted-Hülle (Test); HTML-Mails mit verstecktem Text liefern `hidden_text_removed: true` (Test).
7. **Audit:** Jede Operation erzeugt ein Ereignis ohne Mailinhalt; `UPDATE`/`DELETE` sind gesperrt; der tägliche Export mit Prüfsumme läuft; `audit verify` erkennt eine veränderte Zeile (Test).
8. **Geheimnisse:** Mit Kanarienwerten als Passwort und Schlüssel taucht kein Wert in Logs, Audit, Fehlermeldungen, `--json` oder MCP-Ausgaben auf (Test).
9. **Qualität:** Jede Operation hat einen Test; `go vet` und `go test ./...` grün; Cross-Compile für linux/amd64, windows/amd64, darwin/arm64 grün (CI).
10. **Installation:** Windows 11 und Linux: Binärdatei laden, `postkauz doctor`, Konto anlegen, erster Sync in unter 15 Minuten nach README.

## S2: Entwürfe (draft-only)

### Stand S2.0, Teil 1 (A2, A6, A8)

Auftrag [A02](../auftraege/A02_s2.md), Teil A (Nachbesserung und Vorarbeiten), Punkte A2, A6 und A8; A1, A3, A4 und A5 laufen getrennt. Migration `0008_s2_prep.sql` (Schema 7 → 8) enthält die Schemaänderungen von A2 und A6.

**A2: Schlüsselbund-Verweis je Datenverzeichnis (Frage 6 b) — erledigt.**

- **Akzeptanz** („Test mit Fake-Schlüsselbund: zwei Datenverzeichnisse, gleicher Kontoname, getrennte Einträge; Umzug getestet"): `TestTwoDataDirsSameAccountNameSeparateKeyringEntries` (CLI, ein gemeinsamer Schlüsselbund im Speicher, zwei Datenverzeichnisse mit Konto `demo` und verschiedenen Passwörtern: zwei Einträge, jedes Konto liest seins, `account remove` in B lässt A unberührt), `TestLegacyKeyringEntryMovedOnOpen` (Konto mit altem Verweis `account/demo`: `doctor` warnt, das nächste Kommando zieht um, danach ein Eintrag unter dem neuen Namen und keine Warnung), `TestLegacyKeyringEntryLeftDoctorWarns` (Löschen scheitert: Kommando läuft, Warnung auf stderr ohne Passwort, beide Einträge bestehen, `doctor` warnt, bis der alte gelöscht ist), `TestLegacyKeyringEntryPostponedWithoutKeyring` (ohne Schlüsselbund, etwa Linux-Server mit `POSTKAUZ_PASSWORD_<NAME>`: nichts zieht um, das Kommando läuft still, `doctor` sagt, dass der Umzug wartet und die Variable gilt), `TestMoveCredentialRefs*` (`internal/ops`: Umzug, Passwort nur aus der Umgebung → nur der Verweis wechselt und nichts landet im Schlüsselbund, zweiter Lauf ohne Wirkung, Schlüsselbund nicht erreichbar → nichts versucht, Schreiben scheitert → alter Verweis bleibt), `TestDataDirIDCreatedOnceAndDistinct` (`internal/store`: Kennung entsteht bei der Migration eines Bestands mit Schema 7, bleibt beim erneuten Öffnen gleich, zwei Datenbanken haben verschiedene, `credential_ref` unverändert), `TestStoredIgnoresEnvironment`, `TestRefAndEnvName` (`internal/secrets`). Ohne die Änderung scheitern die drei CLI-Tests (geprüft: ein gemeinsamer Eintrag `account/demo`; keine Warnung; kein Umzug).
- **Umsetzung:** Migration 0008 legt `meta.datadir_id` an (32 Hex, `randomblob`); `store.DataDirID` liefert die ersten 8 Hex. `secrets.AccountRef(datadirID, name)` = `account/<datadir-id>/<name>`, `secrets.LegacyAccountRef`, `ValidRef` erlaubt die Kennung als mittleren Teil, `EnvName` nimmt nur den Kontonamen (`POSTKAUZ_PASSWORD_<NAME>` bleibt), `secrets.Stored` liest unter der Umgebungsschicht. `accounts.add` vergibt den neuen Verweis. `ops.MoveCredentialRefs` (keine Registeroperation, ein Schritt beim Öffnen wie `AuthenticateKey`) zieht je Konto mit altem Verweis um: lesen alt → schreiben neu → zurücklesen und vergleichen → `credential_ref` umstellen → löschen alt; scheitert das Löschen, steht der alte Verweis in `meta.credential_refs_left`. Die CLI ruft den Umzug in `open` auf, der MCP-Server nicht. `doctor` warnt für Konten mit altem Verweis und für noch bestehende alte Einträge.
- **Abweichung von A02:** Die Migration (SQL) stellt `credential_ref` **nicht** selbst um, sondern legt nur die Kennung an; den Verweis stellt der Umzug im Programm um, und zwar erst, nachdem der neue Eintrag geschrieben und geprüft ist. Grund: SQL kann den Schlüsselbund nicht lesen; hätte die Migration den Verweis umgestellt, zeigte er bei nicht erreichbarem Schlüsselbund (gesperrt, kein D-Bus) ins Leere. Bis zum Umzug gilt der alte Verweis weiter.
- **Befunde:** (1) Hatten zwei Datenverzeichnisse schon unter S1 ein Konto mit gleichem Namen (gemeinsamer Eintrag `account/<name>`), zieht das zuerst geöffnete den Eintrag um und löscht den alten; das zweite findet danach keinen Eintrag mehr (`doctor`: `password: missing`). Abhilfe heute: Passwort über `POSTKAUZ_PASSWORD_<NAME>` oder Konto neu anlegen (geht nur ohne archivierte Nachrichten). Ein Kommando zum Neusetzen des Passworts fehlt (siehe „Offene Fragen"). (2) Eine Kopie des Datenverzeichnisses (Wiederherstellung) behält die Kennung und teilt auf demselben Rechner den Eintrag mit dem Original; gewollt (die Kopie soll mit dem Passwort weiterlaufen). (3) Dass das Programm nach dem Umzug nur noch den neuen Namen kennt, heißt auch: Eine ältere Programmversion (0.1.0-rc1) findet das Passwort nicht mehr; sie verweigert ohnehin die Datenbank mit Schema 8 (`too_new`).

**A6, Vorarbeit: Operationen nur für Menschen (E2, minimal) — erledigt.** `ops.Operation.HumanOnly` und `policy.Requirement.HumanOnly`; `policy.Key.Human` (nur der Mensch an der CLI ohne `--key`; Standard „Schlüssel", fail closed; eine ID `key:<name>` ist nie Mensch). `policy.Check` lehnt HumanOnly mit Schlüssel vor allen anderen Regeln mit `human_only` ab (`TestCheckHumanOnly`: jede Rolle, mit und ohne Trockenlauf; `TestExecutorHumanOnly`: verweigert und auditiert, Handler läuft nie, der Mensch darf). Der Vertragstest `TestContractEveryOperationRoleAndDryRun` läuft jetzt zusätzlich mit der Rolle `human` (Mensch ohne Schlüssel) und erwartet für HumanOnly-Operationen bei jedem Schlüssel `deny`/`human_only` im Audit; `TestEveryOperationIsBoundOrExcluded` verlangt für sie einen Eintrag in `notOverMCP`; `policy.get` listet sie für Schlüssel nicht. Ohne die Prüfung in `policy.Check` scheitern `TestCheckHumanOnly` und `TestExecutorHumanOnly` (geprüft). Der Rest von E2 (Information „Schlüssel im Spiel" für Freigaben, `policy show` mit Grund je Operation) folgt in S2.1 ff.

**A6: Aliasse (Frage 10) — erledigt.**

- **Akzeptanz** („Test: Alias im Betreff-Rückfall der Threads"): `TestThreadSubjectFallbackTreatsAliasesAsOwn` (`internal/store`: Gespräch von Anna an den Alias, dann „AW: Rechnung" ohne Verweise von einem Fremden an denselben Alias; ohne Alias am Konto landet der Fremde im Thread, mit Alias nicht; Annas eigene Antwort ohne Verweise hängt in beiden Fällen an). Ohne die Änderung in `selfAddresses` scheitert der Test (geprüft: „orphan reply of a stranger joined only via the alias"). Dazu `TestAccountsSetAliases` (`internal/ops`: Trockenlauf, Normalisierung, ungültige Adressen, leere Liste, unbekanntes Konto, Audit nur mit Feldnamen und `aliases_count`, admin-Schlüssel `human_only`), `TestAccountSetAliasesCLI` (`account set`, `accounts list` als Text und `--json`, `--key adm` → Exit 3 `human_only` ohne Wirkung, `policy show --key` ohne `accounts.set`), Vertragstests Register ↔ CLI (`TestCLIContractRegistry`, neues Beispiel) und Register ↔ MCP (`accounts.set` in `notOverMCP`), `TestContractEveryOperationRoleAndDryRun` mit `accounts.set` (jede Rolle als Schlüssel `deny`/`human_only`, der Mensch schreibt).
- **Umsetzung:** Migration 0008: `accounts.aliases` (JSON-Liste, `CHECK` auf Array). `store.Account.Aliases`, `SetAccountAliases`; `selfAddresses` (eigene Adressen im Betreff-Rückfall) nimmt die Aliasse dazu. Neue Operation `accounts.set` (R1, `admin`, HumanOnly, idempotent; Eingabe `name`, `aliases` ersetzt die Liste; Adressen per `net/mail` geprüft, nur Adresse ohne Namen, klein, sortiert, ohne Dubletten und ohne Kontoadresse, höchstens 50; Redact: Feldnamen und Anzahl). `accounts.list` liefert `aliases` (immer eine Liste; auch MCP `mail_list_accounts` und `mail://accounts`), die CLI zeigt die Spalte `ALIASES`. CLI `account set <name> --aliases a@…,b@…` (`--aliases ""` leert). Doku: `docs/cli.md` (2a), `docs/mcp.md`, `docs/datenmodell.md`, `docs/sicherheit.md` (Abschnitt 2).
- **Befunde:** (1) Geänderte Aliasse ordnen vorhandene Nachrichten nicht neu zu (wie bisher bei allen Regeln des Rückfalls); wer Aliasse nachträgt, behält bestehende Fehlzuordnungen. (2) `accounts.set` kennt vorerst nur `aliases`; das Schema ist so angelegt, dass weitere Felder (z. B. ein neues Passwort, siehe „Offene Fragen") dazukommen können.

**A8: Entschiedene Fragen — erledigt.** Die Tabelle „Entscheide des Betreibers (18.10.2026)" aus A02, Abschnitt 0, steht unter „Entschiedene Fragen" als Nummern 19 bis 33 (Datum 18.10.2026, Verweis auf A02, frühere Nummer); „Offene Fragen" ist bis auf die neue Frage aus S2.0/A2 geleert (aus A02 selbst derzeit keine). Neuer Abschnitt „Umfeld: verwandte Projekte (Stand 21.10.2026)" mit dem Ergebnis des Make-or-buy-Gates (Text des Betreibers, unverändert); `README.md`, „Non-goals": kein Archiv für Chat, Kalender und Meetings, keine Web-UI vor 1.0 (Verweis auf msgvault). Nachweis: diese Roadmap.

**Umfang:** `drafts.*`: neu, antworten, allen antworten, weiterleiten mit korrekten `Message-ID`/`In-Reply-To`/`References`, Zitat, Anhänge aus vorhandenen Nachrichten oder Dateien; Ablage per `APPEND` im Entwürfe-Ordner; Policy-Datei; Freigabe-Zustände (`draft` → `pending_approval` → `ready` → `sent_by_human`) mit `preview_hash`; Prüf-Hook; Erkennung des Versands im Ordner „Gesendet"; Resources `mail://…/draft/…` und `mail://style/…`; Prompt `draft_reply`.

**Nicht enthalten:** SMTP, `mail_request_send`, `auto`-Versand, Regeln.

**Akzeptanzkriterien:**

1. Ein über MCP entworfener und vom Menschen im Mailclient gesendeter Entwurf erscheint beim Empfänger **im richtigen Thread** (Prüfung der Header-Kette bei drei Empfängertypen, z. B. Webmail, Desktop-Client, zweiter Anbieter; mit eigenen Testpostfächern).
2. Der Entwurf ist im Entwürfe-Ordner **in einem zweiten Mailclient sichtbar** und trägt die vorab vergebene `Message-ID`.
3. Der Versand durch den Menschen wird erkannt (`sent_by_human`), auch wenn der Client eine neue `Message-ID` vergibt (Rückfall-Abgleich; Verhalten je Client dokumentiert).
4. **Kein Agenten-Schlüssel kann `ready` setzen.** Bewiesen durch Vertragstests, die jede R1–R3-Operation mit jedem Scope-Satz durch den Executor schicken.
5. Eine Änderung nach der Freigabe macht diese ungültig (`preview_hash`-Test).
6. Der Prüf-Hook blockiert den Übergang nach `pending_approval`, wenn er scheitert.
7. Jeder Zustandsübergang steht mit Akteur im Audit.
8. Es existiert **kein SMTP-Code** im Repository (Test oder CI-Prüfung auf Importe von `net/smtp`).

## S3: Aktionen, Sicherung, Fernzugriff

**Umfang:** Tags (lokal, optional als IMAP-Keywords); `mark`, `move`, `trash` (nur bis zum Papierkorb, `confirm_count`); `attachments.save` ins Exportverzeichnis; Export (mbox/EML), Backup (`VACUUM INTO` + Blobs) und Restore; Löschkonzept; **HTTP-MCP mit OAuth 2.1** aus Standardkomponenten auf einem Dauerläufer (EU-VPS, privates Netz, TLS), Betriebsanleitung (Windows-Aufgabe, systemd, Docker).

**Akzeptanzkriterien:**

1. Tag-, Flag-, Verschiebe- und Papierkorb-Operationen sind im Trockenlauf und real getestet; kein Agenten-Schlüssel erreicht endgültiges Löschen (Vertragstest, Provider-Interface enthält kein Expunge).
2. Lokale Änderungen landen innerhalb von 10 Sekunden auf dem Server; Konflikte löst der nächste Sync mit „Server gewinnt" (außer lokale Tags).
3. **Restore-Test:** Ein Backup wird auf einer sauberen Maschine wiederhergestellt; Suche und Audit liefern identische Ergebnisse.
4. Export als mbox/EML-Verzeichnis ist von einem Standard-Mailclient lesbar.
5. HTTP-MCP: Ein Cloud-Agent erreicht den Dienst über das private Netz; Token-Weitergabe wird abgelehnt; ohne Schlüssel kein Zugriff; Zertifikat und Ablauf geprüft.
6. Backups und Datenbank liegen verschlüsselt bzw. auf verschlüsseltem Datenträger; die Anleitung benennt das ausdrücklich.

## S4 und später (nicht zugesagt)

Reihenfolge nach Nutzen und Risiko, jeweils mit eigener Entscheidung:

1. **SMTP-Versand mit Freigabe** (`mail_request_send`, Scope `mail:send`, Cool-down, Limits, Allowlist). Erst nach bewährtem draft-only-Betrieb.
2. **Regeln** mit Trockenlauf gegen die letzten N Tage; Agenten schlagen nur vor (deaktiviert), Menschen aktivieren.
3. **REST + OpenAPI** als dritter Adapter über demselben Register.
4. **JMAP-Provider** (gegen Stalwart und einen JMAP-Anbieter).
5. Gmail-API-/Graph-Anbindung (bring your own client), Embeddings/semantische Suche (lokal), Mehrbenutzerbetrieb mit Rollen je Mensch.
6. **GUI** frühestens nach 1.0 und nur gegen REST (siehe Nicht-Ziele).

## Umfeld: verwandte Projekte (Stand 21.10.2026)

Am 21.10.2026 hat der Betreiber ein Make-or-buy-Gate durchgeführt (Vergleichsdokument außerhalb des Repos). Nächster Verwandter ist **msgvault** (github.com/kenn-io/msgvault; Go, MIT, Wes McKinney; lokales Archiv für Mail, Chat, Kalender und Meetings mit SQLite+FTS5, CLI, HTTP-API, TUI, Web-UI, MCP; IMAP-Sync seit 03/2026, Entwürfe draft-only seit 09/2026). Messung gegen denselben Dovecot und Korpus (v0.21.0): deutscher Suchtestsatz 7/22 (PostKauz 22/22), Thunderbird-Import gegen das gesyncte Konto +612 Doppelte (PostKauz 0), versteckter Text wird unmarkiert ausgegeben, IMAP-Passwort im Klartext, kein Audit der Agentenaktionen, kein IDLE, Agent-Tokens nur im RAM, Telemetrie standardmäßig an, 146-MB-CGO-Binary. msgvault hat, was PostKauz nicht hat: Web-UI, TUI, PST-Import, Vektorsuche, über 60 Beiträger. Entscheid: PostKauz wird weitergebaut; msgvault ist Referenz für Import-Formate und Backup-Kommandos und wird im Januar 2027 neu geprüft. Weitere geprüfte Projekte: Himalaya (Rust-CLI, kein Spiegel), OpenAgentEmail (Agenten-Postfächer), mbsync+notmuch+MCP (Unix-only), codefuturist/email-mcp (Wrapper mit Cache, sendet selbst).

## Entschiedene Fragen (14. bis 18.10.2026)

1. **Gate:** bestanden mit Auflagen (ADR-007).
2. **Go-Version:** Zielversion 1.27, Anhebung als Schritt 0 von A01 (ADR-008).
3. **Postfach-Aktionen:** Tags, Move und Trash bleiben in S3.
4. **Sprache:** Programmausgaben Englisch, Doku Deutsch.
5. **Erkennung des Versands (S2):** `Message-ID` zuerst, **Rückfall-Abgleich** über Empfänger, Betreff, Body-Hash und Zeitfenster; das Verhalten je Client wird in S2 gemessen.
6. **Commit-Sprache** (15.10.2026): Englisch.
7. **Passwörter ohne Schlüsselbund** (15.10.2026): kein Dateispeicher mit Passphrase; es bleibt bei Umgebungsvariable bzw. `insecure-file` nur für Testserver.
8. **Race-Detector** (15.10.2026): eigenes Ziel `make race` (CGO) und CI-Job ab S1.3, nicht Teil von `make check`.
9. **Ordnerrolle `all`** (15.10.2026): kommt mit Migration 0002 in die Datenbank (umgesetzt).
10. **Testserver-Versionen** (15.10.2026): Dovecot 2.4.5 und GreenMail 2.1.14 festgelegt (umgesetzt), Stalwart bleibt 0.11.8.
11. **Snapshot vor Migrationen** (15.10.2026): `VACUUM INTO` mit Migration 0002 umgesetzt.
12. **Watchdog** (16.10.2026, vorher offene Frage 6): Schweigen messen bleibt, dazu Obergrenze je Kommando, konfigurierbar, Standard 10 min (ADR-007 Nachtrag 1, umgesetzt).
13. **Testkorpus im Repository** (16.10.2026): bleibt synthetisch (Commit-Sprache Englisch bestätigt, siehe 6).
14. **Versteckter Vorschautext in Newslettern** (16.10.2026, vorher offene Frage 8): bleibt als `hidden_text` markiert (Hinweis, kein Urteil).
15. **Extraktion als Warteschlange** (16.10.2026, vorher offene Frage 9): mit S1.7 über die Tabelle `jobs` zusammen mit dem Index (umgesetzt).
16. **PDF unter Windows** (16.10.2026, vorher offene Frage 10): nur über das optionale `pdftotext`, Installation im README (umgesetzt; übersprungene PDFs werden nachgeholt, sobald das Werkzeug da ist).
17. **Teilweise geladene Thunderbird-Nachrichten** (16.10.2026, vorher offene Frage 15): werden mit Hinweis importiert, nicht übersprungen; kommt die vollständige Fassung später per Sync, stehen beide im Archiv (umgesetzt S1.10).
18. **Täglicher Audit-Export** (16.10.2026, Auftrag S1.11): als Windows-Aufgabe (PowerShell-Registrierungsskript, Windows PowerShell 5.1, UTF-8 mit BOM, ASCII, keine Aliase) und als systemd-Timer; Ziel konfigurierbar (`[audit] export_dir`, z. B. ein Google-Drive-Ordner); JSONL mit SHA-256-Kette je Datei und Manifest (umgesetzt S1.11).

Entscheide des Betreibers vom **18.10.2026** ([A02](../auftraege/A02_s2.md), Abschnitt 0, „Entscheide des Betreibers"): Alle bis dahin offenen Fragen (Stand nach S1.11) sind mit ihrer Standardantwort entschieden. Die Nummern 19 bis 33 entsprechen den früheren offenen Fragen 1 bis 15; die Abschnitte „Stand S1.x" oben verweisen mit „Frage N" auf diese früheren Nummern (Frage N = Nummer 18 + N).

19. **Name** (18.10.2026, A02; vorher Frage 1): PostKauz ist geprüft und bleibt (15.10.2026); Umbenennung erledigt.
20. **Mehrere Menschen an einem Postfach** (18.10.2026, A02; vorher Frage 2): Im MVP gibt **ein** Mensch frei; `decided_by` ist der Name aus `[operator] name` der Konfiguration, sonst der Benutzername des Betriebssystems. Zuordnung je Person kommt mit Mehrbenutzerbetrieb (S4+).
21. **Spike-Lücken** (18.10.2026, A02; vorher Frage 3): P1 und P2 misst der Betreiber nach D3 mit seinem Konto, nur lesend; P6 und P8 bleiben zurückgestellt.
22. **Kopien als eine Nachricht** (18.10.2026, A02; vorher Frage 4): so lassen.
23. **Dauerläufer und `mail_sync_now`** (18.10.2026, A02; vorher Frage 5): S3 (Job-Typ `sync`, MCP reiht ein). S2 legt mit dem Job `draft_push` die Mechanik dafür an (A02, Abschnitt 1, E1).
24. **Befunde der Gegenprüfung (a–d)** (18.10.2026, A02; vorher Frage 6): alle vier als Nachbesserung in S2.0 (A02, Teil A: A1, A2, A3, A4; A2 erledigt, siehe „Stand S2.0, Teil 1").
25. **Suchlatenz** (18.10.2026, A02; vorher Frage 7): auf dem Windows-PC nachmessen, erst bei spürbaren Wartezeiten optimieren.
26. **Import-Durchsatz** (18.10.2026, A02; vorher Frage 8): auf dem Windows-PC nachmessen, Parallelisierung erst bei Bedarf.
27. **Emoji-Sequenzen** (18.10.2026, A02; vorher Frage 9): so lassen (Sicherheit vor Darstellung).
28. **Weitere eigene Adressen (Aliasse)** (18.10.2026, A02; vorher Frage 10): Feld am Konto in S2.0; S2 nutzt es für „Allen antworten" und die Versanderkennung (umgesetzt S2.0/A6: `accounts.aliases`, `account set --aliases`).
29. **Teilwortsuche ohne Umlaut** (18.10.2026, A02; vorher Frage 11): hinnehmen.
30. **Grenzen von `alternative_mismatch`** (18.10.2026, A02; vorher Frage 12): hinnehmen.
31. **Code-Signatur** (18.10.2026, A02; vorher Frage 13): bis 1.0 unsigniert mit `SHA256SUMS`.
32. **Audit-Rotation** (18.10.2026, A02; vorher Frage 14): mit dem Löschkonzept in S3.
33. **Panics in Nebengoroutinen** (18.10.2026, A02; vorher Frage 15): eigene Hilfsgoroutinen in S2.0 mit demselben `recover` versehen (A02, Teil A, A5).

## Offene Fragen

Stand S2.0, Teil 1 (21.10.2026). Die Fragen bis S1.11 sind entschieden (oben, Nummern 19 bis 33). Aus Auftrag A02 selbst: derzeit keine. Neu aus S2.0 (je mit Vorschlag als Standardantwort):

1. **Passwort eines Kontos neu setzen** (neu, S2.0/A2): Es gibt kein Kommando, um das gespeicherte Passwort eines Kontos zu ersetzen; `account remove` und `account add` gehen nur ohne archivierte Nachrichten. Nötig ist das bei einem geänderten App-Passwort und für den seltenen Fall, dass zwei Datenverzeichnisse unter S1 ein Konto gleichen Namens hatten (das zweite verliert beim Umzug den gemeinsamen Eintrag, Befund A2). Vorschlag: `account set <name> --password-stdin` (Operation `accounts.set`, HumanOnly) in S2.1; bis dahin hilft `POSTKAUZ_PASSWORD_<NAME>`.
