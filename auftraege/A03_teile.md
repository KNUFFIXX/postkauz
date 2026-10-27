# A03 in Teilen: Schnitt 3 in Cloud-Sitzungen (Stand 27.10.2026)

Für: die Claude-Code-Cloud-Sitzungen, die Schnitt 3 bauen, und die Prüfsitzung des Betreibers, die sie startet, gegenprüft, zusammenführt und abnimmt.
Grundlage: [A03_s3.md](A03_s3.md) mit den Entscheiden E1 bis E13, Teil A, Teil C (S3.0 bis S3.8) und Teil D (D7 bis D10). **Dieser Plan ändert nur Reihenfolge, Schnitte und Haltepunkte, nicht den Inhalt der Schritte.** Er ersetzt den „Vorschlag zur Schnittfolge“ aus dem Nachtrag vom 24.10.2026.

## Warum diese Reihenfolge

- Seit dem 26.10.2026 läuft 0.2.0-rc8 beim Betreiber im Dauerbetrieb (zwei Gmail-Konten, Schema 16, Datenbank rund 1,4 GB, mehr als 50.000 Nachrichten). Fehler, die schon in Schnitt 2 wirken, kommen deshalb zuerst (T1).
- Der Abnahmemaßstab des Betreibers (roadmap.md) braucht als Nächstes Tags und „Label entfernen“ (Vorgänge 1 und 5) und gesicherten Dauerbetrieb (Vorgang 6): T2, dann T3.
- Ein drittes Postfach liegt bei Microsoft (IMAP nur mit OAuth2): T4 läuft parallel, weil es kaum Code der anderen Teile berührt.
- Fernzugriff über HTTP-MCP (T5) ist für den Betrieb auf einem VPS nötig. Der Betriebsentscheid dazu fällt frühestens nach sieben Tagen Dauerbetrieb, also nachrangig. Der Spike läuft trotzdem früh, weil er das größte technische Risiko klärt.

## Rahmen für alle Teile

1. **Start:** je Teil eine frische Cloud-Sitzung auf diesem Repository (Opus 5.5, Effort hoch, Modus Auto). Die Hauptsitzung orchestriert; Code darf in Subagenten mit eigenen Worktrees entstehen (bewährt in S2). Startprompt am Ende dieser Datei.
2. **Pflichtlektüre:** `CLAUDE.md`, A03 Abschnitt 0 und 1, diese Datei (Rahmen und eigener Teil), dann die im Teil genannten Abschnitte aus A03 und der Quelltext der berührten Pakete.
3. **Zweig:** Die Sitzung arbeitet auf ihrem eigenen Arbeitszweig (Präfix `claude/` ist in Ordnung) vom genannten Ausgangsstand. **Kein Merge nach `main`, kein Tag, kein GitHub-Release.** Am Ende den Zweig pushen. Zusammenführen, taggen und veröffentlichen macht die Prüfsitzung nach der Gegenprüfung.
4. **Migrationen:** nächste freie Nummer auf dem Ausgangsstand. Parallel laufende Teile (T4, T5) nummerieren beim Rebase auf das dann aktuelle `main` neu; eine Migration, die schon auf `main` liegt, wird nie geändert. Jede Migration läuft auf großen Beständen: Snapshot vorher (vorhanden), Laufzeit gemessen an einer synthetischen Datenbank mit 50.000 Nachrichten und 20.000 Audit-Zeilen, Ergebnis im Protokoll. Keine Migration verwirft vorhandene Daten. Eine alte Programmversion, die eine neuere Datenbank öffnet, bricht mit klarer Meldung ab (prüfen, Test).
5. **Toolchain:** Bringt der Container nur ein älteres Go mit, die Version aus `go.mod` (go1.27.3) einmal über den offiziellen Weg holen (`GOTOOLCHAIN=go1.27.3 go version`, prüfsummengesichert über den Go-Proxy), ihr `bin`-Verzeichnis aus dem Modulcache vor `PATH` legen und danach mit `GOTOOLCHAIN=local` arbeiten. Das ist für diese Version ausdrücklich erlaubt (Festlegung 27.10.2026) und gilt nicht als stilles Nachladen im Sinn von `CLAUDE.md`; jede andere Version: melden.
6. **Testserver:** `make testmail-up`. Fehlt der Docker-Daemon, `dockerd` von Hand starten (bisheriger Weg); geht das nicht, Dovecot per `apt` und GreenMail als Jar nach `deploy/testmail/README.md`. Integrationstests (`POSTKAUZ_IT`) sind Pflicht; „übersprungen“ nur mit Grund im Protokoll.
7. **Nie echte Postfächer, nie echte Zugangsdaten** (A03 Abschnitt 0). PII-Gate grün.
8. **Je Commit** `make check`; vor dem Push `make race`, `make integration`, `make vuln`, `make cross`. Roadmap („Stand S3.x“) und `CHANGELOG.md` je Schritt.
9. **Entscheiden statt fragen:** Die Sitzung läuft unbeaufsichtigt. Bei offenen Detailfragen den Default nehmen und in `docs/roadmap.md` als „Festlegung (Einspruch möglich)“ eintragen. Anhalten nur nach der Stopp-Regel aus A03 Abschnitt 0 (Widerspruch zu einem ADR oder Entscheid) und an den Haltepunkten unten.
10. **Abnahme-Skripte:** Jede Windows-Abnahme liefert der Teil als `docs/abnahme_<Name>.ps1` plus `.md` (Muster `abnahme_D4.ps1`): läuft ohne Go und ohne Docker, nur mit dem Programm aus `dist/`; Pfade des Betreibers sind Parameter, nie im Repository; Ausgabe je Schritt OK/FAIL gegen „Erwartet“.
11. **Ende jedes Teils:** Build mit `make release VERSION=<siehe Teil>` in `dist/` (nicht taggen), letzte Meldung nach `CLAUDE.md` „Abschluss“ mit Commit-Liste, offenen Festlegungen und höchstens drei Fragen mit Default.

