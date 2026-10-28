# T1b — Nachbesserung und Merge mit `main` (28.10.2026)

Auftrag: [auftraege/A03_T1b_nachbesserung.md](../../auftraege/A03_T1b_nachbesserung.md) (Prüfsitzung DRACO-PK09). Arbeitszweig `claude/magical-goldberg-lwcx1n`, Ausgangsstand `898bd95` (= `6d1d3d4` plus Auftrag), `main` `bac9f12` hineingemergt (Merge, kein Rebase). Nichts nach `main`, kein Tag, kein Release. Build **0.3.0-alpha.3** (nur für die Abnahme T1 an einer Kopie). Stand vor der Nachbesserung: [T1b_stand.md](T1b_stand.md). Stand und Festlegungen stehen jetzt in [roadmap.md](../roadmap.md) (Abschnitt S3 „Stand T1b“, Entschiedene Fragen 54 und 55).

## Commits (Reihenfolge des Auftrags)

| Teil | Commit | Inhalt | Regressionstest (scheitert vorher, besteht nachher) |
|---|---|---|---|
| A-F3 | `95b0f1c` | `drafts.updated_by` und `drafts.push_hold` (Migration 0019); Push prüft den letzten Schreiber; Entwurf des Menschen → `draft`, Freigabe `rejected`, zurückgehalten | `TestAF3RevokedLastWriterStopsPush` (Mensch legt an, Agent ändert, Widerruf, Push → `KeyRevoked 1`, 0 APPENDs; Freigabe durch Änderung oder eigene Vorlage des Menschen). Gegenprobe ohne Prüfung von `updated_by`: FAIL |
| A-F4 | `0375566` | Zählabgleich in `audit verify` | `TestAuditVerifyFindsInsertedLowID` (ID 0 und −1 mit Zeitstempel eines abgeschlossenen Tages → `database_changed` am 2. März). Gegenprobe ohne Abgleich: `Status:ok` (FAIL) |
| A-F9 | `9d49d2f` | Tote `sync.run`/`import.run`-Starts unter der Kontosperre abschließen; `doctor` je Konto | `TestAF9SyncClosesDeadStarts` (a), `TestAF9DoctorCountsDeadStartPerAccount` (b); beide setzen das neue Verhalten voraus (vorher: kein Abschluss bzw. alle Konten ausgeblendet) |
| Kommentare | `9d49d2f`, `7600239` | `doctor.go` (lange Läufe), `policy/file.go` „über“ | – |
| A-F5 | `8782133` | Zweiter Pflichttest mit Daten des Stands rc8 | `TestMigration16AuditVerify` (Generator `internal/ops/testdata/t1b_schema16/gen/`, an `99a7cc0` ausgeführt) |
| Merge | `1229574` | `main` `bac9f12` | volle Prüfläufe (unten) |
| B-M1 | `3b4dd9f` | Löschfehler der Token-Teile | `TestDeleteKeepsMainEntryWhenAPartFails` |
| B-M2 | `b3ae190` | Warnung im Log, wenn `oauth.refresh` nicht ins Audit kommt | `TestRefreshAuditFailureIsLogged` (Log ohne Token und ohne Fehlertext) |
| B-M3, B-M4 | `921d1cc` | `doctor`: letzter Refresh aus dem Audit (`token_refresh`), Lesefehler des Schlüsselbunds; `doctor --secrets` `Skipped` | `TestDoctorTokenRefreshAndKeyringErrors` (`internal/cli/s37_test.go`; Ereignisse direkt ins Audit) |
| C | `dbf2cbd` | Abnahme T1 für 0.3.0-alpha.3 | PowerShell gibt es im Container nicht: Lesetest (siehe unten) |
| D | `9c71347` | Übertrag nach Roadmap und CHANGELOG, T2-Liste | – |

## Merge mit `main`

