# Roadmap

**Stand 25.10.2026:** Schnitt 1 veröffentlicht (**0.1.0-rc2**, 22.10.2026), Schnitt 2 fertig gebaut und gegengeprüft (**S2.9**, 23.10.2026; Schema 16; Abnahmen D2, D3, D4, D4.8 bestanden). **D5 Runde 1 nicht bestanden** (Gmail sendet das Zitat ohne „>") → Nachbesserung N1, Build **0.2.0-rc2** (nicht getaggt). Gegenprüfung des Gesamtstands am 24.10.2026: ein Blocker für Schnitt 3 (UID 0 in `imapx` = „*"), fünf wichtige Punkte → `auftraege/A03_s3.md`, Nachtrag 24.10.

**25.10.2026, Entscheid C2 (Echtdaten-Probe Gmail + Thunderbird):** Import byte-genau (mbox: nur die Rahmen-Leerzeile am Ende entfernt; Maildir ganz), `content_hash`-Varianten ±1 Zeilenende, `sync` zählt „already known, downloaded anyway"; Build **0.2.0-rc3** = 0.2.0-rc2 plus Fix C2 (nicht getaggt). **rc4** (25.10.2026, Restfall C2, vom Betreiber genehmigt): Verknüpfung per Message-ID auch bei 0 bis 6 Bytes größerer lokaler Kopie (Thunderbird ergänzt Zeilenende bzw. abschließende MIME-Grenze), nie für Entwürfe; Build **0.2.0-rc4** (nicht getaggt). **rc5:** Auflagen der Gegenprüfung (zusätzlich gleicher Betreff und Date, Entwurfs-Ausschluss wie Versanderkennung, Zähler je Nachricht). **rc6** (Echtdaten rc5): Import trennt zwei Fassungen mit gleicher Message-ID (Gmail), `import` extrahiert Anhangstext bis zum Ende (`--no-extract`); Build **0.2.0-rc6** (nicht getaggt).

**Als Nächstes:** Echtdaten-Probe mit 0.2.0-rc6 fortsetzen (bestehenden Bestand einmal mit `import … --rescan` ergänzen) → D5 Runden 1 (Wiederholung), 2 und 3 auf dem Büro-PC mit 0.2.0-rc6 → D6 (Tag auf den dann aktuellen Build, GitHub-Release) → A03 Teil A (Nachbesserungen, inkl. Blocker) → Schnitt 3. **Echtdaten:** Die Probe läuft seit 25.10.2026 auf dem Heim-PC des Betreibers: erster Import des Thunderbird-Spiegels und lesende Anbindung des Gmail-Postfachs; ihr erster Befund (Größe nach Import, unnötige Downloads) ist mit C2 behoben. Abnahmen D2 bis D4.8 liefen auf dem synthetischen Korpus.

Verlauf je Schritt (S1.1 bis S2.9, Teilabnahmen, Gegenprüfungen, Nachbesserungen): [roadmap_verlauf.md](roadmap_verlauf.md). Versionen: [CHANGELOG.md](../CHANGELOG.md). Am Ende jeder Arbeitssitzung werden dieser Stand, der Verlauf und das CHANGELOG gepflegt.

| Phase / Schnitt | Inhalt in einem Satz | Status |
|---|---|---|
| Phase 0 | Gerüst, Architektur, ADRs, Testserver, Spike mit `go-imap/v2`, Gate Go vs. Python | erledigt 14./15.10.2026 (Gate Go bestanden mit Auflagen, ADR-007/008) |
| S1 | Konto, Sync, Spiegel, Threads, Suche, CLI lesend, stdio-MCP lesend, Audit, Import | komplett 16.10.2026; D2/D3 bestanden 21.10.; **veröffentlicht 0.1.0-rc2** 22.10.2026 |
| S2 | Entwürfe als draft-only, Policy-Datei, Freigabe-Zustände, Prüf-Hook, Versanderkennung, MCP | **komplett (S2.0 bis S2.9) 23.10.2026**; D4/D4.8 bestanden; D5 offen (Runde 1 nicht bestanden → N1, 0.2.0-rc2); D6 offen |
| S3 | Tags und Postfach-Aktionen, Export/Backup/Restore, HTTP-MCP mit OAuth 2.1 | Auftrag A03 im Entwurf; Baustart nach D6 und Teil A |
| S4+ | SMTP-Versand mit Freigabe, Regeln, REST, JMAP, später GUI | nicht begonnen |

