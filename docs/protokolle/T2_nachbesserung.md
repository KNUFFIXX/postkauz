# T2 — Nachbesserung und Merge mit `main` (28.10.2026)

Auftrag: [auftraege/A03_T2_nachbesserung.md](../../auftraege/A03_T2_nachbesserung.md) (Prüfsitzung DRACO-PK09). Arbeitszweig `claude/jolly-franklin-sa5rse`, Ausgangsstand `54f6877` (= `da5386b` plus Auftrag), dazu `main` `c44962e`. Unbeaufsichtigte Cloud-Sitzung; Detailfragen mit Default entschieden (Rahmen 11), keine Stopp-Regel ausgelöst (kein Befund widerspricht einem ADR oder einem Entscheid aus A03 Abschnitt 1).

## 1. Commits (Reihenfolge des Auftrags)

`main` wurde zuerst zusammengeführt (Merge, kein Rebase), damit die README-Korrekturen aus V15 nicht später gegen `main` kollidieren.

| Punkt | Commit | Inhalt | Regressionstest (scheitert vorher, besteht nachher) |
|---|---|---|---|
| Merge | `334baa1` | `main` `c44962e` (nur README-Stand); Konflikt nur in der deutschen Statuszeile des README: Wortlaut von `main` plus T2-Zusammenfassung | `make check` |
| V1 | `624e5ab` | Runner führt `move`/`trash` nur aus, wenn der Zielordner unter seiner ID noch Pfad (`Params.TargetMailbox`) und Rolle (`Params.TargetRole`, falls gesetzt) hat und ein `move` nicht im Papierkorb endet; sonst alle offenen Ziele `stale`, Fehler `target_changed`, kein Kommando | `TestRunnerRefusesReusedTargetID` (Archiv gelöscht, ID nach zweitem Abgleich an „Papierkorb“ vergeben), `TestRunnerRefusesTargetRoleChange` (Archiv wird `trash`; mit und ohne gespeicherte Rolle). Gegenprobe ohne Fix: beide rot (`MOVE INBOX -> Archiv` bzw. in den Papierkorb) |
| V22 | `cc89e52`, Nachtrag im Abschluss-Commit | `Register-SyncWatch.ps1` beobachtet nach „starting:“ 15 s lang den neuen Teil des Logs (`ended with exit code` → Warnung) und verlangt einen laufenden `postkauz.exe` mit diesem Pfad; sonst Warnung mit den letzten 10 Logzeilen, Exit 1. Nachtrag: Loggröße erst **nach** dem Stoppen einer schon laufenden Aufgabe messen (sonst zählte deren Endzeile als Fehlstart); Lesen mit `FileShare ReadWrite, Delete` und `try/catch` (Rotation des Wrappers) | Lesetest (kein PowerShell im Container, Download von GitHub gesperrt): Ablauf, StrictMode 2.0, `ErrorActionPreference Stop`, Freigabemodi gegen `AppendAllText` des Wrappers geprüft; nur ASCII außer BOM (`make guard`) |
| V2 | `155bba9` | D7a Schritt B0.3a: `action list --account <konto> --state queued --state running --json`; offene Aktionen → `SKIP B` mit Nummern und Grund | Python-Nachbau mit dem Linux-Programm: frische Kopie → `OK B0.3a`; nach Einfügen einer `queued`-Aktion → `SKIP B offene Aktionen aus einem frueheren Lauf: 1 (mark, queued)` |
| V9 | `8b549c8` | `PlanMove`: liegt die Nachricht im Ziel und außerhalb von Ziel und „Alle Nachrichten“ nirgends → `skipped` (vor „einzige Kopie“) | `TestPlanMoveSkipsMessageAlreadyInTarget`; ohne Fix: Ziel war die Kopie in „[Gmail]/Alle Nachrichten“ |
| V13 | `6e9ed90` | `mailboxes.virtual_view` (Migration 0020, Abschnitt 7; Bestand aus den Gmail-Namen), gesetzt vom Ordnerabgleich aus `\Flagged`/`\Important` bzw. „Starred“/„Markiert“/„Important“/„Wichtig“ unter `[Gmail]`/`[Google Mail]` (`imap.VirtualView`); `PlanMove` behandelt Sichten wie `all` | `TestPlanMoveIgnoresVirtualViews` (INBOX + Alle + Wichtig + Markiert, `trash` ohne `from` → INBOX; ohne Fix `ambiguous_source`), `TestVirtualView`, `TestSyncMailboxesVirtualView`, `TestMigration0020` (Bestand). **D7a B6 nachgebaut** (Abschnitt 3) |
| V7 | `17dcad6` | `moveLink` legt die neue Verknüpfung mit `content_loaded = 0` an, wenn im selben MOVE mehr als eine UID aus dem Quellordner kam | `TestMoveOfSeveralUIDsNeedsHeaderCheckLater` (zwei UIDs → beide neuen Kopien 0, nächste Aktion holt zuerst den Kopf; eine UID → bleibt 1); ohne Fix rot |
| V10 | `e656ad1` | `watchAccount`: Anmeldefehler (`ErrAuth`, OAuth2-Refresh) und fehlende Zugangsdaten → erneuter Versuch nach `allRetryMax` (30 min) mit Zeile `retrying in 30m`; Vermerk `accounts.status = error`, `last_error` „sync --watch --all: <Art> at <Zeit>; retrying every 30m“ (`store.SetAccountStatus`), gelöscht von der ersten erfolgreichen Runde; `doctor` zeigt `last error:` und warnt | `TestSyncWatchAllRetriesAfterAuthFailure` (ein Konto erst abgelehnt, eins ohne Passwort; beide laufen nach dem Warten, Status danach `ok`); ohne Fix rot (Konto endete). `TestDoctorShowsAuthRetryOfWatchAll`; angepasst `TestSyncWatchAllAccounts`, `TestSyncWatchAllInputAndEnd` |
| V20 + V15 | `83c40cf` | Scope `mail:mark` (nur `operator`, `admin`); `messages.mark` und der Runner (Herkunftsprüfung) verlangen ihn; MCP-`instructions` trennen Tag und Markieren; `mcp --help`, `sync --help`, README berichtigt; D7a A3.1/A3.2/A3.10/A3.11 und `-ExpectVersion` angepasst | `TestToolListFollowsScopes` (assistant ohne `mail_mark`, triage ohne `mail_mark`, admin mit), `TestTriageRoleCanDoNothingElse`, `TestMessagesMarkQueues` (assistant `missing_scope`), `TestInstructionsFollowScopes`, `TestMCPAssistantSession` (20 Tools), `TestMCPLongTextFollowsT2` (Hilfetexte und README) |
| V4 | `7ba5479` | `store.SaveTargetResults` je Quellordner direkt nach dessen Kommando (gemeinsame Routine `applyResults` mit `FinishAction`) | `TestRunnerSavesResultsPerMailbox` (INBOX ok, Archiv Verbindungsabbruch → Ergebnis von INBOX gespeichert, Spiegel umgetragen, Aktion `running`; zweiter Lauf nur `MOVE Archiv -> Projekte`); ohne Fix rot |
| V6 | `692a492` | `sync`: Zeile `actions: N done, M failed, K stale (A actions; see action list)`; `sync --watch`: dieselbe Zeile mit Zeit je Takt mit Aktionen | `TestSyncShowsActionCounts`; sichtbar im Probelauf B6 |

