# A03 in Teilen: Schnitt 3 in Cloud-Sitzungen (Stand 27.10.2026)

Für: die Claude-Code-Cloud-Sitzungen, die Schnitt 3 bauen, und die Prüfsitzung des Betreibers, die sie startet, gegenprüft, zusammenführt und abnimmt.
Grundlage: [A03_s3.md](A03_s3.md) mit den Entscheiden E1 bis E13, Teil A, Teil C (S3.0 bis S3.8) und Teil D (D7 bis D10). **Dieser Plan ändert Reihenfolge, Schnitte und Haltepunkte und ergänzt die in den Teilen genannten Punkte** (roadmap.md, Entschieden 48). Er ersetzt den Satz „Reihenfolge wie Abschnitt 3“ in E13, „Reihenfolge einhalten“ in Teil C und den „Vorschlag zur Schnittfolge“ aus dem Nachtrag vom 24.10.2026. Alles andere aus A03 gilt unverändert; wo dieser Plan nichts sagt, gilt A03.

## Warum diese Reihenfolge

- Seit dem 26.10.2026 läuft 0.2.0-rc8 beim Betreiber im Dauerbetrieb (zwei Gmail-Konten, Schema 16, Datenbank rund 1,4 GB, mehr als 50.000 Nachrichten; dazu ein MCP-Server für Claude). Fehler, die schon in Schnitt 2 wirken, kommen zuerst (T1a).
- Der Abnahmemaßstab des Betreibers (roadmap.md) braucht als Nächstes Tags und „Label entfernen“ (Vorgänge 1 und 5) und gesicherten Dauerbetrieb (Vorgang 6): T2, dann T3.
- Ein weiteres Postfach liegt bei Microsoft (IMAP nur mit OAuth2): T4 läuft parallel, ohne Migration.
- Fernzugriff über HTTP-MCP (T5) braucht erst ein Betrieb auf einem VPS; der Betriebsentscheid fällt frühestens nach sieben Tagen Dauerbetrieb. Der Spike läuft trotzdem früh, weil er das größte technische Risiko klärt.

## Rahmen für alle Teile

