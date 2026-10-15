Stand: **S1.4 und S1.5 erledigt** (15.10.2026: Sync mit Erstsync in Fenstern, Wiederaufnahme ohne Doppelte, inkrementell mit CONDSTORE bzw. UID-Scan, Löschabgleich per UID SEARCH, UIDVALIDITY-Wechsel mit Abgleich per Message-ID, `sync --watch` mit IDLE; `mailbox list`, minimales `show`; Migration 0002 mit Snapshot; P2-Messung mit 50.000 Nachrichten). **Abnahme D1 durch den Betreiber steht aus** (`docs/abnahme_D1.md`). Danach: S1.6 (Threading, Body-Bereinigung, Anhänge). Am Ende jeder Arbeitssitzung wird dieser „Stand" und `CHANGELOG.md` gepflegt.

| Phase / Schnitt | Inhalt in einem Satz | Status |
|---|---|---|
| Phase 0 | Gerüst, Architektur, ADRs, Testserver, Spike mit `go-imap/v2`, Gate Go vs. Python | **S0 (Spike und Gate) erledigt 14.10.2026**: Gate Go bestanden mit Auflagen, Bericht `spike/s0/REPORT.md`, ADR-007/008. **Schritt 0 (Go 1.27) erledigt 15.10.2026** |
| S1 | Konto, Sync, Spiegel, Threads, Suche, CLI lesend, stdio-MCP lesend, Audit, Import | **S1.1 bis S1.5 erledigt 15.10.2026** (Abnahme D1 offen); S1.6 ff. offen |
| S2 | Entwürfe als draft-only, Policy-Datei, Freigabe-Zustände, Prüf-Hook | offen |
| S3 | Tags und Postfach-Aktionen, Export/Backup/Restore, HTTP-MCP mit OAuth 2.1 | offen |
| S4+ | SMTP-Versand mit Freigabe, Regeln, REST, JMAP, später GUI | nicht begonnen |

Aufwandsschätzung für S1–S3: **40–80 Sitzungen, drei bis sechs Monate** (Code-Anteil schnell, Tests gegen echte Server und Entscheidungen des Betreibers bestimmen das Tempo). Die Schätzung ist grob und wird nach S1 neu gesetzt.

## Phase 0: Gerüst und Gate

**Erledigt:** Repository, Lizenz, ADR-001 bis ADR-006, Architektur-/Datenmodell-/MCP-/CLI-/Sicherheitsdokumente, Go-Gerüst (Register, Executor, Policy-Stub, Audit-Typ, Migrationen mit FTS5, Provider-Interface, CLI mit `version`/`doctor`/`accounts list`), CI, Testserver-Compose.

**S0 erledigt (14.10.2026): Spike und Gate.** `go-imap/v2` v2.0.0-beta.8 besteht gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail: Erstsync in Fenstern (rund 1.000 bis 1.250 Nachrichten/s lokal), Inkrementalsync (CONDSTORE bzw. Vollvergleich), Löschungserkennung, Draft-APPEND mit identischen Bytes, IDLE, Reconnect. **Urteil: Gate Go bestanden mit Auflagen**, Bericht und Rohdaten in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md). Auflagen und Entscheidungen: ADR-007 (go-imap-Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Go-Version). Der Spike-Code ist Wegwerfcode in einem eigenen Go-Modul (`spike/s0`) und nicht Teil von `./...`.

**Schritt 0 erledigt (15.10.2026):** `go.mod` auf `go 1.27` (Toolchain go1.27.2, `GOTOOLCHAIN=local` in Makefile und CI), `modernc.org/sqlite` v1.45.0 → v1.60.1 (SQLite 3.53.4; mitgezogen: `modernc.org/libc` v1.77.1, `golang.org/x/sys` v0.48.0). `make check` und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 4,9 MB, windows/amd64 5,1 MB, darwin/arm64 4,8 MB (die CLI bindet SQLite noch nicht ein; mit Store-Anbindung in S1.1 wachsen sie, Spike: rund 11 MB mit `-s -w`).