**Hinweis zur Nummerierung:** Die Commit-Nachricht von `17dcad6` nennt den Fund versehentlich „V5“; gemeint ist **V7** des Auftrags (Zuordnung nach MOVE mehrerer UIDs). V5 des Auftrags ist die Abweichung des Offline-Teils von D7a (Abschnitt 4). Kommentare und Doku sind berichtigt; die schon gepushte Commit-Nachricht bleibt (kein Umschreiben gepushter Geschichte).

## 2. Festlegungen (Einspruch möglich, Rahmen 11)

Übertragen nach roadmap.md, Entschiedene Frage 59:

1. **V20:** `mail:mark` nur in `operator`/`admin`. `assistant` und `triage` markieren nicht am Server; lokale Tags bleiben `mail:tag`. Keywords (`[tags] sync_keywords = true`) laufen weiter unter `mail:tag`, weil der Mensch sie in `config.toml` ausdrücklich einschaltet. Die Scopes stehen je Schlüssel in der Datenbank: Ältere `operator`-/`admin`-Schlüssel bekommen `mail:mark` nicht nachträglich (neu anlegen; Hinweis in `docs/betrieb.md` 1.7a). Der Mensch an der CLI (Rolle `admin`) markiert wie bisher.
2. **V1:** Ohne gespeicherte Rolle (Aktion per Pfad mit Ordner ohne Rolle) zählt nur der Pfad; ein `move` endet aber nie im Papierkorb. Fehlt `Params.TargetMailbox` ganz, gilt das Ziel als geändert (fail closed; alle Aktionen aus dem Code haben es).
3. **V13:** Nur in Sichten und sonst nirgends → `ambiguous_source` (statt aus einer Sicht zu verschieben, was in Gmail nur ein Label entfernt); mit `--from` bleibt jede Sicht Quelle. ~~Die Spalte kommt in Migration 0020~~ **Berichtigt in der Nachprüfung (Abschnitt 7):** Die Spalte steht in **Migration 0021**; 0020 ist wieder bytegleich mit `da5386b`. Der Nachtrag in 0020 war ein Fehler: Der Läufer vergleicht nur Nummern, eine von `0.3.0-alpha.4` migrierte Datenbank hätte die Spalte nie bekommen, und jeder Sync wäre gescheitert.
4. **V10:** Ein Prozess, in dem alle Konten an der Anmeldung scheitern, endet nicht mehr von selbst; Exit 6 kommt nur noch aus Läufen mit einem Konto. Der Wrapper wartet bei Exit 6 weiter 30 min (unverändert). Endet `--all` mit Strg+C, während ein Konto auf den nächsten Anmeldeversuch wartet, nennt der Bericht dieses Kontos den Anmeldefehler.
5. **V7:** `content_loaded = 0` nur bei mehr als einer UID je Quellordner und MOVE.
6. **V6:** Die Zahlen zählen Ziele (Serverkopien), die Zahl der Aktionen steht in Klammern.
7. **Build:** `0.3.0-alpha.5` (Auftrag); T3 nimmt damit `0.3.0-alpha.6` (in `A03_teile.md` T3 nachgetragen).