## Übersicht

| Teil | Inhalt | Ausgangsstand | Haltepunkt |
|---|---|---|---|
| T1 | Teil A, Härtung Dauerbetrieb, A12 | `main` nach D6 | Abnahme T1 |
| T2 | S3.1 und S3.2, Windows-Dauerbetrieb | `main` nach Merge T1 | Abnahme D7a |
| T3 | S3.3 und S3.4 | `main` nach Merge T2 | Abnahme D7 |
| T4 | S3.7 Microsoft | `main` nach D6, parallel zu T1 | Abnahme D9 |
| T5 | S3.0-Spike, dann S3.5 und S3.6 | Spike: `main` nach D6; Bau: nach ADR-010 und Merge T2 | ADR-010, später D8 |

S3.8 (Abschluss, 0.3.0-rc1) übernimmt der zuletzt zusammengeführte Teil; liegt der nach dem 04.11.2026, die Prüfsitzung.

## T1 — Teil A, Härtung des Dauerbetriebs, A12

**Inhalt (A03 Abschnitt 2 und Nachträge):**

1. A1 (Variante A, entschieden: R3), A2, A3 (Diagnose-Kommando und Leser bauen; die Messmatrix auf dem Büro-PC macht die Abnahme), A4, A5.1 bis A5.6, A7, A8, A9.
2. A6 zusammen mit A13.4 lösen: Import-Zuordnungen tragen heute UID 0 unter Serverpfaden. Der CHECK aus A6 darf sie nicht brechen; die Lösung (Import-Herkunft getrennt kennzeichnen oder CHECK nur für Serverordner) wählt die Sitzung, Begründung in der Roadmap. Aktionen lesen Ziele nie aus `Locations`. Dazu: UIDVALIDITY im Schreibvertrag (`provider.go`), UID 4294967295 führt in go-imap zu einer Endlosschleife (`imapnum/numset.go`) — vor jedem Kommando abweisen, Test.
3. Aus A14 die Punkte, die schon im Dauerbetrieb wirken:
   - Leere LIST-Antwort sperrt abgelegte Entwürfe dauerhaft auf `missing`.
   - Leeres `UID SEARCH ALL` löst alle Zuordnungen eines Ordners. Regel: Eine leere Antwort bei vorher bekannten Nachrichten gilt erst als Löschung, wenn `STATUS`/`EXISTS` 0 bestätigt; sonst Lauf für den Ordner abbrechen, Zähler und Warnung.
   - Später Abschluss-Eintrag des Dauerläufers schreibt alte Exporttage neu.
