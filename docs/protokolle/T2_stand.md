# T2: Stand, Festlegungen, Prüfläufe (28.10.2026)

Für: Prüfsitzung DRACO-PK09 und Betreiber. Auftrag: [A03_teile.md](../../auftraege/A03_teile.md) Teil T2 mit den Abschnitten aus [A03_s3.md](../../auftraege/A03_s3.md) (S3.1, S3.2, E1–E5, A5.1, A13.1, A14) und den Zusätzen der Prüfsitzung vom 28.10.2026 (Build `0.3.0-alpha.4`; tote Starts erst nach zehn Minuten abschließen; `make vuln` in der Cloud gesperrt). Ausgangsstand `main` `8d47497`, Zweig `claude/jolly-franklin-sa5rse`. Das Arbeitspaket WP-A (Dauerbetrieb, `mail_sync_now`, kleine Punkte aus T2 Punkt 7) lief parallel in einem eigenen Worktree und ist zusammengeführt; sein Protokoll: [T2_wpa.md](T2_wpa.md).

## 1. Was gebaut ist

1. **Migration 0020** (Schema 20): Tabellen `actions` und `action_targets` (Aktion gebunden an Nachricht, Ordner, UID und UIDVALIDITY; `CHECK` gegen UID 0 und 4294967295), Job-Typ `mailbox_action` (Neubau von `jobs`, `key_id`/`audit_id` bleiben), `message_mailboxes.pending_flags` und `content_loaded`, `accounts.drafts_mailbox` (A5.1), Neubau von `tags`/`message_tags` mit `CHECK` für Namen und `set_by` (E5) samt Vorprüfung, Nachtrag von `drafts.updated_by` aus dem Audit. Laufzeit an 50.000 Nachrichten, 80.000 Ordnerkopien und 20.000 Audit-Zeilen: **0,33 s** (`TestMeasureMigration0020`, Linux-Container).
2. **Policy und Executor:** R3 geht dem Trockenlauf vor (A14; Trockenlauf eines R3-Aufrufs liefert `needs_approval`, legt aber nichts an). Operationen mit `ApprovalRequest` (nur `messages.trash`) laufen bei `needs_approval` als Antrag; Abschluss-Eintrag `needs_approval`. `PolicyInput` mit Panic-Schutz; ohne `Rules` fail closed (`policy_rules_missing`). `bulk_confirm_threshold` 1 bis 1.000. Verengung je Schlüssel `[keys.<name>]` (nur enger als der Standard), `[agent_defaults] dry_run_default`, `key create --dry-run-default`, `[approval] ttl`, `[tags] sync_keywords`. `move` in den Ordner mit Rolle `trash` ist für jeden verboten (`target_is_trash`).
3. **Operationen:** `messages.tag` (R1, `mail:tag`), `tags.list` (R0), `messages.mark` (R2, `mail:tag`), `messages.move` (R2, `mail:move`), `messages.trash` (R3, `mail:trash`, nur Antrag), `actions.approve`/`actions.reject` (nur Mensch), `actions.get`/`actions.list` (R0), `drafts.cleanup_copies` (nur Mensch). Je Operation Test, Zeile im Vertragstest, Bindung in CLI (`tag`, `mark`, `move`, `trash`, `action`, `draft cleanup`) und MCP (`mail_tag`, `mail_list_tags`, `mail_mark`, `mail_move`, `mail_trash`, `mail_get_action`, `mail_list_actions`), Einträge in `docs/cli.md` 2k und `docs/mcp.md`.
4. **Ausführung (E1, ADR-009):** Job `mailbox_action`, ausgeführt nur vom Prozess mit Zugangsdaten (`sync`, `sync --watch` im Jobs-Takt). Vor jedem Ziel: Herkunft frisch prüfen (Schlüssel widerrufen/gelöscht → Aktion `failed`), Ziel gegen den Spiegel prüfen, bei nie selbst geladener Kopie den Kopf holen und vergleichen (Abweichung → `stale`, nichts passiert; T2 Punkt 3). STORE bzw. MOVE je Quellordner; Ergebnis je Ziel (`ok`, `not_found`, `stale`, `unsupported`, `failed`, `skipped`).
5. **A13.1:** `imapx.Client.Move` prüft `MOVE` direkt vor dem Aufruf und liefert sonst `ErrUnsupported`; kein Rückfall auf COPY + `\Deleted` + EXPUNGE. `make guard` verbietet `Expunge(`, `UIDExpunge`, `FlagDeleted` und erlaubt `.c.Move(` nur an dieser Stelle. Test gegen einen Server ohne MOVE: weder STORE noch COPY noch EXPUNGE.
6. **Nacht-Triage (Vorgang 5):** Rolle `triage` (lesen, suchen, Entwurf, Tag/Markieren); `TestTriageRoleCanDoNothingElse` geht alle Operationen des Katalogs durch.
7. **MCP-Text:** `Instructions` beschreibt die Schreibrechte nach den Scopes des Schlüssels („never … moves mail“ nur ohne `mail:move`; Test).
8. **A5.1:** `account set --drafts-mailbox` je Konto, geht `[draft] mailbox` und der Rolle `drafts` vor.
9. **T2 Punkt 7** (Rest neben WP-A): abgelaufener Schlüssel nach menschlicher Freigabe (Offene Frage 1, Default umgesetzt), MCP-Fehlerlog mit `audit_id` und festem Code, Stildatei öffnet nicht blockierend (FIFO), `updated_by`-Nachtrag (Migration 0020), Hinweis auf `draft cleanup` bei alten Entwurfskopien.
10. **WP-A:** siehe [T2_wpa.md](T2_wpa.md) (`sync --watch --all`, `sync.request`/`mail_sync_now`, Windows-Aufgabe, Exit 6, Sperrversuche, tote Starts erst nach 10 min, Import sperrt vor Discover — Zusatz (b) der Prüfsitzung).