**Offen (Auftrag A01):** S1 ab S1.6 (nach der Abnahme D1). Die Spike-Befunde sind in A01 als Vorgaben und Prüfliste festgehalten.

## S1: Lesen, Spiegeln, Suchen

### Stand S1.4/S1.5 (15.10.2026, erledigt; Abnahme D1 offen)

**Akzeptanz S1.4 erfüllt:** Die 1.000 Korpus-Nachrichten werden in Fenstern gespiegelt (Rohmail als Blob, Metadaten und geparste Felder in SQLite, Text in `bodies`), `sync --full -a <konto>` endet mit `1000 messages mirrored` und `0 errors` (Dovecot, Stalwart, GreenMail; Integrationstest und von Hand mit der Binärdatei). Abbruch mitten im Fenster, nach einem Fenster und **echtes `kill -9` eines Kindprozesses** (`TestKill9DuringInitialSync`; unter Windows wäre das `TerminateProcess`, dort aber **nicht ausgeführt, ungetestet**: nur kompiliert) setzen beim nächsten Lauf fort: dieselbe Anzahl, keine Doppelten (Nachrichten, Zuordnungen, Message-IDs geprüft). Von Hand gegen Dovecot: `kill -9` und Strg+C, danach `1000 messages mirrored`.

**Akzeptanz S1.5 erfüllt:**

- **Test je Strategie:** `condstore` gegen Dovecot und Stalwart, `uid-scan` gegen GreenMail und den In-Prozess-Server; je Server Flag-Änderung, Löschung und neue Nachricht von außen nach einem Lauf sichtbar (`TestIntegrationSync`). Leerlauf-Inkrementallauf bei 1.000 Nachrichten: 7 ms (Stalwart), 20 ms (Dovecot), 390 ms (GreenMail).
- **Blackhole:** Verstummt der Server während IDLE, fällt das beim fälligen Neustart am unbeantworteten `DONE` auf (Test mit 200 ms Neustart, erkannt nach rund 0,5 s), schweigt er schon vorher, beim IDLE-Start; danach Reconnect mit Backoff (S1.3).
- **IDLE:** neue Nachricht in der INBOX nach 60 bis 570 ms lokal sichtbar (Stalwart, GreenMail, Dovecot mit Hibernate); IDLE startet sich alle 5 bis 10 Minuten selbst neu (Standard 8, `--idle-restart`).
- **Flag von außen → `show <id> --headers` zeigt `seen`** (CLI-Test `TestSyncMailboxListShow`; für D1.6 `corpusload -mark-seen`).
- **UIDVALIDITY-Wechsel (P5):** Ordner gelöscht, neu angelegt, gleiche Nachrichten: Reset, Abgleich per Message-ID und Größe, keine Doppelten, **kein Inhalt erneut geladen** (alle drei Server); Nachrichten ohne Message-ID per `content_hash`.
- **P2** (50.000 Nachrichten lokal, Dovecot): Erstsync 78 s (638/s), Leerlauf-Abgleich 0,15 bis 0,19 s, `UID SEARCH ALL` über 50.000 UIDs 60 ms (ESEARCH) bzw. 72 ms, voller Flag-Abgleich 198 ms. Einzelheiten und WAN-Hochrechnung: [`spike/s0/REPORT.md`](../spike/s0/REPORT.md), Nachtrag P2. WAN-Messung durch den Betreiber offen.
- `mailbox list -a <konto> --json` mit `total` (laut Server), `mirrored` und `unread` je Ordner (D1.5, vorgezogen aus S1.8).

**Umsetzung:** `internal/mailsync` (Engine, Watch, Runner), IMAP-Provider `Changes`/`FetchHeaders`/`FetchRaw`/`Idle`, Store-Spiegel mit einer Transaktion je Fenster, Operationen `sync.run`, `mailboxes.list`, `messages.get`; Ablauf in [architektur.md](architektur.md), „Änderungserkennung". Migration 0002 (Rolle `all`, Sync-Phase, Flags je Ordnerkopie) mit Snapshot per `VACUUM INTO` vor der Migration. Testserver auf Dovecot 2.4.5 und GreenMail 2.1.14 festgelegt. `make race` grün (rund 55 s, der Korpus-Test dominiert).

