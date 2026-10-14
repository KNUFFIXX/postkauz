# Bericht Phase-0-Spike S0 – Go + go-imap v2 als Protokollbasis

Stand: 14.10.2026 · Linux-Container, 2 Kerne, 7 GB RAM, Go 1.24.7, Server lokal im Docker (keine WAN-Latenz).
Rohdaten: reports/scenario-{dovecot,stalwart,greenmail}.json, reports/parse.json, reports/fts.json, reports/sync-dovecot-plain-word-decoder.json.
(Bericht zum Spike-Lauf; Rohdaten unter reports/.)

## Urteil: Gate Go bestanden mit Auflagen

Alle Pflichtabläufe (Erstsync in Fenstern, Inkrementalsync mit CONDSTORE bzw. Vollvergleich, Löschungserkennung, Draft-APPEND, IDLE, Reconnect mit gespeichertem Zustand) laufen gegen drei Server fehlerfrei. Die Inkrementalläufe erkennen auf allen drei Servern genau die von außen gemachten Änderungen (5 Flags, 1 Löschung, 1 neue Mail). Parser, Threading, FTS5 und der CGO-freie Cross-Compile funktionieren. Die Auflagen kommen aus Lücken in go-imap v2 und aus der Go-Version; keine ist ein Blocker.

### Auflagen
1. Go-Version neu entscheiden: Go 1.24 bekommt seit Go 1.26 (Feb. 2026) keine Sicherheitsupdates mehr (aktuell 1.27.2). Das neueste modernc.org/sqlite (v1.60.1) braucht Go 1.26; mit 1.24 ist v1.46.1 (SQLite 3.51.2) das Maximum. Empfehlung: Go 1.26/1.27. Bis dahin sqlite auf v1.46.x festhalten und in CI GOTOOLCHAIN=local setzen (sonst hebt `go get` die go-Zeile still an – im Spike passiert).
2. go-imap hinter eine dünne eigene Schicht (internal/imapx), v2.0.0-beta.8 pinnen, Fork-Bereitschaft.
3. Watchdog ist Pflicht: Deadline je Kommando → Close + Reconnect mit Backoff; IDLE selbst alle 5–10 min neu starten (in der Bibliothek fest 28 min); TCP-Keepalive über net.Dialer.KeepAliveConfig.
4. Immer WordDecoder mit go-message/charset.Reader setzen und per Test absichern.
5. Löschungen per UID-SEARCH-Abgleich (umgesetzt); QRESYNC später per Upstream-Beitrag oder Fork.
6. Parser go-message; enmime nicht verwenden.

## Fähigkeitsmatrix
| Fähigkeit | Dovecot 2.4.5 | Stalwart 0.11.8 | GreenMail latest |
|---|---|---|---|
| IMAP4rev2 | ja | ja | nein |
| CONDSTORE | ja | ja | nein |
| QRESYNC | ja | ja | nein |
| IDLE / MOVE / UIDPLUS / LITERAL+ | ja | ja | ja |
| SPECIAL-USE | ja | ja | nein |
| LIST-STATUS | ja | ja | nein (STATUS je Ordner) |
| ENABLE / NAMESPACE | ja | ja | nein |
| Klartext-LOGIN Port 143 | ja (Override) | nein (nur IMAPS) | ja |
| Ordner-Trenner | / | / | . |
Stalwart: HIGHESTMODSEQ ist kontoweit (alle Ordner gleicher Wert) → jede Änderung löst CHANGEDSINCE in allen Ordnern aus (je 1 Roundtrip, leer).