Aufwandsschätzung für S1–S3: **40–80 Sitzungen, drei bis sechs Monate** (Code-Anteil schnell, Tests gegen echte Server und Entscheidungen des Betreibers bestimmen das Tempo). Die Schätzung ist grob und wird nach S1 neu gesetzt. **Ist nach S2 (24.10.2026):** 8 Arbeitssitzungen vom 14. bis 23.10.2026 für Schnitt 1 und 2 (Code, Tests, Gegenprüfungen, Abnahmen D1–D4); offen sind die Teile, die der Betreiber trägt (D5, D6, Lastenheft, Betriebsentscheid, Echtdaten).

## Abnahmemaßstab des Betreibers (Lastenheft, angenommen 24.10.2026)

Neben den technischen Akzeptanzkriterien je Schnitt gilt das Ein-Seiten-Lastenheft des Betreibers (außerhalb des Repos, enthält Postfachdetails). Sechs Vorgänge in der Reihenfolge ihres Nutzens, jeder mit „fertig, wenn":

1. **Posteingang abarbeiten** (täglich): ungelesene Mails eines Ordners lesen (Text bereinigt, Anhänge als Text), zusammenfassen, danach den Ordner/das Label entfernen — fertig, wenn der Agent das vollständig über PostKauz-MCP erledigt, ohne die bisherigen Gmail-Werkzeuge; „ungelesen" stimmt mit dem Server überein. → S1 (lesen) + S3 (Postfach-Aktion).
2. **Antwort im Thread als Entwurf** (mehrmals täglich): Zitat, korrekte Thread-Kopfzeilen, Signatur, Empfänger beim Anlegen gefüllt, Prüf-Hook, Entwurf im Entwürfe-Ordner des Anbieters; der Mensch sendet — fertig, wenn der Entwurf binnen 10 s dort liegt (D4), beim Empfänger im Thread ankommt und der Versand erkannt wird (D5). → S2.
3. **Vorgänge recherchieren** (mehrmals wöchentlich): Suche in Betreff, Text und Anhangstext über Jahre, Threads vollständig — fertig, wenn zehn echte Suchen des Betreibers treffen und sich „sofort" anfühlen; Erstbestand aus dem Thunderbird-Spiegel einmal importiert. → S1 + Import auf dem Büro-PC (**noch offen**).
4. **Weiterleitung im Thread** (wöchentlich): Weiterleitungs-Entwurf mit Zitatkopf und Anhängen, nur im Original-Thread — fertig, wenn der Entwurf im Entwürfe-Ordner liegt und der Mensch sendet. → S2 (`forward`).
5. **Unbeaufsichtigte Triage** (nachts): neue Mails klassifizieren, Tags vergeben, Antwortentwürfe vorbereiten, Bericht — fertig, wenn ein eigener Schlüssel mit den Scopes lesen + Entwurf + Tag läuft, der Vertragstest beweist, dass er nichts anderes kann, und das Audit die Nachtaktionen zeigt. → S2 + S3 (Tags).
6. **Dauerbetrieb ohne Eingriff:** `sync --watch` als Dienst/Aufgabe, Index aktuell binnen 5 min, täglicher Audit-Export, Backup/Restore geprüft — fertig nach sieben Tagen ohne Handeingriff, `doctor` ohne Warnung, Restore auf einen zweiten Rechner. → S3 + **Betriebsentscheid: 24.10.2026 Option A — Phase 1 auf dem Büro-PC (Standby bei Netzbetrieb aus), bis auf Weiteres; Mini-Server oder EU-VPS erst nach sieben Tagen Echtbetrieb neu bewerten.**