1. **Start:** je Teil eine frische Cloud-Sitzung auf diesem Repository (Opus 5.5, Effort hoch, Modus Auto). Die Hauptsitzung orchestriert; Code darf in Subagenten mit eigenen Worktrees entstehen (bewährt in S2). Startprompt am Ende dieser Datei.
2. **Pflichtlektüre:** `CLAUDE.md`, A03 Abschnitt 0 und 1, diese Datei (Rahmen und eigener Teil), dann die im Teil genannten Abschnitte aus A03 und der Quelltext der berührten Pakete. Zeilenangaben in A03 stammen teils vom Stand `50b37e4`; maßgeblich ist der Code.
3. **Zweig:** Die Sitzung arbeitet auf ihrem eigenen Arbeitszweig (Präfix `claude/` ist in Ordnung) vom genannten Ausgangsstand. **Kein Merge nach `main`, kein Tag, kein GitHub-Release.** Am Ende den Zweig pushen. Zusammenführen und Rebase macht die Prüfsitzung nach der Gegenprüfung, mit vollem `make check race integration`. Zwischenstände bekommen keinen Tag; der erste Tag ist `v0.3.0-rc1` nach D7 bis D9 (E13).
4. **Migrationen:** Nummern sind fest vergeben, damit nichts neu nummeriert werden muss (`schema_migrations` vergleicht nur die Nummer, Lücken sind verboten): **T1a 0017, T1b ab 0018**; T2, T3 und T5 nehmen die nächste freie Nummer auf ihrem Ausgangsstand; **T1c und T4 schreiben keine Migration** (`auth_type = 'oauth2'` ist seit 0001 erlaubt). Eine Migration, die auf `main` liegt, wird nie geändert. Jede Migration: Snapshot vorher (vorhanden), Laufzeit gemessen an einer synthetischen Datenbank mit 50.000 Nachrichten und 20.000 Audit-Zeilen, Ergebnis im Protokoll; keine Migration verwirft Daten.
5. **Nie auf die Live-Datenbank:** Builds, die nicht von `main` stammen, laufen nur in eigenen, frischen Datenverzeichnissen. Jedes Abnahme-Skript verlangt `-DataDir` und `-Config` als Pflichtparameter und bricht ab, wenn der Pfad dem Standard-Datenverzeichnis oder einem als „live“ übergebenen Pfad entspricht.
6. **Toolchain:** Go 1.27.3 nach `CLAUDE.md` (`GOTOOLCHAIN=local`). Fehlt sie im Container, gilt `CLAUDE.md`: melden und anhalten, bis der Betreiber über eine Ausnahme entschieden hat (Rückfrage 27.10.2026).
7. **Testserver:** `make testmail-up`. Fehlt der Docker-Daemon, `dockerd` von Hand starten (bisheriger Weg). Geht Docker gar nicht, laufen die Integrationstests gegen Dovecot aus dem Paketmanager; GreenMail und Stalwart gelten dann als „übersprungen“ mit Grund im Protokoll, Dovecot ist Pflicht.
8. **Nie echte Postfächer, nie echte Zugangsdaten** (A03 Abschnitt 0). PII-Gate grün.
9. **Je Commit** `make check`; vor dem Push `make race`, `make integration`, `make vuln`, `make cross`.
10. **Stand und CHANGELOG:** Teile, die von `main` nach dem letzten Merge starten (T1a, T1b, T2, T3), pflegen `docs/roadmap.md` und `CHANGELOG.md`. **Parallel laufende Teile (T1c, T4, T5-Spike) schreiben Stand und CHANGELOG-Zeilen nur nach `docs/protokolle/<Teil>_stand.md`;** die Prüfsitzung überträgt sie beim Merge.
11. **Entscheiden statt fragen:** Die Sitzung läuft unbeaufsichtigt. Bei offenen Detailfragen den Default nehmen und in der Roadmap (bzw. `<Teil>_stand.md`) als „Festlegung (Einspruch möglich)“ eintragen. Anhalten nur nach der Stopp-Regel aus A03 Abschnitt 0 und am Haltepunkt.
12. **Haltepunkt heißt:** Abnahme-Skript liegt bei, Zweig ist gepusht, Sitzung endet. Ausgeführt wird die Abnahme von der Prüfsitzung auf dem Büro-PC; die Cloud-Sitzung wartet nicht darauf.
13. **Abnahme-Skripte:** `docs/abnahme_<Name>.ps1` plus `.md` (Muster `abnahme_D4.ps1`): ohne Go und Docker, nur mit dem Programm aus `dist/`; Pfade des Betreibers sind Parameter, nie im Repository; je Schritt OK/FAIL gegen „Erwartet“.
14. **Ende jedes Teils:** Build mit `make release VERSION=<siehe Teil>` in `dist/`, letzte Meldung nach `CLAUDE.md` „Abschluss“ mit Commit-Liste, Festlegungen und höchstens drei Fragen mit Default.

## Übersicht

| Teil | Inhalt | Ausgangsstand | Haltepunkt |
|---|---|---|---|
| T1a | Härtung Dauerbetrieb, UID-Sicherheit | `main` nach D6 | Build |
| T1b | Audit, Jobs, Policy (Teil A Kern) | `main` nach Merge T1a | Abnahme T1 |
| T1c | Windows-Passwortabfrage, Prüfwerkzeuge, Kleines | `main` nach D6, parallel | Build |
| T2 | S3.1, S3.2, Windows-Dauerbetrieb | `main` nach Merge T1b | Abnahme D7a |
| T3 | S3.3, S3.4 | `main` nach Merge T2 | Abnahme D7 |
| T4 | S3.7 Microsoft | `main` nach D6, parallel | Abnahme D9 |
| T5 | Phase 1 Spike, Phase 2 S3.5 und S3.6 | Spike: `main` nach D6, parallel; Bau: nach ADR-010 und Merge T2 | ADR-010, später D8 |

Zusammenführen in dieser Reihenfolge: T1a → T1c → T4 → T1b → T2 → T3 → T5. S3.8 (Abschluss, `0.3.0-rc1`) übernimmt der zuletzt zusammengeführte Teil. D10 bleibt beim Betreiber.

## T1a — Härtung des Dauerbetriebs und UID-Sicherheit (Migration 0017)