## Zahlen
Korpus: 1.000 Mails (4,9 MB), 60 Threads/293 Mails, 45 Newsletter, 157 mit Anhängen, 50 kaputt; utf-8/iso-8859-1/windows-1252, RFC 2047 B+Q, RFC 2231.
| Messung | Dovecot | Stalwart | GreenMail |
|---|---|---|---|
| Seeding APPEND (Python) | 18,8/s | 22,2/s | 21,8/s |
| Erstsync 980–986 Mails, Fenster 200 | 0,79 s = 1.248/s | 0,90 s = 1.087/s | 3,27 s = 300/s |
| Meta-FETCH / BODY[] / SQLite+FTS+Blob | 0,16/0,10/0,51 s | 0,17/0,15/0,57 s | 0,86/1,55/0,49 s |
| Spitzen-Heap / Sys | 3,8/12 MB | 3,2/16 MB | 3,1/12 MB |
| Envelope-Betreff korrekt | 929/929 | 930/930 | 930/930 |
| Inkrementell nach Mutation | 26 ms (CHANGEDSINCE) | 25 ms | 816 ms (Vollvergleich) |
| 5 Flags / 1 Löschung / 1 neu erkannt | ok | ok | ok |
| Leerlauf-Inkrementallauf | 20 ms | 16 ms | 701 ms |
| Draft-APPEND + Zurücklesen | 5 ms, 10/10 Prüfungen, Bytes identisch | 2 ms ok | 2 ms ok |
| IDLE-Meldung (APPEND nach 3 s / 10 s) | 505 / 18 ms | 10 / 6 ms | 1,6 / 1,2 ms |
| RST während IDLE erkannt / Reconnect | <1 ms / 17 ms | <1 ms / 3 ms | <1 ms / 45 ms |
| doveadm kick erkannt / Reconnect | 98–109 ms / 15 ms | – | – |
| Blackhole (stiller Abbruch) erkannt | nein (20 s) | nein | nein |
| Wiederaufnahme mit gespeichertem Zustand | 3/3 UIDs | 3/3 | 3/3 |
Dovecot geht nach 5 s in Hibernate (imap_hibernate_timeout = 5s im Image); daher 505 ms im aktiven IDLE und 18 ms im Hibernate.
Ohne eigenen Charset-Decoder (-plain-word-decoder): 149 von 929 Envelope-Betreffen (16 %) bleiben still roher =?windows-1252?q?…?=-Text.

