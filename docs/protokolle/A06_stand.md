# A06 „Alltag 2: Adressen, Zitat, Erkennung“: Stand, Messungen, Prüfläufe, Build 0.3.0-rc4

Auftrag [auftraege/A06_alltag2.md](../../auftraege/A06_alltag2.md) (Betreiber-Sitzung DRACO-PK14, 05.11.2026). Zweig `claude/youthful-wright-fimlkq`, Basis `main` `ed76b6a` (Stand 0.3.0-rc3, Build-Commit `28941b7`). Kein Tag, kein Release, kein Merge. Kein Schema-Upgrade, keine Migration (Schema 22).

## 1. Commits

| Commit | Inhalt |
|---|---|
| `746364c` | Auftrag A06 wörtlich abgelegt, CLAUDE.md Pflichtlektüre Punkt 4 auf A06 |
| `0b4cae1` | Teil 1: Adressen RFC-5322-sicher (`compose.FormatAddress`) |
| `7c724ce` | Teil 2: Zitat im HTML in Gmails Form, Golden-Datei neu |
| `748edd7` | Teil 3: verwaiste Fortsetzungszeilen (`sanitize.JoinWrappedQuotes`), `SentCheckRev` 2 |
| `544d725` | Teil 4: ersetzte Entwürfe (`abandoned`, Grund `replaced`) |
| `e821a3e` | Teil 5: Warnung bei abweichendem Betreff |
| `fc46320` | Teil 6: CHANGELOG, Roadmap, Abnahme D12, dieses Protokoll (erste Fassung); **Build-Commit von 0.3.0-rc4** |
| (danach) | Prüfsummen und Prüfläufe in diesem Protokoll |

Die Teile 1 bis 4 sind aus einem gemeinsamen Arbeitsstand in Einzel-Commits zerlegt; jeder Zwischenstand ist geprüft (frischer Arbeitsbaum je Commit: `gofmt -l` leer, `go vet ./...`, `go test -short` für compose, ops, drafts, cli, sanitize, store, mailsync: alle grün).

## 2. Festlegungen (Standardantworten von Claude, Einspruch bis zum Merge)

1. **Teil 1, Ort und Umfang:** `compose.FormatAddress` liegt neben `parseAddress` (der Parser, der den Text wieder liest). Zusätzlich zur Liste des Auftrags kommt ein Name mit `=?` in Anführungszeichen: `net/mail` dekodiert encoded-words nur in ungequoteten Namen, sonst wäre der Roundtrip verletzt.
2. **Teil 1, CLI:** `draft show`, `draft new|reply|forward|edit` zeigen die Texte aus der Operation; die Absenderzeile gespiegelter Nachrichten (`show`, `list`, `messages.from_addr`) bleibt wie bisher.
3. **Teil 2, kanonische Form:** Verschachtelt wird nur, was der Klartext Byte für Byte gleich wiedergibt: k-mal `> ` vorn ist Ebene k, eine Zeile aus k-mal `> ` ohne das letzte Leerzeichen (`>`, `> >`) ist eine leere Zeile auf Ebene k. `>foo`, `>>foo` und der Rest nach dem ersten nicht kanonischen `>` bleiben wörtlicher Text auf ihrer Ebene. Eine leere letzte Zeile auf einer inneren Ebene bekommt ein `<br>` (sonst fiele sie weg).
4. **Teil 3, Regel im Detail:** Eine Zeile mit Leerraum vor `>` gilt als Zitatzeile (wie in StripQuotes); verbunden wird mit einem Leerzeichen, die Fortsetzung ohne Rand-Leerraum; CRLF wird vorher zu LF. Sind Klartext und HTML-Text leer, nimmt `sentText` wie bisher `text_unquoted`.
5. **Teil 4, „nach der letzten Ablage“:** Date-Kopfzeile der Antwort echt nach dem späteren Zeitpunkt von `content_changed_at` und `pushed_date` (Date der abgelegten Kopie), auf Sekunden gekürzt. Im Audit steht die älteste passende Antwort.
6. **Teil 4, Zustandsmaschine:** kein neues Ereignis; das System nutzt `EvAbandon` (wie `CloseForKey`), der Kommentar in `state.go` nennt die beiden Systemfälle.
7. **Teil 4, `draft show`:** Der Grund kommt ohne Migration aus dem Audit (`drafts.replaced_detected`, ±1 s um `closed_at` über den Index `audit_events_ts`). Nach `audit rotate` über diesen Tag fehlt er (Entwurf bleibt `abandoned`).
8. **Teil 4, bestehende Tests:** `TestThreadConditions` (Fälle (g)) und `TestThreadOneDraftPerMessage` erwarten jetzt `abandoned` statt `draft` (gewollte Verhaltensänderung: (a) bis (f) erfüllt, (g) nicht).
9. **Teil 5:** Vergleich mit `messages.subject` des Originals im Spiegel; die Warnung bildet `writeOutput` für `drafts.create` und `drafts.update` (auch im Trockenlauf). Eine Antwort ohne Original lässt sich weder anlegen noch ändern (compose verlangt es); der Fall „Original fehlt“ ist deshalb direkt an der Prüffunktion getestet.
10. **D12:** Ein erwarteter, aber nicht geschlossener Entwurf ist FAIL (3.2c), ebenso ein nicht erwarteter Abschluss (3.2b). Bei einem FAIL in Teil 3 kommt das alte Programm automatisch zurück. `draft edit <CommaDraft> --dry-run` „ohne Änderung“ nutzt `--quote=<gespeicherter Wert>`, weil `draft edit` ein Feld verlangt; das ist genau der Weg `drafts.update` ohne neue Empfänger.

