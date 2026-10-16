# CLI

Stand: S1.9 (16.10.2026). Implementiert und getestet sind `version`, `doctor`, `accounts list` (aus der Datenbank), `account add`, `account test`, `account remove`, `sync` (Erstsync, inkrementell, `--watch`; danach Extraktion und Suchindex), `mailbox list`, `show` (voll: Kopf, Absenderprüfung, Text, Hinweis auf versteckten Text, Links, Anhangsliste, `--strip-quotes`, `--max-chars`), `search` (Abfragesprache, `--limit`, `--cursor`, `--mailbox`, `--fields`, NDJSON), `thread`, `attach list`, `attach text`, `key create/list/revoke`, `audit`, `audit export`, `audit verify`, `policy show` sowie die globalen Flags `--json`, `--dry-run`, `--account`/`-a`, `--key`, `--quiet`, `--profile` und `--explain`; seit S1.9 `mcp` (Abschnitt 2h). Alles andere in diesem Dokument ist **Entwurf** und mit dem Schnitt markiert, in dem es entsteht.

## 1. Befehlsstruktur

```
postkauz <objekt> <verb> [ziel] [optionen]        Alias: pk
```

Jedes Kommando, das eine Operation des Registers ausführt, ist in `internal/cli/bindings.go` ausdrücklich an diese Operation gebunden (Tabelle, keine Ableitung). Ohne Argumente zeigt `postkauz` die Hilfe (ein interaktiver Modus ist nicht geplant).

Der Alias `pk` ist eine Kopie oder Verknüpfung der Binärdatei (`make install` legt sie an); cobra kennt für die Wurzel keine Aliase.

### Globale Optionen

| Option | Bedeutung | Stand |
|---|---|---|
| `--json` | maschinenlesbare Ausgabe (Listen wie `search` und `audit` als NDJSON); Fehler als JSON auf stderr | Gerüst, NDJSON S1.7/S1.8 ✓ |
| `--dry-run` | beschreiben, was geschähe, nichts verändern | Gerüst |
| `--account`, `-a` | Konto wählen (sonst das einzige Konto; bei mehreren ist es Pflicht) | S1.4 ✓ |
| `--key <name>` | mit den Rechten dieses API-Schlüssels handeln (Probe der Policy; Abschnitt 2e). Erwartet den **Namen**, nie den Schlüssel selbst | S1.8 ✓ |
| `--quiet`, `-q` | keine Fortschrittsausgabe (Sync, Index) auf stderr | S1.8 ✓ |
| `--profile <name>` | getrennte Instanz: eigene Konfiguration und eigene Daten (Abschnitt 5) | S1.8 ✓ |
| `--explain` | Policy-Entscheid und Audit-ID des Aufrufs auf stderr, ohne Inhalte; bei `search` zusätzlich die Übersetzung der Anfrage (Abschnitt 2d) | S1.8 ✓ |
| `--yes` | Rückfrage bestätigen (nie interaktiv im `--json`-Modus) | S2 |

### Kommandos nach Schnitt

| Schnitt | Kommando | Operation |
|---|---|---|
| Gerüst | `version`, `doctor` | – (Hilfskommandos, keine Operationen) |
| Gerüst | `accounts list` | `accounts.list` |
| S1.1 ✓ | `account add`, `account test`, `account remove` (siehe Abschnitt 2a) | `accounts.add`, `accounts.test`, `accounts.remove` (Scope `admin`, R1; nicht über MCP) |
| S1.4/S1.5 ✓ | `sync [--full] [--watch]` (siehe Abschnitt 2b) | `sync.run` (Scope `mail:sync`, R1) |
| S1.5 ✓ | `mailbox list` | `mailboxes.list` (R0) |
| S1.5–S1.8 ✓ | `show <id>` bzw. `show <Message-ID>`, `--headers`, `--strip-quotes`, `--max-chars` | `messages.get` (R0, `mail:read`; Untrusted-Hülle seit S1.8) |
| S1.6/S1.8 ✓ | `thread <thread-id>` bzw. `thread --message <id>`, `--strip-quotes`, `--max-messages`, `--max-chars` | `threads.get` (R0, `mail:read`) |
| S1.6/S1.8 ✓ | `attach list <message-id>`, `attach text <attachment-id> [--max-chars N]` | `attachments.list`, `attachments.text` (R0, `mail:read`) |
| S1.7/S1.8 ✓ | `search "<abfrage>"` (siehe Abschnitt 2d) | `messages.search` (R0, `mail:search`) |
| S1.8 ✓ | `key create`, `key list`, `key revoke` (Abschnitt 2e) | `keys.create` (R1), `keys.list` (R0), `keys.revoke` (R1); Scope `admin`, nur Mensch, nicht über MCP |
| S1.8 ✓ | `audit [--since …]`, `audit export --to <ordner>`, `audit verify` (Abschnitt 2f) | `audit.list` (R0, `audit:read`), `audit.export` (R1, `admin`), `audit.verify` (R0, `audit:read`) |
| S1.8 ✓ | `policy show` (Abschnitt 2e) | `policy.get` (R0, jeder Schlüssel) |
| S1.9 ✓ | `mcp` (Abschnitt 2h) | startet den stdio-MCP-Server (kein Registerbefehl; jeder Tool-Aufruf läuft einzeln durch den Executor) |
| S1 | `import mbox`, `import maildir` | `import.run` |
| S2 | `draft new`, `draft reply`, `draft forward`, `draft list`, `draft show`, `draft edit` | `drafts.*` |
| S2 | `approve <id>`, `reject <id>` | `approvals.decide` |
| S3 | `tag`, `mark`, `move`, `trash`, `attach save` | `messages.*`, `attachments.save` |
| S3 | `export`, `backup`, `restore` | `export.run`, `backup.run`, `restore.run` |