## Probleme mit go-imap v2 (v2.0.0-beta.8)
1. Weiterhin Beta, keine stabile v2.
2. Kein QRESYNC: Enable(QRESYNC) wird clientseitig abgelehnt („not supported"), kein SELECT-QRESYNC, kein VANISHED-Parsing. Workaround UID SEARCH UID 1:<uidnext-1> + lokaler Abgleich (Kosten linear, für 50k+ messen).
3. Keine Kommando-Timeouts: Login().Wait() hängt bei schweigendem Server endlos (im Spike passiert), IdleCommand.Close/Wait hängt, Blackhole wird nicht erkannt, IDLE-Neustart fest 28 min.
4. Standard-WordDecoder ohne Charsets, Fehler werden verschluckt (16 %). Workaround: Options.WordDecoder mit charset.Reader.
5. Unilaterale Daten nur per Callback beim Verbindungsaufbau, synchron im Lese-Goroutine; EXPUNGE nur mit Sequenznummer (Seq→UID-Abbildung selbst führen).
6. Kein Raw-Command-Notausgang (QRESYNC, X-GM-EXT-1 nur per Fork).
7. Ohne Workaround ok: Streaming-FETCH in Fenstern, CHANGEDSINCE, APPENDUID, LIST RETURN (SPECIAL-USE STATUS), UID EXPUNGE, IDLE, ESEARCH. Keine Panics; Race-Detector nicht gelaufen.

## Parser-Vergleich (1.000 Rohmails offline)
| | go-message v0.18.2 | enmime v1.3.0 |
|---|---|---|
| Laufzeit | 0,06 s | 0,47 s |
| harte Fehler/Panics | 0 | 0 |
| Betreff korrekt (950) | 950 | 950 |
| Anhangsnamen RFC 2231 (157) | 157 | 157 |
| Message-ID (inkl. ohne <>) | 1.000 | 1.000 |
| Text mit Umlauten ohne Ersatzzeichen | 942 | 914 |
| Mojibake im Text | 0 | 30 Mails |
| fehlendes Date | Nullzeit ohne Fehler (9) | Fehler (9) |
| Zeitzone „0100" | Fehler (8) | Fehler (8) |
| 40-fach verschachtelt, 40-KB-Zeile, 8-Bit-Header | ok | ok |
| Threads (Union-Find Message-ID/In-Reply-To/References) | 60/60 exakt, 0 vermischt, 707 Einzelmails | identisch |
942 vs. 950: Texte ohne Umlaute (0 Ersatzzeichen). enmime überschreibt per Heuristik korrekt deklariertes UTF-8 („Character Set Declaration Mismatch") → Mojibake. Empfehlung go-message; App muss fehlendes Date (Nullzeit → INTERNALDATE) und „0100" selbst abfangen. Betreff-basiertes Threading nicht implementiert.

## FTS5 (modernc.org/sqlite v1.46.1 = SQLite 3.51.2, ENABLE_FTS5, kein CGO)
Tokenizer: unicode61 (+remove_diacritics 2), ascii, porter, trigram (+case_sensitive 0 remove_diacritics 1) ja; icu nein. Indexaufbau 1.000 Mails, 2 Tabellen: 0,25 s. Contentless-Tabellen mit contentless_delete=1 funktionieren.
| Abfrage | Tabelle | Treffer | Soll |
|---|---|---|---|
| "Zählerstände" | unicode61 | 325 | 325 |
| zahlerstande | unicode61 rd2 | 325 | 325 |
| steuer (Teilwort) | unicode61 | 0 | 310 |
| Grund* | unicode61 | 310 | 310 |
| Erdgeschoss / Erdgeschoß | unicode61 | 0 / 80 | ß≠ss |
| "steuer" | trigram | 310 | 310 |
| "ählerst" / "ahlerst" | trigram rd1 | 325 / 325 | 325 |
| "üb" | trigram | 0 | <3 Zeichen |
unicode61 + Trigram trägt; ß/ss-Faltung und Komposita-Teilwörter muss der app-seitige Normalisierer/Stemmer übernehmen oder die Suche fällt auf die Trigram-Tabelle zurück.

## Cross-Compile (CGO_ENABLED=0)
| Ziel | kalt | inkrementell | Größe | -s -w |
|---|---|---|---|---|
| linux/amd64 | 37,0 s | 0,98 s | 16,7 MB | 11,4 MB |
| windows/amd64 | 34,1 s | 1,15 s | 17,2 MB | 11,7 MB |
| darwin/arm64 | 34,4 s | 1,22 s | 16,2 MB | 11,3 MB |
Kalt dominiert modernc sqlite/libc. Windows/macOS nur gebaut, nicht ausgeführt.

## Testserver-Probleme
- Kein IPv6 im Container: Dovecot `listen = *` per Override, Stalwart sed auf 0.0.0.0 (testmail/stalwart-setup.sh).
- Stalwart 0.16.26 (latest) startet im Bootstrap-Modus, lädt die Web-UI aus dem Internet und hat kein /api/principal mehr → nach 2 Versuchen 0.11.8; 0.16 später nachtesten.
- Stalwart: kein LOGIN auf Klartext-Port → IMAPS mit -insecure (nur Docker).
- doveadm kick trennt alle Sitzungen des Nutzers.

## Offene Fragen
1. Echte Anbieter (Gmail X-GM-EXT-1/kein QRESYNC, M365, IONOS/Strato/T-Online). 2. 50k+ Mails, WAN-Latenz. 3. OAuth2 via go-sasl, Keyring. 4. QRESYNC: Upstream oder Fork. 5. Go-Version (Auflage 1). 6. COMPRESS, BINARY, UIDVALIDITY-Wechsel nicht gezielt getestet. 7. Race-Detector und Unit-Tests fehlen.

Spike-Code ist Wegwerf-Code; Tabellen (mailboxes, messages, message_mailboxes, FTS5) und blobs/<sha256>.eml entsprechen Entscheidung 4 und taugen als Ausgangspunkt für S1.