## 3. Prüfläufe

- **`make check`** vor jedem Commit grün (gofmt, vet, `go test ./...` ohne `-short`, Guard, PII-Gate; rund 4:40 min je Lauf). Nie parallel zu `make race`.
- **`make integration`** gegen **Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail 2.1.14** (Images über `mirror.gcr.io`, dann auf die Namen aus `docker-compose.yml` umbenannt; `make testmail-up testmail-setup integration`): **41 PASS, 0 SKIP, 0 FAIL** in 3:33 min, u. a. `TestIntegrationMailboxActions` (dovecot 0,98 s, stalwart 0,58 s, greenmail 3,51 s), `TestIntegrationWatchActionsAndSyncNow` (alle drei), `TestIntegrationDraftPush*`, `TestIntegrationSync`, `TestIntegrationIdle`, `TestIntegrationSentDetection`, `TestIntegrationImportOverlapDovecot`, `TestIntegrationCapabilitiesAndRoles`, `TestIntegrationWrongPassword`.
- **Probelauf D7a B6 (V13)** gegen den Dovecot-Container mit dem Linux-Programm: Testmail (Message-ID `b6probe@example.org`) per APPEND in INBOX, „[Gmail]/Alle Nachrichten“ (Rolle `all` aus dem Namen) und „[Gmail]/Wichtig“ (Rolle `custom`, virtuelle Sicht); `sync`, `trash 1 --yes` **ohne** `--from` → Exit 0, Aktion 1 freigegeben; `sync` → `actions: 1 done, 0 failed, 0 stale (1 actions; see action list)`, Server: INBOX 0, Trash 1, Alle 1, Wichtig 1; Spiegel: Trash, Alle, Wichtig. Gegenprobe mit dem Stand vor V13 (`8b549c8`): `trash 1 --yes` → Exit 3 `denied (ambiguous_source)` — genau der Fund.
- **Migration 0020** (mit `virtual_view`): 0,28 s an 50.000 Nachrichten, 80.000 Ordnerkopien, 20.000 Audit-Zeilen (`POSTKAUZ_MEASURE_MIGRATION=1`, `TestMeasureMigration0020`, einschließlich Snapshot).
- **`make race`:** siehe Abschnitt 5 (Nachtrag).
- **`make cross`:** grün (linux/amd64, windows/amd64, darwin/arm64).
- **`make vuln`:** in dieser Cloud-Umgebung gesperrt (`vuln.go.dev` → 403 vom Proxy). `go.mod`/`go.sum` sind seit `da5386b` unverändert; die Prüfsitzung meldete dort keinen Fund. Nachholen in der Prüfsitzung bzw. im CI-Job.
- **PowerShell:** nur Lesetest (kein `pwsh`, GitHub gesperrt). Geändert: `Register-SyncWatch.ps1` (V22), `abnahme_D7a.ps1` (V2, V20, Version). Python-Nachbau für V2 (oben). `abnahme_D7a_mcp.py` (A3.10/A3.11) läuft in Teil A unter Windows.

