# T1b — Stand, Festlegungen und CHANGELOG-Zeilen (27.10.2026)

Teil **T1b** von Schnitt 3 nach [auftraege/A03_teile.md](../../auftraege/A03_teile.md) (Audit, Jobs, Policy; Teil A Kern). Ausgangsstand `main` `f536810` (enthält T1a), Zweig `claude/magical-goldberg-lwcx1n`, Build **0.3.0-alpha.2** (nur für die Abnahme T1 an einer Kopie, nicht für den Dauerbetrieb). Parallel laufen T1c und T4; deshalb steht der Stand nach Rahmen 10 hier und nicht in `docs/roadmap.md` und `CHANGELOG.md`. **Die Prüfsitzung überträgt ihn beim Merge.** Haltepunkt: Abnahme T1 ([abnahme_T1.md](../abnahme_T1.md), [abnahme_T1.ps1](../abnahme_T1.ps1)); ausgeführt von der Prüfsitzung.

## Stand T1b (für `docs/roadmap.md`, Abschnitt S3)

- **Erledigt:**
  - **A1 Variante A (E12, R3), Migration 0018:** `audit_events` neu gebaut mit `result = started` und `ref_id` (eindeutiger Teilindex, CHECK nie an einem Start; IDs, AUTOINCREMENT-Folge, Trigger und Index übernommen). Executor: Policy-Ablehnung, widerrufener Schlüssel und ungültige Eingabe = **ein** Eintrag; Erlaubnis = **zwei** (`started` vor dem Handler, fail closed; danach immer der Abschluss `ok`/`denied`/`error` mit eigenem Zeitstempel, Dauer und `ref_id`, auch nach Strg+C mit Frist 5 s). Scheitert nur der Abschluss: Ergebnis kommt zurück, Log meldet es (ohne Text), `doctor` zählt Starts ohne Abschluss älter als 10 Minuten. `audit`/`mail_audit_log` je Aufruf eine Zeile (`end_id`), `--raw`/`raw` alle Zeilen; `--result` filtert das Endergebnis. **Pflichttest** `TestMigration0018AuditVerify`: Datenbank und Export mit dem Stand T1a erzeugt (Testdaten mit Generator in `internal/ops/testdata/t1b_schema17/`), migriert, `audit verify` ok, alte Zeilen Wert für Wert gleich, neuer Export lässt die alten Tage Byte für Byte stehen, `verify` wieder ok. Vertragstest: genau ein Eintrag bei Ablehnung, genau zwei bei Erlaubnis. ADR-004 Nachtrag.
  - **A7:** `drafts.push` mit `started` vor dem APPEND (in der Transaktion des Versuchs, ID in `push_attempt.audit_id`), Abschluss durch Vermerk (`appended`, `adopted`), Ablehnung (`push_rejected`) oder `attempt_without_copy`. Verwerfen/Als-gesendet übernimmt eine schon gespiegelte Kopie; sonst übernimmt die Prüfung nach dem nächsten Sync mit fehlerfrei gespiegeltem Entwürfe-Ordner sie für abgeschlossene Entwürfe (`pushed_after_close`) oder schließt den Versuch ab. Test: Abbruch zwischen APPEND und Vermerk, `draft abandon`, Sync → übernommen bzw. ohne Kopie abgeschlossen, Audit vollständig.
  - **A8, Migration 0019:** `jobs.key_id`, `jobs.audit_id` (Herkunft des einreihenden Aufrufs). Vor jedem APPEND prüft der Push die Schlüssel hinter dem Entwurf (`created_by`, `requested_by` einer lebenden Freigabe) frisch: Widerruf, Ablauf, Scope `mail:draft` über die Policy. Sonst kein APPEND, Freigabe `rejected` (`key_revoked` …, `decided_by` `system`), Entwurf `abandoned`, Systemereignis. Test: Schlüssel zwischen Freigabe und Push widerrufen → kein APPEND.
  - **A9:** `Operation.PolicyInput` liefert aus der geprüften Eingabe die Facts (Aktion, Zahl der Ziele, `confirm_count`, Zielrolle, anderes Konto); `policy.CheckInput` entscheidet nach `[move]`, `[delete]`, `bulk_confirm_threshold` und der Obergrenze 1.000; Ablehnung = ein Eintrag `denied` vor jedem `started`. Die CLI gibt die Regeln der Policy-Datei an den Executor. Vertragstest `TestContractPolicyInputCases` (jede Operation mit Eingabegrenzen braucht abgelehnte und erlaubte Fälle; bis T2 hat keine Katalog-Operation `PolicyInput`), Executor-Test mit Testoperation.
  - **A2:** `drafts.request_ready` liefert `human_only: true` und mit Freigabe `next_step` („A human approves with: postkauz approve <id> — or rejects with: postkauz reject <id> --reason '…'. This server cannot approve or send.“); dasselbe in Tool-Beschreibung, `instructions` und Prompt `draft_reply`.
  - **A4:** `remote_status` (`present` | `imported_only` | `deleted_remote`) in `messages.get`, `messages.search`, `threads.get` und den MCP-Ausgaben, ohne Migration.
  - **Weitere Punkte aus A03_teile.md T1b:** `sync.run` und `drafts.request_ready` R2; `Redact` erst nach der Schemaprüfung (ungültige Eingabe: Parameter `{}`); gescheiterte Schlüsselanmeldung des MCP-Servers im Audit (`keys.authenticate`, `denied`, ohne Schlüssel); MCP meldet und loggt keine rohen Fehlertexte (fester Satz, Log nur Fehlertypen über `ops.ErrorKinds`); Doku-Drift (`sicherheit.md` Stand, `architektur.md` Abschnitt 1 und 6, Kommentare in `policy.go`/`file.go`).
  - **Offen aus der Nachprüfung T1a, erledigt:** Audit-Plan lädt nur die Kandidaten der offenen Tage; der Export liest abgeschlossene Tage gar nicht (unveränderte Datei, Kette setzt an), `verify` und ein Export an einen neuen Ort lesen sie tageweise über den ID-Bereich des Nachweises (Test zählt gelesene Zeilen). Test, in dem ein zweiter Prozess zwischen erster Schemaprüfung und exklusiver Sperre migriert (`TestMigrateRereadsSchemaUnderExclusiveLock`; scheitert ohne das erneute Lesen).
  - **Eigene Gegenprüfung des Diffs** (Review-Lauf, nur lesend): `doctor` zählte den offenen Start eines laufenden `sync --watch` und eines offenen Ablageversuchs als Absturz → behoben; Log ohne Diagnose → Fehlertypen; Aufräumen (Schlüssel einmal lesen, ein Baustein für Push-Ereignisse, Standards der Regeln an einer Stelle).
