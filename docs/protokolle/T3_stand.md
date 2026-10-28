# T3: Stand, Festlegungen, Prüfläufe (28.10.2026)

Teil T3 nach [auftraege/A03_teile.md](../../auftraege/A03_teile.md) (S3.3, S3.4, A10, A11, A13.2, A13.3, A14-Punkte, die nach T3 verschobenen Funde der Nachbesserung T2) und den Zusätzen (a) bis (f) der Prüfsitzung DRACO-PK09. Zweig `claude/determined-fermi-1g8w63`, Ausgangsstand `main` `a2c016c`. Haltepunkt: Abnahme D7 ([abnahme_D7.md](../abnahme_D7.md)), ausgeführt von der Prüfsitzung.

## 1. Arbeitsweise

Die Hauptsitzung legte die gemeinsame Basis selbst an (Migration 0022 mit Höchststand der IDs und Quellpfad je Ziel, Zusätze (b) und (d)) und ließ danach vier Teilpakete in eigenen Worktrees parallel bauen: A (Anhänge und Export, S3.3), B (Backup und Restore, S3.4 Teil 1, A10), C (Löschkonzept, S3.4 Teil 2, A11, A13.2, A13.3, A14), D (Funde V3, V8, V11, V14, V16–V19, V21, Quellpfad, Zusatz (c), `accounts.remove`). Jedes Paket lief `make check` vor seinen Commits; die Hauptsitzung führte die Pakete per Merge zusammen (Konflikte nur additiv bzw. in `audit verify`, siehe Merge-Commits) und prüfte den zusammengeführten Stand.

## 2. Commits (ohne Merges)

| Commit | Inhalt |
|---|---|
| `d9ec6b9` | Zusatz (b): 0021 lässt sein `ALTER` aus, wenn `mailboxes.virtual_view` schon existiert (`store.migrationRewrites`; Werte außer 0/1 brechen ab) |
| `22d4483` | Migration 0022: Höchststand der IDs (`meta`, Trigger, `store/ids.go`), `action_targets.source_path` |
| `8af1ea1` | Zusatz (d): `TestConcurrentWritersOnOneFile` mit großzügiger Wartezeit (`store.OpenWith`) |
| `51375c8` | V3 (Policy bei der Ausführung neu geprüft), V8 (Ziele `junk`/`sent`/`drafts` nur für den Menschen) |
| `7cb7a3e` | Snapshots `.bak` gleich mit 0600, nur die zwei neuesten bleiben (A14) |
| `dd3950f` | `blob.Put` erneuert bei vorhandenem Inhalt das Dateidatum (A14) |
| `d414548` | Quellpfad je Ziel, V11, V17, V19 (Doku), V21 |
| `ec51395` | `retention.purge`, `blobs.gc` (E8, A11, A13.2, A13.3) |
| `19c8cc3` | V14 (Backoff des Jobs bleibt), Zusatz (c) (Ordnerabgleich nur, wenn der Job läuft) |
| `a7a26a4` | V16 (Tags mit Namen von System-Keywords bleiben lokal) |
| `732fa6a` | V18 (`too_soon` bei zweiter Sync-Anforderung binnen 60 s) |
| `e2cae84` | `accounts.remove` lehnt bei offenen Entwürfen oder Aktionen ab, `--force` nur für den Menschen (A14) |
| `81d70e4` | Endungsprüfung sieht Windows-Tricks (`x.bat.`, `::$DATA`, …; A14) |
| `7fb3d2f` | `backup.create`, `restore` mit neuer Identität (E7, A10) |
| `a9c6630` | `audit.rotate` mit Archivdatenbank und Kettenanker (E8, A14) |
| `3e4e1f2` | `attachments.save`, `messages.export` (E6) |
| `5834fb0` | Abnahme D7 (`abnahme_D7.ps1`/`.md`, `abnahme_D7_restore.sh`) |

## 3. Zusätze der Prüfsitzung

- **(a) V8 entschieden** (Betreiber 28.10.2026): `move` nach Rollen `junk`, `drafts`, `sent` ist für Schlüssel gesperrt (`denied (target_role_human_only)`, auch per Pfad), der Mensch darf weiter (`TestMoveToJunkDraftsSentIsHumanOnly`).
- **(b)** Erledigt ohne Änderung der Datei 0021 (`TestMigration0021WithExistingColumn`, vorher rot; `TestMigration0021ExistingColumnWithBadValues`).
- **(c)** Erledigt: Ordnerabgleich nur im ausgeführten Job (`TestWatchSendsNoListWhileActionJobBacksOff`: vorher 22 LIST, jetzt 2).
- **(d)** Erledigt: Ursache ist die nicht faire Warteschlange von SQLite unter Volllast; der Test prüft Warten und Vollständigkeit, nicht die Frist, und öffnet deshalb mit 2 min (`store.OpenWith`); der Betrieb behält 5 s. Nebenbei stabilisierte Paket D `TestWatchAppliesActionsBetweenRounds` (vorher 2–4 von 20–30 Läufen rot unter Last: bei Strg+C fehlten die Zahlen der gerade beendeten Aktionen; 0 von 30 danach).
- **(e)** Release aus sauberem Arbeitsbaum, `go version -m` im Abschnitt 5.
- **(f)** `make check` lief nie parallel zu `make race`; Images über `mirror.gcr.io`; `make vuln` siehe Abschnitt 5.

