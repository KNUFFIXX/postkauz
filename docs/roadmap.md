# Roadmap

Stand: **S1.9 erledigt** (16.10.2026: lesender MCP-Server über stdio mit dem offiziellen Go-SDK v1.8.0, Tools/Resources/Prompts nach Scopes, Untrusted-Hülle, Audit je Aufruf; Abnahme D2 vorbereitet in [abnahme_D2.md](abnahme_D2.md)). **S1.8 erledigt** (16.10.2026: CLI lesend vollständig mit API-Schlüsseln, Audit-Abfrage/-Export/-Prüfung, `--key`, `--quiet`, `--profile`, `--explain`, Untrusted-Hülle in den Ausgaben, Eingabeprüfung im Executor, Vertragstest Register ↔ CLI). **S1.7 erledigt** (16.10.2026: Volltextsuche mit app-seitigem Normalisierer und deutschem Stemmer, Komposita über Trigram, Abfragesprache, Ranking aus BM25 und Datum, Cursor, `search` im CLI; Index und Anhangstext als Jobs über die Tabelle `jobs`; Messung mit 100.000 Nachrichten). **S1.6 erledigt** (16.10.2026) mit Schritt 0 (N1–N3, Watchdog). **S1.1 bis S1.5 erledigt** (15.10.2026) und vom Betreiber abgenommen: **D0 auf Windows und D1 bestanden** (16.10.2026). Als Nächstes: **Abnahme D2 durch den Betreiber** (A01: nach S1.9 anhalten), danach S1.10 (Import). Am Ende jeder Arbeitssitzung wird dieser „Stand" und `CHANGELOG.md` gepflegt.

| Phase / Schnitt | Inhalt in einem Satz | Status |
|---|---|---|
| Phase 0 | Gerüst, Architektur, ADRs, Testserver, Spike mit `go-imap/v2`, Gate Go vs. Python | **S0 (Spike und Gate) erledigt 14.10.2026**: Gate Go bestanden mit Auflagen, Bericht `spike/s0/REPORT.md`, ADR-007/008. **Schritt 0 (Go 1.27) erledigt 15.10.2026** |
| S1 | Konto, Sync, Spiegel, Threads, Suche, CLI lesend, stdio-MCP lesend, Audit, Import | **S1.1 bis S1.5 erledigt 15.10.2026**, D0/D1 bestanden 16.10.2026; **S1.6 bis S1.9 erledigt 16.10.2026**, Abnahme D2 offen; S1.10 ff. offen |
| S2 | Entwürfe als draft-only, Policy-Datei, Freigabe-Zustände, Prüf-Hook | offen |
| S3 | Tags und Postfach-Aktionen, Export/Backup/Restore, HTTP-MCP mit OAuth 2.1 | offen |
| S4+ | SMTP-Versand mit Freigabe, Regeln, REST, JMAP, später GUI | nicht begonnen |

Aufwandsschätzung für S1–S3: **40–80 Sitzungen, drei bis sechs Monate** (Code-Anteil schnell, Tests gegen echte Server und Entscheidungen des Betreibers bestimmen das Tempo). Die Schätzung ist grob und wird nach S1 neu gesetzt.

## Phase 0: Gerüst und Gate

**Erledigt:** Repository, Lizenz, ADR-001 bis ADR-006, Architektur-/Datenmodell-/MCP-/CLI-/Sicherheitsdokumente, Go-Gerüst (Register, Executor, Policy-Stub, Audit-Typ, Migrationen mit FTS5, Provider-Interface, CLI mit `version`/`doctor`/`accounts list`), CI, Testserver-Compose.

**S0 erledigt (14.10.2026): Spike und Gate.** `go-imap/v2` v2.0.0-beta.8 besteht gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail: Erstsync in Fenstern (rund 1.000 bis 1.250 Nachrichten/s lokal), Inkrementalsync (CONDSTORE bzw. Vollvergleich), Löschungserkennung, Draft-APPEND mit identischen Bytes, IDLE, Reconnect. **Urteil: Gate Go bestanden mit Auflagen**, Bericht und Rohdaten in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md). Auflagen und Entscheidungen: ADR-007 (go-imap-Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Go-Version). Der Spike-Code ist Wegwerfcode in einem eigenen Go-Modul (`spike/s0`) und nicht Teil von `./...`.

**Schritt 0 erledigt (15.10.2026):** `go.mod` auf `go 1.27` (Toolchain go1.27.2, `GOTOOLCHAIN=local` in Makefile und CI), `modernc.org/sqlite` v1.45.0 → v1.60.1 (SQLite 3.53.4; mitgezogen: `modernc.org/libc` v1.77.1, `golang.org/x/sys` v0.48.0). `make check` und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 4,9 MB, windows/amd64 5,1 MB, darwin/arm64 4,8 MB (die CLI bindet SQLite noch nicht ein; mit Store-Anbindung in S1.1 wachsen sie, Spike: rund 11 MB mit `-s -w`).

**Offen (Auftrag A01):** Abnahme D2 (vorbereitet, [abnahme_D2.md](abnahme_D2.md)), danach S1.10 und S1.11 (Abnahme D1 bestanden 16.10.2026). Die Spike-Befunde sind in A01 als Vorgaben und Prüfliste festgehalten.