1. **A6 mit A13.4:** Importe liegen schon getrennt (`import_folders`, `message_import_folders`, ohne UID); UID 0 entsteht nur in der Abfrage `MessageByID().Locations` (Rolle `import`). Der CHECK `uid > 0` gilt für `message_mailboxes.uid` (Tabellenumbau mit `-- postkauz:foreign_keys=off`); vorher prüft die Migration `count(*) WHERE uid <= 0` = 0 und bricht sonst mit Meldung ab. Aktionen lesen Ziele nie aus `Locations`. UIDVALIDITY in den Schreibvertrag (`provider.go`).
2. **UID 4294967295:** In go-imap läuft `Range.append` bei diesem Wert endlos. Gesendete UID-Sets: Wert vor jedem Kommando abweisen. **Empfangene** UID-Sets (ESEARCH ALL, `imapx/mailbox.go` mit `set.Nums()`): nie über `Nums()` auflösen, sondern bereichsweise mit Obergrenze (Stop < 2^32−1, Summe ≤ EXISTS). Test mit `1:4294967295` vom Fake-Server.
3. **Leere oder unvollständige Antworten löschen nichts:**
   - LIST liefert keine Ordner oder INBOX fehlt, obwohl Ordner bekannt sind → Lauf abbrechen, nichts entfernen, Warnung und Zähler (heute löscht `SyncMailboxes` alle Ordner und löst alle Zuordnungen; danach Vollabgleich, Tageslimit des Anbieters).
   - `UID SEARCH ALL` liefert eine andere Zahl als EXISTS aus demselben SELECT → Ordner in diesem Lauf überspringen, nichts lösen, Warnung und Zähler (fängt leere und Teil-Listen).
   - Fake-Server-Tests für beide Fälle.
4. **Später Abschluss-Eintrag schreibt alte Exporttage neu** (A14): Export und Kette so, dass ein verspäteter Eintrag einen schon exportierten Tag nicht verändert (Test mit `sync --watch`-Lauf über Mitternacht UTC).
5. **`Session.Do`:** wiederholt Schreibbefehle nur, wenn der Aufrufer Idempotenz zusichert (APPEND mit Message-ID und `findCopy`, wie architektur.md Abschnitt 7); APPEND ohne Message-ID, STORE und MOVE nie. Leere UID-Liste trennt die Verbindung nicht mehr.
6. **A12.4** (Funde des Fremdmodell-Laufs, A03 Nachtrag 27.10.2026): Kopf-Abruf mit Größengrenze, Serverfehler auf feste Codes vor jeder Ausgabe, FIFO in `attach_dir`, Kontolöschung atomar, Bedrohungsmodell Message-ID/Größe in `docs/sicherheit.md`, Audit-Export ersetzt keine fremden Dateien, Maildir-Testnamen unter Windows.
7. **Schutz vor zwei Versionen:** `Migrate` verweigert, solange eine Kontosperre (`mailsync/lock.go`) gehalten wird, und bricht ab, wenn der Name einer angewendeten Migration nicht zur eingebetteten passt. S3.0 Punkt 4 (Schema-Plan für S3 in `docs/datenmodell.md`, mit den festen Nummern aus Rahmen 4).

**Build:** `0.3.0-alpha.1` (nur für Tests der Prüfsitzung, nicht für den Dauerbetrieb).

## T1b — Audit, Jobs, Policy (Migrationen ab 0018)

A1 (Variante A, entschieden: R3; die Abschlusszeile trägt einen eigenen Zeitstempel), A7, A8, A9, A2, A4, dazu: Risikostufe `sync.run` und `drafts.request_ready` R2 · `Redact` vor der Schemaprüfung · fehlgeschlagene Schlüsselanmeldung ins Audit · MCP loggt und meldet keine rohen Fehlertexte · Doku-Drift (Nachtrag 24.10.2026).
**Akzeptanz:** wie A03; `audit verify` vor und nach der Migration `ok` (Pflichttest A1); Vertragstest „ein Eintrag bei Policy-Ablehnung, zwei bei Erlaubnis“.
**Build:** `0.3.0-alpha.2`.
**Haltepunkt, Abnahme T1** (`docs/abnahme_T1.ps1` und `.md`; die Prüfsitzung führt sie aus):

1. **Vorbereitung:** Freier Platz mindestens dreimal die Größe der Datenbank. Kopie nur bei gestopptem Dauerläufer: Aufgabe `sync --watch`, Aufgabe Audit-Export und MCP-Server in den Claude-Konfigurationen (Prozessliste prüfen; Datenbank, `-wal` und `-shm` zusammen kopieren).
2. **Altstand auf der Kopie mit 0.2.0-rc8:** `audit verify`, feste Suchen, `accounts list`, Zähler je Ordner. Danach mit `0.3.0-alpha.2` dieselben Kommandos plus `doctor`; Ergebnisse identisch, Migrationen laufen, Dauer gemessen.
3. **Lesender Sync** der Kopie gegen ein Konto (nur INBOX) nur, wenn `draft list` keine wartenden Entwürfe und keinen offenen Push zeigt (sonst legt `sync` Entwürfe im echten Postfach ab).
4. **Umstellung des Dauerbetriebs** erst nach dem Ja des Betreibers, mit allen rc8-Prozessen gestoppt. **Rückweg** über den Snapshot nur bis zum ersten Audit-Export mit der neuen Version; danach nur vorwärts.

