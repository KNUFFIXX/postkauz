# T2, Arbeitspaket WP-A: Dauerbetrieb, `mail_sync_now`, Kleines aus T2 Punkt 7

Für: die Hauptsitzung von T2 (Übernahme in `docs/roadmap.md` und `CHANGELOG.md`) und die Prüfsitzung. Gebaut in einem eigenen Worktree auf Basis von `8d47497`, parallel zur Hauptsitzung (Tags, Markieren, Verschieben, Papierkorb). Nicht angefasst: `internal/ops/executor.go`, `internal/policy`, `internal/store/migrations`, `docs/roadmap.md`, `CHANGELOG.md`; keine Migration.

## Was gebaut ist

1. **`sync --watch --all`** (A03_teile T2 Punkt 6): ein Prozess beobachtet alle Konten, je Konto eine Goroutine über `crash.Go` mit eigener Kontosperre (`internal/mailsync/lock.go`) und eigenen Zugangsdaten (`Credentials.For`, Passwort oder OAuth2 wie bei `sync.run`). Belegte Sperre oder Verbindungsfehler beim Start: das Konto wartet 1 min, verdoppelt bis 30 min, und versucht es erneut; Anmeldefehler, fehlende Zugangsdaten, andere Fehler: nur dieses Konto endet (Zeile `[konto] stopped: …; the other accounts go on`, WARN im Log). Audit: ein Aufruf `sync.run` mit allen Konten als Ziel (`redactSyncRun`). Fortschritt nennt das Konto (`ops.Progress.Account`, CLI `[konto]`), am Ende je Konto eine Zeile; `--json` mit `accounts[]`. `--all` nur mit `--watch`, nicht mit `-a` oder `--mailbox` (Exit 2). Code: `internal/ops/sync_all.go`, `internal/ops/sync.go`, `internal/cli/sync_bindings.go`.
2. **Operation `sync.request`** (R1, `mail:sync`, nicht HumanOnly; Eingabe `account?`, `mailbox?`), MCP-Tool **`mail_sync_now`** (mit `dry_run`), CLI **`sync request [-a …] [--mailbox …]`**. Reiht den Job `sync` ein (Typ seit 0001 erlaubt, keine Migration): je Konto höchstens einer offen (`jobs_open`), Herkunft `key_id`/`audit_id` wie `draft_push`; eine zweite Anforderung vereinigt die Ordner (ohne Ordner = alle, mehr als 20 = alle) und setzt `requested_at` neu (`merged: true`). Ausgabe `status: queued` (Trockenlauf `would_queue`, nichts eingereiht), `job_id`, `note` mit dem Weg. Ausführung (`internal/mailsync/syncrequest.go`, `ops.SyncJobs`): `sync --watch` prüft bei jedem Jobs-Takt, prüft die Herkunft frisch aus `api_keys` (widerrufen, abgelaufen, gelöscht, ohne `mail:sync` → Job `failed` mit `origin_denied: <grund>`, Systemereignis `sync.job` denied, kein Lauf) und läuft sonst sofort (mit Ordner nur über diesen). Jeder fehlerfreie Lauf ohne gescheiterten Ordner (Dauerläufer und einmaliger `sync`) erledigt die Jobs, die er abdeckt (`SettleSyncJobs`: angefordert vor seinem Beginn, Ordner enthalten). Store: `internal/store/syncjobs.go`.
3. **T2 Punkt 7 (f):** `sync`, `sync --watch` und `import` versuchen die Kontosperre fünfmal in rund 1,6 s (`lockAttempts`, `lockRetryWait`), bevor sie `sync_running` melden.
4. **T2 Punkt 7 (g):** `account remove` sagt bei OAuth2-Konten `stored token deleted` (Ausgabefeld `secret_kind`: `password` | `token`).
5. **T2 Punkt 7 (i) und Zusatz der Prüfsitzung:** Tote Starts werden erst ab `store.DeadStartAge` = 10 min abgeschlossen (derselbe Wert wie `doctor`, `auditUnfinishedAge = store.DeadStartAge`); `import` holt die Kontosperre vor `Discover`; `sync --watch` ruft `CloseDeadStarts` vor jedem Lauf über alle Ordner außer dem ersten erneut auf (`WatchOptions.BeforeFullRound`), nie für den eigenen Start. Dazu: Ein Start mit mehreren Konten (`--all`) wird nur abgeschlossen, wenn kein anderer Prozess die Sperre eines seiner übrigen Konten hält (`otherBusy`; Sperren des eigenen Prozesses zählen nicht, `held`-Register in `lock.go`).
6. **Exit-Code 6 `ExitAuth`** für `auth` und `oauth_refresh` (vorher 1), damit `Run-SyncWatch.ps1` Anmeldefehler erkennt.
7. **Windows-Dauerbetrieb:** `deploy/windows/Register-SyncWatch.ps1` (Aufgabe „PostKauz sync watch“ bei Anmeldung, Prüfungen wie `Register-AuditExport.ps1`: doctor, Umleitung, fehlende Datenbank, dazu: kein Konto, Warnung je Konto ohne gespeichertes Passwort/Token oder mit Passwort nur aus der Umgebung; ohne Zeitgrenze, eine Instanz, `conhost.exe --headless`, `-ShowWindow`, `-NoStart`, `-Unregister`; startet und prüft Zustand `Running` und eine neue Startzeile im Log) und `deploy/windows/Run-SyncWatch.ps1` (Neustart mit Backoff 60 s → 1.800 s, zurück auf 60 s nach ≥ 30 min Laufzeit, bei Exit 6 immer 1.800 s mit Logzeile; Log `<DataDir>\logs\sync-watch.log`, Rotation 10 MB × 5 Dateien; kein Geheimnis im Skript oder Log). Beide im Release-Paket (`make release`, mit CRLF und in `SHA256SUMS`).
8. **Doku:** `docs/betrieb.md` neu (Teil „Windows-Dauerbetrieb“, Teile 2 bis 4 „folgt“), README-Abschnitt „Continuous sync on Windows“, `docs/cli.md` (2b `--all`, `sync request`, Sperrversuche, Exit 6, `secret_kind`), `docs/mcp.md` (`mail_sync_now` umgesetzt), `docs/architektur.md`, `docs/datenmodell.md` (Job `sync`), `docs/sicherheit.md` (tote Starts), `docs/abnahme_D9.md` (erwartete Ausgabe).