Gesamterfolg aus Betreibersicht: Die Werkzeugliste des Betreibers nennt für Lesen, Suchen, Antworten, Weiterleiten und Anhänge nur noch PostKauz; jede Agentenaktion ist im Audit nachlesbar; nichts wird ohne Menschen gesendet. Nicht gefordert (nach hinten oder streichen): REST, automatische Regeln, JMAP, semantische Suche, eigene Google-OAuth-App, GUI/TUI, Mehrbenutzer-Rollen, SMTP-Versand, Kalender/Kontakte/Chat. Stand 24.10.2026: **keiner der sechs Vorgänge ist fertig** — der Code für 2 und 4 ist da (D5 offen), 3 wartet auf den Import der Echtdaten, 1/5/6 auf Schnitt 3.

## Phase 0: Gerüst und Gate

**Erledigt:** Repository, Lizenz, ADR-001 bis ADR-006, Architektur-/Datenmodell-/MCP-/CLI-/Sicherheitsdokumente, Go-Gerüst (Register, Executor, Policy-Stub, Audit-Typ, Migrationen mit FTS5, Provider-Interface, CLI mit `version`/`doctor`/`accounts list`), CI, Testserver-Compose.

**S0 erledigt (14.10.2026): Spike und Gate.** `go-imap/v2` v2.0.0-beta.8 besteht gegen Dovecot 2.4.5, Stalwart 0.11.8 und GreenMail: Erstsync in Fenstern (rund 1.000 bis 1.250 Nachrichten/s lokal), Inkrementalsync (CONDSTORE bzw. Vollvergleich), Löschungserkennung, Draft-APPEND mit identischen Bytes, IDLE, Reconnect. **Urteil: Gate Go bestanden mit Auflagen**, Bericht und Rohdaten in [`spike/s0/REPORT.md`](../spike/s0/REPORT.md). Auflagen und Entscheidungen: ADR-007 (go-imap-Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Go-Version). Der Spike-Code ist Wegwerfcode in einem eigenen Go-Modul (`spike/s0`) und nicht Teil von `./...`.

**Schritt 0 erledigt (15.10.2026):** `go.mod` auf `go 1.27` (Toolchain go1.27.2, `GOTOOLCHAIN=local` in Makefile und CI), `modernc.org/sqlite` v1.45.0 → v1.60.1 (SQLite 3.53.4; mitgezogen: `modernc.org/libc` v1.77.1, `golang.org/x/sys` v0.48.0). `make check` und `make cross` grün; Binärgrößen ohne `-s -w`: linux/amd64 4,9 MB, windows/amd64 5,1 MB, darwin/arm64 4,8 MB (die CLI bindet SQLite noch nicht ein; mit Store-Anbindung in S1.1 wachsen sie, Spike: rund 11 MB mit `-s -w`).

**Offen (Auftrag A01):** Abnahme D2 ([abnahme_D2.md](abnahme_D2.md)) und Gesamtabnahme D3 ([abnahme_D3.md](abnahme_D3.md)) durch den Betreiber; alle Schritte S1.1 bis S1.11 sind erledigt (Abnahme D1 bestanden 16.10.2026). Die Spike-Befunde sind in A01 als Vorgaben und Prüfliste festgehalten.
## S1: Lesen, Spiegeln, Suchen

Komplett seit 16.10.2026 (S1.1 bis S1.11), veröffentlicht als 0.1.0-rc2 am 22.10.2026. Einzelschritte, Messungen und die Nachbesserung nach der Gegenprüfung: [roadmap_verlauf.md](roadmap_verlauf.md), Abschnitt S1.

## S2: Entwürfe (draft-only)

Komplett seit 23.10.2026 (S2.0 bis S2.9, Schema 16, ADR-009). Abnahmen D4 (47 OK) und D4.8 bestanden; D5 Runde 1 nicht bestanden → N1 (Rückfall schneidet am eigenen Zitat des Entwurfs), Wiederholung mit 0.2.0-rc2 offen; D6 offen. Einzelschritte, Gegenprüfungen, Abnahmeprotokolle: [roadmap_verlauf.md](roadmap_verlauf.md), Abschnitt S2.