- **Schema:** 19 (0018 `audit_events`, 0019 `jobs`). Laufzeit an der synthetischen Datenbank (50.000 Nachrichten, 80.000 Ordnerkopien, 20.000 Audit-Zeilen, 28 MB, Schema 17 → 19, samt Snapshot): 320 ms, 312 ms, 317 ms (`POSTKAUZ_MEASURE_MIGRATION=1 go test ./internal/store -run TestMeasureMigrationsT1b -v`). Von rc8 (Schema 16) laufen 0017 bis 0019 nacheinander nach einem Snapshot; Messung an der Kopie der echten Datenbank: Abnahme T1, Schritt 2b.0.
- **Prüfläufe:** siehe unten.

## Festlegungen (Einspruch möglich, Rahmen 11)

1. Der Abschluss-Eintrag wiederholt Akteur, Schlüssel, Scope, Operation, Parameter und Ziele des Starts (jede Zeile für sich lesbar; eine Zeile mehr je Aufruf).
2. Eine ungültige Eingabe ist **ein** Eintrag `error`/`invalid_input` ohne `started` (der Handler läuft nicht an), Parameter `{}`.
3. `--explain`, `_meta.audit_id` und `Result.AuditID` nennen die ID des Start-Eintrags.
4. `doctor` zählt Starts ohne Abschluss, die älter als 10 Minuten sind; nicht gezählt: `sync.run`/`import.run`, solange eine Kontosperre gehalten wird (dafür holt `doctor` die Kontosperren einen Augenblick lang, wie `Migrate`; ein genau dann startender Sync meldet `sync_running`), und Starts offener Ablageversuche.
5. Nach einem Audit-Export bleibt der heutige Tag offen (der Abschluss-Eintrag des Exports entsteht nach den Dateien); `doctor` nennt dann „1 day not exported yet“, gewarnt wird wie bisher erst nach 48 h.
6. Der Export ersetzt eine unveränderte Datei eines abgeschlossenen Tages nicht mehr, auch wenn Datenbankzeilen dieses Tages geändert wurden (vorher hätte er sie mit den geänderten Zeilen neu geschrieben); das meldet `audit verify` (`database_changed`).
7. A7: Ein offener Ablageversuch eines verworfenen oder als gesendet markierten Entwurfs bleibt stehen, bis ein Sync den Entwürfe-Ordner fehlerfrei gespiegelt hat; dann übernommen (`pushed_after_close`) oder `attempt_without_copy`.
8. A8: Bei ungültigem Schlüssel wird der Entwurf **verworfen** (`abandoned`), nicht nur auf `draft` zurückgesetzt (sonst legte der nächste Push ihn als Entwurfskopie ab); die Freigabe wird `rejected` (Standard aus A03). Der Job scheitert nicht (Jobs gelten je Konto; die übrigen Entwürfe laufen weiter). Entwürfe des Menschen (`cli`) prüft der Push nicht.
9. `jobs.key_id`: Bei einem schon offenen Job zählt die Herkunft des ersten Aufrufs; vom Sync eingereihte Jobs tragen `system`.
10. A9: „Über `bulk_confirm_threshold`“ heißt `>` (E4: „über dieser Zahl“); die Obergrenze 1.000 gilt für jeden, die übrigen Regeln nur mit Schlüssel; `within_account` lehnt ein anderes Konto mit `cross_account` ab. Randwert und Obergrenze von `bulk_confirm_threshold` in der Policy-Datei (heute bis 100.000) bleiben bei T2 (A03_teile.md T2 Punkt 2).
11. R2 für `sync.run` und `drafts.request_ready` ist eine Kennzeichnung; die Policy behandelt R1 und R2 gleich (Trockenlauf erlaubt, kein Antrag).
12. Gescheiterte Anmeldung: Operation `keys.authenticate` (kein Registereintrag), nur der MCP-Server; ein ganz fehlender Schlüssel wird nicht auditiert (der Prozess öffnet die Datenbank dann gar nicht).
13. MCP, unbekannter Fehler: fester Satz mit Code `error`, Abbruch `canceled`; das Log nennt die Typen der Fehlerkette und bekannte Gründe, nie den Text.
14. A4: `present` auch für eine Nachricht ohne Löschmarke und ohne bekannte Serverkopie (Randfall).
15. Testdaten des Pflichttests A1 sind eine mit dem Stand T1a erzeugte Datenbank (gzip, 15 KB) samt Export; der Generator liegt daneben (`gen/main.go`, nicht übersetzt).