**Befunde:**

- **Stalwart 0.11.8:** Nach vielen schnellen Schreibvorgängen kann die zwischengespeicherte Ordnerliste veralten, bis zur nächsten Änderung am Konto; neue Sitzungen sehen eben angelegte Ordner nicht. Der Korpus-Lader macht deshalb nach einer Sekunde eine kleine Änderung. Im Betrieb heißt das höchstens: Ein neuer Ordner erscheint einen Sync später.
- **GreenMail 2.1.14:** liefert für `BODY.PEEK[HEADER.FIELDS ("Message-ID")]` nichts (go-imap setzt Feldnamen in Anführungszeichen), daher holt der Sync den ganzen Kopf; sendet bei manchen rohen 8-Bit-Kopfzeilen eine falsche Literal-Länge (go-imap verliert den Takt; jetzt `ErrProtocol`, die Nachricht wird eingegrenzt und als Fehler übersprungen); einzelne `UID FETCH` dauern bei 18.000 Nachrichten im Ordner 12 bis 13 s (für Lastmessungen ungeeignet).
- **go-imap-Testserver** (In-Prozess): liefert für Nachrichten mit einer Kopfzeile ohne Doppelpunkt einen leeren `BODY[]`; der Sync zählt das als Fehler (Server meldet Größe, liefert nichts) statt eine leere Nachricht zu speichern. Die Unit-Tests laden diese zwei Korpus-Nachrichten deshalb nicht.
- **Kopien derselben Nachricht** (Gmail-Labels, Kopien, verschobene Nachrichten) werden eine Zeile in `messages` mit mehreren Zuordnungen; Flags stehen je Kopie, die Nachricht zeigt ein Flag, wenn eine Kopie es trägt (Frage an den Betreiber unten).

