# golangci-lint: Erstlauf, eingestuft (A12.1)

| | |
|---|---|
| Werkzeug | golangci-lint **v2.15.0**, gebaut mit go1.27.3 (`go run …@v2.15.0`) |
| Datum | 27.10.2026 |
| Stand | 9f48174 (main 1c9847a plus `.golangci.yml` und `make lint`; im Worktree des Subagenten als 0ba8501 gelaufen, Code gleich); Nachlauf nach den Behebungen auf 5311845: 78 Befunde; Nachlauf über den ganzen Teil T1c: Abschnitt „Nachlauf T1c“ |
| Kommando | `make lint` (= `go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0 run ./...`) |
| Konfiguration | `.golangci.yml`: Standardsatz (errcheck, govet, ineffassign, staticcheck, unused) plus gosec, bodyclose, gocritic; vollständiger Bericht (keine Obergrenzen, `uniq-by-line: false`) |

Einstufung: **echt** (ein Fehler oder ein Mangel, der behoben werden soll), **Fehlalarm** (mit Begründung), **später** (berechtigter Hinweis, aber nicht jetzt; mit Grund). „echt, behoben“ heißt: im Commit 5311845 erledigt, `make check` grün.

## Gesamtzahl je Linter

| Linter | ohne Ausschlüsse | mit `.golangci.yml` | davon behoben | offen |
|---|---:|---:|---:|---:|
| errcheck | 347 | 5 | 4 | 1 |
| gosec | 224 | 57 | 3 | 54 |
| staticcheck | 17 | 17 | 3 | 14 |
| unused | 5 | 5 | 0 | 5 |
| gocritic | 3 | 3 | 0 | 3 |
| ineffassign | 1 | 1 | 0 | 1 |
| bodyclose | 0 | 0 | 0 | 0 |
| govet | 0 | 0 | 0 | 0 |
| **Summe** | **597** | **88** | **10** | **78** |

„Ohne Ausschlüsse“ ist derselbe Linter-Satz ohne den Abschnitt `exclusions` (gemessen mit einer Kopie der Konfiguration außerhalb des Repositorys).

Einstufung der 88 Befunde mit Konfiguration: **echt** 16 (davon 10 behoben, 6 offen), **Fehlalarm** 44, **später** 28.

## Ausschlüsse in `.golangci.yml` (senken 509 Fehlalarme)

Alle Regeln stehen mit Begründung in `.golangci.yml`; `warn-unused: true` meldet eine Regel, die nichts mehr trifft.

| Regel | trifft (ohne Ausschlüsse) | Begründung kurz |
|---|---:|---|
| errcheck: `defer x.Close()` | 111 | Schließen eines Lesers (Datei, Blob, `sql.Rows`, Verbindung) ist Go-Idiom; Schreibpfade prüfen Close im Code ausdrücklich, ein nicht verschobener Close bleibt gemeldet |
| errcheck: `defer os.Remove(…)` | 2 | Aufräumen einer Temporärdatei nach Rename (`ops/auditexport.go:392`, `secrets/file.go:92`) |
| errcheck: `fmt.Fprint*` in `internal/cli/` und `internal/extract/child.go` | 220 | Terminalausgabe; ein Schreibfehler auf stdout/stderr ist nicht handlungsfähig. Nur diese Pfade, damit ein `Fprintf` in eine Datei anderswo gemeldet bleibt |
| errcheck: `os.Stdout/Stderr.WriteString` in `_test.go` | 9 | Hilfsprozesse in Tests (gespielte Hooks); der Test prüft die Ausgabe |
| gosec G104 | 13 | doppelt errcheck (trat erst nach den errcheck-Ausschlüssen zutage) |
| gosec in `_test.go`: G101, G115, G122, G202, G204, G301, G302, G304, G306, G404, G702, G703 | 141 | Pfade aus `t.TempDir`, Testdateien mit 0644, eigenes Testbinary als Unterprozess, Kanarienwerte, fester Zufall. **G116 (Bidi-Zeichen) bleibt in Tests an** |
| gosec in `internal/corpus/`: G115, G301, G306, G404 | 13 | Generator für synthetische Testdaten (nicht im Programm): übliche Dateirechte, deterministischer Zufall, gewolltes Abschneiden auf Bytes |

