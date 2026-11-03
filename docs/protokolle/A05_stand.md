# A05 „Erkennung und Text“: Stand, Messungen, Prüfläufe, Build 0.3.0-rc3

Auftrag [auftraege/A05_erkennung.md](../../auftraege/A05_erkennung.md) (Betreiber-Sitzung DRACO-PK13, 03.11.2026). Zweig `claude/quirky-feynman-1jsuxj`, Basis `main` `cb8919d` (Stand 0.3.0-rc2, Build-Commit `7231758`). Kein Tag, kein Release, kein Merge. Kein Schema-Upgrade, keine Migration (Schema 22).

## 1. Commits

| Commit | Inhalt |
|---|---|
| `8e91600` | Auftrag A05 wörtlich abgelegt, CLAUDE.md Pflichtlektüre Punkt 4 auf A05 |
| `8ebe47b` | Teil 1: symmetrische Kürzung des Zitats im Rückfall (Outlook-Verlauf), einmalige Neuprüfung offener Entwürfe (`meta` `sent_check_rev/<Konto>`) |
| `88c05be` | Teil 2: dritter Weg der Versanderkennung über den Verlauf (Grund `thread`) |
| `fb920eb` | Teil 3: HTML-Quelltext im Klartextteil gilt als HTML (`sanitize.PlainIsHTML`, Aufbereitungsstand 4, gezielte Neuaufbereitung) |
| `5cdab24` | Teil 4: CHANGELOG (rc2 festgeschrieben, neuer Abschnitt „Unreleased“), Roadmap, Abnahme D11, Protokoll (erste Fassung); **Build-Commit von 0.3.0-rc3** |
| (danach) | dieses Protokoll mit Build und Prüfsummen |

## 2. Festlegungen (Standardantworten von Claude, Einspruch bis zum Merge)