## T1c — Windows-Passwortabfrage, Prüfwerkzeuge, Kleines (ohne Migration)

A3 (Diagnose-Kommando und Leser; die Messmatrix macht die Prüfsitzung auf dem Büro-PC, der Betreiber tippt nur auf Aufforderung) · A5.2 bis A5.6 · A12.1 bis A12.3 (golangci-lint im Berichtsmodus mit eingestuftem Erstlauf, CodeQL, Pre-Commit-Hook) · Ordnernamen und Serverfehler gefiltert ins Terminal · Secret-Scan mit JWT-Muster · Message-ID-Suche bei Dubletten eindeutig · `ParseSince` mit Überlaufprüfung · Guard ohne `vendor/`.
**Build:** `0.3.0-alpha.c1` (nur Abnahme A3).

## T2 — Tags, Markieren, Verschieben, Papierkorb, Windows-Dauerbetrieb

S3.1 und S3.2 wie A03 Teil C (E1 bis E5), dazu:

1. A5.1 (`[draft] mailbox` je Konto; Migration mit S3.1) · Tags-Schema an E5 anpassen.
2. A13.1 (Blocker: `imapclient.Move` weicht ohne MOVE still auf Löschen aus) und aus A14: Trockenlauf hat Vorrang vor R3 · Aktionen an (Nachricht, Ordner, UID, UIDVALIDITY) binden · „Ziele nur aus Leseergebnissen“ als Code; Akzeptanz: kein Ziel aus `Locations` · `remote_previous` nicht allein für `drafts.cleanup_copies` (nur Kopien mit eigenem Hash) · Randwert und Obergrenze von `bulk_confirm_threshold` · MCP-Text „never … moves mail“ an die Scopes binden (Test).
3. **Verknüpfung über Message-ID und Größe** (Bedrohungsmodell aus T1a): Vor jeder Aktion auf einer Serverkopie, deren Inhalt PostKauz nie selbst geladen hat, lädt der Job den Kopf (`BODY.PEEK[HEADER]`) und vergleicht ihn mit dem lokalen; Abweichung → `stale`, nichts passiert.
4. **„Label entfernen“ (Vorgang 1):** In Gmail entspricht das dem Verschieben aus dem Label-Ordner in den Ordner mit Rolle `all` (Indiz, im Testserver nicht prüfbar). `move --role all` ist unter `archive_only` erlaubt (E4); Doku in `README.md` und `docs/cli.md`; gemessen in D7a.
5. **Schlüssel für die Nacht-Triage (Vorgang 5):** Scope-Satz „lesen + Entwurf + Tag“ ohne `mail:move`/`mail:trash`; Vertragstest beweist, dass er nichts anderes kann.
6. **Windows-Dauerbetrieb** (aus S3.5 vorgezogen): `deploy/windows/Register-SyncWatch.ps1` mit Wrapper (Neustart nach Absturz mit Backoff 60 bis 1.800 s, Log-Rotation, kein Passwort im Skript, Regeln wie `Register-AuditExport.ps1`), `sync --watch --all` (Goroutine je Konto, Sperre je Konto), Operation `sync.request` mit MCP `mail_sync_now` (E1), `docs/betrieb.md` Teil Windows.

**Build:** `0.3.0-alpha.3`.
**Haltepunkt, Abnahme D7a** (`docs/abnahme_D7a.ps1`): D7 Schritte 1 bis 3 und 6 offline; danach am echten Konto nur mit Zustimmung des Betreibers je Schritt und nur an Mails, die er sich selbst geschickt hat: taggen, als gelesen markieren, in einen Label-Ordner verschieben, Label wieder entfernen, in den Papierkorb mit Freigabe (nicht endgültig). Diese Schreibvorgänge erlaubt Entschieden 48 ausdrücklich.

## T3 — Export, Anhänge, Backup, Restore, Löschkonzept