Hinweis zur Messung: Mit der Standardeinstellung `uniq-by-line: true` verdeckt ein Befund einen zweiten an derselben Zeile (errcheck verdeckte gosec G104, G304 verdeckte G122, G204 verdeckte G702). Die Konfiguration schaltet das ab, damit kein Befund erst nach einem Ausschluss auftaucht.

## Befunde mit Konfiguration

### errcheck (5)

| Ort | Einstufung | Begründung |
|---|---|---|
| `internal/cli/cli_test.go:399`, `internal/imapx/imapx_test.go:142`: `ln.Close()` | echt, behoben | Fehler von `net.Listen` und `Close` wurden verworfen; jetzt `t.Fatal` |
| `internal/ops/auditexport_test.go:28`, `:172`: `e.run(…"keys.list"…)` | echt, behoben | Der Aufruf erzeugt den Audit-Eintrag, auf dem der Test aufbaut; scheiterte er, prüfte der Test still etwas anderes. Jetzt `t.Fatal` |
| `internal/mailsync/watch.go:90`: `<-idleDone` im `defer` | Fehlalarm | Wartet beim Ende von `Watch` (nach `cancel`) nur, bis die IDLE-Schleife steht; ihr Fehler im laufenden Betrieb wird in Zeile 176 ausgewertet, beim Abbruch ist er erwartet (Kontext beendet) |

### gosec (57)

| Regel | Ort | Einstufung | Begründung |
|---|---|---|---|
| G116 Bidi-Steuerzeichen im Quelltext (3) | `internal/compose/review23_test.go:74`, `internal/extract/extract_test.go:233`, `internal/ops/review23_test.go:103` | echt, behoben | U+202E stand als rohes Zeichen im Quelltext (Trojan Source, Prüfer sehen es nicht). Jetzt als Escape `\u202e`; die Bytes der Testeingabe sind dieselben |
| G304 Datei aus Variable (13) | `blob/blob.go:216,229`, `cli/draft_bindings.go:84`, `cli/mcp_cmd.go:90`, `hook/hook.go:249`, `importer/sources.go:144`, `mailsync/lock.go:42`, `ops/auditexport.go:63,501,568`, `ops/draftstyle.go:179`, `policy/file.go:158`, `secretscan/secretscan.go:94` | Fehlalarm | Pfade stammen aus der eigenen Konfiguration, dem Datenverzeichnis (Blob-Namen sind geprüfte SHA-256-Werte) oder einer Angabe des Menschen in der CLI; kein Pfad kommt aus Mailinhalt oder von einem Agenten. Anhänge aus `attach_dir` (Pfad vom Agenten) öffnet `internal/compose/attach.go` über `os.Root`; die Stelle erscheint hier nicht. Bleibt gemeldet, damit neue Stellen auffallen; vor der Pflicht-Schaltung je Stelle `//nolint:gosec // Grund` |
| G304 (2) | `corpus/corpus.go:748,757` | Fehlalarm | Korpus-Leser für Tests; Verzeichnis vom Entwickler, Dateiname per `filepath.Base` |
| G703 Pfad per Taint-Analyse (10) | `cli/mcp_cmd.go:83,90`, `config/redirect.go:122,157,161,195,196`, `importer/sources.go:52,66,91` | Fehlalarm | Schlüsseldatei aus der Umgebung des MCP-Clients (Betreiber), Windows-Paketpfade aus `%LOCALAPPDATA%`, Importquelle aus der CLI. Alle vom Menschen, nicht aus Daten |
| G202 SQL-Verkettung (8) | `store/audit_query.go:83`, `store/drafts.go:358,401`, `store/index.go:85,110,115`, `store/scan.go:24`, `store/search_text.go:340` | Fehlalarm | Verkettet werden nur Konstanten (Spaltenlisten, `dateExpr`), Ganzzahlen (`LIMIT %d`, IDs per `strconv.FormatInt`) oder ein Tabellenname nach Prüfung gegen die feste Liste `ScanTables`; Werte laufen als Parameter |
| G115 Integer-Umwandlung (12) | `store/draftcopies.go:54`, `store/draftpush.go:182` (2), `:232`, `store/drafts.go:231`, `store/messages.go:105`, `store/mirror.go:134` (2× uint32), `:182`, `store/skipped.go:31,69` (2) | später | `int64` aus SQLite → `uint32` (UID, UIDVALIDITY, UIDNEXT). Geschrieben wurden die Werte als `uint32`, im Betrieb kein Überlauf; eine beschädigte Datenbank würde aber still abgeschnitten statt gemeldet. Grund für später: eine geprüfte Umwandlung an einer Stelle im Store (Fehler statt Abschneiden) ist ein eigener kleiner Umbau mit Test, kein Lint-Nachtrag |
| G115 (1) | `drafts/remote.go:83` | später | `uint32(d.RemoteUID)` hinter `RemoteUID <= 0`; ein Wert über 2³²−1 kommt nur aus einer beschädigten Datenbank. Mit dem Store-Umbau oben |
| G115 (2) | `store/mirror.go:134` (`uint64(hm)`), `store/mirror.go:592` (`int64(v)`) | Fehlalarm | HIGHESTMODSEQ ist nach RFC 7162 höchstens 63 Bit; selbst ein größerer Wert übersteht den Hin- und Rückweg `uint64`→`int64`→`uint64` bitgleich |
| G115 (1) | `blob/blob.go:236` | Fehlalarm | `MaxSize` ist positiv (0 wird auf `DefaultMaxSize` gesetzt, 256 MiB) |
| G115 (1) | `imapx/imapx.go:394` | Fehlalarm | `why` ist ein kleiner Grund-Code des Watchdogs |
| G204 Unterprozess aus Variable (2) | `extract/extract.go:262`, `hook/hook.go:329` | Fehlalarm | Gewollt: Kindprozess der Textextraktion (eigenes Programm aus `os.Executable` für DOCX, das vom Betreiber eingestellte PDF-Werkzeug) und der Prüf-Hook des Betreibers aus der Policy (als Liste, kein Shell-Aufruf; [sicherheit.md](../sicherheit.md)) |
| G302 Dateirechte (1) | `hook/proc_unix.go:43` | Fehlalarm | `0o700` auf ein **Verzeichnis** (braucht das x-Bit); nur für den Nutzer |
| G101 Zugangsdaten im Code (1) | `store/datadir.go:53` | Fehlalarm | Name eines Meta-Schlüssels (`credential_refs_left`), kein Geheimnis |