4. Kleinere Punkte (Nachtrag 26.10.2026): `Session.Do` wiederholt nur lesende Befehle, nie APPEND/STORE · Ordnernamen und Serverfehler gefiltert ins Terminal · leere UID-Liste trennt die Verbindung nicht mehr · MCP loggt und meldet keine rohen Fehlertexte · fehlgeschlagene Schlüsselanmeldung ins Audit · Secret-Scan mit JWT-Muster · Message-ID-Suche bei Dubletten eindeutig · Risikostufe `sync.run`/`drafts.request_ready` R2 · Doku-Drift (Nachtrag 24.10.2026).
5. **A12 (neu, in A03 als Nachtrag 27.10.2026 beschrieben):** CI-Prüfwerkzeuge und die Funde des Fremdmodell-Laufs vom 25.10.2026.

**Akzeptanz:** wie die Einzelpunkte in A03; zusätzlich Migrationen gemessen (Rahmen, Punkt 4), `audit verify` vor und nach der Migration `ok` (Pflichttest A1), Vertragstest und Kanarientest grün, Fake-Server-Tests für leere LIST/SEARCH.
**Build:** `0.3.0-alpha.1`.
**Haltepunkt, Abnahme T1** (`docs/abnahme_T1.ps1`, Claude führt sie auf dem Büro-PC aus): (1) Kopie des Echtbestands mit dem neuen Programm öffnen: Migration, `doctor`, `audit verify` vorher und nachher; (2) Alt/Neu-Vergleich auf der Kopie: feste Suchen, `accounts list`, Zähler je Ordner identisch; (3) ein lesender `sync` der Kopie gegen ein Konto, nur INBOX; (4) Messmatrix A3, der Betreiber tippt nur auf Aufforderung; (5) Umstellung des Dauerbetriebs erst nach dem Ja des Betreibers, Rückweg = Snapshot.

## T2 — Tags, Markieren, Verschieben, Papierkorb

**Inhalt:** S3.1 und S3.2 wie A03 Teil C (E1 bis E5), dazu:

1. A13.1 (Blocker: `imapclient.Move` weicht ohne MOVE still auf Löschen aus) und aus A14: Trockenlauf hat Vorrang vor R3 · Aktionen an (Nachricht, Ordner, UID, UIDVALIDITY) binden, nicht an die lokale ID allein · „Ziele nur aus Leseergebnissen“ als Code, nicht nur als Doku · `remote_previous` taugt nicht allein für `drafts.cleanup_copies` (nur Kopien mit eigenem Hash anfassen) · Randwert und Obergrenze von `bulk_confirm_threshold`.
2. **Verknüpfung über Message-ID und Größe** (Fremdmodell-Fund 25.10.2026, Bedrohungsmodell): Ein Absender bestimmt beide Werte selbst. Vor jeder Aktion auf einer Serverkopie, deren Inhalt PostKauz nie selbst geladen hat (Verknüpfung oder Toleranz aus rc4/rc5), lädt der Job den Kopf (`BODY.PEEK[HEADER]`) und vergleicht ihn mit dem lokalen; Abweichung → `stale`, nichts passiert. Default so; Abschnitt in `docs/sicherheit.md`.
3. **„Label entfernen“ (Abnahmemaßstab Vorgang 1):** In Gmail entspricht das Entfernen eines Labels dem Verschieben aus dem Label-Ordner in den Ordner mit Rolle `all` (Indiz, im Testserver nicht prüfbar). `move --role all` muss unter `archive_only` erlaubt sein (E4 sieht das vor); Doku in `README.md` und `docs/cli.md`; gemessen wird es in D7a.
4. **Schlüssel für die Nacht-Triage (Vorgang 5):** eine Rolle oder ein Scope-Satz „lesen + Entwurf + Tag“ ohne `mail:move`/`mail:trash`; Vertragstest beweist, dass dieser Schlüssel nichts anderes kann.
5. **Windows-Dauerbetrieb** (aus S3.5 vorgezogen): `deploy/windows/Register-SyncWatch.ps1` mit Wrapper (Neustart nach Absturz mit Backoff 60 bis 1.800 s, Log-Rotation, kein Passwort im Skript, Regeln wie `Register-AuditExport.ps1`), `sync --watch --all` (Goroutine je Konto, Sperre je Konto), `docs/betrieb.md` Teil Windows.

**Build:** `0.3.0-alpha.2`.
**Haltepunkt, Abnahme D7a** (`docs/abnahme_D7a.ps1`): D7 Schritte 1 bis 3 und 6 offline; danach am echten Konto nur mit Zustimmung des Betreibers je Schritt und nur mit Mails, die der Betreiber sich selbst geschickt hat: taggen, als gelesen markieren, in einen Label-Ordner verschieben, Label wieder entfernen, in den Papierkorb mit Freigabe (nicht endgültig).