1. **Teil 1, innere Zitate:** Das Zitat des Entwurfs wird in zwei Fassungen gekürzt, die bessere zählt: alle Ebenen „>" entfernt (Mailprogramm sendet das Zitat ganz ohne „>") und nur die äußere entfernt, danach StripQuotes (innere Zitate kommen mit „>"). Beide sind Teilmengen des Zitats; der Nachsatz-Schutz (Jaccard) bleibt.
2. **Teil 1, Nachsatz unter dem Outlook-Verlauf:** StripQuotes schneidet den gesendeten Text am Outlook-Kopfblock ab; ein Nachsatz ganz unten **unter dem Verlauf** ist deshalb unsichtbar und der Entwurf gilt als unverändert gesendet. Ein Nachsatz zwischen zitiertem Text und Verlauf verhindert den Schnitt (Pflichttest 2). Dokumentiert in [versanderkennung.md](../versanderkennung.md) Abschnitt 3.
3. **Teil 1, Neuprüfung:** Je Konto ein Schlüssel `sent_check_rev/<Konto-ID>` in `meta` (wie `incomplete_answers/<Konto-ID>`), Zähler `drafts.SentCheckRev` = 1.
4. **Teil 2, (f) „übernommene Fassung“:** wie `checkCopy`: neuere Nachricht mit der Message-ID des Entwurfs im Entwürfe-Ordner (`taken_over`). Beim Schließen prüft `SentDetected` mit Grund `thread` (f) in derselben Transaktion noch einmal (ein Push dazwischen legte sonst eine neue Kopie ab).
5. **Teil 2, (g) „8 Wörter“:** 8 **verschiedene** Wörter des eigenen Texts ohne Signatur (dieselbe Wortmenge wie beim Rückfall).
6. **Teil 2, Reihenfolge:** Message-ID → Rückfall → Verlauf; die Zuordnung „eine Nachricht schließt höchstens einen Entwurf“ gilt über alle drei Wege (gemeinsame Menge `claimed`).
7. **Teil 3, Regel:** Nach `<!doctype html`, `<html`, `<head`, `<body`, `<meta` muss der Name enden (Leerraum, `>`, `/` oder Textende): `<header>`, `<metadata>`, `<htmlx` bleiben Klartext. Leerraum: Leerzeichen, Tabulator, LF, CR (wie `ltrim(x, char(32,9,10,13))`).
8. **Teil 3, Neuaufbereitung:** Die SQL-Vorauswahl (`plainHTMLCond`) fasst etwas weiter (`<header`, `<?xml` ohne HTML); solche Nachrichten werden nur unnötig neu aufbereitet, mit demselben Ergebnis. Das Heben auf Stand 4 läuft nur, wenn der Lauf Kandidaten mit Stand 3 ohne Treffer sieht (im Normalbetrieb also gar nicht).

## 3. Messungen

### 3.1 Teil 2: `BenchmarkCheckAfterSync22k` (`internal/drafts/bench_r1_test.go`)

22.000 Nachrichten in „Alle Nachrichten“ (jede zweite eine Antwort), 30 offene neue Entwürfe; `steady_replies` zusätzlich 30 offene, abgelegte Antworten mit Original. Cloud-Container, 4 Kerne, `-count 3`, Zeit je Prüfung nach dem Spiegeln. „Vorher“ = `88c05be^` (Teil 1) mit demselben Benchmark.

| Teil | vorher | nachher |
|---|---|---|
| `steady` | 4,54–4,67 ms | 4,38–4,65 ms |
| `one_new` | 8,44–8,62 ms | 8,30–8,91 ms |
| `steady_replies` | 8,63–8,82 ms | 15,24–15,72 ms |

Erste Fassung der Abfrage (Verknüpfung mit der Message-ID des Originals in einer Abfrage): 1,76–1,80 s für `steady_replies`, weil SQLite den Index über das Konto nahm; zweite (zwei Schritte): 195 ms; endgültig mit fester Reihenfolge (`thread_refs CROSS JOIN messages`): 15,5 ms, also etwa 0,2 ms je offener, abgelegter Antwort und Lauf.

### 3.2 Teil 3: Neuaufbereitung

- `TestA05ReprocessOnlyPlainHTML` (`internal/mailsync`): 100 Nachrichten mit HTML im Klartextteil neu aufbereitet und 399 übrige auf Stand 4 gehoben in **58 ms** (Cloud-Container, Rohmails im Blob-Store).
- Synthetische Datenbank mit 61.500 Nachrichten (je etwa 4 KB Text, 130 Treffer; Wegwerf-Test, nicht im Repository): erste Kandidatenabfrage 80 ms, **Heben von 61.370 Nachrichten einmalig 1,6 s**, Kandidatenabfrage danach 58 ms; im Normalbetrieb (alles auf Stand 4) bleibt es bei der schon vorhandenen Abfrage (57 ms je Lauf).

## 4. Prüfläufe

Umgebung: Cloud-Sitzung, Go 1.27.3 (einmal über `GOTOOLCHAIN=go1.27.3 go version` geholt, danach `GOTOOLCHAIN=local`, Ausnahme laut CLAUDE.md), Docker-Dienst in der Sitzung gestartet.

| Lauf | Ergebnis |
|---|---|
| `make check` | grün vor jedem Commit (Laufzeit 5:17 bis 7:16 min, an `5cdab24` 2:28 min mit Testcache) |
| `make integration IT=dovecot,stalwart,greenmail` an `fb920eb` | grün (Exit 0, 3:44 min): 44 PASS (mit Untertests), 0 FAIL, 2 SKIP (`TestIntegrationAuthelia` nicht gewählt; `TestIntegrationMailboxActions/greenmail`, bekannt). Neu: `TestIntegrationSentDetectionReplies` (Fälle F, D, E je Server); Fall F mit dem alten Code rot (`Missing:1`), mit Teil 1 grün |
| `make race` an `fb920eb` | grün (Exit 0, 28:32 min; 40 Pakete ok, kein Data Race; `internal/cli` 27:16 min, `internal/mailsync` 13:43 min, `internal/ops` 13:15 min, `internal/drafts` 4:05 min) |
| `make cross` an `5cdab24` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make vuln` | nicht gelaufen (in der Cloud gesperrt) |
| Rückweg-Probe (Linux, Dovecot, Abschnitt 6) | rc2 läuft mit der von rc3 aufbereiteten Datenbank weiter |

Neue Tests: `TestFallbackOutlookHistory` (alle Formen aus `originalRe`, deutsch und englisch, Kopfblock ohne Trennlinie), `TestFallbackNestedQuote`, `TestFallbackOutlookHistoryPostscript`, `TestFallbackOutlookHistoryLookAlikeIntro`, `TestCutAtOriginal`, `TestFallbackRecheckAfterUpgrade` (Teil 1); `TestThreadSentenceAdded`, `TestThreadConditions` (15 Fälle: (a) neu, Original gelöscht; (b) nur Posteingang, `\Draft`, im Entwürfe-Ordner, „Alle Nachrichten“ mit fremdem Absender; (c) anderes In-Reply-To; (d) anderer Empfänger; (e) vor und nach dem Fenster; (f) Kopie noch da, nie abgelegt, bearbeitete Fassung da; (g) stark umgeschrieben, unter 8 Wörtern), `TestThreadScores`, `TestThreadAfterTakenOver`, `TestThreadSentBeforeCopyGone`, `TestThreadForward`, `TestThreadOneDraftPerMessage`, `TestThreadRechecksCopyOnClose`, Zeile in `TestRenderDraftCheck` (Teil 2); `TestPlainIsHTML`, `TestA05PlainPartWithHTML`, `TestA05ReprocessOnlyPlainHTML`, `TestShowPlainPartWithHTML`, `TestQuoteOfPlainPartWithHTML` (Teil 3). Die bestehenden Schutztests (W1 Schein-Einleitung, W2 eigene `quote_line`, Nachsatz) sind unverändert grün.

## 5. Build 0.3.0-rc3

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags (`git clone --no-tags --branch claude/quirky-feynman-1jsuxj`), je `git checkout 5cdab24` (Teil 1 bis 3 und die Doku von Teil 4), `git status --short` leer, keine Tags, `make release VERSION=0.3.0-rc3`. **Build-Commit `5cdab24`**; danach kam nur dieses Protokoll dazu.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`, sie enthält die Bauzeit).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103143458-5cdab2453bc2`, `vcs.revision=5cdab2453bc2d3fb2720daa7145a698c848895f9`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc3 (go1.27.3 linux/amd64)`. Rauchtest mit dem gebauten Linux-Programm an der Datenbank der Rückweg-Probe (Abschnitt 6): `doctor` `database ok`, Schema 22, `sync` 0 Fehler, `show` `text_source html`, `alternative_mismatch false`, keine Tags.