## 3. Messungen

### 3.1 `BenchmarkCheckAfterSync22k` (`internal/drafts/bench_r1_test.go`)

22.000 Nachrichten in „Alle Nachrichten“ (jede zweite eine Antwort), 30 offene neue Entwürfe; `steady_replies` zusätzlich 30 offene, abgelegte Antworten mit Original, deren Kandidaten nicht zu den Empfängern passen. Cloud-Container, 4 Kerne, je drei Läufe zu 20 Prüfungen, die drei Stände **abwechselnd** gemessen (die Umgebung schwankt um etwa ±15 %). Zeit je Prüfung:

| Teil | rc3 (`ed76b6a`) | mit Teil 3 (`748edd7`) | mit Teil 3 und 4 (`e821a3e`) |
|---|---|---|---|
| `steady` (keine neue Nachricht) | 4,9–6,4 ms | 5,8–6,6 ms | 5,9–7,4 ms |
| `one_new` (eine neue Nachricht) | 9,5–10,7 ms | 10,3–11,1 ms | 11,8–14,7 ms |
| `steady_replies` | 22,0–23,1 ms | 23,2–25,0 ms | 30,0–35,2 ms |

Teil 3 liest nicht mehr Nachrichten als vorher (der Text wird weiter erst für Kandidaten gelesen, die die Kopfbedingungen erfüllen) und rechnet nur `JoinWrappedQuotes` und `StripQuotes` je gelesenem Kandidaten: etwa 1–2 ms in `steady_replies`, in `steady` im Rauschen. Teil 4 schlägt je offener, abgelegter Antwort mit Original die eigenen Antworten im Verlauf nach (`OwnReplies`, wie der Weg über den Verlauf über `thread_refs` und `messages_thread`): etwa 0,25–0,4 ms je solcher Antwort und Lauf. Eine erste Messung vor dem Zerlegen (nicht abwechselnd) ergab für rc3 5,5–7,4 / 10,1–11,2 / 20,0–22,0 ms und mit Teil 3 6,3–7,8 / 11,8–13,8 / 22,4–25,3 ms.

## 4. Prüfläufe

Umgebung: Cloud-Sitzung, Go 1.27.3 (einmal über `GOTOOLCHAIN=go1.27.3 go version` geholt, danach `GOTOOLCHAIN=local`, Ausnahme laut CLAUDE.md). **Kein Docker** in dieser Sitzung.