## 2. Festlegungen (Standardantworten, Einspruch möglich; Rahmen 11)

1. **Papierkorb auch für den Menschen als Antrag:** `trash` endet ohne `--yes` mit Exit 4 und nennt `action approve <n>`; `trash --yes` stellt den Antrag und gibt ihn im selben Aufruf frei (zwei Audit-Einträge). Grund: ein Weg für alle, die Freigabe steht immer im Audit.
2. **`action approve`/`action reject` nehmen die Nummer der Aktion**, nicht die der Freigabe (die steht in der Ausgabe). Grund: Agent und Mensch reden über dieselbe Nummer.
3. **Quellkopie bei move/trash:** `from` nennt den Ordner; ohne `from` die einzige Kopie außerhalb der Rolle `all`; mehrere → `ambiguous_source`; liegt die Nachricht schon im Ziel → `skipped`. `move` in den Papierkorb ist verboten (`target_is_trash`), dafür gibt es `trash`.
4. **mark wirkt auf alle Serverkopien** einer Nachricht (bei Gmail zeigen alle Label-Ordner dieselben Flags), move und trash auf genau eine.
5. **Vorhandene Ordnerkopien gelten nach 0020 als nie selbst geladen** (`content_loaded = 0`); nur Kopien aus eigenem Download zählen als geladen. Folge: Die erste Aktion an einer alten Kopie holt einmal den Kopf. Sicher statt schnell.
6. **`pending_flags`:** Der Spiegel zeigt den Wunsch sofort; der Flag-Abgleich überschreibt Kopien mit offener Aktion nicht. Lehnt der Server ab, gilt der Stand davor, danach gewinnt der Server.
7. **Keywords:** Mit `[tags] sync_keywords = true` erzeugt `messages.tag` zusätzlich eine Aktion `keywords` unter R1/`mail:tag`; nur in Ordnern mit `PERMANENTFLAGS \*`, sonst Ergebnis `unsupported`. Standard aus.
8. **Rolle `triage`** = `mail:read`, `mail:search`, `mail:draft`, `mail:tag` (ohne `audit:read`, ohne `mail:sync`).
9. **Rolle `reader`** sieht die lesenden Werkzeuge der Aktionen und Tags (`mail_list_tags`, `mail_get_action`, `mail_list_actions`; Scope `mail:read`).
10. **`drafts.updated_by`-Nachtrag** aus dem letzten `drafts.update` eines Schlüssels im Audit (auch gescheiterte Versuche, strenger); ohne Eintrag bleibt NULL (dann gilt `created_by`).
11. **Abgelaufener Schlüssel** (nicht widerrufen, nicht gelöscht) hält einen vom Menschen freigegebenen Entwurf (`ready`, Freigabe `approved`, gebunden an die Nachricht) und eine vom Menschen freigegebene Aktion nicht mehr auf. Widerrufen oder gelöscht hält weiter auf. Offene Frage 1 damit mit ihrer Standardantwort entschieden.
12. **Eigene Operation `tags.list`** (R0) statt Liste als Teil von `messages.tag`.
13. **Abgebrochene Aktion (`running`)** wird beim nächsten Lauf erneut versucht; Ziele mit Ergebnis behalten es.
14. **Jobs-Takt bleibt 10 s** (`--jobs-interval`). Gemessen gegen Dovecot mit Standardtakt: `mark --seen` nach 10,0 s auf dem Server, `mail_sync_now` nach 10,0 s im Spiegel (Abschnitt 3). „Binnen 10 s“ heißt wie bei Entwürfen (Entscheid 44) „im nächsten Takt“; im ungünstigsten Fall kommen die Laufzeit des Jobs dazu.
15. **„Label entfernen“ bei Gmail** = `move <id> --from <Label-Ordner> --role all`; unter `archive_only` erlaubt. Im Testserver nicht prüfbar, gemessen in D7a.
16. **`draft cleanup`** nur für Kopien, die der Spiegel unter Ordner, UID und UIDVALIDITY mit genau dem abgelegten Inhalt führt (`remote_previous` allein reicht nicht; A14).
17. **Server ohne MOVE:** Ziel `unsupported`, nichts passiert, kein Ersatz.
18. **Build** heißt `0.3.0-alpha.4` (Zusatz (a); `0.3.0-alpha.3` ist vergeben).