## Festlegungen (Einspruch möglich)

1. **`sync.request` ist R1**, wie im Auftrag und in A03 S3.5; `docs/mcp.md` nannte vorher „R2 wie `sync.run`“. Grund: Die Operation schreibt nur eine Zeile in `jobs`; was am Server passiert, verantwortet der Dauerläufer (`sync.run`, R2), und der prüft die Herkunft vor jedem Lauf.
2. **CLI-Name `sync request`** (Unterkommando von `sync`). Grund: gehört fachlich zu `sync`, ist in der Hilfe direkt darunter; `sync` selbst nimmt keine Argumente, also keine Verwechslung.
3. **Eingabe `mailbox` (ein Ordner), Job speichert eine Liste**; offene Jobs vereinigen die Ordner, ohne Ordner gewinnt „alle“, ab 21 Ordnern „alle“. Grund: je Konto nur ein offener Job (Index `jobs_open`), keine Anforderung geht verloren.
4. **Herkunft eines offenen Jobs bleibt die des ersten Aufrufs** (wie `draft_push`, A8). Folge: Wird dieser Schlüssel widerrufen, scheitert auch eine spätere Anforderung eines gültigen Schlüssels, die in denselben Job ging; der nächste reguläre Lauf erledigt sie ohnehin.
5. **Abgelehnte Herkunft: Job sofort `failed`** (kein Wiederholen) und Systemereignis `sync.job` (`denied`, Grund, `requested_by`, Zahl der Ordner, nie Ordnernamen). Grund: Wiederholen ändert am Schlüssel nichts; das Audit soll zeigen, dass ein Anstoß verweigert wurde.
6. **Erledigen durch Läufe:** Jeder fehlerfreie Lauf ohne gescheiterten Ordner erledigt Jobs, die vor seinem Beginn angefordert wurden und deren Ordner er gespiegelt hat (auch ein IDLE-Lauf der INBOX einen INBOX-Job). Eine Anforderung während des Laufs bleibt fällig (`requested_at`).
7. **Fehler beim angeforderten Lauf:** Verbindung oder Anmeldung → Job zurück, fällig nach 1 min; anderer Fehler → Wartezeit 1, 2, 4 … min, nach fünf Fehlern `failed`; `last_error` nur Fehlerart (`ops.ErrorKinds`), kein Servertext.
8. **`--all` wiederholt pro Konto nur bei belegter Sperre und Verbindungsfehler** (1 min, verdoppelt bis 30 min), endet bei Anmeldefehler, fehlenden Zugangsdaten und allem anderen. Grund: Ein manueller `sync` oder ein Netzausfall beim Start soll ein Konto nicht bis zum nächsten Neustart ausschließen; Anmeldefehler nicht wiederholen (Kontosperre beim Anbieter).
9. **`--all` nicht mit `--mailbox`** (Ordnernamen unterscheiden sich je Konto) und nicht mit `-a`.
10. **Enden alle Konten unter `--all` von selbst**, liefert der Prozess den Anmeldefehler, wenn es einen gab, sonst den ersten Fehler; mit Strg+C/Aufgabe beendet: Exit 0 bzw. `interrupted` wie bisher.
11. **Exit-Code 6 (`ExitAuth`) für `auth` und `oauth_refresh`**, vorher 1. Grund: Das Wrapper-Skript muss Anmeldefehler ohne Textauswertung erkennen. `no_token`/`no_secret` bleiben 1 (sie hämmern keinen Server). Bruch für Skripte, die auf 1 prüfen; in den Abnahme-Skripten gibt es keine solche Prüfung.
12. **Sperrversuche: fünf im Abstand von 400 ms** (rund 1,6 s), für `sync`, `sync --watch` und `import`.
13. **`DeadStartAge` = 10 min als eine Konstante in `store`**, die `doctor` übernimmt. Wiederprüfung im Watch vor jedem Lauf über alle Ordner (Standard alle 5 min) statt eines eigenen Takts.
14. **Mehrkonten-Start nur abschließen, wenn kein fremder Prozess eine seiner anderen Kontosperren hält**; die Prüfung holt die Sperre einen Augenblick (wie `doctor`), daher Festlegung 12.
15. **Wrapper:** Log je Zeile mit Ortszeit, Rotation beim Schreiben (10 MB, 5 alte Dateien), stdout und stderr von `postkauz.exe` zusammen; die Aufgabe startet nur bei Anmeldung (Netzlaufwerke, Anmeldeinformationsverwaltung), ohne Zeitgrenze, `RestartCount 3` für den Fall, dass das Skript selbst abstürzt.
16. **Aufgabenname „PostKauz sync watch“**, Log unter `<DataDir>\logs`.