**Nicht in S1.4/S1.5 (bewusst verschoben):** Tabelle `jobs` (der Dauerläufer arbeitet ohne sie; mit Index/Extraktion S1.6/S1.7); `sync --watch` für mehrere Konten in einem Prozess (je Konto ein Prozess); Threads, Anhänge, HTML-Text (S1.6); volles `show` mit Untrusted-Hülle (S1.8/S1.9); `--quiet` (S1.8); Gmail `X-GM-EXT-1` (P1, manuell; bis dahin Abgleich der „Alle Nachrichten"-Dubletten per Message-ID); Speicherbedarf beim Erstsync messen (mit der WAN-Messung).

### Stand S1.3 (15.10.2026, erledigt)

**Akzeptanz S1.3 erfüllt:** Integrationstest gegen Dovecot 2.4, Stalwart 0.11.8 und GreenMail (`make testmail-up testmail-setup integration`): erwartete Fähigkeiten, Strategie (`condstore`, `condstore`, `uid-scan`), Rollen und `ErrAuth` bei falschem Passwort, je Server grün. Ohne `POSTKAUZ_IT` werden die Tests übersprungen; `make check` läuft ohne Docker. Watchdog-Tests mit stummem Server: Blackhole-Listener ohne Gruß und Blackhole-Proxy mitten in der Sitzung werden in der konfigurierten Deadline (300 ms) erkannt; danach baut die Session mit Backoff neu auf. `make race` grün (lokal, auch mit den Integrationstests) und als CI-Job.

- **`internal/imapx`:** `Session` (Reconnect mit exponentiellem Backoff und Jitter, Standard 1 s bis 5 min, höchstens 5 Versuche je Aufruf; `ErrAuth` und Ablehnungen `ErrRejected` nie wiederholt; Kontextabbruch schließt ein hängendes Kommando), LIST mit `RETURN (SPECIAL-USE)` und LIST-STATUS, CREATE (idempotent), NOOP. `make guard` prüft zusätzlich den Pin auf v2.0.0-beta.8.
- **`internal/provider/imap`:** `Connect`, `Capabilities`, `Strategy`, `ListMailboxes` (Rollen: SPECIAL-USE vor Namen, jede Rolle einmal, Gmail-Namen unter `[Gmail]`/`[Google Mail]` als Rückfall; neue Rolle `all`). `account test` nutzt denselben Weg. `MailProvider` vollständig erst mit S1.4/S1.5.
- **Testserver:** `deploy/testmail/stalwart-setup.sh` (`make testmail-setup`) richtet Stalwart ein (IPv4, Domain, Konto `test`).
- **Befunde:** Stalwart nennt SPECIAL-USE nur auf `RETURN (SPECIAL-USE)` und liefert in LIST-STATUS `HIGHESTMODSEQ 0` trotz CONDSTORE (S1.4: Wert aus SELECT nehmen); GreenMail kann weder SPECIAL-USE noch LIST-STATUS (Zustand erst mit SELECT) und antwortet auf CREATE eines vorhandenen Ordners ohne `ALREADYEXISTS`; Dovecot meldet zusätzlich `COMPRESS=DEFLATE` und `BINARY` (P6).
- **Beifang:** CI auf `actions/checkout@v7` und `actions/setup-go@v7` (Dependabot-PRs #1/#2 damit überholt).

**Nicht in S1.3 (bewusst verschoben):** IDLE und dessen Neustart alle 5 bis 10 Minuten (S1.5; braucht eine eigene Verbindung, weil `Session.Do` Aufrufe nacheinander ausführt); SELECT/FETCH/Changes (S1.4/S1.5); Rolle `all` in der Datenbank (Migration 0002, S1.4).

### Stand S1.2 (15.10.2026, erledigt)

**Akzeptanz S1.2 erfüllt:** Der Parser besteht den Standardkorpus (1.000 Nachrichten, Samen 42) gegen das Manifest: Betreff, Absender, Empfänger, Message-ID, Datum samt Quelle, Anhangsnamen, Inline-Teile, Textprobe, List-Id; die 60 Threads werden exakt gebildet, keine Einzelnachricht wird angehängt (`TestParserAgainstCorpusManifest`). Eine Zip-Bombe (256 MiB entpackt) wird nicht entpackt, Größe und Hash sind die der gepackten Bytes; zu große Teile werden nur bis zur Grenze dekodiert (`TestZipBombIsNotExtracted`, `TestOversizedPartIsTruncated`, `TestNoDecompressorImports`).

- **Blob-Store** `internal/blob`: SHA-256 der unkomprimierten Bytes, `blobs/ab/cd/<sha256>`, atomar über `blobs/tmp` + fsync + rename, Deduplizierung, optional zstd (`.zst`), Größenlimit 256 MiB, `Verify`. Neue Abhängigkeit `github.com/klauspost/compress` v1.20.3 (BSD-3; Go 1.27 hat keinen zstd-Encoder). Die Zeile in `blobs` schreibt erst der Sync (S1.4).
- **Parser** `internal/mimeparse` auf `go-message` mit `charset.Reader` (ADR-007): RFC 2047 B/Q in allen Zeichensätzen, rohe 8-Bit-Köpfe als Windows-1252, kaputte Kopfzeilen und NUL-Bytes entfernt, Message-IDs tolerant (auch ohne Klammern), Datum tolerant (Zone ohne Vorzeichen; fehlend/unlesbar → `INTERNALDATE`, ohne beides Nullzeit mit `date_source = none`); Limits für Kopf, Rohgröße, Teile, Tiefe, Teilgröße, Text. Abweichungen stehen in `Problems`, nicht als Fehler.
- **Threading** `internal/thread`: Union-Find über Message-ID, In-Reply-To, References (Spike-Verfahren). Betreff-Rückfall im Zeitfenster folgt mit S1.6.
- **Korpus** `internal/corpus`, `make corpus` (Inhalt und Lücken: [testkorpus.md](testkorpus.md), Abschnitt 1a).

**Nicht in S1.2 (bewusst verschoben):** Magic-Bytes, HTML-Bereinigung, versteckter Text, `message/rfc822` rekursiv lesen (S1.6); Betreff-Threading (S1.6); Blobs in der Datenbank verbuchen (S1.4); `doctor --blobs` (S1.11).

### Stand S1.1 (15.10.2026, erledigt)

**Akzeptanz S1.1 erfüllt:** Konto anlegen und auflisten (aus der Datenbank); Passwort nur im Schlüsselbund bzw. in der Umgebung, Kanarientest grün (Datenbank samt WAL, Log, Audit, stdout/stderr; auch über eine echte IMAP-Anmeldung an einen Testserver im Prozess); `doctor` meldet Pfade, Schlüsselbund, Datenbank und Schemaversion.

- **Konfiguration** `internal/config`: TOML (`POSTKAUZ_CONFIG`), unbekannte Schlüssel sind Fehler; `POSTKAUZ_DATA_DIR`, `POSTKAUZ_SECRETS_BACKEND`; Datenverzeichnis je Betriebssystem (Windows `%LOCALAPPDATA%`, nicht Roaming), 0700, Warnung bei Cloud-Ordnern.
- **Store:** Konten-Repository, SQLite-Audit-Writer (ersetzt `audit.Nop` im CLI), Schema-Schutz gegen neuere Datenbanken, Datenbankdatei 0600. `0001_init.sql` um `accounts.insecure_test_server` ergänzt (letzte Änderung an 0001; ab jetzt nur neue Migrationen).
- **Executor:** Schwärzung je Operation (`Redact`: nur Feldnamen und Ziele wie `account:demo`), Geheimnisse getrennt von der Eingabe (`Call.Secret`). **Policy-Korrektur:** `--dry-run` gilt jetzt auch für R1 (lokal schreibende Operationen); vorher wurde es bei R1 stillschweigend ignoriert.
- **Operationen** `accounts.add/test/remove` (Scope `admin`, R1, nicht über MCP), `accounts.list` aus der Datenbank. `account remove` verweigert bei archivierten Nachrichten (ADR-003).
- **P4 erledigt:** Schlüsselbund über `go-keyring` mit Zeitgrenze; ohne Schlüsselbund kein stiller Rückfall, sondern Hinweis auf `POSTKAUZ_PASSWORD_<NAME>`/`--password-from-env`; unsicherer Dateispeicher nur ausdrücklich (`insecure-file`, für Testserver). Getestet mit simuliertem D-Bus-Fehler, Zeitüberschreitung und dem echten Schlüsselbund dieses Linux-Containers (kein Secret Service → `unavailable`). **Windows und macOS** sind nur kompiliert, nicht ausgeführt: Abnahme D0 Punkt 5 und D1 Punkt 1 auf dem Windows-Rechner des Betreibers.
- **P3 als Entwurf:** SASL XOAUTH2 (eigen) und OAUTHBEARER (go-sasl) in `internal/imapx`, Token-Quelle mit Refresh und Sperrzeit in `internal/oauth`; Tests gegen einen Fake-IMAP-Server und einen Fake-Refresher. Offen: Autorisierungs-Flow (Loopback/Device Code), HTTP-Refresher, Kontenanlage mit `oauth2`.
- **`internal/imapx` vorgezogen (Ausschnitt):** Dial mit TCP-Keepalive, Watchdog je Kommando (Blackhole-Test grün), `WordDecoder` mit Charset-Reader (Test), Login, CAPABILITY, LIST; `internal/provider/imap` bestimmt daraus Fähigkeiten, Ordnerrollen (SPECIAL-USE, sonst Namen auf Deutsch/Englisch) und die Strategie (QRESYNC maskiert, ADR-007). `make guard` prüft, dass nur `imapx` go-imap importiert. Handgeprüft gegen Dovecot (`condstore`, Rollen INBOX/Sent/Drafts/Trash/Junk) und GreenMail (`uid-scan`); TLS mit selbstsigniertem Zertifikat (Dovecot 11993) erreicht die Anmeldung.
- **Binärgrößen** (`make cross`, ohne `-s -w`): linux/amd64 17,6 MB, windows/amd64 17,1 MB, darwin/arm64 16,4 MB; mit `-s -w` 12,0 / 11,7 / 11,1 MB (Spike: rund 11 MB).
- `go test -race ./...` lokal grün (braucht CGO; seit S1.3 `make race` und CI-Job, P7).

**Nicht in S1.1 (bewusst verschoben):** globale Optionen `--account`, `--key`, `--quiet`, `--profile` (mit S1.8); `doctor --secrets` und `doctor --blobs` (S1.11); `VACUUM INTO` vor Migrationen (mit der ersten Migration 0002); gemountete Secret-Dateien.

**Umfang:** Konto anlegen und testen (`account add/test`); Erstsync in Fenstern, inkrementell mit CONDSTORE wo vorhanden, Löschungen per UID-SEARCH-Abgleich (QRESYNC fehlt in `go-imap`, ADR-007), IDLE auf INBOX, Reconnect mit Backoff und Watchdog; Spiegel (Rohmail im Blob-Store, Metadaten in SQLite); MIME-Parser und Threading; Textextraktion für PDF/DOCX/TXT (abschaltbar); Volltextsuche mit deutscher Stemmung und Trigram; CLI mit `--json`; stdio-MCP **lesend** (Tools, Resources, Prompts aus [mcp.md](mcp.md)); Audit; Import aus mbox/Maildir mit Message-ID-Abgleich und Überlappung; Schlüsselverwaltung.

**Nicht enthalten:** Entwürfe, Tags, Verschieben, Papierkorb, HTTP, REST, Regeln, Versand.

**Akzeptanzkriterien:**

1. **Zwei Serverfamilien bestehen die Testmatrix** (Dovecot, Stalwart; je per Docker), nachts und auf Abruf. Ein dritter Anbieter (z. B. ein gehostetes Postfach) wird vom Betreiber **manuell** gegengeprüft; er ist nie Teil der CI.
2. **Erstsync:** Ein synthetisches Postfach mit 50.000 Nachrichten (Testkorpus rund 245 MB, rund 4,9 KB je Nachricht; lokal gemessen 78 s, P2) wird auf einem normalen PC in unter 60 Minuten gespiegelt; echte Postfächer mit Anhängen sind deutlich größer (Hochrechnung mit 5 GB über WAN in `spike/s0/REPORT.md`, Messung durch den Betreiber offen); im Leerlauf dauert ein inkrementeller Abgleich unter 5 Sekunden. Ein Abbruch mitten im Erstsync (`kill -9`) setzt ohne Doppelte fort (Test).
3. **Änderungen von außen:** Eine Flag-Änderung oder Löschung auf dem Server ist nach spätestens einem Sync-Intervall lokal sichtbar; in der INBOX nach wenigen Sekunden (IDLE). Ein `UIDVALIDITY`-Wechsel führt zu einem Abgleich per Message-ID, nicht zu Verlust oder Doppelten (Test).
4. **Suche:** Antwort unter 300 ms bei 100.000 Nachrichten; deutsche Flexionen, Umlaute und Teilwörter werden gefunden (Testsatz aus [testkorpus.md](testkorpus.md)).
5. **Import:** Ein mbox- bzw. Maildir-Bestand mit Überlappung zu einem bereits gesyncten Konto erzeugt keine Doppelten; die Zählung (neu/vorhanden/übersprungen) wird ausgegeben.
6. **MCP:** Claude Code und ein zweiter MCP-Client sehen die lesenden Tools; Tool-Liste folgt den Scopes; jede Ausgabe mit Mailinhalt ist in der Untrusted-Hülle (Test); HTML-Mails mit verstecktem Text liefern `hidden_text_removed: true` (Test).
7. **Audit:** Jede Operation erzeugt ein Ereignis ohne Mailinhalt; `UPDATE`/`DELETE` sind gesperrt; der tägliche Export mit Prüfsumme läuft; `audit verify` erkennt eine veränderte Zeile (Test).
8. **Geheimnisse:** Mit Kanarienwerten als Passwort und Schlüssel taucht kein Wert in Logs, Audit, Fehlermeldungen, `--json` oder MCP-Ausgaben auf (Test).
9. **Qualität:** Jede Operation hat einen Test; `go vet` und `go test ./...` grün; Cross-Compile für linux/amd64, windows/amd64, darwin/arm64 grün (CI).
10. **Installation:** Windows 11 und Linux: Binärdatei laden, `postkauz doctor`, Konto anlegen, erster Sync in unter 15 Minuten nach README.

## S2: Entwürfe (draft-only)

**Umfang:** `drafts.*`: neu, antworten, allen antworten, weiterleiten mit korrekten `Message-ID`/`In-Reply-To`/`References`, Zitat, Anhänge aus vorhandenen Nachrichten oder Dateien; Ablage per `APPEND` im Entwürfe-Ordner; Policy-Datei; Freigabe-Zustände (`draft` → `pending_approval` → `ready` → `sent_by_human`) mit `preview_hash`; Prüf-Hook; Erkennung des Versands im Ordner „Gesendet"; Resources `mail://…/draft/…` und `mail://style/…`; Prompt `draft_reply`.

**Nicht enthalten:** SMTP, `mail_request_send`, `auto`-Versand, Regeln.

**Akzeptanzkriterien:**

1. Ein über MCP entworfener und vom Menschen im Mailclient gesendeter Entwurf erscheint beim Empfänger **im richtigen Thread** (Prüfung der Header-Kette bei drei Empfängertypen, z. B. Webmail, Desktop-Client, zweiter Anbieter; mit eigenen Testpostfächern).
2. Der Entwurf ist im Entwürfe-Ordner **in einem zweiten Mailclient sichtbar** und trägt die vorab vergebene `Message-ID`.
3. Der Versand durch den Menschen wird erkannt (`sent_by_human`), auch wenn der Client eine neue `Message-ID` vergibt (Rückfall-Abgleich; Verhalten je Client dokumentiert).
4. **Kein Agenten-Schlüssel kann `ready` setzen.** Bewiesen durch Vertragstests, die jede R1–R3-Operation mit jedem Scope-Satz durch den Executor schicken.
5. Eine Änderung nach der Freigabe macht diese ungültig (`preview_hash`-Test).
6. Der Prüf-Hook blockiert den Übergang nach `pending_approval`, wenn er scheitert.
7. Jeder Zustandsübergang steht mit Akteur im Audit.
8. Es existiert **kein SMTP-Code** im Repository (Test oder CI-Prüfung auf Importe von `net/smtp`).

## S3: Aktionen, Sicherung, Fernzugriff

**Umfang:** Tags (lokal, optional als IMAP-Keywords); `mark`, `move`, `trash` (nur bis zum Papierkorb, `confirm_count`); `attachments.save` ins Exportverzeichnis; Export (mbox/EML), Backup (`VACUUM INTO` + Blobs) und Restore; Löschkonzept; **HTTP-MCP mit OAuth 2.1** aus Standardkomponenten auf einem Dauerläufer (EU-VPS, privates Netz, TLS), Betriebsanleitung (Windows-Aufgabe, systemd, Docker).

**Akzeptanzkriterien:**

1. Tag-, Flag-, Verschiebe- und Papierkorb-Operationen sind im Trockenlauf und real getestet; kein Agenten-Schlüssel erreicht endgültiges Löschen (Vertragstest, Provider-Interface enthält kein Expunge).
2. Lokale Änderungen landen innerhalb von 10 Sekunden auf dem Server; Konflikte löst der nächste Sync mit „Server gewinnt" (außer lokale Tags).
3. **Restore-Test:** Ein Backup wird auf einer sauberen Maschine wiederhergestellt; Suche und Audit liefern identische Ergebnisse.
4. Export als mbox/EML-Verzeichnis ist von einem Standard-Mailclient lesbar.
5. HTTP-MCP: Ein Cloud-Agent erreicht den Dienst über das private Netz; Token-Weitergabe wird abgelehnt; ohne Schlüssel kein Zugriff; Zertifikat und Ablauf geprüft.
6. Backups und Datenbank liegen verschlüsselt bzw. auf verschlüsseltem Datenträger; die Anleitung benennt das ausdrücklich.

## S4 und später (nicht zugesagt)

Reihenfolge nach Nutzen und Risiko, jeweils mit eigener Entscheidung:

1. **SMTP-Versand mit Freigabe** (`mail_request_send`, Scope `mail:send`, Cool-down, Limits, Allowlist). Erst nach bewährtem draft-only-Betrieb.
2. **Regeln** mit Trockenlauf gegen die letzten N Tage; Agenten schlagen nur vor (deaktiviert), Menschen aktivieren.
3. **REST + OpenAPI** als dritter Adapter über demselben Register.
4. **JMAP-Provider** (gegen Stalwart und einen JMAP-Anbieter).
5. Gmail-API-/Graph-Anbindung (bring your own client), Embeddings/semantische Suche (lokal), Mehrbenutzerbetrieb mit Rollen je Mensch.
6. **GUI** frühestens nach 1.0 und nur gegen REST (siehe Nicht-Ziele).

## Entschiedene Fragen (14./15.10.2026)

1. **Gate:** bestanden mit Auflagen (ADR-007).
2. **Go-Version:** Zielversion 1.27, Anhebung als Schritt 0 von A01 (ADR-008).
3. **Postfach-Aktionen:** Tags, Move und Trash bleiben in S3.
4. **Sprache:** Programmausgaben Englisch, Doku Deutsch.
5. **Erkennung des Versands (S2):** `Message-ID` zuerst, **Rückfall-Abgleich** über Empfänger, Betreff, Body-Hash und Zeitfenster; das Verhalten je Client wird in S2 gemessen.
6. **Commit-Sprache** (15.10.2026): Englisch.
7. **Passwörter ohne Schlüsselbund** (15.10.2026): kein Dateispeicher mit Passphrase; es bleibt bei Umgebungsvariable bzw. `insecure-file` nur für Testserver.
8. **Race-Detector** (15.10.2026): eigenes Ziel `make race` (CGO) und CI-Job ab S1.3, nicht Teil von `make check`.
9. **Ordnerrolle `all`** (15.10.2026): kommt mit Migration 0002 in die Datenbank (umgesetzt).
10. **Testserver-Versionen** (15.10.2026): Dovecot 2.4.5 und GreenMail 2.1.14 festgelegt (umgesetzt), Stalwart bleibt 0.11.8.
11. **Snapshot vor Migrationen** (15.10.2026): `VACUUM INTO` mit Migration 0002 umgesetzt.

## Offene Fragen

1. **Name:** Ergebnis der Namensprüfung; danach Umbenennung gemäß [architektur.md](architektur.md), Abschnitt 8.
2. **Mehrere Menschen an einem Postfach:** Wie wird eine Freigabe einer Person zugeordnet, wenn mehrere Menschen dasselbe Postfach bedienen (getrennte Logins/Delegation beim Anbieter)? Relevant ab S2.
3. **Spike-Lücken** (Prüfliste in A01): Gmail `X-GM-EXT-1` (P1, manuell), WAN-Latenz (P2, Betreiber), COMPRESS/BINARY (P6, später), QRESYNC per Beitrag oder Fork (P8, nach S1; nach P2 für 50.000 Nachrichten nicht nötig). Erledigt bzw. als Entwurf: P2 lokal (50.000 Nachrichten, S1.5), P3 (OAuth2/SASL, Entwurf), P4 (Schlüsselbund; Windows/macOS noch manuell abzunehmen), P5 (UIDVALIDITY-Wechsel, S1.5), P7 (Race-Detector: `make race` und CI-Job, S1.3).
4. **Kopien als eine Nachricht** (S1.5): Liegt dieselbe Nachricht in mehreren Ordnern (Kopie, Gmail-Label), ist sie lokal eine Nachricht mit mehreren Ordnern; ein Flag gilt als gesetzt, wenn eine Kopie es trägt. Vorschlag: so lassen.
5. **Dauerläufer für mehrere Konten** (S1.5): `sync --watch` beobachtet ein Konto; für mehrere Konten je ein Prozess. Vorschlag: ein gemeinsamer Dauerläufer erst mit dem Betriebskonzept (S3).