Konflikte nur in drei Dateien: `internal/ops/sync.go` (T4 übergibt `cred`, T1b-Nachbesserung `AuditID` → beides), `internal/cli/doctor.go` (Importe `flock` und `oauth`), `docs/cli.md` (Zeile S3.7 aus `main`, R2 aus T1b). Der Guard aus T1c verlangt für `docs/*.ps1` UTF-8 mit BOM: `docs/abnahme_T1.ps1` hat ihn im Merge-Commit bekommen (Inhalt war schon ASCII). Keine Migration auf `main` nach `f536810` (T1c und T4 ohne Migration); 0019 lag nicht auf `main` und durfte geändert werden.

## Festlegungen dieser Sitzung (Einspruch möglich, Rahmen 11)

Siehe roadmap.md, Entschiedene Frage 55 (a) bis (f). Kurz: Entwurf des Menschen wird nie verworfen, Inhalt eines ungültigen Schlüssels wird zurückgehalten, bis der Mensch ändert oder selbst vorlegt; Zählabgleich statt Laden aller Zeilen; tote Starts erst ab einer Minute und nur ältere als der eigene, ohne Ziel nur bei genau einem Konto; Trockenlauf-Fehler beim Refresh zählen; `-NewVersion` mit Standard `0.3.0-alpha.3`, Zeile 2d nur Information.

**Hinweis Versionsnummer:** `A03_teile.md` sieht `0.3.0-alpha.3` auch als Build von T2 vor. Vorschlag: T2 nimmt `0.3.0-alpha.4` (und T3 `alpha.5`); entscheidet die Prüfsitzung.

## Abnahme-Skript (Teil C): Lesetest

Kein PowerShell im Container. Ein unabhängiger Subagent hat `abnahme_T1.ps1` nur lesend gegen die Go-Ausgaben geprüft (Flags `draft list --status/--limit`, `draft show --json` mit `created_by`, `updated_by`, `approval_id`, `approval_status`, `approval show --json` mit `requested_by`, `key list --json` mit `status` und `scopes`): kein Blocker, keine PowerShell-5.1-Inkompatibilität, Guard „BOM + ASCII“ grün. Seine neun kleineren Punkte sind eingearbeitet: Pfadmasken vor dem ersten Protokolleintrag, Rechnername nicht im Protokoll; bei Fehlern in 2d nur Exit-Codes und stderr (keine Betreffs); erst maskieren, dann kürzen; Kontonamen in einem Durchgang; Zählung 2d genau wie `originDenied` (Scope `mail:draft`, `push_hold`, Vorlage durch den Menschen); Prozessprüfung nur am ersten Wort der Kommandozeile; Abbruchtext nennt OneDrive, Sicherung, Virenscanner; Marker mit `-LiteralPath`/.NET und Abbruch, wenn er sich nicht löschen lässt; Version mit `(?![0-9])`. Trockenlauf-Beschreibung je Schritt: [abnahme_T1.md](../abnahme_T1.md), Tabelle „Schritte und Erwartung“.

## Prüfläufe

Umgebung: Cloud-Container Linux, 4 Kerne, `go version go1.27.3 linux/amd64` (nach der Ausnahme in `CLAUDE.md` aus dem Modulcache, danach `GOTOOLCHAIN=local`). `make check` lief vor jedem Commit (in einem eigenen Arbeitsbaum mit genau dem zu committenden Stand); die Doku-Commits C und D ohne Go-Änderung mit `make guard pii-check`, danach `make check` am Endstand.