## S1: Lesen, Spiegeln, Suchen

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

## Entschiedene Fragen (14. bis 16.10.2026)

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

## Offene Fragen

1. **Name:** Ergebnis der Namensprüfung; danach Umbenennung gemäß [architektur.md](architektur.md), Abschnitt 8.
2. **Mehrere Menschen an einem Postfach:** Wie wird eine Freigabe einer Person zugeordnet, wenn mehrere Menschen dasselbe Postfach bedienen (getrennte Logins/Delegation beim Anbieter)? Relevant ab S2.
3. **Spike-Lücken** (Prüfliste in A01): Gmail `X-GM-EXT-1` (P1, manuell), WAN-Latenz (P2, Betreiber), COMPRESS/BINARY (P6, später), QRESYNC per Beitrag oder Fork (P8, nach S1; nach P2 für 50.000 Nachrichten nicht nötig). Erledigt bzw. als Entwurf: P2 lokal (50.000 Nachrichten, S1.5), P3 (OAuth2/SASL, Entwurf), P4 (Schlüsselbund; Windows/macOS noch manuell abzunehmen), P5 (UIDVALIDITY-Wechsel, S1.5), P7 (Race-Detector: `make race` und CI-Job, S1.3).
4. **Kopien als eine Nachricht** (S1.5): Liegt dieselbe Nachricht in mehreren Ordnern (Kopie, Gmail-Label), ist sie lokal eine Nachricht mit mehreren Ordnern; ein Flag gilt als gesetzt, wenn eine Kopie es trägt. Vorschlag: so lassen.
5. **Dauerläufer für mehrere Konten** (S1.5): `sync --watch` beobachtet ein Konto; für mehrere Konten je ein Prozess. Vorschlag: ein gemeinsamer Dauerläufer erst mit dem Betriebskonzept (S3).
6. *(entschieden 16.10.2026, siehe „Entschiedene Fragen" 12 und ADR-007 Nachtrag 1)*
7. **Weitere Befunde der Gegenprüfung (15.10.2026), bewusst noch nicht geändert:**
   - **Löschungen** werden inkrementell nur erkannt, wenn die Zahl der Nachrichten nicht passt (`internal/provider/imap/sync.go`, `KnownCount + neu != EXISTS`). Löscht jemand eine Nachricht und kommt gleichzeitig eine neue hinzu, fällt die Löschung erst beim nächsten Lauf mit Abweichung oder bei `sync --full` auf. Vorschlag: `--full` im Dauerbetrieb periodisch (z. B. stündlich) oder UID-Abgleich bei jedem n-ten Lauf.
   - **Schlüsselbund-Verweis** `account/<name>` (`internal/ops/accounts.go`) hängt nur am Kontonamen, nicht am Datenverzeichnis: Zwei Datenverzeichnisse (z. B. Abnahme und Alltag) mit gleichem Kontonamen teilen und überschreiben denselben Eintrag; `account remove` im einen löscht das Passwort des anderen. Vorschlag: Verweis um eine Kennung des Datenverzeichnisses ergänzen (Migration bestehender Einträge).
   - **Vorhandenes Datenverzeichnis** wird nicht auf 0700 verschärft (`config.EnsureDataDir` setzt die Rechte nur beim Anlegen). Vorschlag: beim Öffnen prüfen, verschärfen und in `doctor` melden.
   - **`io.ReadAll` ohne Grenze** in `internal/imapx/mailbox.go` (FETCH-Literal, das vor seiner UID ankommt): Ein Server kann so beliebig viel Speicher belegen. Vorschlag: auf die Grenze des Blob-Stores (256 MiB) begrenzen bzw. in eine temporäre Datei puffern.
8. *(entschieden 16.10.2026, siehe „Entschiedene Fragen" 14)*
9. *(entschieden 16.10.2026, siehe „Entschiedene Fragen" 15)*
10. *(entschieden 16.10.2026, siehe „Entschiedene Fragen" 16)*
11. **Suchlatenz bei sehr häufigen Wörtern** (S1.7): Im synthetischen Korpus steht jedes Wort in 25–50 % aller Nachrichten; dafür liegt der Median knapp unter 300 ms, einzelne Läufe und zwei solche Wörter über die CLI darüber (bis rund 390 ms auf 2 Kernen). Vorschlag: so lassen; die Messung auf dem Windows-PC des Betreibers wiederholen (mehr Kerne, Kommandos in [suche_messung.md](suche_messung.md)) und erst weiter optimieren (Vollscan-Pfad, Bleve laut ADR-005 als Option), wenn im Alltag Anfragen spürbar über 300 ms liegen.
12. *(entschieden 16.10.2026: Teilwortsuche ab 3 Zeichen bleibt immer an; Worst-Case-Suchwörter (Frage 11) erst auf Windows nachmessen; mmap bleibt)*
13. **`mail_sync_now`** (S1.9): Der MCP-Server synct nicht selbst; ein Agent kann also keinen Sync anstoßen. Vorschlag: mit dem Job-Typ `sync` nachrüsten (MCP reiht ein, `sync --watch` führt aus), zusammen mit dem Dauerbetrieb für mehrere Konten (Frage 5); bis dahin hält `sync --watch` die Daten aktuell.