### staticcheck (17)

| Regel | Ort | Einstufung | Begründung |
|---|---|---|---|
| ST1018 unsichtbare Zeichen in Zeichenkette (3) | `drafts/sentmatch_test.go:25` (U+200B), `extract/extract_test.go:233`, `ops/review23_test.go:103` (U+202E) | echt, behoben | wie G116: jetzt als Escape geschrieben |
| SA4005 wirkungslose Zuweisung (1) | `importer/rc7_test.go:117` | echt, offen | `s.swap = nil` auf einem Wertempfänger wirkt nicht; der Austausch der Datei läuft bei jedem Aufruf statt einmal. Die Behebung (Zeigerempfänger) ändert den Ablauf des Tests und braucht einen Blick auf die Absicht (rc7); nicht risikolos genug für diesen Teil |
| SA1019 veraltete API (1) | `cli/canary_test.go:344` | echt, offen | `runtime.GOROOT()` ist seit Go 1.24 veraltet (Rückfall, wenn `go` nicht im `PATH` ist). Ersatz `go env GOROOT` braucht selbst `go`; der Rückfall kann entfallen. Klein, aber eigener Commit |
| SA4006 Wert nie benutzt (1) | `store/mirror_test.go:61` | später | `mid` aus dem ersten Aufruf wird überschrieben; reiner Teststil, ohne Wirkung. Gleiche Stelle wie ineffassign |
| QF1001 De Morgan (3) | `blob/blob.go:94`, `drafts/detect.go:214`, `mimeparse/corpus_test.go:93` | später | Stilvorschlag; die jetzige Form liest sich als „nicht (gültiges Zeichen)“ und ist so gewollt. Beim nächsten Anfassen der Stelle |
| QF1008 eingebettetes Feld im Selektor (5) | `cli/sync_test.go:24`, `imapx/mailbox_test.go:194`, `importer/overlap_test.go:33`, `mailsync/mailsync_test.go:62`, `provider/imap/append_test.go:206` | später | `l.Reader.Len()` → `l.Len()`; Stil, nur Tests |
| QF1011, ST1023 Typ in Deklaration (2) | `imapx/imapx.go:292` (beide Regeln an derselben Stelle) | Fehlalarm | `var conn net.Conn = raw` ist gewollt: `conn` wird danach mit `*tls.Conn` überschrieben, der Schnittstellentyp muss feststehen |
| S1011 Schleife statt `append(…...)` (1) | `mimeparse/parse.go:444` | später | Stil; beim nächsten Anfassen des Parsers |