## Commits

Zweig `worktree-agent-a7a8dad7a30793df8` (nicht gepusht), auf `8d47497`:

| Commit | Betreff |
|---|---|
| `a5982dd` | fix: retry the account lock before reporting sync_running |
| `1b02db0` | fix: name the token, not a password, when removing an OAuth2 account |
| `9952dd3` | fix: close dead audit starts only after 10 minutes and recheck them |
| `9216daf` | feat: queue a sync from MCP with mail_sync_now (sync.request) |
| `c315bc7` | feat: watch all accounts in one process with sync --watch --all |
| `b7f3def` | feat: exit with code 6 when the mail server rejects the login |
| `561318d` | test: avoid a data race in the lock retry test |
| `d10ede8` | feat: add a Windows task that keeps sync --watch --all running |
| `acc1602` | docs: add the operations guide with the Windows continuous sync part |
| (dieser) | docs: record work package A of T2 |

Berührte gemeinsame Dateien (je eine Stelle): `internal/ops/catalog.go` (Zeile `SyncRequest(d)` nach `SyncRun`), `internal/ops/ops_test.go` (`contractInputs` Zeile `sync.request`), `internal/mcp/tools.go` (Eintrag `mail_sync_now` nach `mail_get_policy`), `internal/mcp/notbound.go` (Grund bei `sync.run`), `internal/mcp/server_test.go` und `internal/cli/s28_test.go` (Zahl der Tools für `assistant` 15 → 16; die Hauptsitzung zählt ihre neuen Tools dazu), `docs/cli.md`, `docs/mcp.md`; dazu `internal/ops/ops.go` (`Progress.Account`), `internal/cli/exit.go`, `internal/mailsync/watch.go`, `internal/mailsync/runner.go`, `internal/store/audit_query.go`.

## Testläufe

- Vor jedem Commit `make check` (gofmt, vet, `go test ./...` ohne `-short`, guard, PII) auf einer Kopie des Index: alle grün (je rund 4,5 min).
- Neue Tests: `TestLockAccountRetriesBriefHolds`, `TestRenderAccountRemoveNamesTheSecret`, `TestCloseDeadStartsAgeAndOtherAccounts`, `TestSlowRunBeforeLockIsNotClosed`, `TestWatchClosesStartsThatAgeLater`, `TestImportLocksBeforeDiscover`, `TestSyncRequestQueuesOneJob`, `TestSyncJobsChecksOrigin`, `TestWatchRunsRequestedSync` (Ausführung im Watch gegen den In-Prozess-IMAP-Server, gemessen 26 bis 52 ms nach dem Einreihen bei `JobsInterval` 50 ms), `TestWatchRefusesRequestOfRevokedKey`, `TestOneOffSyncSettlesRequests`, `TestSyncWatchAllAccounts`, `TestSyncWatchAllInputAndEnd`, `TestSyncAllAndRequestCLI`; Vertragstest mit Zeile `sync.request` (alle Rollen und Trockenlauf-Varianten), CLI-Vertragstest mit Beispielen für `sync request` und `sync --watch --all`, `TestToolListFollowsScopes` (assistant 16 Tools), `TestExitCodesMatchDocs` (Exit 6).
- `make race` gezielt für alle neuen Tests (`ops`, `mailsync`, `store`, `importer`, `cli`): grün, nachdem ein Wettlauf im Test `TestLockAccountRetriesBriefHolds` selbst behoben war (`561318d`); der volle `make race` lief nicht (Zeit).
- `make cross` (linux/amd64, windows/amd64, darwin/arm64) grün; `make release VERSION=0.3.0-alpha.3` probehalber: `dist/` enthält `Register-SyncWatch.ps1` und `Run-SyncWatch.ps1` (CRLF, in `SHA256SUMS`). Rauchtest mit dem Linux-Programm: `sync --watch --all` ohne Konto → Exit 2 `no account configured yet`, `sync --all` → Exit 2 `--all needs --watch`.
- PowerShell: Im Container gibt es weder `pwsh` noch PSScriptAnalyzer; das Herunterladen von `pwsh` wurde von der Sitzungsrichtlinie abgelehnt. Geprüft wurden nur BOM und ASCII (`make guard`) und eine grobe Prüfung auf ausgeglichene Klammern und Anführungszeichen sowie Aliase (eigenes Hilfsskript, ohne Befund). **Die Skripte sind auf Windows ungetestet.**

