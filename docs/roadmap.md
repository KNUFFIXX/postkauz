# Roadmap

Stand: **Phase 0, Gerüst** (Repository, Dokumentation und kompilierendes Go-Gerüst stehen; kein Sync, keine Suche). Am Ende jeder Arbeitssitzung wird dieser „Stand" und `CHANGELOG.md` gepflegt.

| Phase / Schnitt | Inhalt in einem Satz | Status |
|---|---|---|
| Phase 0 | Gerüst, Architektur, ADRs, Testserver, Spike mit `go-imap/v2`, Gate Go vs. Python | **S0 (Spike und Gate) erledigt 14.10.2026**: Gate Go bestanden mit Auflagen, Bericht `spike/s0/REPORT.md`, ADR-007/008. Nächstes: `auftraege/A01_phase0.md` (Schritt 0: Go-Anhebung) |
| S1 | Konto, Sync, Spiegel, Threads, Suche, CLI lesend, stdio-MCP lesend, Audit, Import | offen |
| S2 | Entwürfe als draft-only, Policy-Datei, Freigabe-Zustände, Prüf-Hook | offen |
| S3 | Tags und Postfach-Aktionen, Export/Backup/Restore, HTTP-MCP mit OAuth 2.1 | offen |
| S4+ | SMTP-Versand mit Freigabe, Regeln, REST, JMAP, später GUI | nicht begonnen |

Aufwandsschätzung für S1–S3: **40–80 Sitzungen, drei bis sechs Monate** (Code-Anteil schnell, Tests gegen echte Server und Entscheidungen des Betreibers bestimmen das Tempo). Die Schätzung ist grob und wird nach S1 neu gesetzt.

## Phase 0: Gerüst und Gate

**Erledigt:** Repository, Lizenz, ADR-001 bis ADR-006, Architektur-/Datenmodell-/MCP-/CLI-/Sicherheitsdokumente, Go-Gerüst (Register, Executor, Policy-Stub, Audit-Typ, Migrationen mit FTS5, Provider-Interface, CLI mit `version`/`doctor`/`accounts list`), CI, Testserver-Compose.

**S0 erledigt (14.10.2026): Spike und Gate.** `go-imap/v2` v2.0.0-beta.8 besteht gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail: Erstsync in Fenstern (rund 1.000 bis 1.250 Nachrichten/s lokal), Inkrementalsync (CONDSTORE bzw. Vollvergleich), Löschungserkennung, Draft-APPEND mit identischen Bytes, IDLE, Reconnect. **Urteil: Gate Go bestanden mit Auflagen**, Bericht und Rohdaten in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md). Auflagen und Entscheidungen: ADR-007 (go-imap-Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Go-Version). Der Spike-Code ist Wegwerfcode in einem eigenen Go-Modul (`spike/s0`) und nicht Teil von `./...`.

**Offen (Auftrag A01):** Schritt 0 (Go-Anhebung auf die Zielversion, Toolchain-Check), danach S1. Die Spike-Befunde sind in A01 als Vorgaben und Prüfliste festgehalten.

## S1: Lesen, Spiegeln, Suchen

**Umfang:** Konto anlegen und testen (`account add/test`); Erstsync in Fenstern, inkrementell mit CONDSTORE wo vorhanden, Löschungen per UID-SEARCH-Abgleich (QRESYNC fehlt in `go-imap`, ADR-007), IDLE auf INBOX, Reconnect mit Backoff und Watchdog; Spiegel (Rohmail im Blob-Store, Metadaten in SQLite); MIME-Parser und Threading; Textextraktion für PDF/DOCX/TXT (abschaltbar); Volltextsuche mit deutscher Stemmung und Trigram; CLI mit `--json`; stdio-MCP **lesend** (Tools, Resources, Prompts aus [mcp.md](mcp.md)); Audit; Import aus mbox/Maildir mit Message-ID-Abgleich und Überlappung; Schlüsselverwaltung.

**Nicht enthalten:** Entwürfe, Tags, Verschieben, Papierkorb, HTTP, REST, Regeln, Versand.

