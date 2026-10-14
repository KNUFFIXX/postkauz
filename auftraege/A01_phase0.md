# Auftrag A01: Phase 0 abschließen, Gate entscheiden, Schnitt 1 bauen

Für: die erste Claude-Code-Cloud-Sitzung (oder eine Folge kurzer Sitzungen) in diesem Repository.
Betreiber: kein Programmierer. Er führt die Abnahme-Kommandos aus (Teil D) und entscheidet.

## 0. Ausgangslage und Regeln

- Das Gerüst steht: `make check` und `make cross` sind grün. Lies zuerst `CLAUDE.md`, `docs/entscheidungen.md`, `docs/roadmap.md`, dann `docs/architektur.md`, `docs/datenmodell.md`, `docs/sicherheit.md`, `docs/mcp.md`, `docs/cli.md`.
- Ein **Spike** hat (außerhalb dieses Repositorys) geprüft, ob `github.com/emersion/go-imap/v2` für den Sync taugt. Sein Ergebnis wird unter `spike/s0/` abgelegt (Teil A).
- **Nie gegen echte Postfächer.** Nur die Docker-Testserver (`make testmail-up`). **Kein SMTP-Code** (ADR-002).
- **Stopp-Regel:** Fällt das Gate durch (Teil B) oder widerspricht ein Befund einem ADR, **hör auf**, schreibe den Befund in `docs/roadmap.md` und frage den Betreiber, bevor du Schnitt-1-Code schreibst.
- Fragen an den Betreiber: höchstens drei auf einmal, kurz, mit Standardantwort.

## Teil A: Spike-Ergebnis übernehmen

1. Prüfe, ob `spike/s0/` existiert und enthält: Quellcode des Spikes, ein `README` oder `ERGEBNIS.md` mit Messwerten, die verwendeten Serverversionen. Fehlt etwas, melde es und arbeite mit dem, was da ist; **erfinde keine Messwerte**.
2. Führe den Spike gegen die Testserver erneut aus (`make testmail-up`, Server bereitstellen: Dovecot sofort, Stalwart braucht Domain und Konto, siehe `deploy/testmail/README.md`). Ergebnis in `spike/s0/ERGEBNIS.md` (Tabelle: Kriterium, Dovecot, Stalwart, GreenMail, Bemerkung).
3. Übernimm Erkenntnisse in die Doku: bestätigte oder widerlegte Annahmen in `docs/architektur.md` (Abschnitt 4), `deploy/testmail/README.md` (Versionen festnageln) und `docs/roadmap.md`.
4. `spike/s0/` ist Wegwerfcode: Er wird **nicht** Teil von `./...`-Builds (eigenes `go.mod` oder Build-Tag `spike`) und fließt nur als Vorlage in `internal/provider/imap` ein.

**Abnahme A:** `docs/` und `spike/s0/ERGEBNIS.md` benennen die Ergebnisse; `make check` bleibt grün.

## Teil B: Gate

Das Gate ist bestanden, wenn `go-imap/v2` gegen **Dovecot und Stalwart** alles Folgende leistet (GreenMail nur für den Fallback-Pfad):

| # | Kriterium |
|---|---|
| G1 | Login über TLS und STARTTLS, Fähigkeitserkennung (CONDSTORE, QRESYNC, IDLE, MOVE, SPECIAL-USE, UIDPLUS) |
| G2 | Ordnerliste mit Rollen, Umlaut-Ordner (IMAP-UTF-7) korrekt dekodiert |
| G3 | Fenster-Erstsync von 1.000 Nachrichten, Abbruch und Wiederaufnahme ohne Doppelte |
| G4 | Inkrementell: neue UIDs, Flag-Änderungen per `CHANGEDSINCE`, Löschungen (VANISHED oder Abgleich) |
| G5 | `APPEND` in den Entwürfe-Ordner mit `APPENDUID` |
| G6 | IDLE mit automatischem Reconnect nach Verbindungsabbruch |
| G7 | Rohmail streamen (`BODY.PEEK[]`) ohne `\Seen` zu setzen |

**Entscheidung:** Alle bestanden: ADR-001 auf „akzeptiert (Gate bestanden, Datum)" setzen, `docs/roadmap.md` aktualisieren, weiter mit Teil C. Einzelne Lücken mit Umgehung: dokumentieren, Aufwand schätzen, Betreiber fragen. Grundsätzliches Scheitern: **Stopp**, Empfehlung (Python-Rückfall nach ADR-001) an den Betreiber; kein Schnitt-1-Code.

## Teil C: Schnitt 1, Schritt für Schritt

Gemeinsame Regeln je Schritt: kleine Commits; `make check` grün; Tests mit Docker-Servern laufen lokal, Unit-Tests ohne Docker; nach jedem Schritt `docs/roadmap.md` und `CHANGELOG.md` anpassen. Die Akzeptanzkriterien S1 stehen vollständig in `docs/roadmap.md`; hier die Zuordnung.