```
2ca5dd97f86d468cb646c35be8f34f01b6b66f04008e4df696722e557ef60228  postkauz-windows-amd64.exe
981a77d1a4b2744243a001a1b5634c11ad88de84c185a4943d7549dd70e5ea66  postkauz-linux-amd64
7af53469d3bafc8557e264f42f002fbae9284067a9bd7f6da40613d89c3314c8  postkauz-darwin-arm64
```

Windows-Programm: 22.615.040 Byte. `make cross` an `5cdab24` grün.

## 6. Rückweg-Probe

Linux-Programme aus `7231758` (rc2) und dem Arbeitsstand von Teil 3 (rc3) an **einer** Datenbank, Dovecot-Testserver, frisches Testpostfach mit zwei Mails mit HTML im Klartextteil (eine mit, eine ohne HTML-Teil) und einer normalen:

```
== 1. rc2: account add + sync
  pk-rc2 plainhtml-1: source=plain mismatch=True tags=True len=141
  pk-rc2 plainhtml-2: source=plain mismatch=False tags=True len=145
  pk-rc2 normal-1: source=plain mismatch=False tags=False len=29
  clean_version: [(3, 3)]  schema: (22,)
== 2. rc3: doctor + sync
  database: ok schema: 22
  pk-rc3 plainhtml-1: source=html mismatch=False tags=False len=29
  pk-rc3 plainhtml-2: source=html mismatch=False tags=False len=29
  pk-rc3 normal-1: source=plain mismatch=False tags=False len=29
  clean_version: [(4, 3)]  schema: (22,)
== 3. Rückweg rc2: doctor, show, neue Mail, sync
  database: ok schema: 22
  pk-rc2 plainhtml-1: source=html mismatch=False tags=False len=29
  pk-rc2 plainhtml-2: source=html mismatch=False tags=False len=29
  0 errors, exit 0
  pk-rc2 plainhtml-3: source=plain mismatch=False tags=True len=143
  clean_version: [(3, 1), (4, 3)]  schema: (22,)
== 4. wieder rc3: sync
  pk-rc3 plainhtml-3: source=html mismatch=False tags=False len=29
  clean_version: [(4, 4)]  schema: (22,)
```

Ergebnis: **Fall „ohne Migration“.** rc2 startet mit der Datenbank (`database: ok`, kein `too_new`), lässt Texte mit Stand 4 unverändert (es bereitet nur Stände unter 3 neu auf) und spiegelt neue Mails mit Stand 3; der nächste Lauf von rc3 erfasst sie. Eine Wiederherstellung der Sicherung ist für den Rückweg nicht nötig.

## 7. Nachtrag 2: Gegenprüfung von `5cdab24`/`1e30ec6`

Gegenprüfung der Betreiber-Sitzung (drei unabhängige Läufe, 2× Opus, 1× GPT, Nachprüfung am Quelltext): Code PASS mit Hinweisen, Abnahmeskript D11 so nicht einsatzfähig; der rc3-Build an `5cdab24` ist vom Betreiber bytegleich nachgebaut. Umgesetzt, je Gruppe ein Commit:

| Commit | Inhalt |
|---|---|
| `515d9b4` | Punkt 5: Nacharbeit (Threads, Neuaufbereitung) **vor** der Prüfung der Entwürfe. Gewählt statt „sent_check_rev erst setzen, wenn keine HTML-Kandidaten mehr übrig sind“, weil es einfacher ist (eine Verschiebung, keine neue Abfrage je Lauf, und der Weg über den Verlauf sieht frisch zugeordnete Threads). Endet die Nacharbeit nicht vollständig (Fehler oder vorübergehend nicht ablegbarer Anhang), läuft die Prüfung mit `KeepMark` und vermerkt nichts. Test `TestA05RecheckAfterCleanup` (gesendete Nachricht im Stand von rc2 mit HTML im Klartext): mit der alten Reihenfolge rot (`SentByFallback 0`, `Missing 1`), jetzt grün |
| `28941b7` | F10: eine Regel `sanitize.PlainAndHTML` für Sync und compose (ein HTML-Teil ohne Text wird nicht durch das Klartext-HTML ersetzt; Tests `TestPlainAndHTML`, `TestQuoteHTMLPartWithoutText`, Golden-Fall `empty-html`). F8: Tests für den Ausschluss per Inhalts-Hash auf dem Weg thread (ohne den Filter rot) und `TestSentRepliesNormalizesInReplyTo` (Spitzklammern und Leerraum auf beiden Seiten, Posteingang zählt nicht, ohne Message-ID nichts). **Letzter Code-Commit, Build-Commit des neuen rc3** |
| `b96e424` | Punkte 1 bis 4, F3, F6 in `abnahme_D11.ps1` (NDJSON für `audit` und `search`, try/catch/finally mit Aufgaben wieder an und Schlusszeile, Abbrüche vor dem Tausch, Rückkopie bei FAIL in 2.1/2.2, kein stderr von `sync` im Protokoll, Rückweg nur ohne vorhandene `postkauz-rc3.exe`); F11 (Upgrade-Folge `approval_stale`) in CHANGELOG und `abnahme_D11.md`; F12 (Grenze von `sent_check_rev` nach einem Rückweg) in `abnahme_D11.md` Abschnitt 5; F9 als Offene Frage in der Roadmap (seit Nachtrag 3: Offene Frage 2) |

Das Skript ist weiterhin nur von Hand geprüft (Klammern, Funktionsnamen, Aufrufe gegen die CLI-Ausgaben: `audit` und `search` liefern mit `--json` NDJSON, `draft list`, `show`, `doctor`, `sync` ohne `--watch` und `audit verify` ein Objekt); PowerShell gibt es im Container nicht.

### 7.1 Prüfläufe Nachtrag 2

| Lauf | Ergebnis |
|---|---|
| `make check` | grün vor jedem der drei Commits |
| `make cross` an `b96e424` | grün |
| `make integration IT=dovecot,stalwart,greenmail` an `b96e424` | grün (Exit 0, 4:00 min): 44 PASS, 0 FAIL, 2 SKIP (wie oben) |
| `make race` an `b96e424` | grün (Exit 0, 29:57 min; 40 Pakete ok, kein Data Race; `internal/cli` 29:31 min, parallel lief der Nachbau) |

### 7.2 Build 0.3.0-rc3 (neu, ersetzt den Build an `5cdab24`)

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags, je `git checkout 28941b7` (letzter Code-Commit), `git status --short` leer, `make release VERSION=0.3.0-rc3`. `dist/SHA256SUMS` beider Klone gleich (ohne `VERSION`); `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261103160518-28941b790c3f`, `vcs.revision=28941b790c3f51de78fc2a7feaf5da4e9eea7ba1`, `vcs.modified=false`. `postkauz version`: `postkauz 0.3.0-rc3 (go1.27.3 linux/amd64)`.

```
f7789cf35cfd9c16114b764add0e178bea61899da47a773ebb063f1f9e34e934  postkauz-windows-amd64.exe
5677538c9984d1eac667762a8891c78b83d230584ccc8f556c6f623feb4183a6  postkauz-linux-amd64
affb0a75e45104902fb07769d63ed3c6eef5d75dc2d0e5553b70fcfef1bf7ca1  postkauz-darwin-arm64
```

Windows-Programm: 22.616.064 Byte. Der frühere rc3-Build an `5cdab24` (Windows `2ca5dd97…ef60228`) ist ersetzt; nicht verwenden. Das Abnahmeskript gehört zum Stand `b96e424` (`docs/abnahme_D11.ps1`), nicht zum Programmpaket.

## 8. Offen

- Abnahme D11 auf dem Büro-PC ([abnahme_D11.md](../abnahme_D11.md)); das Skript ist in der Cloud weder ausgeführt noch von PowerShell geparst (kein PowerShell im Container), nur gegen die CLI-Ausgaben geprüft.
- Ob Gmail im Browser beim Senden eines Antwort-Entwurfs `In-Reply-To` behält (Voraussetzung für den Weg über den Verlauf), zeigt erst D11 bzw. der Alltag; der Testserver-Fall D bildet es nach.
- Offene Frage 2 der Roadmap (Bedingung (f) wie gebaut). Die Defaults von Teil 2 und 3 hat der Betreiber am 03.11.2026 bestätigt (Entschiedene Fragen 74, 75).
