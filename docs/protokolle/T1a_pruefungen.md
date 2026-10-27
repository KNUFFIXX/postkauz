# T1a — Prüfläufe (27.10.2026)

Umgebung: Cloud-Container Linux, 4 Kerne, `go version go1.27.3 linux/amd64` (nach der Ausnahme in `CLAUDE.md` über `GOTOOLCHAIN=go1.27.3` geholt, danach `GOTOOLCHAIN=local`). Zweig `claude/intelligent-fermat-4gmewu`, Ausgangsstand `main` `1c9847a`. Go-Code für alle Läufe außer `make check` vor den einzelnen Commits: Stand `bc7bb93` (danach nur Doku, Protokoll und der standardmäßig übersprungene Messtest).

| Befehl | Ergebnis | Laufzeit |
|---|---|---|
| `make check` | vor jedem Commit exit 0 (gofmt, vet, alle Pakete, guard, pii-check); Basis vor Beginn ebenfalls exit 0 | je rund 3–4 min |
| `make race` | exit 0: 29 Pakete ok, kein Datenrennen (längste: importer 843 s, ops 785 s, mailsync 691 s, cli 649 s) | 991 s |
| `make integration` | 8 Integrationstests mit 24 Teilfällen PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14 (Sync, IDLE, DraftPush, DraftPushRefusals, DraftPushWatchAndOffline, SentDetection, CapabilitiesAndRoles, WrongPassword); **1 FAIL vorbestehend:** `TestIntegrationImportOverlapDovecot` (siehe unten) | rund 4 min |
| `make vuln` | **nicht gelaufen:** Die Netzwerk-Richtlinie dieser Cloud-Umgebung weist `vuln.go.dev` ab (Proxy 403). Nachholen in der Prüfsitzung bzw. im CI-Job `vuln` (läuft bei Pull Requests und auf `main`). | – |
| `make cross` | exit 0: linux/amd64 26,4 MB, windows/amd64 26,3 MB, darwin/arm64 24,9 MB | < 1 min |

**Docker:** Der Daemon wurde von Hand gestartet (`dockerd`). Docker Hub lieferte beim Pull `429 Too Many Requests`; die drei Images kamen unverändert (gleiche Tags) über den öffentlichen Spiegel `mirror.gcr.io` und wurden lokal auf die Namen aus `deploy/testmail/docker-compose.yml` getaggt.

**Vorbestehender Fehlschlag, nicht T1a:** `TestIntegrationImportOverlapDovecot` meldet `new 307, existing 171, skipped 3` statt `305/173/3`. Derselbe Test am Ausgangsstand `1c9847a` (eigener Worktree, gleiche Testserver) liefert dasselbe Ergebnis. Bei S2.9 (23.10.2026) war er grün (`docs/protokolle/S29_pruefungen.txt`); die Abweichung stammt also aus den Import-Änderungen rc6 bis rc8 (zwei Fassungen mit gleicher Message-ID werden getrennt). Zwei Nachrichten des Thunderbird-Exports gelten jetzt als neue Fassung statt als vorhanden. Ob die Erwartung des Tests oder der Import angepasst werden muss, entscheidet die Prüfsitzung (Vorschlag: Ursache an den zwei Nachrichten prüfen; liegt sie in der Umkodierung durch Dovecot, die Erwartung des Tests anpassen).

## Laufzeit Migration 0017 (Rahmen 4)

`POSTKAUZ_MEASURE_MIGRATION=1 go test ./internal/store -run TestMeasureMigration0017 -v`: synthetische Datenbank mit Schema 16, 50.000 Nachrichten, 80.000 Ordnerkopien (alle in „Alle Nachrichten“, 30.000 zusätzlich in INBOX), 20.000 Audit-Zeilen, Datei 28 MB.

| Lauf | Dauer (Snapshot `VACUUM INTO` + Vorprüfung + Tabellenumbau) |
|---|---|
| 1 | 426 ms |
| 2 | 403 ms |
| 3 | 454 ms |

Zeilen vorher und nachher gleich (Test prüft das). Die echte Datenbank des Betreibers (rund 1,4 GB, mehr als 50.000 Nachrichten) ist vor allem wegen Volltextindex und Texten größer; der Tabellenumbau wächst mit der Zahl der Ordnerkopien, der Snapshot mit der Dateigröße. Die Dauer an der Kopie der echten Datenbank misst die Prüfsitzung in Abnahme T1, Schritt 2.

## Nachbesserung nach zwei Gegenprüfungen (27.10.2026, Build 0.3.0-alpha.1b)

Gegenprüfungen des Stands `6683edc`: unabhängiger Claude-Prüfer **PASS mit Hinweisen**, Fremdmodell Codex **FAIL vor dem Merge**. Behoben auf demselben Zweig, je Punkt ein Commit mit Test:

| Punkt | Fund | Behebung | Commit | Test |
|---|---|---|---|---|
| W1 | Schutz gegen zwei Programmversionen lückenhaft: `AccountLocks` nur für vorhandene Sperrdateien, Schemaprüfung und Sperre zeitlich getrennt, MCP-Server ohne Sperre, `Migrate` las die Version vor dem Sperren | Prozesssperre `<datenbank>.lock`: geteilt in jedem Prozess ab `store.Open` bis `Close`, Schema erst danach geprüft; `Migrate` exklusiv, Version unter der Sperre neu gelesen, Kontosperren weiter für rc8; neuer Code `database_busy`; README „Updating“, `docs/abnahme_T1.md` Schritt 0 | `9f8dc64` | `TestTryLockShared`, `TestMigrateWaitsForProcessLock`, `TestMigrateRefusedByOtherProcess` (echter Kindprozess) |
| W2 | Speichergrenze empfangener UID-Sets hing nur am servergesteuerten `EXISTS` (`EXISTS 4294967294`, `1:4294967294` → rund 16 GiB) | Grenze `min(EXISTS, 10 Mio.)`, Bereiche erst gezählt, dann nur ab der gesuchten UID aufgelöst | `bb1b19c` | `TestReceivedUIDSetsAreBounded` (Fälle `huge exists`, `above own limit`), `TestReceivedUIDSetsMemory` |
| W3 | `TestIntegrationImportOverlapDovecot` rot (`new 307, existing 171` statt `305/173`), schon am Ausgangsstand | Ursache an den zwei Nachrichten (`pk.00366…`, `pk.00378…`, Kategorie „NUL-Bytes“): Dovecot speichert NUL als 0x80, gleiche Größe, andere Bytes; der Thunderbird-Export stammt aus dem Korpus. Seit rc6 legt der Import eine Nachricht mit bekannter Message-ID und anderem Inhalt als zweite Fassung an (Gmail-Regel) — hier richtig. Testerwartung angepasst, Import unverändert | `e0cb1f7` | der Integrationstest selbst (prüft zusätzlich: genau diese Nachrichten gibt es zweimal) |
| K1 | `PRAGMA foreign_key_check` über die ganze Datenbank, Meldung ohne Tabelle | nur `message_mailboxes` (`store.fkCheckTables`), Meldung mit Tabelle, Zeile, Zieltabelle | `a9f4485` | `TestMigration0017ForeignKeyCheckScope` |
| K2 | Mehr UIDs als `EXISTS` (Mail zwischen SELECT und SEARCH) übersprang den Ordner | neue UIDs geholt, nur Löschabgleich ausgelassen (`ChangeSet.PresentIncomplete`), Zähler je Konto in `meta`, `doctor` zeigt ihn | `548990e` | `TestLongerUIDListFetchesNewSkipsDeletions`, `TestDoctorShowsIncompleteAnswers` |
| K3 | Köpfe über dem Budget wurden ganz übertragen, verworfen und neu angefordert | FETCH bricht beim ersten Kopf über dem Budget samt Verbindung ab (`imapx.ErrHeaderBudget`), Rest auf neuer Verbindung | `43038cb` | `TestFetchMetaAbortsAtBudget` (200 × 900 KB: Server schreibt rund 17 MB statt 180 MB), `TestFetchHeadersOverBudget` |
| K4 | „Symlinks in `attach_dir` werden jetzt abgelehnt“ undokumentiert | geprüft: Symlinks mit Ziel in `attach_dir` werden weiter angenommen, nach außen abgelehnt; nur reguläre Dateien; so in `docs/sicherheit.md` und `docs/cli.md` | `801eb07` | `TestAttachDirSymlinksAndSpecialFiles` |
| K5 | Sortierung per int-Differenz | `cmp.Compare` | `deacdac` | `TestFetchMetaSortsLargeUIDs` (scheitert mit der alten Sortierung unter `GOARCH=386`) |
| K6 | Negativtests 0017 mit vorhandenem Paar (6,7) scheiterten auch ohne CHECK/UNIQUE | je Test ein frisches Paar, Fehlertext geprüft, Gegenprobe | `e58a488` | `TestMigration0017UIDPositive` |
| — | Audit-Plan lädt das ganze Protokoll (`internal/ops/auditexport.go`) | nur als offener Punkt in die Roadmap | — | — |

**Prüfläufe nach der Nachbesserung** (Go-Code Stand `548990e`; danach nur Doku): Umgebung wie oben (Go 1.27.3, Testserver über `mirror.gcr.io`, `dockerd` von Hand).

| Befehl | Ergebnis | Laufzeit |
|---|---|---|
| `make check` | vor jedem Commit exit 0 | je rund 3–4 min |
| `make race` | exit 0: 29 Pakete ok, kein Datenrennen (längste: importer 892 s, ops 805 s, mailsync 722 s, cli 674 s) | 1035 s |
| `make integration` | exit 0: 9 Integrationstests mit 24 Teilfällen PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14, **darunter `TestIntegrationImportOverlapDovecot`** (`new 307, existing 171, skipped 3`, davon 2 NUL-Nachrichten als zweite Fassung, W3); kein FAIL, kein SKIP | 160 s |
| `make vuln` | **nicht gelaufen:** Proxy der Cloud-Umgebung weist `vuln.go.dev` ab (`Forbidden`); nachholen im CI-Job `vuln` bzw. in der Prüfsitzung | – |
| `make cross` | exit 0: linux/amd64 26,5 MB, windows/amd64 26,3 MB, darwin/arm64 24,9 MB | < 1 min |
| `make release VERSION=0.3.0-alpha.1b` | `dist/` mit Programmen für linux/amd64, windows/amd64, darwin/arm64 und `SHA256SUMS` (Werte in der Abschlussmeldung; nur für Tests der Prüfsitzung) | < 1 min |