## 4. Festlegungen (Standardantworten von Claude, Einspruch des Betreibers möglich)

**Basis und Zusätze**

1. IDs: Höchststand in `meta` (`id_floor_messages`, `id_floor_mailboxes`) statt Neubau der größten Tabellen (Default des Auftrags); neue IDs vergibt der Store ausdrücklich oberhalb von `max(größte ID, Höchststand)`. Nur `messages` und `mailboxes` (die Tabellen aus A14).
2. Zwischenbuild-Datenbanken (Zusatz b): Der Läufer passt das SQL einer Migration an (`migrationRewrites`), statt eine Datei auf `main` zu ändern.
3. `store.DefaultBusyTimeout` bleibt 5 s; nur der Test nimmt mehr.

**Export (Paket A)**

4. Nachrichten ohne Serverkopie unter `_imported/<Import-Ordner>`, ganz ohne Ordner unter `_no_folder`; mbox-Ablage wie Thunderbird (`Archiv.sbd/2024`, Dateien ohne Endung).
5. Ein relatives Ziel der Operation gilt unter `export_dir`; die CLI macht `--to` vorher absolut (aktueller Ordner).
6. Ziel nicht neu oder leer (auch Symlink oder Datei) → `denied (target_not_fresh)`; im `--subdir` wird jeder Symlink abgelehnt.
7. Maßgeblich ist die letzte wirksame Endung (`x.exe.pdf` nicht ausführbar, `rechnung.pdf.exe` schon); der Inhalt wird beim Ablegen zusätzlich geprüft.
8. Nachrichten ohne Zeilenende am Schluss stehen am Ende ihrer mbox; mehrere davon bekommen bis auf die letzte ein Zeilenende (`newline_added`).
9. Trennzeile mit Absenderadresse (sonst `MAILER-DAEMON`) und asctime in UTC; `index.csv` mit Spalte `file`, Formelzeichen am Anfang bekommen `'`.
10. Namen höchstens 150 Bytes, Formatzeichen fallen weg, Punkt/Bindestrich am Anfang wird `_`.
11. Inhalt zweimal gelesen (Typ und Hash, dann Schreiben); passt er nicht zum Hash: `blob_damaged`, nie eine halbe Nachricht.
12. Ausgabe von `attachments.save` in der Untrusted-Hülle (Dateiname aus der Mail).

**Backup und Restore (Paket B)**