## Offene Punkte (für `docs/roadmap.md`)

- `make vuln`: siehe Prüfläufe (in der Cloud-Umgebung bisher gesperrt).
- Abnahme-Skript `abnahme_T1.ps1` ist hier nicht ausgeführt (kein PowerShell im Container; das Herunterladen von PowerShell hat die Sitzungsrichtlinie abgelehnt). Ein Subagent hat es gegen die Ausgaben des Programms gelesen (siehe Prüfläufe); die Prüfsitzung führt es zum ersten Mal aus.
- Ein Executor mit einem Audit-Writer ohne IDs (nur Testattrappen) schreibt Abschlüsse ohne `ref_id`; der SQLite-Writer kennt IDs. Kein Handlungsbedarf, solange es keinen weiteren Writer gibt.
- Verengung je Schlüssel (`[keys.<name>]`) und `[agent_defaults] dry_run_default` (E4) sowie die Operationen mit `PolicyInput` (move, trash, mark) kommen mit T2.
- Aus T1a weiter offen: Stildatei (`drafts.style`) öffnet nach `Lstat` blockierend (T1c oder T2); Kopf prüfen vor Aktionen auf nie geladenen Serverkopien (T2).
- Die Prüfsitzung trägt diesen Stand und die CHANGELOG-Zeilen beim Merge in `docs/roadmap.md` und `CHANGELOG.md` ein; dort auch Festlegungen 1 bis 15 als „Entschiedene Fragen“ (nächste freie Nummer).