Es gibt **kein** `send`, kein `rule` und kein `serve --rest` vor Schnitt 4.

## 2. Beispiele

Mit „(Entwurf)" markierte Beispiele laufen erst im genannten Schnitt.

```sh
# Heute lauffähig
postkauz version --json
postkauz doctor
postkauz accounts list --json
postkauz account add demo --imap imap.example.org:993 --user demo@example.org   # Passwort wird abgefragt, landet im Schlüsselbund
postkauz account test demo                                                      # Login, Fähigkeiten, Ordnerrollen
postkauz account remove demo --dry-run

postkauz sync -a demo --full                                                    # Erstsync in Fenstern, wiederaufnehmbar
postkauz sync --watch                                                           # Dauerläufer mit IDLE
postkauz mailbox list -a demo --json
postkauz show 18233 --headers

postkauz search "from:anna after:2026-01-01 has:attachment" --limit 50 --json   # S1.7 ✓

postkauz thread 4411 --strip-quotes                                        # S1.6/S1.8 ✓
postkauz attach list 18233                                                  # S1.6 ✓
postkauz key create --name assistant-1 --role assistant --expires 90d       # S1.8 ✓, Klartext erscheint genau einmal
postkauz search Rechnung --key assistant-1 --explain                        # S1.8 ✓, Probe: was darf der Schlüssel?
postkauz audit --since 24h --actor agent --json                             # S1.8 ✓
postkauz audit export --to D:\audit-export                                  # S1.8 ✓
postkauz audit verify                                                       # S1.8 ✓, Export-Prüfsummen gegen die Datenbank

# S1 (Entwurf)
postkauz import maildir ./Mail/Archive --account demo --mailbox Archive-2024

# S2 (Entwurf)
postkauz draft reply 18233 --all --body-file antwort.md --quote             # legt den Entwurf in den Entwürfe-Ordner
postkauz draft list --status pending_approval
postkauz approve 17                                                         # Mensch gibt den Entwurf frei (Status ready)
```

Der Mensch sendet den Entwurf in seinem gewohnten Mailclient; PostKauz erkennt den Versand beim nächsten Sync.

## 2a. Konten (S1.1)

`account add <name> --imap host[:port] --user <login> [Optionen]`

| Option | Bedeutung |
|---|---|
| `--imap` | Server als `host:port`; ohne Port 993 |
| `--user` | Anmeldename |
| `--email` | Adresse; Standard ist der Anmeldename, wenn er ein `@` enthält |
| `--tls` | `tls`, `starttls` oder `none`. Ohne Angabe: `tls` bei Port 993 (bzw. …993), sonst `starttls`; mit `--insecure-test-server` `none` |
| `--auth` | `password` (Standard) oder `app_password`; `oauth2` ist noch gesperrt (Prüfpunkt P3, nur Entwurf) |
| `--insecure-test-server` | erlaubt Klartext und selbstsignierte Zertifikate, **nur** für Hosts auf dem eigenen Rechner (`localhost`, `127.0.0.1`, `::1`) |
| `--password-stdin` | Passwort aus der Standardeingabe (eine Zeile), z. B. für Skripte |
| `--password-from-env` | kein Passwort speichern; es kommt zur Laufzeit aus `POSTKAUZ_PASSWORD_<NAME>` (Server, Docker) |

Ohne `--password-stdin`/`--password-from-env` fragt die CLI das Passwort verdeckt ab; im `--json`-Modus oder ohne Terminal ist das ein Eingabefehler (Exit 2). Das Passwort wird **nie** als Kommandozeilenargument angenommen. Kontonamen: Kleinbuchstaben, Ziffern, Bindestrich (max. 63); daraus entsteht die Variable `POSTKAUZ_PASSWORD_<NAME>` (Bindestrich wird `_`).

- `account test <name>` meldet sich an, liest Fähigkeiten und Ordner (mit Rolle) und meldet sich ab; am Server ändert sich nichts. Lokal werden Fähigkeiten und Status am Konto gespeichert (`doctor` zeigt den Status). Abgelehnte Anmeldung: Exit 1, Code `auth` (kein automatischer Wiederholungsversuch); Server nicht erreichbar oder Zeitüberschreitung: Exit 5, Code `connection`.
- `account remove <name>` löscht Konto und gespeichertes Passwort. Hat das Konto archivierte Nachrichten, verweigert es das (ADR-003; Archivlöschung kommt mit dem Löschkonzept).
- `--dry-run` wirkt bei allen drei Kommandos (prüfen, nichts ändern).