## 4. Nach T3 verschoben

V3, V8, V11, V14, V16, V17, V18, V19, V21 mit Kurzbeschreibung in [A03_teile.md](../../auftraege/A03_teile.md) T3 und in der Roadmap (Stand T2, „Nachbesserung“). V5 der Prüfsitzung (Offline-Teil von D7a weist D7.1/2 nicht nach) steht als Abweichung in [abnahme_D7a.md](../abnahme_D7a.md).

## 5. `make race` und Build (Stand `fd70f19`, überholt durch Abschnitt 7)

- **`make race`** (allein, ohne parallelen `make check`) an `692a492` plus den Doku-Änderungen: **alle Pakete grün** in 15:12 min; längste Pakete `internal/cli` 812 s, `internal/importer` 794 s, `internal/mailsync` 688 s, `internal/ops` 476 s (alle unter der Grenze von 30 min je Paket).
- **Build:** `make release VERSION=0.3.0-alpha.5` an `ab94add` (Go 1.27.3, `CGO_ENABLED=0`, `-trimpath`); `postkauz-linux-amd64 version` → `postkauz 0.3.0-alpha.5`. SHA-256:
  - `postkauz-windows-amd64.exe` `edb37daf2751fc2618ffa2291841c681a023d21d6278b10dff6317e6f7b01202`
  - `postkauz-linux-amd64` `acdb229b632160f1ed0ab23c2f7cbcb5c9fa7150a624affeee022470798216c8`
  - `postkauz-darwin-arm64` `6d4a2920bc4baf29e14edf2f6bf918c07a1cdce250439b2d00aa23a4ca602a48`
  - `Register-SyncWatch.ps1` `0dafa1012e7aad6d77c5a0a3c1f98d29cfc8de687b16e630565ed55a0cb3b8dc`, `Run-SyncWatch.ps1` `aa9653d09f718067b058dc54a2e041168a7f1d6618489a36ae94e8faab1d36c4`

  `dist/` liegt nur im Container (nicht im Repository); die Prüfsitzung baut nach. Nach dem Build-Commit ändern sich nur dieses Protokoll und die Roadmap (kein Code). Bytegleich ist ein Nachbau nur an `ab94add` selbst: Go stempelt den Commit in das Programm (`vcs.revision`), `dist/VERSION` nennt ihn.
- **Rauchtest mit dem Release-Programm:** Probelauf B6 (Abschnitt 3) noch einmal mit `dist/postkauz-linux-amd64`: `trash 1 --yes` Exit 0, `actions: 1 done, 0 failed, 0 stale`, Server INBOX 0 / Trash 1 / Alle 1 / Wichtig 1.

## 6. Offen

1. Abnahme D7a mit dem neu gebauten `0.3.0-alpha.5` (Abschnitt 7) durch die Prüfsitzung am Büro-PC (frisches `-DataDir`/`-WorkDir` wie immer; eine schon von `alpha.4` migrierte Kopie bekommt die Spalte jetzt über 0021).
2. `make vuln` (hier gesperrt).
3. PowerShell-Skripte unter Windows: `Register-SyncWatch.ps1` (15-s-Prüfung, auch mit absichtlich falschem `-DataDir` → Warnung und Exit 1), `abnahme_D7a.ps1` (B0.3a).
4. Gmail-Verhalten bei MOVE und „Label entfernen“ nur in D7a messbar (unverändert).