13. Je Lauf ein neuer Schnappschuss `postkauz-<UTC>.db`; Manifest zuletzt atomar ersetzt, danach alte Schnappschüsse gelöscht.
14. Ein Blob gilt im Ziel als vorhanden bei gleicher Größe und Eintrag im vorigen Manifest (wird nicht neu gelesen; Bitfäule findet `restore --dry-run`).
15. Ziel neu, leer oder eine frühere Sicherung dieses Programms; nie im oder über dem Datenverzeichnis.
16. `--tar -`: Schnappschuss kurz unter `<datadir>/backup-tmp/`, nie inkrementell, nie in ein Terminal; Ausgabe auf stderr.
17. Audit nennt das Ziel nur als `target_hash`; Zähler im Systemereignis `backup.done`.
18. `restore` verlangt `--data-dir` ausdrücklich; Standard- und konfiguriertes Datenverzeichnis und Ordner darin sind verboten.
19. A10 ohne `--keep-identity`: Credential-Refs zeigen auf die neue Kennung, `credential_refs_left` wird gelöscht (sonst zöge `MoveCredentialRefs` den Eintrag des Originals um).
20. Nach dem Restore: Jobs `sync`/`draft_push`/`mailbox_action` → `failed (restored)`, Aktionen abgeschlossen, Anträge abgelehnt, `push_attempt` geleert, wartende Entwürfe `push_hold = restored`, offene Audit-Starts `error (restored)`; `audit_exports.location` geleert, `audit verify` prüft diese Tage nur gegen die Datenbank (`origin_days`).
21. Pfade mit `\` im Manifest abgelehnt; Manifest streng geparst; neues `restore --dry-run`; Fehlercodes `backup_target`, `backup_invalid`, `blob_corrupt` (Exit 1).

**Löschkonzept (Paket C)**

22. Purge-Regel ohne neue Spalte (A13.2): nur ohne Zeile in `message_mailboxes` **und** ohne Zeile in `message_import_folders` und mit `deleted_remote_at` vor dem Stichtag.
23. `purge` lässt jede Nachricht stehen, auf die ein Entwurf verweist (`in_reply_to_msg`, `sent_message_id`), auch bei geschlossenen Entwürfen.
24. `--older-than` überschreibt `[retention] deleted_remote_after` in beide Richtungen; ohne beides Exit 2.
25. Die drei Operationen sind R2 (nicht R3): R3 hieße `needs_approval` auch für den Menschen; Schutz ist `HumanOnly`.
26. `purge` und `blob gc` holen ohne Warten alle Kontosperren (sonst `sync_running`); Trockenlauf ohne Sperre.
27. `blob gc`: erst Zeile (mit erneuter Verweisprüfung im SQL), dann Datei; Mindestalter mindestens 1 h, Standard 24 h; ohne Datei zählt das Alter der Zeile. Misslingt das Erneuern des Dateidatums in `blob.Put`, ist das kein Fehler.
28. „Per audit verify bestätigt“: `audit rotate` führt `verify` selbst aus und verlangt `ok`.
29. Archive in der Tabelle `audit_archives` (append-only); eine Archivdatei ohne Eintrag (Abbruch) ersetzt der nächste Lauf.
30. Archivierte Einträge zeigen `audit`/`mail_audit_log` nicht mehr; `verify` und `export` lesen sie weiter. `doctor --blobs` sagt „without reference“ (JSON-Feld bleibt).
31. `[audit] retain` mindestens 1 Tag; `config.Duration` nimmt `365d`, Go-Dauern oder 0; `Operation.AuditCounts` ergänzt den Abschluss-Eintrag um Zähler.

**Aktionen und Konten (Paket D)**

32. V8: Grund `target_role_human_only` (Liste `policy.HumanOnlyMoveRoles`), Prüfung in der Policy.
33. V3: Es gilt die beim Start des ausführenden Prozesses geladene Policy, auch nach menschlicher Freigabe; ohne Regeln nichts (`policy_rules_missing`). Grenzen zählen das Größere aus Zielzahl und `confirm_count`; Keyword-Aktionen speichern `confirm_count`.
34. V11: Fehlt eine UID in COPYUID → `not_found`; weder COPYUID noch EXPUNGE → alle `not_found`; EXPUNGE ohne COPYUID → wie bisher `ok`.
35. V17 und Quellpfad: Ergebnis je Ziel bleibt `stale` (feste CHECK-Liste), der Grund steht an der Aktion: `no_local_copy` bzw. `source_changed`; gespeichert wird der Pfad aus dem Plan; auch die Freigabe prüft ihn.
36. V14: kein neuer Job, solange einer offen ist oder ein gescheiterter seine Wartezeit nicht hinter sich hat. (c): Fehler im Ordnerabgleich lässt den Job mit Wartezeit scheitern.
37. V16: reservierte Namen `junk`, `nonjunk`, `notjunk`, `$junk`, `$nonjunk`, `$notjunk`, `$forwarded`, `$mdnsent`, `$phishing`, `$notphishing`, `$submitpending`, `$submitted`, `$important`, `$label1`–`$label5` (ohne Groß-/Kleinschreibung); Status `local_only`.
38. V18: Mindestabstand 60 s ab der letzten Anforderung des Kontos; ein noch wartender Job nimmt sie auf; sonst `too_soon` mit `retry_after` (auch für den Menschen, Exit 0).
39. `accounts.remove`: Gründe `open_drafts`, sonst `open_actions`; Prüfung auch im Trockenlauf und erneut in der Lösch-Transaktion; `force` mit Schlüssel → `human_only` (nur `force` ist HumanOnly).

**Abnahme**

40. D7.7 abweichend von A03: `purge --older-than 0s` zählt importierte Mails nur als `kept (imported archive)` (A13.2); D7 lässt es deshalb auch echt laufen (löscht 0).
41. Die D7-Sicherung wird mit `tar.exe` gepackt (Pfade mit `/`), nicht mit `Compress-Archive`.

## 5. Prüfläufe

*(wird nach den Läufen ergänzt)*

## 6. Offen

- Abnahme D7 auf dem Büro-PC (Prüfsitzung), Restore im Linux-Container und auf dem Heim-PC, Thunderbird-Prüfung durch den Betreiber (Akzeptanzkriterium 4).
- Laufzeit von `purge` an einer **Kopie der echten Datenbank** (Auftrag T3, IDs-Punkt): hier nur synthetisch gemessen (15.000 von 50.000 Nachrichten in 8,5 s).
- Verhalten von Gmail, Stalwart, GreenMail bei MOVE einer verschwundenen UID (V11): COPYUID/EXPUNGE ungemessen.
- Keyword-Aktionen von vor T3 mit mehr als 50 IDs haben kein `confirm_count`; V3 kann sie mit `confirm_count_required` ablehnen.
- `doctor --secrets` durchsucht `audit_archives`, nicht die Archivdateien selbst.
- Das Manifest einer Sicherung ist nicht signiert; eine passend geänderte Kombination aus Datenbank und Manifest fällt nur über `backup.done` im Audit des Originals auf.
- Eine Kopie, die in den Exportordner des Originals exportiert, kann dort Dateien ersetzen; die Anleitung verlangt einen eigenen Exportordner.