## T3 — Export, Anhänge, Backup, Restore, Löschkonzept

**Inhalt:** S3.3 und S3.4 wie A03 Teil C (E6 bis E8), dazu A10, A11, A13.2, A13.3 und aus A14: Migrations-Snapshots `.bak` aufräumen (die letzten zwei behalten) und mit Rechten 0600 anlegen · `blob.Put` mit `Existed` erneuert das Dateidatum · Audit-Kette nach Rotation mit Anker · Purge und GC brechen offene Entwürfe nicht · Endungsprüfung (`x.bat.`, `::$DATA`) · keine Wiederverwendung von IDs nach Löschungen (`AUTOINCREMENT` oder gleichwertig, vor dem ersten Purge) · `accounts.remove` lehnt bei offenen Entwürfen ab (mit `--force` für Menschen). Kleinere Punkte: Export-Dateinamen gegen Windows-Reservierungen (Fuzz), `ParseSince` mit Überlaufprüfung, Guard ohne `vendor/`.
**Build:** `0.3.0-alpha.3`.
**Haltepunkt, Abnahme D7** (A03 Teil D, Schritte 4, 5 und 7; Restore im Container der Prüfsitzung und auf dem Heim-PC).

## T4 — Microsoft-Postfächer per OAuth2 (parallel zu T1)

**Inhalt:** S3.7 wie A03 Teil C (E11, entschieden: R2 — OAuth2-Konten nur auf Rechnern mit Schlüsselbund), dazu:

1. Windows-Schlüsselbund nimmt höchstens 2.560 Byte je Eintrag (`go-keyring`). Default: nur Refresh-Token und Ablauf speichern, Access-Token nur im Speicher; ist der Refresh-Token länger, auf nummerierte Einträge verteilen. Test mit 4-KB-Token.
2. Kleinere Punkte: Scheitert das Speichern eines neuen Refresh-Tokens, bricht der Sync laut ab (nicht still im Speicher weiter); Token-JSON in Ausgaben gezielt schwärzen.
3. `docs/betrieb.md` Abschnitt „Microsoft“ (App-Registrierung, markiert „Stand Oktober 2026, ungeprüft“).

Berührt möglichst nur `internal/oauth`, `account add/login/test`, Schlüsselbund und Doku. Braucht das Schema eine Migration (`auth_type = oauth2`), wird sie als letzter Commit geschrieben und beim Rebase neu nummeriert.
**Build:** `0.3.0-ms.1` (nur für die Abnahme).
**Haltepunkt, Abnahme D9** (A03 Teil D; App-Registrierung und Anmeldung mit dem Betreiber, nur lesend).

## T5 — Fernzugriff: Spike, HTTP-MCP, OAuth 2.1

**Phase 1 (parallel ab D6):** S3.0 Punkte 2 und 3: Spike P-AUTH nach der Prüfliste in A03, Protokoll `docs/protokolle/S30_pauth.md`, ADR-010 als Entwurf. Spike-Code nicht nach `main`. **Haltepunkt:** ADR-010 braucht das Ja des Betreibers.
**Phase 2 (eigene Sitzung nach ADR-010 und Merge von T2):** S3.5 (zuerst Modus `auth = "key"`, entschieden: R1) und S3.6. `Register-SyncWatch.ps1` und `sync --watch --all` liegen dann schon aus T2 vor.
**Build:** `0.3.0-alpha.<n>` nach dem letzten Merge.
**Haltepunkt, Abnahme D8** vorläufig mit einem PC des Betreibers als Tailnet-Knoten, bis über einen VPS entschieden ist.

## Startprompt (für jede Cloud-Sitzung, `<Tn>` ersetzen)

```
PostKauz, Auftrag A03, Teil <Tn>. Lies CLAUDE.md, dann auftraege/A03_teile.md
(Abschnitte „Rahmen für alle Teile" und „<Tn>") und die dort genannten Abschnitte
aus auftraege/A03_s3.md. Arbeite den Teil vollständig ab und halte am genannten
Haltepunkt an. Keine echten Postfächer, keine echten Zugangsdaten. Du arbeitest
unbeaufsichtigt: Detailfragen mit Default entscheiden und in docs/roadmap.md
festhalten; anhalten nur nach der Stopp-Regel aus A03 Abschnitt 0. Am Ende nur
deinen Arbeitszweig pushen, nichts nach main.
```