## 2b. Sync, Ordner, Anzeige (S1.4/S1.5)

`sync [-a <konto>] [--full] [--watch] [--window N] [--interval D] [--idle-restart D] [--mailbox P ...]`

| Option | Bedeutung |
|---|---|
| (ohne) | Erstsync neuer Ordner in Fenstern, sonst inkrementell: neue Nachrichten, Flag-Änderungen (CONDSTORE `CHANGEDSINCE` bzw. Abgleich aller Flags), Löschungen (UID-SEARCH-Abgleich, nur wenn die Zahlen nicht passen) |
| `--full` | zusätzlich vollständiger Lösch- und Flag-Abgleich ohne `CHANGEDSINCE`; holt Fehlendes nach |
| `--watch` | Dauerläufer: sofort ein Lauf, danach alle `--interval` (Standard 5 min) und bei jeder IDLE-Meldung der INBOX (IDLE-Neustart alle `--idle-restart`, 5 bis 10 min, Standard 8 min); Strg+C beendet ihn |
| `--window` | Nachrichten je Fenster im Erstsync (Standard 200) |
| `--mailbox` | nur diese Ordner (mehrfach möglich) |
| `--dry-run` | meldet Ordner und lokalen Stand, schreibt nichts |

- Ausgabe ohne `--json`: Tabelle je Ordner (neu, geändert, entfernt, Fehler, gespiegelt, laut Server), am Ende genau die Zeilen `N messages mirrored` und `N errors`. Fortschritt laufender Erstsyncs steht auf stderr. `N messages mirrored` zählt die Nachrichten des Kontos, die der Server führt, ohne Dubletten über Ordner hinweg.
- Mit `--json`: ein Objekt (`account`, `mode`, `strategy`, `mailboxes[]`, `new`, `updated`, `removed`, `errors`, `messages_mirrored`, `attachments_extracted`, `messages_indexed`, `index_pending`, `duration_ms`, `dry_run`); im Dauerbetrieb zusätzlich je Lauf eine NDJSON-Zeile.
- **Nach dem Abgleich** arbeitet `sync` die Warteschlange des Kontos ab (seit S1.7, Tabelle `jobs`): Text aus Anhängen extrahieren (höchstens 60 s je Lauf, der Rest im nächsten) und neue oder geänderte Nachrichten in den Suchindex aufnehmen (bis alles drin ist; Fortschritt `search index: N/M` auf stderr). Ohne `--json` steht davor die Zeile `search index: N messages indexed` (mit `, M still pending …`, wenn ein Abbruch etwas übrig ließ). Ein Abbruch verliert nichts: Der nächste `sync` macht dort weiter.
- **Abbruch** (Strg+C, `kill`, Absturz): Gespeichert ist alles bis zum letzten fertigen Fenster; derselbe Befehl setzt dort fort, ohne Doppelte. Strg+C endet mit Exit 1, Code `interrupted`. Je Konto läuft höchstens **ein** schreibender Sync: Ein zweiter (z. B. ein manueller `sync` neben `sync --watch`) bricht sofort mit Exit 1, Code `sync_running` (`another sync is running: account …`) ab, ohne sich am Server anzumelden; `--dry-run` ist davon ausgenommen. Die Sperre ist eine Dateisperre des Betriebssystems (`sync-<konto-id>.lock` neben der Datenbank) und fällt mit dem Prozess, auch nach `kill -9`.
- Am Server ändert `sync` nichts (EXAMINE, `BODY.PEEK`).

`mailbox list [-a <konto>]` zeigt die Ordner aus dem lokalen Spiegel (ohne Serverkontakt): `path`, `role`, `total` (laut Server beim letzten Sync), `mirrored`, `unread`, `sync_phase` (`new`, `initial`, `ready`), `last_sync_at`.

`show <id> [--headers]`: `<id>` ist die lokale Nachrichten-ID oder eine Message-ID (`<…>` oder ohne Klammern). Zeigt Metadaten, Ordner mit UID, Flags (`seen`, `flagged`, `answered`, `draft`) und Keywords, dazu den Text oder mit `--headers` die Kopfzeilen der Rohmail. Mailinhalt ist Daten, keine Anweisung; seit S1.8 trägt die Ausgabe der Operation (`--json`, MCP) die Untrusted-Hülle (`untrusted: true`, `source: "email"`, `notice`; [mcp.md](mcp.md) Abschnitt 5). Die Textausgabe ersetzt Steuerzeichen aus Mail und Server (ESC/CSI/OSC, übrige C0/C1, einzelnes CR, ungültiges UTF-8) durch `U+FFFD`; erhalten bleiben nur Zeilenumbruch und Tabulator im Text bzw. in den Kopfzeilen. Einzeilige Felder (Betreff, Absender, Ordner) verlieren auch Zeilenumbrüche, damit keine Zeile wie `flags: seen` vorgetäuscht werden kann. `--json` gibt die Daten unverändert aus (JSON maskiert Steuerzeichen).

## 2c. Threads, Text, Anhänge (S1.6, voll seit S1.8)