19. **Nachtrag der Prüfsitzung (Codex-Runde auf `589bb8d`), Punkt 1:** `CloseDeadStarts` schließt Starts im Trockenlauf nie ab, `doctor` zählt sie nicht (sie halten keine Kontosperre und laufen womöglich parallel; dass ihr Prozess tot ist, lässt sich nicht nachweisen). Ein hart beendeter Trockenlauf bleibt als offener Start stehen. Regressionstest `TestDryRunStartIsNotClosedByParallelRun` (schlägt ohne den Fix fehl).
20. **Nachtrag, Punkt 2:** `abnahme_T1.ps1` und `abnahme_D7a.ps1` legen das Protokoll exklusiv neu an (`FileMode.CreateNew`); vorhandene Datei, Link oder `-WorkDir` als Reparse Point → Abbruch statt Leeren. Wiederholungslauf: altes Protokoll umbenennen oder neues `-WorkDir`.

Festlegungen von WP-A: [T2_wpa.md](T2_wpa.md), „Festlegungen“ 1–16.

## 3. Prüfläufe

- **`make check`** vor jedem Commit grün (gofmt, vet, `go test ./...` ohne `-short`, Guard, PII-Gate).
- **Integration:** Docker Hub antwortete mit 429 (Rate-Limit) für alle Images, Maven Central mit 429 für die GreenMail-JAR. Nach Rahmen 7 lief deshalb **Dovecot 2.3.21 aus den Debian-Paketen** (nicht 2.4.5 wie in `deploy/testmail`) auf 127.0.0.1:11143 mit SPECIAL-USE, MOVE, UIDPLUS, CONDSTORE, QRESYNC. `POSTKAUZ_IT=dovecot go test -run Integration ./...`: alle grün, darunter neu `TestIntegrationMailboxActions` (mark und move in einem `sync`, 32–42 ms; trash erst nach Freigabe, landet im Ordner mit Rolle `trash`; zweiter Client prüft je Schritt) und `TestIntegrationWatchActionsAndSyncNow` (mit `sync --watch` und Standardtakt: `mark --seen` nach 9,997 s auf dem Server, `sync.request` für einen Ordner ohne IDLE nach 10,002 s im Spiegel). **Stalwart und GreenMail übersprungen** (nicht ladbar); nachzuholen in der Prüfsitzung mit `make testmail-up testmail-setup integration`.
- Der Integrationstest fand einen Fehler: `messages.trash` ohne verdrahteten Entwurfsdienst endete mit nil-Zeiger (Frist der Freigabe wurde über ihn gelesen). Behoben in `d174d7e`, Regressionstest `TestMessagesTrashWithoutDrafts`. Im Programm ist der Dienst immer verdrahtet; betroffen waren nur Tests.
- **Abnahme D7a, Probelauf im Container** (Subagent, ohne PowerShell): Teil A als Python-Nachbau mit denselben Kommandos und Mustern, 30 Prüfungen OK, MCP-Probe 3 OK; Teil B gegen einen eigenen Dovecot 2.3 mit Gmail-ähnlichen Ordnern (selbst geschickte Mail in INBOX, Sent, All Mail): tag, mark, move in einen Label-Ordner, move `--role all`, trash mit Freigabe, Audit und `verify`, 30 Prüfungen OK. Zwei Funde, beide behoben: D7a-1 (unbekannte Aktion endete mit Code `error` und dem Text des Stores statt `not_found`; CLI und MCP) und D7a-2 (Hilfetext von `key create --role` ohne `triage`).
- **`make race`:** siehe unten.
- **`make cross`:** siehe unten.
- **`make vuln`:** in dieser Cloud-Umgebung gesperrt (`vuln.go.dev` nicht erreichbar; Zusatz (c)). Nachholen in der Prüfsitzung bzw. im CI-Job.
- **PowerShell:** `Register-SyncWatch.ps1`, `Run-SyncWatch.ps1` und `abnahme_D7a.ps1` sind auf Windows ungetestet (kein `pwsh` im Container); geprüft nur BOM/ASCII (`make guard`).

## 4. Offen

1. Abnahme D7a ([abnahme_D7a.md](../abnahme_D7a.md), [abnahme_D7a.ps1](../abnahme_D7a.ps1)) durch die Prüfsitzung am Büro-PC.
2. Integration gegen Stalwart und GreenMail sowie Dovecot 2.4.5; `make vuln`.
3. PowerShell-Skripte auf Windows (PSScriptAnalyzer, Registrieren, Neustart, Exit 6, Rotation).
4. Gmail „Label entfernen“ (Festlegung 15) und Verhalten der Gmail-Ordner bei MOVE nur in D7a messbar.