| Schritt | Inhalt | Akzeptanz (testbar) |
|---|---|---|
| S1.1 | Konfiguration (TOML, `MAILWERK_CONFIG`), Datenverzeichnis, Store-Anbindung, SQLite-Audit-Writer (ersetzt `audit.Nop` im CLI), Schlüsselbund-Anbindung, `account add/test/remove`, `accounts.list` aus der Datenbank | Konto anlegen und auflisten; Passwort nur im Schlüsselbund (Test mit Kanarienwert: nicht in DB, Log, Audit); `doctor` meldet Pfade, Schlüsselbund, Datenbank, Schema-Version |
| S1.2 | Blob-Store (atomar, SHA-256, optional zstd); MIME-Parser auf `net/mail` plus Bibliothek für Charsets; Korpus-Generator `make corpus` mit Manifest (`docs/testkorpus.md`) | Parser besteht den Korpus gegen das Manifest (Betreff, Absender, Anhänge, Threads); Zip-Bombe wird nicht entpackt |
| S1.3 | `internal/provider/imap`: Verbinden, Capabilities, `ListMailboxes` mit Rollen | Integrationstest gegen Dovecot, Stalwart, GreenMail: erwartete Fähigkeiten und Rollen |
| S1.4 | Sync: Erstsync in Fenstern, resume-fähig, Idempotenz über `(mailbox_id, uid)` | 1.000 Nachrichten gespiegelt; `kill -9` mittendrin, Neustart ergibt dieselbe Anzahl ohne Doppelte |
| S1.5 | Inkrementeller Sync (QRESYNC, CONDSTORE, UID-Scan), IDLE auf INBOX, Reconnect mit Backoff, UIDVALIDITY-Wechsel | Test je Strategie; Flag-Änderung von außen nach einem Intervall sichtbar; Reset führt zu Abgleich per Message-ID |
| S1.6 | Threading, Body-Bereinigung (HTML → Text mit Allowlist, Quote-Stripper, versteckter Text), Anhänge mit Magic Bytes, Textextraktion im Subprozess | Threads des Korpus stimmen mit dem Manifest; Mail mit verstecktem Text liefert `hidden_text`; Extraktor-Timeout wird gehalten |
| S1.7 | FTS-Indexierung (`index`-Job), deutscher Stemmer (Bibliothek wählen, Entscheidung in `docs/datenmodell.md` festhalten), Abfragesprache, Ranking, Cursor | Suchtestsatz aus dem Korpus grün; 100.000-Nachrichten-Messung dokumentiert (Ziel unter 300 ms) |
| S1.8 | CLI lesend: `sync`, `mailbox list`, `search`, `show`, `thread`, `attach list/text`, `key create/list/revoke`, `--json`/NDJSON, `--explain` | je Kommando ein Test; Vertragstest Register ↔ CLI; Exit-Codes wie `docs/cli.md` |
| S1.9 | MCP stdio lesend (offizielles Go-SDK, Version per `go get`): Tools, Resources, Prompts aus `docs/mcp.md` (Abschnitt „S1"), Untrusted-Hülle, Scope-Filter | Tool-Liste folgt den Scopes; Vertragstest Register ↔ MCP; Ausgabe jeder Mail in der Hülle; Claude Code sieht die Tools |
| S1.10 | Import mbox/Maildir mit Message-ID-/Hash-Abgleich und Überlappung | Import eines Bestands mit 30 % Überlappung zu einem gesyncten Konto: keine Doppelten, Zählung ausgegeben |
| S1.11 | Audit-Export mit Prüfsumme, `audit verify`, Kanarientest über alle Ausgabekanäle, Installationsanleitung im README, Release-Binärdateien per `make cross` | `audit verify` erkennt eine geänderte Zeile; Kanarientest grün; Installation in unter 15 Minuten nach README |

Reihenfolge einhalten; ein Schritt gilt erst als fertig, wenn seine Akzeptanz grün ist. Nach S1.5 und nach S1.9 jeweils **anhalten und den Betreiber abnehmen lassen** (Teil D).

## Teil D: Abnahme-Checkliste für den Betreiber

Du musst nichts programmieren. Jeder Punkt: Kommando eingeben, Ergebnis mit „Erwartet" vergleichen, abhaken. Bei Abweichung: Kommando und Ausgabe an Claude zurückgeben, nichts reparieren. Voraussetzungen: Docker läuft, das Repository ist ausgecheckt, im Repository-Ordner ein Terminal öffnen.

### D0: Gerüst (sofort prüfbar)

1. `make check` → am Ende `pii-check: ok`, kein Fehler.
2. `make build` → danach `./bin/mailwerk version` zeigt eine Zeile mit `mailwerk`, Version und Go-Version.
3. `./bin/mailwerk accounts list` → `No accounts configured yet.`
4. `./bin/mailwerk accounts list --json` → `{"accounts":[]}`
5. `./bin/mailwerk doctor` → Zeilen für version, config, keyring, database. Beim Gerüst steht bei keyring/database `not_checked`.
6. `./bin/mailwerk bogus; echo $?` → Fehlermeldung und danach `2`.

### D1: nach S1.5 (Sync)

Testserver: `make testmail-up`; Dovecot ist auf Port 11143 (Nutzer `tester`, Passwort `pass`).

1. `./bin/mailwerk account add demo --imap 127.0.0.1:11143 --user tester --insecure-test-server` (fragt nach dem Passwort: `pass`) → `account demo added`.
2. `./bin/mailwerk account test demo` → Zeile `login: ok`, eine Liste der Fähigkeiten (z. B. `IDLE`), eine Ordnerliste mit `INBOX` (Rolle `inbox`).
3. Korpus laden (Skript nennt Claude in der Abnahmemeldung), dann `./bin/mailwerk sync --full -a demo` → am Ende `1000 messages mirrored` (genaue Zahl nennt Claude) und `0 errors`.
4. Sync **abbrechen** (Strg+C nach einigen Sekunden) und erneut starten → Zahl am Ende ist **dieselbe** wie in Punkt 3, nicht größer.
5. `./bin/mailwerk mailbox list -a demo --json` → JSON mit `total` je Ordner, `INBOX` passt zur Zahl aus Punkt 3.
6. In einem Mailclient (oder per Skript von Claude) eine Mail in `INBOX` als gelesen markieren; nach einem Sync (`./bin/mailwerk sync -a demo`) zeigt `./bin/mailwerk show <id> --headers` das Flag `seen`.

### D2: nach S1.9 (Suche, CLI, MCP)

1. `./bin/mailwerk search "Rechnung" -a demo --limit 5` → bis zu fünf Zeilen mit `id`, Datum, Absender, Betreff; die Treffer enthalten auch „Rechnungen" (Flexion).
2. `./bin/mailwerk search "prüfen"` und `./bin/mailwerk search "pruefen"` → beide liefern dieselben Treffer.
3. `./bin/mailwerk search "Vollstreck"` → findet die Mail mit „Zwangsvollstreckung" im Betreff.
4. `./bin/mailwerk search "has:attachment" --json | head -n 3` → drei JSON-Zeilen, je eine Nachricht.
5. `./bin/mailwerk show <id>` einer Testmail mit verstecktem Text → Ausgabe enthält den Hinweis `hidden text removed`.
6. Schlüssel erzeugen: `./bin/mailwerk key create --name test-agent --role reader` → Der Schlüssel wird **einmal** angezeigt; mit `./bin/mailwerk key list` erscheint er **ohne** Klartext.
7. MCP: In Claude Code die Konfiguration aus dem README eintragen (Befehl `mailwerk mcp`, Umgebungsvariable `MAILWERK_KEY`) → Claude Code listet nur lesende Tools (`mail_search`, `mail_get_message`, …); **kein** Tool zum Senden, Löschen oder Entwerfen.
8. Frage in Claude Code: „Suche Mails über Rechnungen und fasse sie zusammen" → Claude nutzt `mail_search`/`mail_get_message`; in `./bin/mailwerk audit --since 10m` stehen die Aufrufe mit Schlüsselname und **ohne** Mailtext.
9. `./bin/mailwerk audit verify` → `ok`; nach einer absichtlich veränderten Exportdatei (Claude nennt die Datei) → Meldung `mismatch` und Exit-Code 1.

### D3: Gesamtabnahme S1

1. Alle Punkte D0–D2 erneut auf einem zweiten Server (Stalwart, Port 12143/12993) und auf GreenMail (Port 13143).
2. `make check && make cross` → grün; im Ordner `dist/` liegen drei Programme (linux, windows, darwin).
3. Installation: `dist/mailwerk-windows-amd64.exe` auf einen Windows-Rechner kopieren, `mailwerk.exe doctor` → läuft ohne Installation weiterer Software.
4. Importprobe: `./bin/mailwerk import maildir <Ordner> --account demo --mailbox Archive` mit einem Testbestand → Ausgabe `new: N, existing: M, skipped: 0`; zweiter Lauf: `new: 0`.
5. Entscheidung des Betreibers: S1 angenommen / Nachbesserung (Liste) / S2 starten.

## Abschluss der Sitzung

`make check` und `make cross` grün; `docs/roadmap.md` (Stand, offene Fragen), `CHANGELOG.md`, ADR-Status aktualisieren; Abschlussmeldung mit: was geht, was nicht, die nächsten Abnahmepunkte (Teil D) und die anstehende Entscheidung.