| Befehl | Stand | Ergebnis | Laufzeit |
|---|---|---|---|
| `make check` | jeder Code-Commit; Endstand `9c71347` | exit 0 (gofmt, vet, alle Pakete, guard, pii-check) | je rund 4–6 min |
| `make race` | Merge-Commit `1229574` | exit 0: 30 Pakete ok, kein Datenrennen (längste: ops 1047 s, importer 974 s, cli 828 s, mailsync 815 s) | rund 40 min (parallel zu anderer Arbeit) |
| `make race` | Endstand `9c71347` | exit 0: 30 Pakete ok, kein Datenrennen (längste: ops 899 s, importer 791 s, cli 667 s, mailsync 659 s) | 17 min 21 s |
| `make integration` | Endstand `9c71347` | exit 0: 9 Integrationstests PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14 (ImportOverlapDovecot, DraftPush, DraftPushRefusals, DraftPushWatchAndOffline, Sync, Idle, SentDetection, CapabilitiesAndRoles, WrongPassword); kein FAIL, kein SKIP | 2 min 24 s |
| `make vuln` | – | **nicht gelaufen:** Die Netzwerk-Richtlinie der Cloud-Umgebung weist `vuln.go.dev` ab (`Forbidden`), wie in T1a und T1b; nachholen im CI-Job `vuln` bzw. in der Prüfsitzung | – |
| `make cross` | Endstand | exit 0: linux/amd64 28,3 MB, windows/amd64 28,1 MB, darwin/arm64 26,6 MB | 2 min |
| `make release VERSION=0.3.0-alpha.3` | `9c71347` (ersetzt, siehe Nachtrag) | wie unten, Windows-Programm `7556d69120a1e4d243ab801b747cc2aeea44ee96213cf21fc7075a98a86c90ab`; nicht mehr gültig | < 1 min |
| Rauchtest Linux-Programm aus `dist/` | – | frisches Datenverzeichnis: `version` 0.3.0-alpha.3; `doctor` Datenbank ok, Schema 19 = bekannt, 0 Aufrufe ohne Abschluss; `audit --raw --json` zeigt Start und Abschluss | – |
| Messung Migrationen 0018 + 0019 | Endstand | 50.000 Nachrichten, 20.000 Audit-Zeilen, 28 MB, samt Snapshot: 272 ms, 238 ms, 271 ms (`POSTKAUZ_MEASURE_MIGRATION=1 go test ./internal/store -run TestMeasureMigrationsT1b -v`); die zwei neuen Spalten in `drafts` sind `ALTER TABLE ADD COLUMN` ohne Umbau | – |

**Docker:** Daemon von Hand gestartet (`dockerd`); die drei Images kamen diesmal direkt von Docker Hub (gleiche Tags wie in `deploy/testmail/docker-compose.yml`).

## Nachtrag: `golang.org/x/net` v0.62.0 (GO-2026-6860)

Die Prüfsitzung meldete aus `make vuln` (govulncheck v1.9.0) auf `main` `bac9f12` und `898bd95` einen erreichbaren Fund: GO-2026-6860 in `golang.org/x/net` v0.61.0 (`html.Parse`, quadratischer Aufwand beim Zusammenfassen von Textknoten; Aufrufpfad `internal/sanitize/html.go` → `sanitize.HTML`). Commit `c17802e`: `go get golang.org/x/net@v0.62.0`, `go mod tidy` (nur `x/net` ändert sich, Lizenz BSD-3-Clause), CHANGELOG unter „Security“. Danach volle Prüfläufe am Stand `c17802e`:

| Befehl | Ergebnis | Laufzeit |
|---|---|---|
| `make check` | exit 0 | rund 5 min |
| `make race` | exit 0: 30 Pakete ok, kein Datenrennen (längste: ops 906 s, importer 847 s, cli 728 s, mailsync 692 s) | 18 min 27 s |
| `make integration` | exit 0: dieselben 9 Integrationstests PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14; kein FAIL, kein SKIP | 3 min |
| `make vuln` | **nicht gelaufen:** `vuln.go.dev` weiter gesperrt (`Forbidden`); die Prüfsitzung prüft nach | – |
| `make cross` | exit 0: linux/amd64 28,3 MB, windows/amd64 28,1 MB, darwin/arm64 26,6 MB | 2 min |
| `make release VERSION=0.3.0-alpha.3` | neu gebaut an `c17802e` ohne lokale Änderungen; **gültiger Build**; Windows-Programm `d14470ed2b20d349dca323ac0b68ca17b1d00646aa837b3076ba7e058d5d5cf9`; `go version -m` zeigt `golang.org/x/net v0.62.0` | < 1 min |

## Offen

- Abnahme T1 an einer Kopie auf dem Büro-PC (Prüfsitzung); erste Ausführung der Fassung für 0.3.0-alpha.3.
- `make vuln` (Netz).
- Nach T2 verschoben: [A03_teile.md](../../auftraege/A03_teile.md) T2 Punkt 7; Rückfrage „abgelaufener Schlüssel nach menschlicher Freigabe“ in roadmap.md, Offene Frage 1.
