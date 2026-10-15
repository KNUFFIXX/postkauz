# Auftrag A01: Go anheben, Spike-Auflagen umsetzen, Schnitt 1 bauen

Für: die erste Claude-Code-Cloud-Sitzung (oder eine Folge kurzer Sitzungen) in diesem Repository.
Betreiber: kein Programmierer. Er führt die Abnahme-Kommandos aus (Teil D) und entscheidet.

## 0. Ausgangslage und Regeln

- Das Gerüst steht: `make check` und `make cross` sind grün. Lies zuerst `CLAUDE.md`, `docs/entscheidungen.md` (besonders ADR-007 und ADR-008), `docs/roadmap.md`, dann `docs/architektur.md`, `docs/datenmodell.md`, `docs/sicherheit.md`, `docs/mcp.md`, `docs/cli.md`.
- Der **Spike S0 ist erledigt** (14.10.2026): Code und Bericht liegen unter `spike/s0/` (eigenes Go-Modul, nicht Teil von `./...`). **Gate Go bestanden mit Auflagen**; die Auflagen stehen in ADR-007 und ADR-008 und sind unten als Vorgaben für S1 übernommen.
- **Nie gegen echte Postfächer.** Nur die Docker-Testserver (`make testmail-up`). **Kein SMTP-Code** (ADR-002).
- **Stopp-Regel:** Widerspricht ein Befund einem ADR, **hör auf**, schreibe den Befund in `docs/roadmap.md` und frage den Betreiber, bevor du weiterbaust.
- Fragen an den Betreiber: höchstens drei auf einmal, kurz, mit Standardantwort.
- Entschieden (14.10.2026): Go-Zielversion 1.27; Tags/Move/Trash bleiben in S3; Programmausgaben Englisch, Doku Deutsch; Versanderkennung in S2 mit Rückfall-Abgleich (siehe unten).

## Schritt 0: Go-Anhebung und Toolchain-Check (ADR-008)