| Lauf | Ergebnis |
|---|---|
| `make check` auf dem Gesamtstand vor dem Zerlegen | grün (8:13 min; gofmt, vet, alle Tests, Guards, PII-Gate) |
| Zwischenstände `0b4cae1`, `7c724ce`, `748edd7`, `544d725` | je grün (gofmt, `go vet ./...`, `go test -short` der berührten Pakete) |
| `make check` an `fc46320` | grün (Code unverändert, Testcache) |
| `make cross` an `fc46320` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make race` an `fc46320` | grün (Exit 0, 36:25 min; 40 Pakete mit Tests ok, kein Data Race; `internal/cli` 34:48 min, `internal/importer` 20:20 min, `internal/ops` 16:42 min, `internal/mailsync` 16:31 min). `internal/cli` nähert sich der Zeitgrenze von 45 min (vorher 27 bis 30 min). |
| `make integration` | nicht gelaufen (kein Docker) |
| `make vuln` | nicht gelaufen (in der Cloud gesperrt) |

Tests: 23 neue Testfunktionen (A06: `TestA06…` in compose, ops, cli, drafts; `TestJoinWrappedQuotes`, `TestJoinWrappedQuotesThenStrip` in sanitize); geändert: `TestQuoteAndSignature` (Prüfung auf `blockquote.gmail_quote`), `TestGoldenReplyAll` (Golden-Datei neu), `TestThreadConditions` und `TestThreadOneDraftPerMessage` (ersetzt statt offen). Im Repository jetzt 1.072 Testfunktionen. Die Pflichttests zu Teil 1 bis 3 sind gegen den alten Code rot (Teil 1: `invalid_address`; Teil 3: Fall A, B und Umlaut-Kante bleiben offen) und mit der Änderung grün.

## 5. Build 0.3.0-rc4

Nach [betrieb.md](../betrieb.md) Abschnitt 7: zwei frische Klone ohne Tags von GitHub (`git clone --no-tags --branch claude/youthful-wright-fimlkq`), je `git checkout fc46320`, `git status --short` leer, keine Tags, `make release VERSION=0.3.0-rc4`. **Build-Commit `fc46320`**; danach kam nur dieser Nachtrag zum Protokoll dazu.

- `dist/SHA256SUMS` beider Klone gleich (ohne die Zeile `VERSION`).
- `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261105143031-fc463206ecc8`, `vcs.revision=fc463206ecc83a81f0e24c313bf886b808b38a6f`, `vcs.modified=false`.
- `postkauz version`: `postkauz 0.3.0-rc4 (go1.27.3 linux/amd64)`; `doctor --json` an einem leeren Datenverzeichnis: `schema_known 22` (keine Migration).

```
f4f0ef69cd28b2a97a08be1aff9241c5ba27fb122dec5dd30138cfe7c5e37085  postkauz-windows-amd64.exe
773e018d6ddb2a2735d2c56803be01577662a300dd5bdcc36b24c1fdc7bdd32e  postkauz-linux-amd64
db3939bd2449a80e3b178dc6b63f454b2c226f942bd5704bf73e457f70c88b2d  postkauz-darwin-arm64
```

Windows-Programm: 22.638.080 Byte. Das Abnahmeskript gehört zum Stand des Zweigs (`docs/abnahme_D12.ps1`), nicht zum Programmpaket.

### 5.1 Diff der Golden-Datei (Teil 2)

`internal/compose/testdata/reply_all.golden.eml`: Der Klartextteil ist unverändert; anders sind nur der HTML-Teil und die MIME-Grenzen (sie hängen am Inhalt):

```
-<div>On Wed, 21 Oct 2026 09:15 +0200, Erika Beispiel &lt;erika@example.org&=
-gt; wrote:</div>
-<blockquote>Guten Tag,<br>
+<div class=3D"gmail_quote"><div dir=3D"ltr" class=3D"gmail_attr">On Wed, 21=
+ Oct 2026 09:15 +0200, Erika Beispiel &lt;erika@example.org&gt; wrote:<br><=
+/div><blockquote class=3D"gmail_quote" type=3D"cite" style=3D"margin:0px 0p=
+x 0px 0.8ex;border-left:1px solid rgb(204,204,204);padding-left:1ex">Guten =
+Tag,<br>
 …