## 7. Nachtrag: Nachprüfung der Prüfsitzung zu `fd70f19`

Die Prüfsitzung bestätigte alle acht Punkte und V4/V6 am Quelltext (Regressionstests scheitern ohne Fix) und fand sieben weitere Punkte vor dem Merge, davon ein Blocker. Alle behoben:

| Punkt | Commit | Inhalt | Regressionstest (scheitert vorher, besteht nachher) |
|---|---|---|---|
| 1 (Blocker) | `0272098` | `0020_actions.sql` wieder bytegleich mit `da5386b`; `mailboxes.virtual_view` samt Kennzeichnung des Bestands als **Migration 0021** (Schema 21) | `TestSchema20OfAlpha4Migrates`: Schema-20-Datenbank, erzeugt mit dem Code von `da5386b` (`internal/store/testdata/t2_schema20`, Generator `gen/main.go` mit Anleitung), migriert; danach laufen `ServerCopies` (Sicht gekennzeichnet) und `SyncMailboxes`. Gegenprobe mit 0020 von `fd70f19`: `no such column: b.virtual_view` |
| 2 | `ad5345c` | Warten Aktionen, gleicht der Job vorher die Ordnerliste ab (`runActions`, auch im Jobs-Takt von `sync --watch`); scheitert der Abgleich, warten die Aktionen | `TestActionsSeeServerRoleChangeBeforeMove` (Server im Prozess: „Archive“ verliert die Rolle an einen neuen Ordner „Archiv“ ohne vorherigen Lauf → kein MOVE, `stale`, `target_changed`); ohne Fix verschoben |
| 3 | `160228a` | Erste erfolgreiche Runde nach dem Start (und nach jedem Vermerk) setzt Status `error` des Kontos auf `ok` (`store.ClearAccountError`, nur bei `error`) | `TestSyncWatchAllStatusAfterRestartAndStop` (Vermerk aus einem früheren Prozess verschwindet; Konto mit Status `new` bleibt `new`) |
| 4 | `160228a` | Konto, das unter `--all` an einem anderen Fehler endet: `last_error` `sync --watch --all: stopped (<Art>) at <Zeit>; it runs again after a restart of the task`, `doctor` warnt | derselbe Test (Konto `beta`), `TestDoctorShowsAuthRetryOfWatchAll` (Fall „stopped“) |
| 5 | `b2633a1` | `Register-SyncWatch.ps1` wertet nur den neuen Teil des Logs aus, rotationsfest: Hat sich die erste Zeile des Logs geändert, zählen der Rest von `sync-watch.1.log` ab der alten Größe und die ganze neue Datei (Erstelldatum taugt nicht: NTFS-Tunneling); alte `starting:`-Zeilen zählen nicht mehr | Lesetest; dazu Python-Nachbau der drei Funktionen (Scratchpad, nicht im Repository) mit sechs Fällen: ohne neue Zeilen, ohne Rotation, Rotation nach bzw. vor „starting:“ (neue Datei größer als die alte Größe), leere neue Datei, Fehlstart nach Rotation: alle OK; die alte Prüfung (Datei größer als vorher) meldete die Rotationsfälle als Fehlstart |
| 6 | `dee6df2` | `runOne` gibt bei Abbruch die schon gespeicherten Ergebnisse zurück; `Run` zählt sie, bevor es endet | `TestRunnerSavesResultsPerMailbox` um Zähler ergänzt (Abbruch: `actions 1, ok 1`; zweiter Lauf: `ok 1`); ohne Fix `ok 0` |
| 7 | `11568ef` | README „Continuous sync on Windows“, `docs/betrieb.md` 1.1 und die Hilfe von `Register-SyncWatch.ps1`/`Run-SyncWatch.ps1`: mit `--all` beendet eine abgelehnte Anmeldung das Programm nicht; Exit 6 nur bei Läufen mit einem Konto | – (Text) |
| T3 | `11568ef` | Nicht gespeicherter Quellpfad eines Ziels in `A03_teile.md` T3 und Roadmap | – |

