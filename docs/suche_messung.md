# Messung Volltextsuche mit 100.000 Nachrichten (S1.7)

Stand: 16.10.2026. Akzeptanz S1.7 (A01, Teil C): Indexdauer und Suchlatenz bei 100.000 synthetischen Nachrichten, Ziel unter 300 ms je Anfrage (ADR-005, Roadmap S1 Kriterium 4).

## Kurzfassung

| Größe | Ergebnis |
|---|---|
| Index aufbauen (index-Job, 99.800 Nachrichten) | **104 s** (959 Nachrichten/s) direkt nach dem Sync; 120 s (834/s) beim Neuaufbau, während nebenher `make check` lief |
| Platz | Datenbank ohne Index 253 MB, mit Index 500 MB: Wortindex 67 MB, Trigram-Index 180 MB |
| Suche, Operation (warme Verbindung, 15 Läufe je Anfrage) | **Median aller 18 Anfrage-Typen unter 300 ms**; schlechtester Median 291 ms (zwei Wörter, die je in rund 35–50 % aller Nachrichten stehen); einzelne Läufe solcher Anfragen bis 345 ms |
| Suche, CLI Ende-zu-Ende (neuer Prozess je Anfrage) | typische Anfragen 30–120 ms; ein Wort in jeder zweiten Nachricht 260–310 ms; zwei solche Wörter 290–390 ms; feste Grundlast eines CLI-Aufrufs rund 25 ms |
| Migration 0004 → 0005 einer 500-MB-Datenbank | 3,5 s einschließlich Snapshot (`VACUUM INTO`) |

**Bewertung:** Das Ziel ist für alle Anfrage-Typen im Median erreicht. Überschritten wird es nur bei Suchwörtern, die in einem Viertel bis der Hälfte **aller** Nachrichten vorkommen, und nur in einzelnen Läufen (Operation) bzw. bei zwei solchen Wörtern über die CLI. Der synthetische Korpus ist dafür ein Extremfall: Er besteht aus 14 Betreffzeilen und 16 Sätzen, jedes Wort steht also in sehr vielen Nachrichten („Rechnung" in 49 %, „Erdgeschoß" in 35 %). In einem echten Postfach treffen auch häufige Wörter selten mehr als einige Prozent; die Latenz wächst mit der Zahl der Treffer (rund 5 µs je Kandidat und Begriff), bei 5.000 Treffern also um 50 ms. Gemessen auf 2 Prozessorkernen; die Wort- und Teilwortabfragen laufen nebeneinander, mehr Kerne verkürzen Anfragen mit mehreren Wörtern.

## Umgebung und Aufbau

- Linux-Container, 2 Kerne, 8 GB RAM, SSD; Go 1.27.2, `modernc.org/sqlite` v1.60.1 (SQLite 3.53.4), `CGO_ENABLED=0`.
- Korpus: `corpus.Generate` mit 100.000 Nachrichten (Samen 42), 158 MB Rohmails. 200 Nachrichten mit einer Kopfzeile ohne Doppelpunkt liefert der IMAP-Server im Prozess nicht aus (bekannt seit S1.4), gespiegelt wurden **99.800**. Ordner INBOX 25.131, Archiv/2024 36.255, Archiv/2025 36.414, Drafts 2.000. Anhänge wurden für die Messung nicht extrahiert (nur Dateinamen im Index; der Anhangstext des Korpus ist klein).
- Gespiegelt mit dem normalen Sync gegen den go-imap-Testserver im Prozess (`TestS17BuildMeasurementDB`); Dovecot wurde versucht, nahm per APPEND ab rund 30.000 Nachrichten in diesem Container aber nur noch etwa 10 Nachrichten/s an (Laden hätte über 1,5 Stunden gedauert). Für Index und Suche spielt der Server keine Rolle. Der Sync selbst war mit dem Testserver im Prozess langsam (68 Nachrichten/s; der Testserver wird mit wachsendem Ordner langsamer) und ist hier nicht Gegenstand; die Sync-Messung mit Dovecot steht in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md), Nachtrag P2.

Nachmessen:

```sh
# Datenbank bauen (rund 30 min; Blob-Store liegt neben der Datei)
POSTKAUZ_S17_BUILD=/tmp/messung/pk.db go test -run TestS17BuildMeasurementDB -timeout 3h -v ./internal/mailsync/
# Suchlatenz (15 Läufe je Anfrage); mit POSTKAUZ_S17_REINDEX=1 vorher Index neu aufbauen und Dauer messen
POSTKAUZ_S17_DB=/tmp/messung/pk.db go test -run TestS17SearchMeasurement -v ./internal/jobs/
# Ende-zu-Ende: Datenbank als postkauz.db in ein Datenverzeichnis kopieren
POSTKAUZ_DATA_DIR=/tmp/messung-cli ./bin/postkauz search "Rechnung" --limit 20
```

## Suchlatenz je Anfrage-Typ (Operation, Endstand)

Store-Aufruf mit `limit` 20 über alle Konten, 15 Läufe, der erste bei kalter Verbindung; „Seite 2" mit dem Cursor des 20. Treffers.

| Anfrage-Typ | Anfrage | Treffer | erste (kalt) | Median | p95 | max | Seite 2 (Cursor) |
|---|---|---:|---:|---:|---:|---:|---:|
| Wort, häufig (Stamm + Teilwort) | `Rechnung` | 49045 | 277 ms | 247 ms | 324 ms | 324 ms | 258 ms |
| Wort mit Umlaut | `prüfen` | 27490 | 168 ms | 159 ms | 228 ms | 228 ms | 139 ms |
| Umlaut umschrieben | `pruefen` | 27490 | 141 ms | 162 ms | 249 ms | 249 ms | 145 ms |
| Teilwort im Kompositum | `Vollstreck` | 6791 | 45 ms | 48 ms | 61 ms | 61 ms | 59 ms |
| Teilwort, selten | `Wärmemengenzähler` | 0 | 2 ms | 2 ms | 2 ms | 2 ms | – |
| ß/ss | `Erdgeschoß` | 34457 | 293 ms | 246 ms | 286 ms | 286 ms | 185 ms |
| zwei Wörter | `Rechnung Erdgeschoss` | 18431 | 256 ms | 291 ms | 341 ms | 341 ms | 248 ms |
| drei Wörter | `Heizung Keller Lüftung` | 1507 | 153 ms | 166 ms | 216 ms | 216 ms | 145 ms |
| Phrase | `"Erdgeschoss links"` | 1 | 9 ms | 6 ms | 9 ms | 9 ms | – |
| kurz (Präfix) | `ab` | 44382 | 237 ms | 264 ms | 345 ms | 345 ms | 275 ms |
| Ausschluss | `steuer -grundsteuer` | 1 | 115 ms | 107 ms | 132 ms | 132 ms | – |
| Absender + Wort | `from:fiktiv steuer` | 2432 | 62 ms | 65 ms | 84 ms | 84 ms | 66 ms |
| Betreff-Operator | `subject:Zwangsvollstreckung` | 6791 | 56 ms | 72 ms | 82 ms | 82 ms | 51 ms |
| Ordner + Wort | `in:INBOX Heizung` | 1790 | 46 ms | 46 ms | 65 ms | 65 ms | 48 ms |
| nur Operator (Datum sortiert) | `has:attachment` | 16366 | 11 ms | 8 ms | 15 ms | 15 ms | 8 ms |
| nur Datum | `after:2025-01-01 before:2025-02-01` | 3023 | 2 ms | 0 ms | 1 ms | 1 ms | 1 ms |
| nur Ordner | `in:Archiv/2024` | 36255 | 99 ms | 98 ms | 118 ms | 118 ms | 112 ms |
| kein Treffer | `Xylophon` | 0 | 1 ms | 0 ms | 0 ms | 0 ms | – |

Die Werte schwanken zwischen Läufen um etwa ±20 % (geteilte Kerne im Container); ein früherer Lauf desselben Stands hatte als schlechtesten Einzelwert 297 ms.

## Ende-zu-Ende über die CLI

Je Anfrage fünf Aufrufe von `postkauz search "<anfrage>" --limit 20` (neuer Prozess: Konfiguration, Datenbank öffnen, Schemaprüfung, Audit-Eintrag, Suche, Ausgabe):