### unused (5)

| Ort | Einstufung | Begründung |
|---|---|---|
| `ops/ops.go:69` (`deniedErr`), `ops/ops.go:89` (`Call.progress`) | echt, offen | toter Code im Register; prüfen, ob für S3 vorgesehen (Fortschritt), sonst entfernen. Eigener Commit, weil dafür die Absicht hinter `Call.progress` zu klären ist |
| `sanitize/html.go:99` (`linkStart`), `sanitize/html.go:115` (`text`) | echt, offen | ungenutzte Felder im HTML-Bereiniger; risikolos zu entfernen, aber eigener Commit am Bereiniger mit dem Korpus-Test |
| `cli/canary_test.go:314` (`mcpOut`) | später | ungenutztes Feld im Kanarientest; vermutlich für den MCP-Teil vorgesehen, beim nächsten Anfassen des Tests klären |

### gocritic (3)

| Ort | Einstufung | Begründung |
|---|---|---|
| `extract/child.go:46`, `magic/check.go:98`, `sanitize/html.go:202` (`singleCaseSwitch`) | später | Stil (`switch` mit einem Fall); die Stellen sind als Erweiterungspunkte geschrieben. Vor der Pflicht-Schaltung entscheiden: umschreiben oder die Prüfung in `.golangci.yml` abschalten |

### ineffassign (1)

| Ort | Einstufung | Begründung |
|---|---|---|
| `store/mirror_test.go:61` (`mid`) | später | dieselbe Stelle wie SA4006 |

## Nächste Schritte

1. Offene echte Befunde (6): SA4005 `rc7_test.go`, SA1019 `canary_test.go`, unused in `ops` (2) und `sanitize` (2 Felder) als eigene kleine Commits.
2. Fehlalarme ohne Ausschlussregel (G304, G703, G202, G204, G302, G101, G115 Einzelfälle, watch.go, imapx.go:292) je Stelle mit `//nolint:<linter> // Grund` markieren, damit der Bericht leer wird.
3. „Später“ (G115 im Store, Stilfragen): Store-Umbau mit Test; Stil beim nächsten Anfassen oder Prüfung abschalten.
4. Danach den CI-Job `lint` zur Pflicht machen (`continue-on-error` entfernen; eigener Commit, Roadmap).

## Nachlauf T1c (27.10.2026, nach Übernahme aller T1c-Commits)

Derselbe Aufruf über den ganzen Zweig (A3, A5.2 bis A5.5, `doctor --blobs`, kleine Punkte): **80 Befunde**, davon 78 wie oben (zwei Zeilennummern verschoben: `secretscan/secretscan.go:94` steht jetzt in Zeile 101, `store/messages.go:105` unverändert) und **2 neue**:

| Befund | Stelle | Einstufung | Begründung |
|---|---|---|---|
| gocritic offBy1 | `cli/a54_test.go:160` | echt, behoben | `out[strings.Index(out, "blobs:"):]` liefe bei fehlendem Abschnitt mit -1 in einen Panic; jetzt `strings.Cut` (Commit „test: avoid an index of -1 in the doctor --blobs test“) |
| G404 schwacher Zufall | `cli/doctor_blobs.go:199` | Fehlalarm | `math/rand/v2` mischt nur die Stichprobe von `doctor --blobs`; kein Sicherheitszweck (kein Schlüssel, kein Token). Vor der Pflicht-Schaltung `//nolint:gosec // Grund` |

Der neue Code in `internal/prompt` (Windows-Leser) und `internal/cli/probe_cmd.go` ergibt keinen Befund. Danach: **79 offen** (6 echt wie oben, Rest Fehlalarm oder später).