## S3: Aktionen, Sicherung, Fernzugriff

**Auftrag:** [auftraege/A03_s3.md](../auftraege/A03_s3.md) (Entwurf 23.10.2026: Entscheide E1–E13 als Vorschläge, Teil A Nachbesserungen aus D4.8/D5, Schritte S3.0–S3.7, Abnahmen D7–D10, drei Rückfragen mit Standardantwort; Baustart nach D6).

**Umfang:** Tags (lokal, optional als IMAP-Keywords); `mark`, `move`, `trash` (nur bis zum Papierkorb, `confirm_count`); `attachments.save` ins Exportverzeichnis; Export (mbox/EML), Backup (`VACUUM INTO` + Blobs) und Restore; Löschkonzept; **HTTP-MCP mit OAuth 2.1** aus Standardkomponenten auf einem Dauerläufer (EU-VPS, privates Netz, TLS), Betriebsanleitung (Windows-Aufgabe, systemd, Docker). **Anmeldung per OAuth2 für Microsoft-Postfächer** (Outlook.com, Hotmail, Microsoft 365; IMAP dort seit 16.09.2024 nur mit OAuth2): Prüfpunkt P3 fertigstellen — Anmelde-Flow (Device Code oder Loopback) und Refresh gegen den Token-Endpunkt, „bring your own client" (der Betreiber meldet eine eigene App bei Microsoft an; keine eigene verifizierte PostKauz-App). Betreiber 23.10.2026 `#`.

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

## Umfeld: verwandte Projekte (Stand 21.10.2026)

Am 21.10.2026 hat der Betreiber ein Make-or-buy-Gate durchgeführt (Vergleichsdokument außerhalb des Repos). Nächster Verwandter ist **msgvault** (github.com/kenn-io/msgvault; Go, MIT; lokales Archiv für Mail, Chat, Kalender und Meetings mit SQLite+FTS5, CLI, HTTP-API, TUI, Web-UI, MCP; IMAP-Sync seit 03/2026, Entwürfe draft-only seit 09/2026). Messung gegen denselben Dovecot und Korpus (v0.21.0): deutscher Suchtestsatz 7/22 (PostKauz 22/22), Thunderbird-Import gegen das gesyncte Konto +612 Doppelte (PostKauz 0), versteckter Text wird unmarkiert ausgegeben, IMAP-Passwort im Klartext, kein Audit der Agentenaktionen, kein IDLE, Agent-Tokens nur im RAM, Telemetrie standardmäßig an, 146-MB-CGO-Binary. msgvault hat, was PostKauz nicht hat: Web-UI, TUI, PST-Import, Vektorsuche, über 60 Beiträger. Entscheid: PostKauz wird weitergebaut; msgvault ist Referenz für Import-Formate und Backup-Kommandos und wird im Januar 2027 neu geprüft. Weitere geprüfte Projekte: Himalaya (Rust-CLI, kein Spiegel), OpenAgentEmail (Agenten-Postfächer), mbsync+notmuch+MCP (Unix-only), codefuturist/email-mcp (Wrapper mit Cache, sendet selbst).