-Erika</blockquote>
+Erika</blockquote></div>
```

## 5a. Bestand und Upgrade

- **Teil 1:** Gespeicherte Entwürfe halten Name und Adresse getrennt (`store.Addr`); sie heilen ohne Datenkorrektur beim nächsten `update`/`request_ready` (Test `TestA06StoredDraftHealsOnUpdate`: mit dem alten Code `invalid_address`, mit dem neuen `pending_approval`). Ein abgelegter Entwurf mit Klammern im Empfängernamen trug bisher den gekürzten Namen in der Kopie; nach dem Update ergibt der Neubau den vollen Namen, `draft show --eml` meldet dann `changed after the copy`.
- **Teil 2:** offene Antwort-Entwürfe mit Zitat: `approve` → `approval_stale`, `ready` → `draft` beim Push, `--eml` „changed after the copy“; `preview_hash` und Übernahme-Erkennung stabil (body_text gleich).
- **Teil 3:** `SentCheckRev` 2 → einmalige Neuprüfung aller offenen Entwürfe im ersten vollständigen Lauf.
- **Teil 4:** greift ab dem ersten Lauf bei jedem Lauf (ohne Hochwassermarke), also auch für Entwürfe von vor dem Update.

## 5b. Nachbesserung 1 (Gegenprüfung von `fc46320`)

Gegenprüfung der Betreiber-Sitzung: opus-pruefer PASS mit Hinweisen, zwei `claude -p`-Läufe und Codex FAIL, fast nur wegen des Abnahmeskripts. `make race` an `fc46320` war zu Beginn schon fertig (Abschnitt 4: grün, 36:25 min). Zwei Commits:

| Commit | Inhalt |
|---|---|
| `e51f5b6` | Code: ersetzte Entwürfe nur mit `remote_state` schon vorher `missing`, bekanntem und gespiegeltem Ablageordner und ohne weiterbearbeitete Antwort im Entwürfe-Ordner (`EditedReplyInDrafts`); beim Schließen erst `adoptOrphan`, dann `checkCopy`. Zitat höchstens 20 Blockquotes tief, leere letzte Zitatzeile nach `sanitize.HTML` geprüft. `drafts.get`-Schema mit `closed_reason`, `replaced_by_message_id`, `updated_by`, `push_hold`; Vertragstest: jedes Ausgabefeld steht im Schema. `json_valid`-Schutz in `DraftReplacedBy`. Betreff-Warnung gegen den gekürzten abgeleiteten Betreff. Test der bekannten Grenze der Fortsetzungsregel. CHANGELOG (rc3 ohne „noch kein Tag“), versanderkennung.md 3b und 8, Roadmap 77. |
| `985ae3b` | Abnahmeskript D12: Rückweg bei jedem FAIL vor dem Neustart der Aufgaben (E.0), Umbenennen statt Löschen mit Prüfung, 3.4c nach Fall (Name gleich Adresse bzw. Sonderzeichen), 1.4 mit `remote` und Erwartung, Grenze 500 und verschwundene Entwürfe, nur Prozesse aus dem Ordner von `-Exe` beenden, 0.6 gegen `accounts list`; Anleitung mit Entscheidungsweg bei unerwartetem Abschluss und dem Hinweis auf den stdio-MCP der Claude-App. **Build-Commit von 0.3.0-rc4 (neu).** |

Gegenproben: Die Tests der Sicherungen (a), (c) und „Ordner entfernt“ werden ohne die jeweilige Prüfung rot; die Prüfung „Ordner gespiegelt“ und „übernommene Kopie → nicht schließen“ sichert `checkCopy` zusätzlich ab (die Tests bleiben dann grün). Die neuen Zitatfälle (600 Ebenen, NUL) sind mit dem Stand vor der Nachbesserung rot. Der Fall „Original fehlt“ der Betreff-Warnung ist über die Operationen nicht erreichbar (compose verlangt das Original) und direkt an der Prüffunktion getestet.

### 5b.1 Benchmark nach Nachbesserung 1

Drei Läufe, abwechselnd, Zeit je Prüfung:

| Teil | rc3 (`ed76b6a`) | Nachbesserung 1 (`985ae3b`) |
|---|---|---|
| `steady` | 5,1–5,9 ms | 5,3–8,4 ms |
| `one_new` | 10,1–12,9 ms | 12,0–12,5 ms |
| `steady_replies` | 20,1–24,3 ms | 20,3–27,5 ms |

Weil ein Entwurf erst ersetzt wird, wenn seine Kopie schon in einem früheren Lauf als fehlend vermerkt ist, schlägt die Regel für die 30 abgelegten Antworten des Benchmarks (`remote: pushed`) gar nicht mehr nach; die Zusatzlast von Teil 4 aus Abschnitt 3.1 (etwa 7–10 ms) entfällt. Die Streuung des ersten Laufs ist Umgebung.

### 5b.2 Prüfläufe Nachbesserung 1

| Lauf | Ergebnis |
|---|---|
| `make check` vor jedem der beiden Commits | grün (7:14 min bzw. mit Testcache) |
| `make cross` an `985ae3b` | grün |
| `make race` an `985ae3b` | grün (Exit 0, 34:25 min; 40 Pakete mit Tests ok, kein Data Race; `internal/cli` 33:58 min, `internal/importer` 19:43 min, `internal/ops` 16:15 min, `internal/mailsync` 16:01 min; parallel lief der Nachbau) |
| `make integration` | nicht gelaufen (kein Docker) |

### 5b.3 Build 0.3.0-rc4 (neu, ersetzt den Build an `fc46320`)

Zwei frische Klone ohne Tags von GitHub, je `git checkout 985ae3b`, `git status --short` leer, `make release VERSION=0.3.0-rc4`. `dist/SHA256SUMS` beider Klone gleich (ohne `VERSION`); `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261105153847-985ae3bcf212`, `vcs.revision=985ae3bcf212ce36ed2a99dd9b049936ba98b7bc`, `vcs.modified=false`; `postkauz version`: `postkauz 0.3.0-rc4 (go1.27.3 linux/amd64)`.

```
ba75203c6c478afd9a6b015e931b05f2b3961a3d84593a6ae469e596413a28f1  postkauz-windows-amd64.exe
8d23eb6b8d63649757a3328961e66ce5390d77f9cb48559b0d4d750aa0d0e1c8  postkauz-linux-amd64
f1a4bdfee1cb9d53aeca2ce0f3641499314cb6a9e99d6f29e6541dab2f8375d2  postkauz-darwin-arm64
```

Windows-Programm: 22.643.712 Byte. Der frühere rc4-Build an `fc46320` (Windows `f4f0ef69…7085`) ist ersetzt; nicht verwenden. **Auch dieser Build ist durch Abschnitt 5c ersetzt.**

## 5c. Nachbesserung 2 (zweite Gegenprüfung von `985ae3b`)

Gegenprüfung der Betreiber-Sitzung: opus-pruefer PASS mit Hinweisen (alle 16 Punkte der Nachbesserung 1 OK), Codex und zwei `claude -p` FAIL, Lücken vor allem im Abnahmeskript. Für `985ae3b` lief zu Beginn nichts mehr (race und Neubau standen schon in `0e13a7f`); beides ist durch diesen Abschnitt ersetzt. Zwei Commits:

| Commit | Inhalt |
|---|---|
| `b41f9eb` | Code: ersetzte Entwürfe mit dem Stand „missing“ von **vor** dem Push zu Beginn des Laufs (`CheckOptions.MissingBefore`), Sicherung (c) ab der letzten Ablage und im Verlauf des Originals, unerreichbarer Zweig „Ordner nicht gespiegelt“ entfernt; `JoinWrappedQuotes` linear; Name gleich Adresse schon beim Bau leer, lokaler Teil mit Leerzeichen wieder gequotet; schärfere Tests; bekannte Grenze der Fortsetzungsregel richtiggestellt. **Build-Commit von 0.3.0-rc4 (endgültig in dieser Runde).** |
| `818b35c` | Abnahmeskript D12 und Anleitung (nur `docs/abnahme_D12.*`): Kennzeichen „Teil 3 vollständig“, Rückweg bei jedem FAIL, jeder Ausnahme und unvollständigem Teil 3, 4.1 nur bei vollständig ohne FAIL und Rückweg, wenn 4.1 scheitert; `-Rueckweg` stellt bei Fehler den Zustand her und lässt die Aufgaben aus; nie löschen; `-Accounts` genau; Hilfsskripte nur dieses Dauerbetriebs; Vorprüfungen 1.2c und 1.4; 3.1 `errors` 0; 3.4/3.4b ganze Ausgabe; 3.4d nur vorher offene; Start-Tasks je Aufgabe; `-ExpectSha256` und `vcs.revision`; Start im Hintergrund, harter Abbruch. |

Gegenproben: Der Test „PushCheck setzt missing im selben Lauf“ wird mit der Prüfung auf `remote_state` statt auf den Stand vor dem Lauf rot; der Test „Gmail-Fassung zwischen Ablage und lokaler Änderung“ mit der alten Grenze; der Test „Übernahme zuerst“ ohne `adoptOrphan`; „erstes drafts.update ändert nichts“ ohne die Normalisierung beim Bau. Jeder Fall in `TestA06ReplacedRechecksInTransaction` schließt, sobald seine eine Verletzung aufgehoben ist.

### 5c.1 Messungen

- `EditedReplyInDrafts` (Sicherung c), neuer Teil `edited_reply_lookup` von `BenchmarkCheckAfterSync22k`, Zeit je Abfrage bei 22.000 Nachrichten: mit der Abfrage aus Nachbesserung 1 (über alle Nachrichten des Kontos) 18,8–19,8 ms, über `thread_refs` → `messages_thread` 0,17–0,37 ms.
- `BenchmarkCheckAfterSync22k`, drei Läufe abwechselnd, Zeit je Prüfung (die Umgebung streut stark):

| Teil | rc3 (`ed76b6a`) | Nachbesserung 2 (`b41f9eb`) |
|---|---|---|
| `steady` | 5,8–7,4 ms | 6,0–6,6 ms |
| `one_new` | 9,3–10,8 ms | 11,4–12,7 ms |
| `steady_replies` | 18,3–28,2 ms | 21,8–31,9 ms |

- `JoinWrappedQuotes` mit einer Kette von 10 MiB unter einer „>“-Zeile: etwa 0,3 s (Test mit Zeitgrenze 5 s; vorher quadratisch).

### 5c.2 Prüfläufe Nachbesserung 2

| Lauf | Ergebnis |
|---|---|
| `make check` vor beiden Commits | grün (7:23 min bzw. mit Testcache) |
| `make cross` an `818b35c` (Code wie `b41f9eb`) | grün |
| `make race` an `818b35c` (Code wie `b41f9eb`) | grün (Exit 0, 32:38 min; 40 Pakete mit Tests ok, kein Data Race; `internal/cli` 32:13 min, `internal/importer` 18:31 min, `internal/mailsync` 15:13 min, `internal/ops` 15:13 min; parallel lief der Nachbau) |
| `make integration` | nicht gelaufen (kein Docker) |

### 5c.3 Build 0.3.0-rc4 (endgültig in dieser Runde, ersetzt die Builds an `fc46320` und `985ae3b`)

Zwei frische Klone ohne Tags von GitHub, je `git checkout b41f9eb` (letzter Code-Commit), `git status --short` leer, keine Tags, `make release VERSION=0.3.0-rc4`. `dist/SHA256SUMS` beider Klone gleich (ohne `VERSION`); `go version -m` beider Windows-Programme gleich: `mod github.com/KNUFFIXX/postkauz v0.0.0-20261105163812-b41f9ebc4e76`, `vcs.revision=b41f9ebc4e7606bec2feaae4a3d7bf175f78ece4`, `vcs.modified=false` (dieselbe Revision steht als Klartext in der Programmdatei; `abnahme_D12.ps1` liest sie in 0.2b); `postkauz version`: `postkauz 0.3.0-rc4 (go1.27.3 linux/amd64)`.

```
bd7abd976db73c0cd893e7e5e5aa07b571295a867e17e8a51f8f85357282a16e  postkauz-windows-amd64.exe
8d01f7a3d6cd3833b00ffea77e358ab022fdd81afc506d071189a88706b58f28  postkauz-linux-amd64
3e832b2c10693b0c2233149631853ab1918144e8b5fe1ba3667108a5710a1e6e  postkauz-darwin-arm64
```

Windows-Programm: 22.646.784 Byte. **Build-Commit `b41f9eb`; danach kamen nur der Skript-Commit `818b35c` (nur `docs/abnahme_D12.ps1` und `docs/abnahme_D12.md`, nicht Teil des Programmpakets) und dieser Nachtrag (nur Doku) dazu.** Für D12: `-ExpectSha256 bd7abd976db73c0cd893e7e5e5aa07b571295a867e17e8a51f8f85357282a16e`, Skript vom Stand des Zweigs.

## 5d. Abnahme D12 (05.11.2026)

Abnahme D12 auf dem Büro-PC bestanden, Programm 0.3.0-rc4 an `b41f9eb` (Windows `bd7abd97…a16e`, im Skript per `-ExpectSha256` und `vcs.revision` geprüft):

- **Teil A** (18:25, Aufruf mit `-KeepStdioMcp`, `-ExpectSha256` und abweichenden Pfaden/Aufgaben): **35 OK, 0 FAIL**, Protokoll [D12_windows.txt](D12_windows.txt) (wörtlich abgelegt, maskiert). Sicherung vor dem Tausch 81.644 Dateien, 46,8 GB; doctor vorher und nachher `database ok`, Schema 22 (keine Migration); `audit verify` ok. Erster vollständiger Lauf: Entwürfe 3 und 15 `sent_by_human` (Rückfall, Teil 3), Entwurf 4 `abandoned` mit Grund `replaced` (Teil 4), keine weiteren Abschlüsse; Entwurf 27 (Empfängername mit Komma): mit rc3 `invalid_address`, mit rc4 `draft edit`/`draft request` im Trockenlauf ohne `invalid_address` und Name in Anführungszeichen (Teil 1). Dauerbetrieb seit 05.11.2026 18:42 auf rc4, das alte Programm liegt als `postkauz-rc3.exe` daneben; der stdio-MCP der Claude-App läuft bis zu ihrem Neustart noch mit rc3.
- **Teil B** (18:45, Mitmach): **8 OK, 0 FAIL**, Protokoll [D12_teilB.txt](D12_teilB.txt). Antwort-Entwurf 28 auf eine eigene Mail mit „>“-Zitat in Gmail im Browser unverändert gesendet, erkannt als `sent_by_human` (Rückfall); in der gesendeten Nachricht stehen ab der Einleitungszeile **19 von 19** Zitatzeilen mit „>“. Damit ist Teil 2 (Zitat in Gmails Form) am echten Gmail bestätigt (Messtabelle in [versanderkennung.md](../versanderkennung.md) Abschnitt 9).
- **Einordnung der doctor-Zeile in 1.3** („3 calls started more than 10 minutes ago without an end entry“): Es sind die drei `sync --watch` (je Konto einer), die 1.1 hart beendet hat (Stop-Process nach 20 s); ihr Start-Eintrag liegt Stunden zurück, ein Abschluss fehlt. Der nächste `sync` des Kontos schließt solche Einträge als `process_ended` (dokumentiert in [betrieb.md](../betrieb.md), Abschnitt zum Stoppen der Aufgaben); in 3.5 steht die Warnung deshalb nicht mehr. Kein Fehler.
- Freigabe: Der Betreiber hat rc4 über das Board freigegeben (Karte PK14-1: rc4 nach Prüfung einsetzen, danach nach `main`).

## 6. Offen

- ~~Abnahme D12 (Teil A und Teil B)~~ bestanden (Abschnitt 5d).
- Integrationstest-Fall für Teil 3: nicht geschrieben, Docker gibt es in dieser Sitzung nicht (`make integration` lief nicht); ungeprüften Integrationscode wollte ich nicht ablegen.
- `abnahme_D12.ps1` ist nur von Hand geprüft (PowerShell gibt es im Container nicht; Klammerbilanz und Zeichensatz maschinell geprüft); die Anleitung beginnt deshalb mit einem Parser-Check auf dem Büro-PC.