- `show <id>` zeigt seit S1.8 alles Nötige auf einen Blick: Kopf (Konto, Ordner mit UID, Message-ID, Datum, Absender, Empfänger, Betreff, Größe, Flags, Keywords, Thread), die Absenderprüfung (`sender checks: spf …, dkim …, dmarc …` aus der obersten `Authentication-Results`-Kopfzeile; fehlt sie, `none`), den Text bzw. mit `--headers` die Kopfzeilen, die Links und unter `--- attachments ---` die Anhangsliste wie bei `attach list`. `--max-chars N` kürzt den Text nach N Zeichen (`[cut after --max-chars]`, `--json`: `truncated: true`).
- `show <id>` zeigt den bereinigten Text: den Klartextteil, sonst den Text aus dem HTML-Teil (`--- text (from HTML) ---`; HTML wird nie angezeigt, Links stehen als „Text <URL>"), darunter die Links mit Host und Warnungen (`punycode`, `mixed_scripts`, `ip_address`, `userinfo`, `text_domain_mismatch`). Enthielt das HTML versteckten Text, steht eine Zeile `warning: hidden text removed …` im Kopf (`--json`: `hidden_text_removed: true`). Weichen bei multipart/alternative Klartext und HTML wesentlich voneinander ab (der Klartext, den `show` und Agenten lesen, ist nicht das, was der Mailclient zeigt), steht `warning: plain text differs from the HTML part …` (`--json`: `alternative_mismatch: true`; `thread --strip-quotes`: `(plain text differs from the HTML part)`). `--strip-quotes` zeigt nur den neuen Text einer Antwort (`text_source` `plain_unquoted`/`html_unquoted`). Die Zeile `thread:` nennt den Thread.
- `thread <thread-id>` zeigt einen Thread: Betreff der ersten Nachricht, Zahl, Zeitspanne, Teilnehmer und je Nachricht ID, Datum, Absender, Betreff, Flags. `--message` nimmt stattdessen eine Nachrichten-ID; `--strip-quotes` hängt den neuen Text jeder Nachricht an. `--max-messages N` zeigt nur die neuesten N Nachrichten (`(K older messages not shown)`, `--json`: `messages_omitted`), `--max-chars N` kürzt jeden Text.
- `attach list <message-id>` listet die Anhänge: ID, MIME-Teil, Dateiname, **erkannter** Typ (Magic Bytes), Größe, Stand der Textextraktion (`pending`, `ok`, `failed`, `skipped`) und Hinweise (`TYPE MISMATCH: declared … content is …`, `EXECUTABLE`, `inline`, `truncated`, `hidden text removed`, Grund einer übersprungenen Extraktion).
- `attach text <attachment-id> [--max-chars N]` zeigt den extrahierten Text. Steht die Extraktion noch aus, extrahiert der Befehl jetzt (Subprozess mit Grenzen, siehe [sicherheit.md](sicherheit.md) Abschnitt 7) und speichert nichts (`note: extracted now; the next sync stores it`); übersprungene oder gescheiterte Anhänge nennen den Grund. Den Text speichert der Sync am Ende jedes Laufs (Zeitbudget 60 s; Ausgabe `attachments_extracted` mit `--json`).
- Fehler: unbekannter Thread oder Anhang → Exit 1, Code `not_found`; keine Zahl als ID → Exit 2.

## 2d. Suche (S1.7, Ergänzungen S1.8)

`search "<abfrage>" [-a <konto>] [--mailbox <ordner>] [--limit N] [--cursor C] [--explain] [--json [--fields a,b]]`

Durchsucht den lokalen Spiegel (ohne Serverkontakt). Ohne `-a` alle Konten. Mehrere Argumente ergeben zusammen die Anfrage (`search from:anna Rechnung`); Phrasen brauchen Anführungszeichen, in der Shell also `search '"Erdgeschoss links"'`.

| Teil | Bedeutung | Beispiel |
|---|---|---|
| Wort | findet Wortformen (Wortstamm: „Rechnungen" ↔ „Rechnung") und, ab 3 Zeichen, Teilwörter in Komposita („Vollstreck" ↔ „Zwangsvollstreckung", „steuer" ↔ „Grundsteuer"); Umlaute und ß in jeder Schreibweise („prüfen" = „pruefen", „Erdgeschoß" = „Erdgeschoss"); kürzere Wörter als Wortanfang („ab" ↔ „Abrechnung") | `Rechnung` |
| `"…"` | Phrase: Wörter direkt hintereinander (Wortformen erlaubt) | `"Erdgeschoss links"` |
| `-…` | schließt aus (Wort, Phrase oder Operator) | `steuer -grundsteuer`, `-has:attachment` |
| `from:` | Absender (Name oder Adresse, auch Teilwort) | `from:anna`, `from:"Anna Beispiel"`, `from:example.org` |
| `to:` | Empfänger (An und Cc) | `to:hausverwaltung` |
| `subject:` | nur im Betreff | `subject:Mahnung` |
| `has:attachment` | mit Anhang (`has:attachments` ebenso) | |
| `is:` | `unread`, `read`, `flagged` (auch `starred`), `answered`, `draft` | `is:unread` |
| `in:`, `mailbox:` | Ordner: Pfad (ohne Groß-/Kleinschreibung) samt Unterordnern, oder Rolle (`inbox`, `sent`, `drafts`, `trash`, `junk`, `archive`, `all`) | `in:inbox`, `mailbox:Archiv` |
| `thread:` | Nachrichten eines Threads (ID aus `show`/`thread`) | `thread:4411` |
| `tag:` | lokales Etikett (Tags ab S3) | `tag:wichtig` |
| `after:`, `before:` | Datum (`YYYY-MM-DD`, auch `YYYY/MM/DD` oder `TT.MM.JJJJ`), Mitternacht in der Zeitzone des Rechners; `after:` einschließlich, `before:` ausschließlich | `after:2026-01-01 before:2026-02-01` |

- Alle Teile sind mit **UND** verknüpft; `OR` ist ein gewöhnliches Wort (kein Operator in S1.7). Unbekannte `name:`-Präfixe bleiben Text (`Re:`, `15:00`). Fehler in Operatoren (`has:pdf`, `after:gestern`) sind Eingabefehler (Exit 2, `invalid_input`); ebenso eine leere Anfrage, mehr als 16 Suchbegriffe oder mehr als 1.024 Bytes.
- **Sortierung:** mit Suchbegriffen nach Relevanz (BM25: Betreff vor Absender vor Text vor Anhängen, Wortstamm-Treffer vor reinen Teilwort-Treffern) mal Datumsanteil (neuere Nachrichten höher); nur mit Operatoren neueste zuerst. Einzelheiten: [datenmodell.md](datenmodell.md), „Volltextsuche".
- Durchsucht werden Betreff, Absender, Empfänger, der bereinigte Text (versteckter Text ist nie im Index) und die Anhänge (Dateinamen und extrahierter Text). Auf dem Server gelöschte Nachrichten bleiben im Archiv und damit auffindbar (`deleted_remote: true`).
- **Ausgabe ohne `--json`:** Tabelle `ID`, `DATE` (Ortszeit), `FROM`, `SUBJECT` (`[attachment]` bei Anhang), darunter `N of M results (by relevance|newest first)`, bei voller Seite `more: --cursor …`, und ein Hinweis, falls Nachrichten noch nicht im Index sind (`note: N messages are not in the search index yet; run sync to finish it`). Keine Treffer: `No messages found.`
- **Mit `--json`: NDJSON, eine Zeile je Treffer** (`id`, `account`, `thread_id`, `date`, `from`, `subject`, `snippet`, `flags`, `has_attachments`, `deleted_remote`, `score`, `cursor`). Jeder Treffer trägt seinen Cursor: Mit `--cursor <cursor eines Treffers>` setzt dieselbe Anfrage genau dahinter fort. Mit `--explain` steht davor eine Zeile `{"explain":…,"total":…,"order":…,"index_pending":…,"next_cursor":…}`. Die Operation selbst (MCP, später REST) liefert ein Objekt mit `hits[]`, `total`, `next_cursor`, `order`, `index_pending` und optional `explain`.
- `--mailbox <ordner>` (seit S1.8) wirkt wie `in:<ordner>` in der Anfrage (Pfad samt Unterordnern oder Rolle); MCP nutzt dafür das Feld `mailbox`.
- `--fields id,subject,date` (seit S1.8, nur mit `--json`): je NDJSON-Zeile nur diese Felder (erlaubt sind die Felder eines Treffers); unbekannte Felder sind ein Eingabefehler (Exit 2).
- `--limit` 1 bis 500 (Standard 20). Ein Cursor gilt nur für dieselbe Anfrage und dasselbe Konto (sonst Exit 2); er ist undurchsichtig.
- **Cursor sind kein Schnappschuss.** Ein Cursor merkt sich Bewertung und ID des letzten Treffers sowie die Uhrzeit der ersten Seite (für die Aktualitätsgewichtung), nicht den Index. Ändert sich der Index zwischen zwei Seiten (ein Sync spiegelt neue Mails, der Index-Job arbeitet nach, eine Nachricht wird neu aufbereitet oder nach einer neuen Index-Version neu aufgebaut), verschieben sich die BM25-Werte: Ein Treffer kann dann auf zwei Seiten stehen oder auf keiner, neue Nachrichten erscheinen erst bei einer neuen Suche, und `total` kann sich von Seite zu Seite ändern. Wer vollständige Listen braucht, sucht ohne laufenden Sync (`sync --watch` anhalten) oder mit großem `--limit` in einem Zug. Stabiler ist die Sortierung nur nach Datum (Anfrage nur mit Operatoren): Neu gespiegelte Nachrichten mit aktuellem Datum stehen vor dem Cursor und erscheinen erst bei einer neuen Suche.
- `--explain` (seit S1.8 eine globale Option, Abschnitt 2g) zeigt bei `search` zusätzlich die Übersetzung: je Suchbegriff Feld, Art (`stem` Wortstamm, `prefix` Wortanfang, `phrase`, `substring` nur Teilwort) und die beiden FTS5-Ausdrücke (Wortindex und Trigram), die Filter, ignorierte Teile und die SQL-Anweisung mit Platzhaltern (`?`; der Suchtext steht nur in den Parametern).
- Ins Audit kommen nur Zahl der Suchbegriffe, Art der Operatoren, `limit` und ob Cursor/Explain/Ordner gesetzt waren; nie der Suchtext.

## 2e. API-Schlüssel und Probe der Policy (S1.8)

`key create --name <name> [--role reader|assistant|operator|admin] [--scopes s1,s2] [--expires 90d|never] [--dry-run-default=false]`

- Legt einen Schlüssel für einen Agenten an (Scope `admin`, nur der Mensch; nicht über MCP). Rollen und Scopes wie [sicherheit.md](sicherheit.md) Abschnitt 2; Standard `assistant`. `--scopes` **verengt** die Rolle (z. B. `--role reader --scopes mail:search` für einen Triage-Agenten ohne Bodies); ein Scope außerhalb der Rolle ist ein Eingabefehler. Laufzeit Standard 90 Tage (`--expires never` ohne Ablauf); `dry_run_default` ist an (schreibende Aufrufe laufen als Trockenlauf, bis der Agent `dry_run: false` setzt).
- Der Schlüssel (`pk_key_` und 43 Zeichen) erscheint **genau einmal** in der Ausgabe (`--json`: Feld `secret`). Gespeichert wird nur sein SHA-256; `key list` zeigt ihn nie, auch nicht den Hash. Verloren: `key revoke` und neu anlegen.
- `key list` zeigt Name, Rolle, Scopes, Status (`active`, `expired`, `revoked`), Trockenlauf-Standard, Anlage, Ablauf und letzte Nutzung.
- `key revoke <name>` widerruft sofort; der Name bleibt belegt (eindeutiges Audit). Ein zweiter Widerruf meldet `already revoked`.
- **`--key <name>`** (global): Der Mensch handelt mit den Rechten dieses Schlüssels, z. B. `search Rechnung --key test-agent --explain` oder `policy show --key test-agent`. So lässt sich vor dem Einsatz prüfen, was ein Agent sehen würde. Erwartet wird der **Name**; ein Schlüssel im Klartext als Argument wird abgewiesen (Exit 2; Kommandozeilen sind in der Prozessliste sichtbar). Unbekannt: Exit 1 `not_found`; widerrufen oder abgelaufen: Exit 3 `denied`. Im Audit steht `key:<name>`.
- `policy show` beschreibt die Richtlinie des (mit `--key` gewählten) Schlüssels: Rolle, Scopes, Trockenlauf-Standard, Ablauf, Zeichenbudget für Agenten (20.000) und die erlaubten Operationen mit Risikostufe. Ohne `--key` ist das der Mensch (`cli`, alle Rechte).

## 2f. Audit (S1.8)

- `audit [--since 24h] [--until …] [--actor human|agent|system] [--key-id key:<name>] [--operation <name>] [--result ok|denied|needs_approval|error] [--limit N] [--after-id N]` zeigt Einträge: ID, Zeit, Akteur, Schlüssel (`cli` für den Menschen, `key:<name>` für API-Schlüssel), Operation, Ergebnis (mit Grund und `dry-run`), Ziele (z. B. `message:42`) und Parameter **ohne Inhalte** (Feldnamen, Zähler; nie Suchtext oder Mailtext). Zeitangaben: Dauer rückwärts (`10m`, `24h`, `7d`), Datum (`2026-10-16`) oder RFC 3339; Standard die letzten 24 Stunden. `--json`: NDJSON, ein Eintrag je Zeile. Bei voller Seite `more: --after-id N`.
- `audit export [--to <ordner>]` schreibt das Audit tageweise (UTC) als JSONL-Dateien `postkauz-audit-JJJJ-MM-TT.jsonl` (0600) an einen **zweiten Ort** (anderer Datenträger oder Rechner) und hält je Tag Prüfsumme, erste/letzte Eintrags-ID und Ort in `audit_exports` fest. Ohne `--to` gilt `[audit] export_dir` (Abschnitt 5). Ein noch laufender Tag wird bei jedem Export neu geschrieben (`updated`). Passt eine früher exportierte Datei nicht mehr zu ihrer Prüfsumme, überschreibt der Export sie nicht (Exit 1, Code `mismatch`): Der Beleg einer Manipulation bleibt.
- `audit verify` prüft jede exportierte Datei gegen ihre Prüfsumme (`export file was changed`, `export file is missing`) und gegen die Zeilen der Datenbank (`database rows differ from the export`). Ergebnis `ok: N exported days checked; …` (Exit 0) oder `mismatch: …` (Exit 1, Code `mismatch`). Tage mit neueren, noch nicht exportierten Einträgen nennt die Zeile `not exported yet (new entries): …` (kein Fehler; `verify` selbst erzeugt einen Eintrag).
- Der tägliche Export als geplante Aufgabe folgt mit S1.11 (bis dahin von Hand oder per Aufgabenplanung).

## 2g. Erklären, leise, Profile (S1.8)

- **`--explain`** (global) schreibt nach jedem Aufruf eine Zeile auf **stderr**: Operation, Scope, Risiko, Schlüssel, Entscheidung der Policy mit Grund, Trockenlauf und die **Audit-ID** des Eintrags, z. B. `explain: messages.search (scope mail:search, risk R0) as key key:leser: allow (ok), dry run no; audit event 42`. Mit `--json` als Zeile `{"explain":{"operation":…,"scope":…,"risk":…,"key":…,"actor":…,"decision":…,"reason":…,"dry_run":…,"audit_id":…}}`. Auch verweigerte Aufrufe werden erklärt. Inhalte stehen nie darin; stdout bleibt die Ausgabe der Operation.
- **`--quiet`**, `-q`: keine Fortschrittszeilen (Sync, Suchindex) auf stderr; Ergebnis und Fehler bleiben.
- **`--profile <name>`**: getrennte Instanz mit eigener Konfiguration und eigenen Daten (Abschnitt 5). Gedacht für Probe- und Abnahmedaten neben dem Alltag.

## 2h. MCP-Server (S1.9)

`postkauz mcp` startet den lesenden MCP-Server über stdin/stdout für einen Agenten (Claude Code, Claude-App, andere MCP-Clients). Tools, Resources, Prompts und Hülle: [mcp.md](mcp.md); Einrichtung: README, „Set up MCP in Claude Code".

- **Schlüssel** nur aus der Umgebung: `POSTKAUZ_KEY` (der Schlüssel aus `key create`) oder `POSTKAUZ_KEY_FILE` (Pfad zu einer Datei mit dem Schlüssel; unter Linux/macOS nur für den Nutzer lesbar, sonst Exit 1). Nie als Argument; `--key` ist bei `mcp` ein Eingabefehler (Exit 2).
- Fehlt der Schlüssel oder ist er unbekannt, widerrufen oder abgelaufen, startet kein Server: Exit 3 (`denied`), Grund auf stderr, nichts auf stdout.
- Die Tool-Liste folgt den Scopes des Schlüssels (`reader`: 9 Tools; verengt auf `mail:search`: nur Suche und Richtlinie). Es gibt kein Tool zum Senden, Löschen, Verschieben oder Entwerfen.
- Jeder Tool-Aufruf und jedes Lesen einer Resource steht im Audit mit `key:<name>` und Akteur `agent`, ohne Mailtext (`postkauz audit --key-id key:<name>`).
- `--profile` und `POSTKAUZ_DATA_DIR` wählen die Datenbank wie bei jedem Kommando. Der Server synct nicht; aktuell hält die Daten `sync` bzw. `sync --watch`.
- Log: Start, Ende und je Aufruf Tool, Schlüssel und Audit-ID in `postkauz.log` (ohne Inhalte); stdout gehört allein dem Protokoll.

## 3. Maschinenlesbarkeit (`--json`)

- **Ein Ergebnis = ein JSON-Objekt** auf stdout, einzeilig. Listen, die streambar sein sollen (Suche, Audit), liefern **NDJSON** (ein Objekt je Zeile) ab S1.
- **Dieselben Felder wie MCP und REST.** Die Ausgabe ist die serialisierte Ausgabe der Operation (ihr `OutputSchema`); es gibt keine CLI-eigene Form. Die Registerversion steht in `postkauz version --json` (`schema_version`). Ausgaben mit Mailinhalt (`show`, `thread`, `search`, `attach list`, `attach text`) tragen oben die Untrusted-Hülle `untrusted`, `source`, `notice` (seit S1.8). Der Vertragstest `TestCLIContractRegistry` prüft jede Eingabe, die ein Kommando baut, gegen das `InputSchema` und jede `--json`-Ausgabe gegen das `OutputSchema` der Operation; `TestBindingsCoverCatalogAndSchemas`, dass jede Operation genau ein Kommando hat und jedes Feld des `InputSchema` über ein Argument oder eine Option erreichbar ist.
- **Fehler** als JSON auf stderr: `{"error":{"code":"…","message":"…","hint":"…"}}`. Codes: `usage`, `invalid_input`, `denied`, `needs_approval`, `unknown_operation`, `exists`, `not_found`, `auth`, `connection`, `secret_store_unavailable`, `no_secret`, `schema_too_new`, `interrupted`, `sync_running`, `mismatch`, `error`.
- **Keine interaktiven Rückfragen im `--json`-Modus.** Fehlt eine nötige Bestätigung, endet der Aufruf mit Exit-Code 2 oder 4; er blockiert nie. Das ist wichtig für Agenten, die die CLI statt MCP nutzen.
- **Keine Geheimnisse in der Ausgabe** (Test mit Kanarienwerten ab S1).
- `--cursor` gibt es seit S1.7 bei `search` (Abschnitt 2d), `--fields id,subject,date` seit S1.8 (nur `search`); `--explain` ist seit S1.8 global (Abschnitt 2g).
- **Eingaben prüft der Executor** gegen das `InputSchema` der Operation (seit S1.8, für CLI und MCP gleich): falscher Typ, Wert außerhalb der Grenzen, unbekanntes Feld → Exit 2, Code `invalid_input`; der Aufruf steht trotzdem im Audit (`error`, Grund `invalid_input`).

## 4. Exit-Codes

Festgelegt in `internal/cli/exit.go`, getestet in `internal/cli/contract_test.go`: `TestExitCodesMatchDocs` liest diese Tabelle und die Liste der Fehlercodes (Abschnitt 3) und vergleicht sie mit dem Code; `TestExitCodesEndToEnd` löst die Codes 0, 1, 2, 3 und 5 mit echten Aufrufen aus (4 ist erst mit Freigaben in S2 erreichbar).

| Code | Name | Bedeutung |
|---|---|---|
| 0 | `ExitOK` | erfolgreich |
| 1 | `ExitError` | allgemeiner Fehler; auch Abbruch per Strg+C (`interrupted`) |
| 2 | `ExitUsage` | Eingabefehler (unbekanntes Kommando oder Flag, fehlendes Argument, ungültige Eingabe der Operation) |
| 3 | `ExitDenied` | Scope oder Policy verweigert |
| 4 | `ExitNeedsApproval` | Freigabe erforderlich (Ausgabe enthält `approval_id`, sobald es Freigaben gibt) |
| 5 | `ExitConnection` | Verbindung zum Server fehlgeschlagen oder Zeitüberschreitung (Watchdog) |

## 5. Konfiguration und Pfade

- **Konfigurationsdatei** (TOML, optional): `POSTKAUZ_CONFIG`, sonst `<Konfigurationsordner des Betriebssystems>/postkauz/config.toml`. Unbekannte Schlüssel sind ein Fehler. Inhalt:

  ```toml
  data_dir  = ""            # leer = Standard des Betriebssystems (siehe unten)
  log_level = "info"        # debug | info | warn | error
  [secrets]
  backend = "auto"          # auto | keyring | insecure-file (UNSICHER, nur Testserver)
  [imap]                    # Watchdog (ADR-007, Nachtrag 16.10.2026); Dauer als Text
  command_timeout = "30s"       # so lange darf der Server während eines Kommandos schweigen (ab 1s)
  command_max_duration = "10m"  # Obergrenze je Kommando (nicht kleiner als command_timeout)
  [audit]
  export_dir = ""           # Standardziel von `audit export`: ein zweiter Ort, nicht das Datenverzeichnis
  ```

- **Profile** (`--profile <name>`, seit S1.8): Konfiguration und Daten liegen in einem Unterordner `profiles/<name>` des normalen Konfigurations- bzw. Datenordners (auch unter `POSTKAUZ_CONFIG` und `POSTKAUZ_DATA_DIR`), z. B. `%LOCALAPPDATA%\postkauz\profiles\test\postkauz.db`. Setzt die Profil-Konfiguration `data_dir`, gilt genau dieser Ordner. Namen wie Kontonamen. **Grenze:** Passwörter im Schlüsselbund hängen am Kontonamen (`account/<name>`), nicht am Profil; zwei Profile mit gleichem Kontonamen teilen sich den Eintrag (offene Frage 7 in [roadmap.md](roadmap.md)).

- **Umgebung** schlägt Datei: `POSTKAUZ_DATA_DIR`, `POSTKAUZ_SECRETS_BACKEND`, `POSTKAUZ_PASSWORD_<NAME>` (Passwort eines Kontos, nur lesen).
- **Daten:** `postkauz.db` (SQLite, 0600), `postkauz.log` (JSON-Zeilen, 0600, ohne Inhalte und Geheimnisse), später `blobs/`. Standard: Linux `$XDG_DATA_HOME/postkauz` bzw. `~/.local/share/postkauz`, Windows `%LOCALAPPDATA%\postkauz` (lokal, nicht „Roaming"), macOS `~/Library/Application Support/postkauz`. Verzeichnis 0700, **nie** in einem Cloud-synchronisierten Ordner (`doctor` warnt bei OneDrive, Dropbox, iCloud u. Ä.). Angelegt wird es erst beim ersten Kommando, das es braucht; `version` und `doctor` legen nichts an.
- **Geheimnisse:** Schlüsselbund (Windows Credential Manager, macOS Keychain, Secret Service) oder Umgebungsvariable; die Datenbank enthält nur den Verweis `account/<name>`. Ohne Schlüsselbund gibt es keinen stillen Rückfall (siehe [sicherheit.md](sicherheit.md), Abschnitt 4).
- **`doctor`** meldet Version, Konfiguration (Pfad, vorhanden, Fehler), Datenverzeichnis, Log, Datenbank (`missing`, `ok`, `needs_migration`, `too_new`, `error`), Schemaversion (vorhanden und bekannt), Schlüsselbund (`available`, `unavailable`, `insecure`) und je Konto, woher das Passwort käme (`stored`, `environment`, `missing`) samt Status der letzten Prüfung; dazu Warnungen. Exit 1 nur bei ungültiger Konfiguration.
- **Sprache:** Ausgaben und Fehlermeldungen sind Englisch (siehe CLAUDE.md); Mehrsprachigkeit ist kein Ziel des MVP.