1. `GOTOOLCHAIN=local` setzen (Umgebung und CI). Prüfen: `go version` zeigt die Zielversion 1.27.x; fehlt sie, dem Betreiber melden, nicht still nachladen.
2. `go.mod`: `go`-Zeile auf die Zielversion; `modernc.org/sqlite` auf die dann aktuelle Version (`go get`, nicht raten); `go mod tidy`.
3. `make check` und `make cross` (linux/amd64, windows/amd64, darwin/arm64, `CGO_ENABLED=0`) grün; Binärgrößen notieren (Spike: rund 11 MB mit `-s -w`).
4. CLAUDE.md, CONTRIBUTING.md, README, CI-Datei und ADR-008 (Status „umgesetzt") nachziehen; `docs/roadmap.md` und `CHANGELOG.md` ergänzen.

**Abnahme 0:** `make check` und `make cross` grün auf der Zielversion; in `go.mod` steht keine Version, die der Spike-Bericht als unzulässig nennt; CI-Lauf grün.

## Spike-Befunde als Vorgaben für S1 (aus `spike/s0/REPORT.md`)

| Befund | Vorgabe für S1 | Schritt |
|---|---|---|
| go-imap v2 ist Beta, hat keinen Raw-Command-Ausgang | Alle IMAP-Zugriffe nur über die dünne Schicht `internal/imapx`; `v2.0.0-beta.8` gepinnt; Fork-Bereitschaft (nichts außerhalb von `imapx` importiert go-imap) | S1.3 |
| Keine Kommando-Timeouts (Login und IDLE-Close hängen, Blackhole unbemerkt, IDLE-Neustart fest 28 min) | **Watchdog:** Deadline je Kommando, bei Überschreitung Close + Reconnect mit Backoff; IDLE alle 5 bis 10 min selbst neu starten; TCP-Keepalive über `net.Dialer.KeepAliveConfig`. Test mit stummem Server (Blackhole) | S1.3, S1.5 |
| Standard-`WordDecoder` ohne Zeichensätze (16 % roher Betreff) | `Options.WordDecoder` immer mit `go-message/charset.Reader`; Regressionstest über den Korpus (windows-1252, iso-8859-1, RFC 2047 B und Q) | S1.3, S1.2 |
| Kein QRESYNC in go-imap | Löschungen per UID-SEARCH-Abgleich; für Postfächer ab 50.000 Nachrichten messen; QRESYNC später per Upstream-Beitrag oder Fork (nicht in S1) | S1.5 |
| Parser-Vergleich: enmime erzeugt Mojibake, ist 7-mal langsamer | Parser `go-message`; `enmime` nicht verwenden | S1.2, S1.6 |
| Fehlendes `Date`: Nullzeit ohne Fehler | Fehlendes oder unparsbares `Date` → `INTERNALDATE` des Servers | S1.2 |
| Zeitzone „0100" (ohne Vorzeichen) lässt das Datumsparsen scheitern | Tolerantes Datumsparsen: Zonen ohne Vorzeichen abfangen (Test mit den acht Fällen des Korpus) | S1.2 |
| FTS5: unicode61 faltet ä/ö/ü, **nicht ß/ss**; Teilwörter nur über Trigram; Trigram erst ab 3 Zeichen | App-seitiger Normalisierer (ß zu ss in Index und Anfrage) vor `unicode61`; Komposita und Teilwörter über `fts_trigram`; Anfragen unter 3 Zeichen nur über Wortindex mit Präfix; Suchtestsatz deckt „Erdgeschoss/Erdgeschoß" und „steuer" in Komposita ab | S1.7 |
| Stalwart: `HIGHESTMODSEQ` kontoweit | Akzeptiert (leere `CHANGEDSINCE`-Abfragen je Ordner); keine Sonderbehandlung | S1.5 |
| Dovecot geht nach 5 s IDLE in Hibernate | Bei Latenzmessungen berücksichtigen; keine Anpassung nötig | S1.5 |
| Stalwart 0.16 (`latest`) startet im Bootstrap-Modus | Testserver bleibt auf 0.11.8; 0.16 später nachtesten | Prüfliste |

## Prüfliste: offene Punkte des Spikes

Jeden Punkt abarbeiten oder ausdrücklich in `docs/roadmap.md` zurückstellen (mit Begründung). Gmail und andere echte Anbieter prüft **nur der Betreiber manuell**, nie die CI und nie Claude mit echten Zugangsdaten im Repository.

| # | Punkt | Wann | Nachweis |
|---|---|---|---|
| P1 | Gmail: `X-GM-EXT-1` (Labels, Alle-Nachrichten-Dubletten), kein QRESYNC; Verhalten mit `go-imap` ohne Fork | vor S1.5-Abnahme, manuell durch den Betreiber | Ergebnis in `docs/roadmap.md` |
| P2 | 50.000+ Nachrichten und WAN-Latenz: Erstsync-Dauer, UID-SEARCH-Abgleichskosten | S1.5 (synthetisch, lokal; WAN durch den Betreiber) | Messwerte in `spike/s0/REPORT.md`-Nachtrag oder `docs/` |
| P3 | OAuth2 über `go-sasl` (XOAUTH2/OAUTHBEARER), Token-Refresh | S1.1 (Entwurf) | Test mit Fake-Server |
| P4 | Schlüsselbund (`go-keyring`) auf Windows, Linux (Secret Service), macOS; Fallback ohne Schlüsselbund | S1.1 | Test und `doctor`-Ausgabe |
| P5 | UIDVALIDITY-Wechsel gezielt (Ordner neu angelegt) | S1.5 | Test: Reset führt zum Abgleich per Message-ID |
| P6 | COMPRESS, BINARY (optional) | später | Entscheidung notieren |
| P7 | Race-Detector (`go test -race ./...`) und Unit-Tests für `imapx`, Sync, Parser | ab S1.3, dauerhaft (Betreiber-Entscheid 15.10.2026: eigenes Ziel `make race` mit CGO, eigener CI-Job; nicht in `make check`) | `make race` und CI-Job `race` grün |
| P8 | QRESYNC: Upstream-Beitrag oder Fork | nach S1 | Entscheidung als ADR |
| P9 | Weitere Anbieter (M365, IONOS, Strato, T-Online) | manuell durch den Betreiber | Eintrag in der Roadmap |

## Vorgabe für S2: Erkennung des Versands

Der Mensch sendet den Entwurf im Mailclient; PostKauz muss das erkennen (`sent_by_human`). Entschieden: **Zuerst `Message-ID`; Rückfall-Abgleich** über Empfänger, Betreff, Body-Hash und Zeitfenster, wenn der Client beim Senden eine neue `Message-ID` vergibt. **In S2 messen**, wie sich Webmail, Desktop-Client und ein zweiter Anbieter verhalten, und das Ergebnis in `docs/sicherheit.md` festhalten (siehe dort, Abschnitt 5). Kein Treffer ist kein Fehler: Der Entwurf bleibt dann im Zustand `ready`, bis der Mensch ihn manuell abschließt.

## Teil C: Schnitt 1, Schritt für Schritt

Gemeinsame Regeln je Schritt: kleine Commits; `make check` grün; Tests mit Docker-Servern laufen lokal, Unit-Tests ohne Docker; nach jedem Schritt `docs/roadmap.md` und `CHANGELOG.md` anpassen. Die Akzeptanzkriterien S1 stehen vollständig in `docs/roadmap.md`; hier die Zuordnung.

| Schritt | Inhalt | Akzeptanz (testbar) |
|---|---|---|
| S1.1 | Konfiguration (TOML, `POSTKAUZ_CONFIG`), Datenverzeichnis, Store-Anbindung, SQLite-Audit-Writer (ersetzt `audit.Nop` im CLI), Schlüsselbund-Anbindung (P3, P4), `account add/test/remove`, `accounts.list` aus der Datenbank | Konto anlegen und auflisten; Passwort nur im Schlüsselbund (Test mit Kanarienwert: nicht in DB, Log, Audit); `doctor` meldet Pfade, Schlüsselbund, Datenbank, Schema-Version |
| S1.2 | Blob-Store (atomar, SHA-256, optional zstd); MIME-Parser auf **`go-message`** (nicht `enmime`) mit `charset`-Reader; fehlendes `Date` → `INTERNALDATE`, Zeitzone „0100" tolerant; Korpus-Generator `make corpus` mit Manifest (`docs/testkorpus.md`) | Parser besteht den Korpus gegen das Manifest (Betreff, Absender, Anhänge, Threads); Zip-Bombe wird nicht entpackt |
| S1.3 | **`internal/imapx`** (dünne Schicht, gepinnte go-imap-Version, Watchdog, `WordDecoder` mit Charset-Reader) und `internal/provider/imap` darüber: Verbinden, Capabilities, `ListMailboxes` mit Rollen | Integrationstest gegen Dovecot, Stalwart, GreenMail: erwartete Fähigkeiten und Rollen |
| S1.4 | Sync: Erstsync in Fenstern, resume-fähig, Idempotenz über `(mailbox_id, uid)` | 1.000 Nachrichten gespiegelt; `kill -9` mittendrin, Neustart ergibt dieselbe Anzahl ohne Doppelte |
| S1.5 | Inkrementeller Sync (CONDSTORE, UID-Scan; Löschungen per UID-SEARCH-Abgleich, kein QRESYNC), IDLE auf INBOX mit Neustart alle 5 bis 10 min, Watchdog, Reconnect mit Backoff, UIDVALIDITY-Wechsel | Test je Strategie; Test mit stummem Server (Blackhole) erkennt den Ausfall; Flag-Änderung von außen nach einem Intervall sichtbar; Reset führt zu Abgleich per Message-ID |
| S1.6 | Threading, Body-Bereinigung (HTML → Text mit Allowlist, Quote-Stripper, versteckter Text), Anhänge mit Magic Bytes, Textextraktion im Subprozess | Threads des Korpus stimmen mit dem Manifest; Mail mit verstecktem Text liefert `hidden_text`; Extraktor-Timeout wird gehalten |
| S1.7 | FTS-Indexierung (`index`-Job), ß/ss-Normalisierer, deutscher Stemmer (Bibliothek wählen, Entscheidung in `docs/datenmodell.md` festhalten), Abfragesprache, Ranking, Cursor | Suchtestsatz aus dem Korpus grün (inkl. Erdgeschoss/Erdgeschoß und Teilwörter); 100.000-Nachrichten-Messung dokumentiert (Ziel unter 300 ms) |
| S1.8 | CLI lesend: `sync`, `mailbox list`, `search`, `show`, `thread`, `attach list/text`, `key create/list/revoke`, `--json`/NDJSON, `--explain` | je Kommando ein Test; Vertragstest Register ↔ CLI; Exit-Codes wie `docs/cli.md` |
| S1.9 | MCP stdio lesend (offizielles Go-SDK, Version per `go get`): Tools, Resources, Prompts aus `docs/mcp.md` (Abschnitt „S1"), Untrusted-Hülle, Scope-Filter | Tool-Liste folgt den Scopes; Vertragstest Register ↔ MCP; Ausgabe jeder Mail in der Hülle; Claude Code sieht die Tools |
| S1.10 | Import mbox/Maildir mit Message-ID-/Hash-Abgleich und Überlappung | Import eines Bestands mit 30 % Überlappung zu einem gesyncten Konto: keine Doppelten, Zählung ausgegeben |
| S1.11 | Audit-Export mit Prüfsumme, `audit verify`, Kanarientest über alle Ausgabekanäle, Installationsanleitung im README, Release-Binärdateien per `make cross` | `audit verify` erkennt eine geänderte Zeile; Kanarientest grün; Installation in unter 15 Minuten nach README |

Reihenfolge einhalten; ein Schritt gilt erst als fertig, wenn seine Akzeptanz grün ist. Ab S1.3 läuft der Race-Detector als `make race` und als eigener CI-Job (P7). Nach S1.5 und nach S1.9 jeweils **anhalten und den Betreiber abnehmen lassen** (Teil D).

## Teil D: Abnahme-Checkliste für den Betreiber

Du musst nichts programmieren. Jeder Punkt: Kommando eingeben, Ergebnis mit „Erwartet" vergleichen, abhaken. Bei Abweichung: Kommando und Ausgabe an Claude zurückgeben, nichts reparieren. Voraussetzungen: Docker läuft, das Repository ist ausgecheckt, im Repository-Ordner ein Terminal öffnen.

### D0: Gerüst (sofort prüfbar)

1. `make check` → am Ende `pii-check: ok`, kein Fehler.
2. `make build` → danach `./bin/postkauz version` zeigt eine Zeile mit `postkauz`, Version und Go-Version.
3. `./bin/postkauz accounts list` → `No accounts configured yet.`
4. `./bin/postkauz accounts list --json` → `{"accounts":[]}`
5. `./bin/postkauz doctor` → Zeilen für version, config, data dir, log, database, schema, keyring. Nach Punkt 3 steht bei database `ok` und bei schema `1 (program knows 1)`; keyring zeigt unter Windows `available (Windows Credential Manager)`.
6. `./bin/postkauz bogus; echo $?` → Fehlermeldung und danach `2`.

### D1: nach S1.5 (Sync)

Testserver: `make testmail-up`; Dovecot ist auf Port 11143 (Nutzer `tester`, Passwort `pass`).

1. `./bin/postkauz account add demo --imap 127.0.0.1:11143 --user tester --insecure-test-server` (fragt nach dem Passwort: `pass`) → `account demo added`.
2. `./bin/postkauz account test demo` → Zeile `login: ok`, eine Liste der Fähigkeiten (z. B. `IDLE`), eine Ordnerliste mit `INBOX` (Rolle `inbox`).
3. Korpus laden (Skript nennt Claude in der Abnahmemeldung), dann `./bin/postkauz sync --full -a demo` → am Ende `1000 messages mirrored` (genaue Zahl nennt Claude) und `0 errors`.
4. Sync **abbrechen** (Strg+C nach einigen Sekunden) und erneut starten → Zahl am Ende ist **dieselbe** wie in Punkt 3, nicht größer.
5. `./bin/postkauz mailbox list -a demo --json` → JSON mit `total` je Ordner, `INBOX` passt zur Zahl aus Punkt 3.
6. In einem Mailclient (oder per Skript von Claude) eine Mail in `INBOX` als gelesen markieren; nach einem Sync (`./bin/postkauz sync -a demo`) zeigt `./bin/postkauz show <id> --headers` das Flag `seen`.

### D2: nach S1.9 (Suche, CLI, MCP)

1. `./bin/postkauz search "Rechnung" -a demo --limit 5` → bis zu fünf Zeilen mit `id`, Datum, Absender, Betreff; die Treffer enthalten auch „Rechnungen" (Flexion).
2. `./bin/postkauz search "prüfen"` und `./bin/postkauz search "pruefen"` → beide liefern dieselben Treffer.
3. `./bin/postkauz search "Vollstreck"` → findet die Mail mit „Zwangsvollstreckung" im Betreff.
4. `./bin/postkauz search "has:attachment" --json | head -n 3` → drei JSON-Zeilen, je eine Nachricht.
5. `./bin/postkauz show <id>` einer Testmail mit verstecktem Text → Ausgabe enthält den Hinweis `hidden text removed`.
6. Schlüssel erzeugen: `./bin/postkauz key create --name test-agent --role reader` → Der Schlüssel wird **einmal** angezeigt; mit `./bin/postkauz key list` erscheint er **ohne** Klartext.
7. MCP: In Claude Code die Konfiguration aus dem README eintragen (Befehl `postkauz mcp`, Umgebungsvariable `POSTKAUZ_KEY`) → Claude Code listet nur lesende Tools (`mail_search`, `mail_get_message`, …); **kein** Tool zum Senden, Löschen oder Entwerfen.
8. Frage in Claude Code: „Suche Mails über Rechnungen und fasse sie zusammen" → Claude nutzt `mail_search`/`mail_get_message`; in `./bin/postkauz audit --since 10m` stehen die Aufrufe mit Schlüsselname und **ohne** Mailtext.
9. `./bin/postkauz audit verify` → `ok`; nach einer absichtlich veränderten Exportdatei (Claude nennt die Datei) → Meldung `mismatch` und Exit-Code 1.

### D3: Gesamtabnahme S1

1. Alle Punkte D0–D2 erneut auf einem zweiten Server (Stalwart 0.11.8, nur IMAPS Port 12993, Konto laut `deploy/testmail/README.md`) und auf GreenMail (Port 13143).
2. `make check && make cross` → grün; im Ordner `dist/` liegen drei Programme (linux, windows, darwin).
3. Installation: `dist/postkauz-windows-amd64.exe` auf einen Windows-Rechner kopieren, `postkauz.exe doctor` → läuft ohne Installation weiterer Software.
4. Importprobe: `./bin/postkauz import maildir <Ordner> --account demo --mailbox Archive` mit einem Testbestand → Ausgabe `new: N, existing: M, skipped: 0`; zweiter Lauf: `new: 0`.
5. Entscheidung des Betreibers: S1 angenommen / Nachbesserung (Liste) / S2 starten.

## Abschluss der Sitzung

`make check` und `make cross` grün; `docs/roadmap.md` (Stand, offene Fragen), `CHANGELOG.md`, ADR-Status aktualisieren; Abschlussmeldung mit: was geht, was nicht, die nächsten Abnahmepunkte (Teil D) und die anstehende Entscheidung.