S3.3 und S3.4 wie A03 Teil C (E6 bis E8), dazu A10, A11, A13.2, A13.3 und aus A14: Migrations-Snapshots `.bak` aufräumen (die letzten zwei behalten) und gleich mit Rechten 0600 anlegen · `blob.Put` mit `Existed` erneuert das Dateidatum · Audit-Kette nach Rotation mit Anker · Purge und GC brechen offene Entwürfe nicht · Endungsprüfung (`x.bat.`, `::$DATA`) · keine Wiederverwendung von IDs nach Löschungen, vor dem ersten Purge (Default: Höchststand in `meta` statt Umbau der größten Tabellen; Laufzeit an einer Kopie der echten Datenbank messen) · `accounts.remove` lehnt bei offenen Entwürfen ab (`--force` nur für Menschen). Kleineres: Export-Dateinamen gegen Windows-Reservierungen (Fuzz).
**Build:** `0.3.0-alpha.4`.
**Haltepunkt, Abnahme D7** (A03 Teil D, Schritte 4, 5 und 7; Restore im Container der Prüfsitzung und auf dem Heim-PC).

## T4 — Microsoft-Postfächer per OAuth2 (parallel, ohne Migration)

S3.7 wie A03 Teil C (E11, entschieden: R2 — OAuth2-Konten nur auf Rechnern mit Schlüsselbund), dazu:

1. Windows-Schlüsselbund nimmt höchstens 2.560 Byte je Eintrag (`go-keyring`). Default: nur Refresh-Token und Ablauf speichern, Access-Token nur im Speicher; ist der Refresh-Token länger, auf nummerierte Einträge verteilen. Test mit 4-KB-Token.
2. Scheitert das Speichern eines neuen Refresh-Tokens, bricht der Sync laut ab (nicht still im Speicher weiter); Token-JSON in Ausgaben gezielt schwärzen (`secrets.go`).
3. Anleitung als **`docs/betrieb_microsoft.md`** (App-Registrierung, markiert „Stand Oktober 2026, ungeprüft“); `docs/betrieb.md` legt T2 an, die Prüfsitzung führt beides beim Merge zusammen.

**Build:** `0.3.0-ms.1`.
**Haltepunkt, Abnahme D9** (A03 Teil D) in einem eigenen Datenverzeichnis (`-DataDir`, nie das Live-Verzeichnis); App-Registrierung und Anmeldung mit dem Betreiber, nur lesend.

## T5 — Fernzugriff: Spike, HTTP-MCP, OAuth 2.1

- **Phase 1 (parallel ab D6):** S3.0 Punkte 2 und 3: Spike P-AUTH nach der Prüfliste in A03, Protokoll `docs/protokolle/S30_pauth.md`, ADR-010 als Entwurf in `docs/protokolle/ADR-010_entwurf.md` (nach `docs/entscheidungen.md` erst nach dem Ja). Spike-Code nicht nach `main`. **Haltepunkt:** ADR-010 braucht das Ja des Betreibers.
- **Phase 2 (eigene Sitzung nach ADR-010 und Merge von T2):** S3.5 (zuerst Modus `auth = "key"`, entschieden: R1; `sync --watch --all`, `mail_sync_now` und `Register-SyncWatch.ps1` liegen dann schon aus T2 vor) und S3.6.
- **Build:** `0.3.0-alpha.<n>` nach dem letzten Merge. **Haltepunkt, Abnahme D8** vorläufig mit einem PC des Betreibers als Knoten im privaten Netz, bis über einen VPS entschieden ist.

## Startprompt (für jede Cloud-Sitzung; `<Teil>` und `<Commit>` ersetzen)

```
PostKauz, Auftrag A03, Teil <Teil>, Ausgangsstand main <Commit>. Lies CLAUDE.md,
dann auftraege/A03_teile.md (Abschnitte „Rahmen für alle Teile" und „<Teil>") und
die dort genannten Abschnitte aus auftraege/A03_s3.md. Arbeite den Teil vollständig
ab und halte am genannten Haltepunkt an: Skript liegt bei, Zweig ist gepusht,
Sitzung endet; die Abnahme führt die Prüfsitzung aus. Keine echten Postfächer,
keine echten Zugangsdaten, nie die Live-Datenbank. Du arbeitest unbeaufsichtigt:
Detailfragen mit Default entscheiden und festhalten (Rahmen 10 und 11); anhalten
nur nach der Stopp-Regel aus A03 Abschnitt 0. Nur deinen Arbeitszweig pushen,
nichts nach main.
```