| Anfrage | Zeiten (ms) |
|---|---|
| `Rechnung` | 273, 305, 309, 283, 288 |
| `prüfen` | 206, 200, 407, 231, 248 |
| `Vollstreck` | 80, 87, 86, 86, 83 |
| `Erdgeschoß` | 285, 309, 261, 257, 271 |
| `Rechnung Erdgeschoss` | 367, 376, 392, 334, 293 |
| `ab` | 306, 317, 295, 305, 289 |
| `from:fiktiv steuer` | 128, 114, 101, 122, 112 |
| `has:attachment` | 39, 42, 37, 29, 31 |
| `"Erdgeschoss links"` | 29, 27, 34, 31, 36 |
| `Xylophon` (kein Treffer, Grundlast) | 26, 55, 38, 32, 27 |

## Was die Zeit kostet und was geändert wurde

Der erste Entwurf rechnete alles in einer SQL-Anweisung (Treffer beider Indizes per `UNION ALL` in eine Zwischentabelle, `GROUP BY` je Begriff, Join mit `messages`, Fensterfunktion für die Gesamtzahl). Bei 100.000 Nachrichten:

| Anfrage | erster Entwurf (Median) | Endstand (Median) |
|---|---:|---:|
| `Rechnung` | 643 ms | 247 ms |
| `Rechnung Erdgeschoss` | 904 ms | 291 ms |
| `prüfen` | 413 ms | 159 ms |
| `ab` | 388 ms | 264 ms |
| `has:attachment` | 119 ms | 8 ms |
| `in:Archiv/2024` | 314 ms | 98 ms |

Aufschlüsselung für „Rechnung" (49.045 Treffer) im ersten Entwurf: Wortindex mit BM25 67 ms, Trigram-Index nur IDs 89 ms, Trigram mit BM25 352 ms, Zwischentabelle und Gruppierung rund 300 ms, Fensterfunktion und Sortierung rund 150 ms. Geändert (`internal/store/search_text.go`, Migration 0005, `store.Open`):

1. **Kein BM25 über den Trigram-Index:** Reine Teilwort-Treffer bekommen einen sehr kleinen festen Wert (sie stehen hinter Wort-Treffern, untereinander entscheidet das Datum).
2. **Mengenarbeit in Go:** Beide FTS-Abfragen je Begriff liefern nach `rowid` sortiert (ohne Sortieraufwand in FTS5); Vereinigung, Schnitt und Ausschluss sind lineare Zusammenführungen. Die Abfragen aller Begriffe laufen nebeneinander auf eigenen Verbindungen.
3. **Schmale Indizes** (Migration 0005): `messages_search` (ID, Konto, Datum, Filterspalten) für das Nachschlagen der Kandidaten, `messages_sortdate` für Anfragen nur mit Operatoren (Indexlauf bis zur Seitengröße, Zählung getrennt). Ordner- und Tag-Filter als Mengen statt `EXISTS` je Zeile.
4. **Memory-Mapped I/O** (`mmap_size` 1 GiB): Jede SQLite-Verbindung hat einen eigenen Seiten-Cache von 2 MB; die parallelen Verbindungen und jeder neue CLI-Prozess starteten kalt (Nachschlagen der Kandidaten 250 statt 100 ms). Mit mmap lesen sie aus dem Seiten-Cache des Betriebssystems. Für drei schwere Anfragen zusammen: 400–830 ms ohne, 205–270 ms mit mmap.

Nicht umgesetzt (Ertrag zu klein für den Aufwand): Vollscan des schmalen Index statt Nachschlagen bei sehr vielen Kandidaten (gemessen 10–20 % schneller nur oberhalb von etwa 40 % Trefferquote); `optimize` der FTS-Tabellen nach dem Aufbau.

## Index aufbauen

- Direkt nach dem Sync (`index`-Job, Transaktionen zu 200 Nachrichten, Text falten und stemmen, beide Tabellen schreiben): 99.800 Nachrichten in 104 s, 959/s. Beim Neuaufbau aus leerem Index (`POSTKAUZ_S17_REINDEX=1`, während parallel `make check` lief) 120 s, 834/s. Für 100.000 Nachrichten also knapp zwei Minuten einmalig nach dem Erstsync; danach nur neue und geänderte Nachrichten.
- Größe: Wortindex 67 MB, Trigram-Index 180 MB (rund 1,1 bzw. 2,7 KB je Nachricht bei 1,6 KB Rohmail; Trigramme kosten Platz). Die Datenbank wächst von 253 auf 500 MB.