**Festlegungen (Einspruch möglich, Rahmen 11), ergänzt in roadmap.md Nr. 59:** (g) Der Ordnerabgleich vor Aktionen läuft nur, wenn Aktionen warten (ein `LIST` je Jobs-Takt mit Arbeit, nicht alle 10 s). (h) Die erste erfolgreiche Runde löscht nur den Status `error` (gleich, wer ihn schrieb: auch ein alter Fehler von `account test` ist dann überholt); andere Status bleiben. (i) Ein „stopped“-Konto läuft erst nach einem Neustart der Aufgabe wieder; der Vermerk nennt nur die Art (`incomplete server answer`, `not supported by the server`, sonst `unexpected error, see the log`).

**Nebenbefund im Prüflauf:** Ein `make check` scheiterte einmal an `internal/drafts` `TestConcurrentWritersOnOneFile` (`database is locked (5) (SQLITE_BUSY)` nach 9,9 s; zwei Schreiber, `busy_timeout` 5 s) — unter Volllast unmittelbar nach einer Migrationsmessung. Der Test stammt aus `main` (`27920c4`), das Paket ist hier nicht geändert; allein 20 von 20 Läufen grün, der nächste volle `make check` grün. Kein Fix in diesem Zweig; Kandidat für T3 (längeres `busy_timeout` im Test oder weniger Last).

**Migrationen:** 0020 an der synthetischen Datenbank 0,38 s, **0021 0,20 s** (50.000 Nachrichten, 80.000 Ordnerkopien, 20.000 Audit-Zeilen, einschließlich Snapshot; `POSTKAUZ_MEASURE_MIGRATION=1`).

**Prüfläufe und Build nach der Nachprüfung:** siehe Abschnitt 8.

## 8. Prüfläufe und Build nach der Nachprüfung (Stand `2f5da97`)

- **`make check`** vor jedem Commit grün (einmal `internal/drafts` unter Last, siehe Abschnitt 7, Wiederholung grün). Nie parallel zu `make race`.
- **`make race`** allein an `11568ef`: alle 31 Pakete grün in 13:39 min (`internal/cli` 788 s, `internal/importer` 777 s, `internal/mailsync` 677 s, `internal/ops` 462 s).
- **`make integration`** gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14 (Images über `mirror.gcr.io`): **41 PASS, 0 SKIP, 0 FAIL** in 3:37 min.
- **Ende-zu-Ende zu Punkt 1 (Upgrade von alpha.4):** Programm aus `da5386b` gebaut; damit gegen Dovecot ein Datenverzeichnis angelegt und gesynct (INBOX und „[Gmail]/Wichtig“ mit derselben Mail; `doctor`: Schema 20). Danach mit dem neuen Stand (Entwicklungsbuild und Release-Programm): `sync` Exit 0 (Migration 0021), `trash 1 --yes` ohne `--from` Exit 0, `sync` → `actions: 1 done, 0 failed, 0 stale`, `doctor --json` Schema 21, Datenbank `ok`.
- **Probelauf D7a B6** mit dem Release-Programm wiederholt: unverändert grün (Server INBOX 0, Trash 1, Alle 1, Wichtig 1).
- **`make cross`:** grün (im eigenen Worktree, damit `dist/` mit dem Release nicht überschrieben wird).
- **`make vuln`:** weiter gesperrt (403); `go.mod`/`go.sum` unverändert seit `da5386b`.
- **Build neu:** `make release VERSION=0.3.0-alpha.5` an `2f5da97` (ersetzt den Build an `ab94add`). SHA-256:
  - `postkauz-windows-amd64.exe` `5ba7e4fa8bae2d947b63b24e0a13f96789395ac2e2c0f0c7c3cf92da9a4388a4`
  - `postkauz-linux-amd64` `218fe40ac4ac96f6b3c1362f30e00c7171d31d5dc806ffc3052e5149625f3c45`
  - `postkauz-darwin-arm64` `fb09ddafbc1072d8c97bac75a4f2808aa9190cb2e08bdc0eef263dd024d71946`
  - `Register-SyncWatch.ps1` `8a50ee74c064cc731446ed6f224d1d3ba8247d7abcea6cb9da834f2e40ec91c1`, `Run-SyncWatch.ps1` `6973c8473a8a244172530c9e926d4d2b854b2bbee05bc013bde1c700bcdf3de3`

  Bytegleich nur am Build-Commit `2f5da97` (Go stempelt den Commit ein); danach ändern sich nur Protokoll und Roadmap.