## Fragen an den Betreiber (Standardantwort)

1. **Entwurf eines widerrufenen Schlüssels:** verwerfen (`abandoned`) statt nur zurück auf `draft`? **Default: verwerfen** (Festlegung 8; der Inhalt bleibt lokal lesbar, nichts landet mehr im Postfach).
2. **Abschluss-Eintrag mit allen Parametern** (eine volle Zeile mehr je Aufruf) statt nur Ergebnis und Verweis? **Default: volle Zeile** (Festlegung 1; `audit.rotate` aus T3 begrenzt das Wachstum).
3. **„1 Tag nicht exportiert“ direkt nach dem Export** (der eigene Abschluss-Eintrag des Exports): so lassen? **Default: so lassen** (ehrlich; Warnung wie bisher erst nach 48 h).

## CHANGELOG-Zeilen (für „Unreleased“)

### Added

- T1b (0.3.0-alpha.2): Migration 0018 (Schema 18): Audit mit Start- und Abschluss-Eintrag (`started`, `ref_id`; A1 Variante A). `audit` und `mail_audit_log` zeigen je Aufruf eine Zeile mit Endergebnis (`end_id`), `--raw`/`raw` alle Zeilen, `--result started` offene Aufrufe; `doctor` zählt Aufrufe ohne Abschluss (`audit_unfinished_calls`).
- T1b: Migration 0019 (Schema 19): `jobs.key_id`, `jobs.audit_id` (Herkunft eines Jobs).
- T1b: `drafts.request_ready` / `mail_request_ready` liefern `human_only` und `next_step` (wie der Mensch freigibt; A2).
- T1b: `remote_status` (`present`, `imported_only`, `deleted_remote`) in `show`, `search`, `thread` und MCP (A4).
- T1b: Die Policy prüft Eingaben zentral (`policy.CheckInput`: `[move]`, `[delete]`, `bulk_confirm_threshold`, höchstens 1.000 Ziele; A9); gebraucht ab T2.
- T1b: Gescheiterte Anmeldungen des MCP-Servers stehen im Audit (`keys.authenticate`).

### Changed

- T1b: Ein erlaubter Aufruf schreibt zwei Audit-Einträge; der Abschluss trägt seinen eigenen Zeitstempel und die Dauer. Einträge vor Schema 18 bleiben unverändert.
- T1b: `sync.run` und `drafts.request_ready` haben Risikostufe R2 (legen per APPEND ab).
- T1b: Die Eingabe wird vor dem Audit gegen das Schema geprüft; eine ungültige Eingabe steht als ein Eintrag `error`/`invalid_input` ohne Parameter im Audit.
- T1b: Audit-Export, `audit verify` und `doctor` laden nicht mehr das ganze Protokoll (nur offene Tage; abgeschlossene Tage tageweise bzw. gar nicht).
- T1b: Der MCP-Server gibt bei unbekannten Fehlern einen festen Satz statt des Fehlertexts zurück und loggt nur die Fehlertypen.

### Fixed

- T1b: Ein Entwurf, dessen API-Schlüssel nach der Freigabe widerrufen wurde, wird nicht mehr abgelegt (Freigabe `rejected`, Entwurf `abandoned`; A8).
- T1b: Ein Absturz zwischen APPEND und Vermerk lässt keinen Ablageversuch ohne Audit zurück; auch für danach verworfene Entwürfe wird die Kopie übernommen oder der Versuch abgeschlossen (A7).