## Offene Punkte

1. **PowerShell-Skripte auf Windows ausführen** (Prüfsitzung): PSScriptAnalyzer, Registrieren, Neustart nach `Stop-Process`, Backoff, Exit 6, Log-Rotation (mit kleinem `-MaxLogBytes` von Hand), `-Unregister`. Ob `Stop-ScheduledTask` das Kindprogramm `postkauz.exe` mit beendet, ist zu prüfen; `docs/betrieb.md` 1.6 sagt vorsichtshalber, wie man nachsieht.
2. **Integrationstest** „`mail_sync_now` → Watcher läuft binnen 10 s“ gegen Dovecot (A03 S3.5 Akzeptanz 5) fehlt; die Unit-Tests nutzen den In-Prozess-Server.
3. `make race` lief nur für die neuen Tests (siehe Testläufe), nicht vollständig; `make integration` und `make vuln` liefen nicht (kein Docker-Lauf in diesem Arbeitspaket).
4. Abnahme-Skript `abnahme_D7a` (Teil der Hauptsitzung) sollte den Dauerbetrieb (Schritte aus `docs/betrieb.md` 1.3 bis 1.6) und `mail_sync_now` aufnehmen.
5. Die Prüfung der Mehrkonten-Starts hält fremde Sperren einen Augenblick; Folge ist höchstens eine Wiederholung beim Gegenüber (Festlegung 12).

## CHANGELOG-Zeilen (Unreleased)

- `sync --watch --all`: ein Prozess beobachtet alle Konten, je Konto mit eigener Sperre und eigenen Zugangsdaten; ein Konto mit belegter Sperre oder ohne Verbindung versucht es später erneut, eines mit abgelehnter Anmeldung endet allein. Fortschritt und Bericht nennen das Konto; im Audit steht ein Aufruf mit allen Konten.
- Neue Operation `sync.request` (R1, `mail:sync`): MCP-Tool `mail_sync_now` und CLI `sync request [-a …] [--mailbox …]` reihen einen Sync ein (`status: queued`, `job_id`); ein laufendes `sync --watch` führt ihn binnen `--jobs-interval` aus, sonst der nächste `sync`. Vor der Ausführung wird der Schlüssel geprüft (widerrufen, abgelaufen, ohne `mail:sync`: kein Lauf, Job `failed`, Audit `sync.job`).
- Windows-Dauerbetrieb: `Register-SyncWatch.ps1` registriert die Aufgabe „PostKauz sync watch“ (bei Anmeldung, ohne Passwort im Skript), `Run-SyncWatch.ps1` startet `sync --watch --all` nach einem Absturz neu (60 s bis 30 min, nach Anmeldefehler 30 min) und schreibt ein rotierendes Log; beide im Release-Paket. Anleitung: `docs/betrieb.md` (neu).
- Exit-Code 6 (`auth`, `oauth_refresh`) statt 1 für abgelehnte Anmeldungen.
- `sync`, `sync --watch` und `import` versuchen die Kontosperre einige Male, bevor sie `sync_running` melden (`doctor` hält Sperren einen Augenblick).
- `account remove` meldet bei OAuth2-Konten `stored token deleted` statt `stored password deleted` (`--json`: `secret_kind`).
- Tote `sync.run`-/`import.run`-Starts werden erst nach zehn Minuten abgeschlossen (wie `doctor` sie zählt); ein langsamer Lauf vor seiner Sperre wird nicht mehr fälschlich als `process_ended` abgeschlossen. `import` sperrt das Konto vor dem Durchsuchen der Quelle, und `sync --watch` prüft tote Starts vor jedem Lauf über alle Ordner erneut.
