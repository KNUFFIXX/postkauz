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
3. **V13:** Nur in Sichten und sonst nirgends → `ambiguous_source` (statt aus einer Sicht zu verschieben, was in Gmail nur ein Label entfernt); mit `--from` bleibt jede Sicht Quelle. Die Spalte kommt in Migration 0020, weil 0020 noch nicht auf `main` liegt (Auftrag). **Folge:** Ein Datenverzeichnis, das ein Build von `0.3.0-alpha.4` schon auf Schema 20 gebracht hat, kennt die Spalte nicht; es braucht ein frisches `-DataDir` (D7a beginnt Teil A ohnehin frisch; `-AccountDataDir` ist eine Kopie und wird neu kopiert).
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

## 5. Nachtrag: `make race` und Build

(siehe unten, nach den Läufen ergänzt)