Zwei Befunde aus der Messung hat der Betreiber am 21.10.2026 an msgvault gemeldet: versteckter Text unmarkiert in der Textfassung ([kenn-io/msgvault#1416](https://github.com/kenn-io/msgvault/issues/1416)) und UTF-8-HTML als Latin-1 decodiert ([kenn-io/msgvault#1417](https://github.com/kenn-io/msgvault/issues/1417)). Beide Fälle deckt der PostKauz-Korpus ab (Art `hidden`, S1.6-Tests); die Issues sind zugleich die Referenz, falls msgvault die Lücken schließt (Wiedervorlage Januar 2027).

## Entschiedene Fragen (14. bis 21.10.2026)

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
12. **Watchdog** (16.10.2026, vorher offene Frage 6): Schweigen messen bleibt, dazu Obergrenze je Kommando, konfigurierbar, Standard 10 min (ADR-007 Nachtrag 1, umgesetzt).
13. **Testkorpus im Repository** (16.10.2026): bleibt synthetisch (Commit-Sprache Englisch bestätigt, siehe 6).
14. **Versteckter Vorschautext in Newslettern** (16.10.2026, vorher offene Frage 8): bleibt als `hidden_text` markiert (Hinweis, kein Urteil).
15. **Extraktion als Warteschlange** (16.10.2026, vorher offene Frage 9): mit S1.7 über die Tabelle `jobs` zusammen mit dem Index (umgesetzt).
16. **PDF unter Windows** (16.10.2026, vorher offene Frage 10): nur über das optionale `pdftotext`, Installation im README (umgesetzt; übersprungene PDFs werden nachgeholt, sobald das Werkzeug da ist).
17. **Teilweise geladene Thunderbird-Nachrichten** (16.10.2026, vorher offene Frage 15): werden mit Hinweis importiert, nicht übersprungen; kommt die vollständige Fassung später per Sync, stehen beide im Archiv (umgesetzt S1.10).
18. **Täglicher Audit-Export** (16.10.2026, Auftrag S1.11): als Windows-Aufgabe (PowerShell-Registrierungsskript, Windows PowerShell 5.1, UTF-8 mit BOM, ASCII, keine Aliase) und als systemd-Timer; Ziel konfigurierbar (`[audit] export_dir`, z. B. ein Google-Drive-Ordner); JSONL mit SHA-256-Kette je Datei und Manifest (umgesetzt S1.11).

Entscheide des Betreibers vom **18.10.2026** ([A02](../auftraege/A02_s2.md), Abschnitt 0, „Entscheide des Betreibers"): Alle bis dahin offenen Fragen (Stand nach S1.11) sind mit ihrer Standardantwort entschieden. Die Nummern 19 bis 33 entsprechen den früheren offenen Fragen 1 bis 15; die Abschnitte „Stand S1.x" oben verweisen mit „Frage N" auf diese früheren Nummern (Frage N = Nummer 18 + N).

19. **Name** (18.10.2026, A02; vorher Frage 1): PostKauz ist geprüft und bleibt (15.10.2026); Umbenennung erledigt.
20. **Mehrere Menschen an einem Postfach** (18.10.2026, A02; vorher Frage 2): Im MVP gibt **ein** Mensch frei; `decided_by` ist der Name aus `[operator] name` der Konfiguration, sonst der Benutzername des Betriebssystems. Zuordnung je Person kommt mit Mehrbenutzerbetrieb (S4+).
21. **Spike-Lücken** (18.10.2026, A02; vorher Frage 3): P1 und P2 misst der Betreiber nach D3 mit seinem Konto, nur lesend; P6 und P8 bleiben zurückgestellt.
22. **Kopien als eine Nachricht** (18.10.2026, A02; vorher Frage 4): so lassen.
23. **Dauerläufer und `mail_sync_now`** (18.10.2026, A02; vorher Frage 5): S3 (Job-Typ `sync`, MCP reiht ein). S2 legt mit dem Job `draft_push` die Mechanik dafür an (A02, Abschnitt 1, E1).
24. **Befunde der Gegenprüfung (a–d)** (18.10.2026, A02; vorher Frage 6): alle vier als Nachbesserung in S2.0 (A02, Teil A: A1, A2, A3, A4; erledigt mit S2.0 = A1 bis A6 und A8, siehe „Stand S2.0, Teil 1" und „Teil 2").
25. **Suchlatenz** (18.10.2026, A02; vorher Frage 7): auf dem Windows-PC nachmessen, erst bei spürbaren Wartezeiten optimieren.
26. **Import-Durchsatz** (18.10.2026, A02; vorher Frage 8): auf dem Windows-PC nachmessen, Parallelisierung erst bei Bedarf.
27. **Emoji-Sequenzen** (18.10.2026, A02; vorher Frage 9): so lassen (Sicherheit vor Darstellung).
28. **Weitere eigene Adressen (Aliasse)** (18.10.2026, A02; vorher Frage 10): Feld am Konto in S2.0; S2 nutzt es für „Allen antworten" und die Versanderkennung (umgesetzt S2.0/A6: `accounts.aliases`, `account set --aliases`).
29. **Teilwortsuche ohne Umlaut** (18.10.2026, A02; vorher Frage 11): hinnehmen.
30. **Grenzen von `alternative_mismatch`** (18.10.2026, A02; vorher Frage 12): hinnehmen.
31. **Code-Signatur** (18.10.2026, A02; vorher Frage 13): bis 1.0 unsigniert mit `SHA256SUMS`.
32. **Audit-Rotation** (18.10.2026, A02; vorher Frage 14): mit dem Löschkonzept in S3.
33. **Panics in Nebengoroutinen** (18.10.2026, A02; vorher Frage 15): eigene Hilfsgoroutinen in S2.0 mit demselben `recover` versehen (A02, Teil A, A5).

34. **Go-Patchstand** (21.10.2026, Betreiber `##`; früher Offene Frage 3 aus S2.0): Programme werden ab jetzt mit **Go 1.27.3** gebaut (`toolchain go1.27.3` in `go.mod`; `go.mod` behält `go 1.27`); CI nutzt `1.27.x` und damit mindestens diesen Stand. Grund: `make vuln` fand mit 1.27.0 neun erreichbare Schwachstellen der Standardbibliothek, mit 1.27.3 keine. Mit `GOTOOLCHAIN=local` bricht ein Build ohne passende Toolchain ab, statt still nachzuladen (ADR-008).

35. **Abnahme D3.5 und erstes Release** (21.10.2026, Betreiber `##`): Schnitt 1 ist angenommen (D0–D3 bestanden, Protokolle `docs/protokolle/`), Nachbesserung A7 (Befund D3.3c) zu Beginn von S2.1. Erstes Release = **0.1.0-rc2 aus `main`** mit Go 1.27.3 statt eines Tags `v0.1.0-rc1` auf den alten Stand `caee5d6`/`64e516c`. Außerdem: Abnahmen führt Claude als Skript auf dem Rechner des Betreibers aus; der Betreiber entscheidet bei Abweichungen und übernimmt nur Schritte, die sein Postfach, seine Mailclients oder sein Urteil brauchen (A02 Teil D wird so umgeschrieben).
36. **Passwort eines Kontos neu setzen** (22.10.2026, Betreiber `#2`; vorher Offene Frage 1 aus S2.0): `account set <name> --password-stdin` (Operation `accounts.set`, `admin`, HumanOnly) kommt in S2.1; bis dahin hilft `POSTKAUZ_PASSWORD_<NAME>`.
37. **Zwischenspeicher der UID-Liste bei Servern ohne UIDNEXT** (22.10.2026, Betreiber `#2`; vorher Offene Frage 2): so lassen, weil kein Server der Testmatrix (Dovecot, Stalwart, GreenMail) betroffen ist; der stündliche volle Abgleich fragt ohnehin frisch.

38. **Ablauf noch nicht entschiedener Vorlagen** (22.10.2026, S2.1; Standardantwort von Claude, Einspruch des Betreibers jederzeit möglich): Auch eine Vorlage in `pending_approval` verfällt nach `approval_ttl` (Standard 7 Tage, gerechnet ab der Vorlage, nach `approve` neu ab der Entscheidung); der Entwurf geht zurück auf `draft`. `approval_ttl` steht in `config.toml`, wie A02 es vorgibt.
39. **Anhänge mit Typ-Widerspruch oder ausführbare Anhänge** (22.10.2026, S2.2; Standardantwort, Einspruch möglich): erlaubt, mit dem erkannten Typ; S2.3 zeigt im Ergebnis von `drafts.create`/`drafts.update` und in `draft show` eine Warnung. Begründung: Der Mensch sieht den Entwurf vor dem Senden; ein hartes Verbot gehört in die Policy (S3).
40. **`policy show --key` und Operationen nur für Menschen** (22.10.2026, S2.1-Befund zu D4.5; Standardantwort): `policy show --key <name>` zeigt sie in S2.3 als eigene Liste mit `human_only`, damit sichtbar ist, was der Schlüssel **nicht** darf; MCP bietet sie weiterhin nie an.
41. **Präfixregel je Art und `mailto:`-Links** (22.10.2026, S2.2; Standardantwort): so lassen (siehe „Stand S2.2").

42. **Festlegungen aus S2.3–S2.5** (22.10.2026; Standardantworten von Claude, Einspruch möglich): `draft list` zeigt den Betreff wie `search`; der Hook läuft auch im Trockenlauf; `draft request` endet bei `hook: fail` mit Exit 0; der Beispiel-Hook kommt in S2.9 ins Release-Paket und in `abnahme-d4-daten.zip`; ein `ready`-Entwurf, der beim Push nicht mehr zur Freigabe passt, fällt auf `draft` und wird danach als gewöhnlicher Entwurf abgelegt; der Text „within 10 s" bleibt (gemessen 10,0 bis 10,2 s, in `docs/cli.md` erklärt).
43. **Mehrere Kopien gleicher Message-ID im Entwürfe-Ordner** (22.10.2026, Betreiber `##`; vorher Offene Frage 1 aus S2.5, Gegenprüfung K6): Der Push legt einen geänderten Entwurf erst ab, wenn er 30 Sekunden unverändert ist (weniger Zwischenstände); `draft show` und `draft list` warnen, solange ältere Kopien im Ordner liegen. Das Aufräumen alter Kopien (Löschen) entscheidet S3. Umsetzung mit S2.6/S2.7.
44. **Festlegungen aus S2.6–S2.8 und D4** (22.10.2026; Standardantworten von Claude, Einspruch des Betreibers jederzeit möglich): Betreff im Rückfall-Abgleich genau wie E5 (Groß-/Kleinschreibung zählt); neuer Entwurf sofort ablegen, geänderter nach 30 s Ruhe; ein Mailprogramm, das die unveränderte Entwurfskopie nach Gesendet **verschiebt**, wird nicht als Versand erkannt (dann `draft mark-sent`) — in D5 messen; `draft edit --identity` gibt es nicht (Entwurf neu anlegen); `reject --reason` bleibt freiwillig; Exit 4 kommt in S2 nicht vor; `draft show` zeigt die Herkunft der Kopffelder nur mit `--json`; Stildatei nur als reguläre Datei, kein Symlink; `abnahme-d4-daten.zip` entfällt (das D4-Skript baut seine Testdaten selbst); der MCP-Eintrag für D4.8 nutzt `POSTKAUZ_KEY_FILE`; `scripts/abnahme-d4-trocken.sh` bleibt im Repository.
45. **Festlegungen aus S2.9** (23.10.2026; Standardantworten von Claude, Einspruch möglich): `[draft] mailbox` bleibt global (je Konto erst in S3; D5 sagt, die Zeile nach Runde 3 zu entfernen); die Warnung „kein Empfänger" erscheint nur bei `create`/`update`, nicht in `draft show`.
46. **Rahmen für D5** (23.10.2026, Betreiber `##` und `#`): (1) Claude führt gegen das echte Postfach nur lesende Kommandos und je Runde genau ein `draft reply` aus; App-Passwort, Senden und Urteil macht der Betreiber. (2) Nach D5 werden die Testkonten entfernt (App-Passwörter widerrufen, Schlüsselbund-Einträge und `C:\PostKauz\d5` löschen). (3) Postfach B ist ein neues, leeres **Hostinger-Testpostfach** (keine Mails Dritter im Spiegel), Ersatz web.de. Outlook.com scheidet für D5 aus: IMAP dort seit 16.09.2024 nur mit OAuth2, und der OAuth-Anmeldeweg von PostKauz ist erst ein Entwurf (Prüfpunkt P3: Token-Quelle und SASL XOAUTH2 vorhanden, Anmelde-Flow und Refresh fehlen).

## Offene Fragen

Stand S2.9 (23.10.2026). Die Fragen bis S2.9 sind entschieden (oben, Nummern 19 bis 46). Derzeit keine offene Frage an den Betreiber; offen sind D5 und D6; dazu die Nachbesserungen aus der Gegenprüfung vom 24.10.2026 (A03 Teil A).