### Docs

- T1b: `docs/abnahme_T1.md` Schritte 1 bis 4 und `docs/abnahme_T1.ps1` (Update einer Kopie von 0.2.0-rc8 auf 0.3.0-alpha.2, Vergleich alt/neu, Laufzeit der Migrationen); ADR-004 Nachtrag (zwei Einträge je Aufruf, Policy prüft die Eingabe); Stand und Festlegungen in `docs/protokolle/T1b_stand.md`.

## Prüfläufe

Umgebung: Cloud-Container Linux, 4 Kerne, `go version go1.27.3 linux/amd64` (nach der Ausnahme in `CLAUDE.md` geholt, danach `GOTOOLCHAIN=local`). Go-Code für alle Läufe: Stand `8d32ac5` (danach nur Doku).

| Befehl | Ergebnis | Laufzeit |
|---|---|---|
| `make check` | vor jedem Commit exit 0 (gofmt, vet, alle Pakete, guard, pii-check); Basis `f536810` vor Beginn ebenfalls exit 0 | je rund 3–4 min |
| `make race` | exit 0: 29 Pakete ok, kein Datenrennen (längste: importer 885 s, ops 848 s, mailsync 707 s, cli 681 s) | 18 min 24 s |
| `make integration` | exit 0: 9 Integrationstests PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14 (ImportOverlapDovecot, DraftPush, DraftPushRefusals, DraftPushWatchAndOffline, Sync, Idle, SentDetection, CapabilitiesAndRoles, WrongPassword); kein FAIL, kein SKIP. Erster Lauf: 4 Push-Tests FAIL mit `key_revoked 1` — die Tests legten Entwürfe mit einem Schlüssel an, der nie in `api_keys` stand; A8 lehnt das zu Recht ab; Testumgebung legt ihn jetzt an (`8d32ac5`) | 2 min 32 s |
| `make vuln` | **nicht gelaufen:** Die Netzwerk-Richtlinie dieser Cloud-Umgebung weist `vuln.go.dev` ab (`Forbidden`), wie in T1a; nachholen im CI-Job `vuln` bzw. in der Prüfsitzung | – |
| `make cross` | exit 0: linux/amd64 26,6 MB, windows/amd64 26,4 MB, darwin/arm64 25,0 MB | < 1 min |
| `make release VERSION=0.3.0-alpha.2` | `dist/` mit Programmen für linux/amd64, windows/amd64, darwin/arm64, `SHA256SUMS` und `VERSION` (Commit `be3fbd1`, ohne lokale Änderungen); Windows-Programm `2e382371fc8bdd00fc6b7e1854ba430576523fd8529c8c114dc502a27587f432` | < 1 min |
| Rauchtest Linux-Programm aus `dist/` | frisches Datenverzeichnis: `doctor` Datenbank ok, Schema 19 = bekannt, 0 Aufrufe ohne Abschluss; `audit --raw --json` zeigt je Aufruf `started` und Abschluss mit `ref_id` | – |
| Messung Migrationen | siehe oben (Schema 17 → 19 rund 0,3 s an 50.000 Nachrichten / 20.000 Audit-Zeilen) | – |

**Docker:** Daemon von Hand gestartet (`dockerd`); Docker Hub lieferte `429 Too Many Requests`, die drei Images kamen unverändert (gleiche Tags) über `mirror.gcr.io` und wurden lokal auf die Namen aus `deploy/testmail/docker-compose.yml` getaggt (wie T1a).

**Abnahme-Skript:** nicht ausgeführt (kein PowerShell; Herunterladen von PowerShell durch die Sitzungsrichtlinie abgelehnt). Ein Subagent hat es nur lesend gegen die Go-Ausgaben von rc8 (`99a7cc0`) und dieses Stands geprüft: Feldnamen und Flags passen; behoben wurden eine Suchanfrage mit führendem `-` (jetzt hinter `--`), eine fehlende Suchdatei, der Exit-Code in 3.3 und die Prüfung von `-WorkDir` vor dem ersten Schreiben.