**Akzeptanzkriterien:**

1. **Zwei Serverfamilien bestehen die Testmatrix** (Dovecot, Stalwart; je per Docker), nachts und auf Abruf. Ein dritter Anbieter (z. B. ein gehostetes Postfach) wird vom Betreiber **manuell** gegengeprüft; er ist nie Teil der CI.
2. **Erstsync:** Ein synthetisches Postfach mit 50.000 Nachrichten (rund 5 GB) wird auf einem normalen PC in unter 60 Minuten gespiegelt; im Leerlauf dauert ein inkrementeller Abgleich unter 5 Sekunden. Ein Abbruch mitten im Erstsync (`kill -9`) setzt ohne Doppelte fort (Test).
3. **Änderungen von außen:** Eine Flag-Änderung oder Löschung auf dem Server ist nach spätestens einem Sync-Intervall lokal sichtbar; in der INBOX nach wenigen Sekunden (IDLE). Ein `UIDVALIDITY`-Wechsel führt zu einem Abgleich per Message-ID, nicht zu Verlust oder Doppelten (Test).
4. **Suche:** Antwort unter 300 ms bei 100.000 Nachrichten; deutsche Flexionen, Umlaute und Teilwörter werden gefunden (Testsatz aus [testkorpus.md](testkorpus.md)).
5. **Import:** Ein mbox- bzw. Maildir-Bestand mit Überlappung zu einem bereits gesyncten Konto erzeugt keine Doppelten; die Zählung (neu/vorhanden/übersprungen) wird ausgegeben.
6. **MCP:** Claude Code und ein zweiter MCP-Client sehen die lesenden Tools; Tool-Liste folgt den Scopes; jede Ausgabe mit Mailinhalt ist in der Untrusted-Hülle (Test); HTML-Mails mit verstecktem Text liefern `hidden_text_removed: true` (Test).
7. **Audit:** Jede Operation erzeugt ein Ereignis ohne Mailinhalt; `UPDATE`/`DELETE` sind gesperrt; der tägliche Export mit Prüfsumme läuft; `audit verify` erkennt eine veränderte Zeile (Test).
8. **Geheimnisse:** Mit Kanarienwerten als Passwort und Schlüssel taucht kein Wert in Logs, Audit, Fehlermeldungen, `--json` oder MCP-Ausgaben auf (Test).
9. **Qualität:** Jede Operation hat einen Test; `go vet` und `go test ./...` grün; Cross-Compile für linux/amd64, windows/amd64, darwin/arm64 grün (CI).
10. **Installation:** Windows 11 und Linux: Binärdatei laden, `mailwerk doctor`, Konto anlegen, erster Sync in unter 15 Minuten nach README.

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

## Entschiedene Fragen (14.10.2026)

1. **Gate:** bestanden mit Auflagen (ADR-007).
2. **Go-Version:** Zielversion 1.27, Anhebung als Schritt 0 von A01 (ADR-008).
3. **Postfach-Aktionen:** Tags, Move und Trash bleiben in S3.
4. **Sprache:** Programmausgaben Englisch, Doku Deutsch.
5. **Erkennung des Versands (S2):** `Message-ID` zuerst, **Rückfall-Abgleich** über Empfänger, Betreff, Body-Hash und Zeitfenster; das Verhalten je Client wird in S2 gemessen.

## Offene Fragen

1. **Name:** Ergebnis der Namensprüfung; danach Umbenennung gemäß [architektur.md](architektur.md), Abschnitt 8.
2. **Mehrere Menschen an einem Postfach:** Wie wird eine Freigabe einer Person zugeordnet, wenn mehrere Menschen dasselbe Postfach bedienen (getrennte Logins/Delegation beim Anbieter)? Relevant ab S2.
3. **Spike-Lücken** (Prüfliste in A01): Gmail `X-GM-EXT-1`, Postfächer ab 50.000 Nachrichten und WAN-Latenz, OAuth2 über `go-sasl`, Schlüsselbund, UIDVALIDITY-Wechsel, COMPRESS/BINARY, Race-Detector, QRESYNC per Beitrag oder Fork.
