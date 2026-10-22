# CLI

Stand: S2.4 (22.10.2026; zuletzt veröffentlicht: 0.1.0-rc2). Implementiert und getestet sind `version`, `doctor` (seit S1.11 mit `--secrets` und dem Stand des Audit-Exports), `accounts list` (aus der Datenbank), `account add`, `account test`, `account remove`, `sync` (Erstsync, inkrementell, `--watch`; danach Extraktion und Suchindex), `mailbox list`, `show` (voll: Kopf, Absenderprüfung, Text, Hinweis auf versteckten Text, Links, Anhangsliste, `--strip-quotes`, `--max-chars`), `search` (Abfragesprache, `--limit`, `--cursor`, `--mailbox`, `--fields`, NDJSON), `thread`, `attach list`, `attach text`, `key create/list/revoke`, `audit`, `audit export`, `audit verify`, `policy show` sowie die globalen Flags `--json`, `--dry-run`, `--account`/`-a`, `--key`, `--quiet`, `--profile` und `--explain` (seit S2.0/A7 auch `--config` und `--data-dir`); seit S1.9 `mcp` (Abschnitt 2h), seit S1.10 `import mbox` und `import maildir` (Abschnitt 2i); seit S1.11 der Audit-Export mit Hash-Kette und Manifest (Abschnitt 2f) und der Fehlercode `internal` (Abschnitt 3); seit S2.0 `account set --aliases` (Abschnitt 2a) und die Spalte `ALIASES` bzw. das Feld `aliases` in `accounts list`; seit S2.1 die Policy-Datei und die Abschnitte `[paths]`, `[operator]`, `[draft]` der Konfiguration (Abschnitt 5) sowie `account set --password-stdin` (Abschnitt 2a); seit S2.3 die Entwürfe in Grundform: `draft new|reply|forward|edit|show|list|request|abandon|mark-sent`, `approve`, `reject`, `approval show`, `identity add|list|set-default|remove` (Abschnitt 2j) und in `policy show --key` die Liste `human_only` (Abschnitt 2e); seit S2.4 der Prüf-Hook aus der Policy-Datei vor `draft request` (Abschnitt 2j und 5). Alles andere in diesem Dokument ist **Entwurf** und mit dem Schnitt markiert, in dem es entsteht.

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
| `--config <datei>` | diese Konfigurationsdatei benutzen (wie `POSTKAUZ_CONFIG`, mit Vorrang; muss existieren; nicht mit `--profile`) | S2.0/A7 ✓ |
| `--data-dir <ordner>` | dieses Datenverzeichnis benutzen (wie `POSTKAUZ_DATA_DIR`, mit Vorrang; mit `--profile` gilt `<ordner>/profiles/<name>`) | S2.0/A7 ✓ |
| `--explain` | Policy-Entscheid und Audit-ID des Aufrufs auf stderr, ohne Inhalte; bei `search` zusätzlich die Übersetzung der Anfrage (Abschnitt 2d) | S1.8 ✓ |
| `--yes` | Rückfrage bestätigen (nie interaktiv im `--json`-Modus) | S2 |

### Kommandos nach Schnitt

| Schnitt | Kommando | Operation |
|---|---|---|
| Gerüst | `version`, `doctor` (Pfade, Schlüsselbund, Schema, `pdftotext`; seit S1.7 wartende Nachrichten des Suchindex `index_pending` und `fts_version`, seit S1.9 wartende Anhang-Extraktionen `extract_pending`, seit S1.11 Stand des Audit-Exports und `doctor --secrets`) | – (Hilfskommandos, keine Operationen) |
| Gerüst | `accounts list` | `accounts.list` |
| S1.1 ✓ | `account add`, `account test`, `account remove` (siehe Abschnitt 2a) | `accounts.add`, `accounts.test`, `accounts.remove` (Scope `admin`, R1; nicht über MCP) |
| S2.0 ✓ | `account set <name> --aliases a@…,b@…` (Abschnitt 2a) | `accounts.set` (Scope `admin`, R1, HumanOnly: mit `--key` Exit 3 `human_only`; nicht über MCP) |
| S2.1 ✓ | `account set <name> --password-stdin` (Abschnitt 2a; Entscheid 36) | `accounts.set` mit `replace_password` (das Passwort selbst geht getrennt von der Eingabe, nie ins Audit) |
| S1.4/S1.5 ✓ | `sync [--full] [--watch]` (siehe Abschnitt 2b; seit S2.5 legt er zuerst wartende Entwürfe im Entwürfe-Ordner ab, `--jobs-interval`) | `sync.run` (Scope `mail:sync`, R1) |
| S1.5 ✓ | `mailbox list` | `mailboxes.list` (R0) |
| S1.9 ✓ | `mailbox overview <mailbox-id> [--since D] [--limit N]` | `mailboxes.overview` (R0, `mail:read`) |
| S1.5–S1.8 ✓ | `show <id>` bzw. `show <Message-ID>`, `--headers`, `--strip-quotes`, `--max-chars` | `messages.get` (R0, `mail:read`; Untrusted-Hülle seit S1.8) |
| S1.6/S1.8 ✓ | `thread <thread-id>` bzw. `thread --message <id>`, `--strip-quotes`, `--max-messages`, `--max-chars`, `-a` | `threads.get` (R0, `mail:read`) |
| S1.6/S1.8 ✓ | `attach list <message-id>`, `attach text <attachment-id> [--max-chars N] [--message ID] [-a KONTO]` | `attachments.list`, `attachments.text` (R0, `mail:read`) |
| S1.7/S1.8 ✓ | `search "<abfrage>"` (siehe Abschnitt 2d) | `messages.search` (R0, `mail:search`) |
| S1.8 ✓ | `key create`, `key list`, `key revoke` (Abschnitt 2e) | `keys.create` (R1), `keys.list` (R0), `keys.revoke` (R1); Scope `admin`, nur Mensch, nicht über MCP |
| S1.8 ✓ | `audit [--since …]`, `audit export --to <ordner>`, `audit verify` (Abschnitt 2f) | `audit.list` (R0, `audit:read`), `audit.export` (R1, `admin`), `audit.verify` (R0, `audit:read`) |
| S1.8 ✓ | `policy show` (Abschnitt 2e) | `policy.get` (R0, jeder Schlüssel) |
| S1.9 ✓ | `mcp` (Abschnitt 2h) | startet den stdio-MCP-Server (kein Registerbefehl; jeder Tool-Aufruf läuft einzeln durch den Executor) |
| S1.10 ✓ | `import mbox <datei\|ordner>`, `import maildir <ordner>` (Abschnitt 2i) | `import.run` (Scope `admin`, R1; nicht über MCP) |
| S2.3 ✓ | `draft new`, `draft reply`, `draft forward` (Abschnitt 2j) | `drafts.create` (R2, `mail:draft`; eine Operation, drei Kommandos) |
| S2.3 ✓ | `draft edit <id>` | `drafts.update` (R2, `mail:draft`) |
| S2.3 ✓ | `draft show <id>`, `draft list` | `drafts.get`, `drafts.list` (R0, `mail:draft`) |
| S2.3 ✓ | `draft request <id>` | `drafts.request_ready` (R1, `mail:draft`) |
| S2.3 ✓ | `approve <approval-id>`, `reject <approval-id> [--reason]`, `draft abandon <id>`, `draft mark-sent <id>` | `drafts.approve`, `drafts.reject`, `drafts.abandon`, `drafts.mark_sent` (R1, `mail:draft`, nur Mensch: mit `--key` Exit 3 `human_only`) |
| S2.3 ✓ | `approval show <approval-id>` | `approvals.get` (R0, `mail:draft`) |
| S2.3 ✓ | `identity add`, `identity list`, `identity set-default <id>`, `identity remove <id>` | `identities.add`, `identities.list`, `identities.set_default`, `identities.remove` (`admin`, nur Mensch; nicht über MCP) |
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

postkauz import maildir ./Mail/Archive --account demo --mailbox Archive-2024   # S1.10 ✓
postkauz import mbox "%APPDATA%\Thunderbird\Profiles\x.default\ImapMail\imap.example.org" -a demo --dry-run   # S1.10 ✓

# S2.3 ✓, Ablage im Entwürfe-Ordner S2.5 ✓
postkauz identity add -a demo --email demo@example.org --name "Demo Büro" --signature-file signatur.txt
postkauz draft reply 18233 --all --body-file antwort.md                     # Entwurf anlegen, Status draft, remote pending
postkauz sync -a demo                                                       # legt den Entwurf zuerst im Entwürfe-Ordner ab, dann Spiegel
postkauz draft show 1                                                       # remote: pushed, copies: uid 7
postkauz draft request 1                                                    # zur Freigabe vorlegen (Prüf-Hook, approval 1)
postkauz draft list --status pending_approval
postkauz approve 1                                                          # Mensch gibt den Entwurf frei (Status ready)
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
- `account set <name> --aliases a@example.org,b@example.org` (seit S2.0) setzt die **weiteren eigenen Adressen** des Kontos (Aliasse, Weiterleitungen auf dasselbe Postfach); die Liste ersetzt die vorhandene, `--aliases ""` entfernt alle. Adressen werden geprüft (nur die Adresse, ohne Namen; sonst Exit 2 `invalid alias`), klein geschrieben, sortiert, ohne Dubletten und ohne Kontoadresse und Anmeldenamen (gleich in welcher Schreibweise); höchstens 50 nach dem Entfernen (die Eingabe darf bis zu 200 Einträge haben). Ohne `--aliases` ist das ein Eingabefehler (`nothing to change`). „Eigene Adressen" sind Kontoadresse, Anmeldename (wenn Adresse) und Aliasse (ab S2.1 dazu die Identitäten): Der Betreff-Rückfall der Threads zählt sie nicht als gemeinsame Beteiligte (gilt für neu zugeordnete Nachrichten), ab S2 lässt „Allen antworten" sie weg und die Versanderkennung nutzt sie. Das Kommando ist **nur für Menschen** (HumanOnly): mit `--key`, auch einem `admin`-Schlüssel, verweigert (Exit 3, `human_only`), und es gibt kein MCP-Tool dafür. Ausgabe: `account demo: aliases a@example.org, b@example.org` bzw. `aliases none`. `accounts list` zeigt die Aliasse in der Spalte `ALIASES` (`-` ohne), `--json` im Feld `aliases` (Liste, auch leer).
- `account set <name> --password-stdin` (seit S2.1, Entscheid 36) **ersetzt das gespeicherte Passwort** eines Kontos, etwa nach einem Passwortwechsel beim Anbieter oder wenn `doctor` `password missing` meldet. Das neue Passwort kommt nur über die Standardeingabe: am Terminal mit verdeckter Eingabe (`New password for account demo:` auf stderr), sonst die erste Zeile (`Get-Content pw.txt | postkauz account set demo --password-stdin`); **nie als Argument** (ein überzähliges Argument oder `--password` ist ein Eingabefehler, Exit 2), leer ist Exit 2. Es geht in den Schlüsselbund (bzw. den unsicheren Testspeicher) unter `account/<datadir-id>/<name>`, wird zurückgelesen und verglichen; hatte das Konto noch den alten Verweis `account/<name>` (Umzug ausstehend), zeigt es danach auf den neuen, der alte Eintrag bleibt unangetastet (`doctor` meldet ihn). Ohne erreichbaren Schlüsselbund scheitert das Kommando ohne Änderung (Exit 1, `secret_store_unavailable`), zusammen mit `--aliases` ändern sich dann auch die Aliasse nicht. Ausgabe: `account demo: password replaced in the OS keyring as account/1a2b3c4d/demo`, mit `--dry-run` `dry run: would replace the password of account demo …`; ist `POSTKAUZ_PASSWORD_DEMO` gesetzt, folgt `note: POSTKAUZ_PASSWORD_DEMO is set and takes precedence over the stored password at runtime`. `--json` liefert `password` (`replaced`\|`would_replace`), `credential_ref`, `secret_store`, ggf. `env_override`, nie das Passwort. Nur für Menschen (mit `--key` Exit 3 `human_only`, kein MCP-Tool). Im Audit steht nur der Feldname `replace_password`. Danach `account test <name>` zum Prüfen.
- `--dry-run` wirkt bei allen vier Kommandos (prüfen, nichts ändern).

## 2b. Sync, Ordner, Anzeige (S1.4/S1.5)

`sync [-a <konto>] [--full] [--watch] [--window N] [--interval D] [--idle-restart D] [--full-check-interval D] [--jobs-interval D] [--mailbox P ...]`

| Option | Bedeutung |
|---|---|
| (ohne) | Erstsync neuer Ordner in Fenstern, sonst inkrementell: neue Nachrichten, Flag-Änderungen (CONDSTORE `CHANGEDSINCE` bzw. Abgleich aller Flags), Löschungen (UID-SEARCH-Abgleich, nur wenn die Zahlen nicht passen) |
| `--full` | zusätzlich vollständiger Lösch- und Flag-Abgleich ohne `CHANGEDSINCE`; holt Fehlendes nach; prüft Nachrichten erneut, die wegen `[imap] max_message_bytes` übersprungen wurden (ohne `--full` werden sie nicht wieder angefordert) |
| `--watch` | Dauerläufer: sofort ein Lauf, danach alle `--interval` (Standard 5 min) und bei jeder IDLE-Meldung der INBOX (IDLE-Neustart alle `--idle-restart`, 5 bis 10 min, Standard 8 min); Strg+C beendet ihn |
| `--full-check-interval` | nur mit `--watch` (seit S2.0): Abstand der vollen UID-Abgleiche aller Ordner, Standard `1h`. Der erste Lauf über alle Ordner und danach der erste Lauf, der mindestens so lange nach dem letzten erfolgreichen vollen Abgleich beginnt (erfolgreich = ohne Fehler und ohne übersprungenen Ordner; sonst gleicht der nächste Lauf erneut voll ab), holt in jedem Ordner die vollständige UID-Liste (`UID SEARCH ALL`) und erkennt so auch Löschungen, die der Zahlenvergleich übersieht (z. B. wenn lokal zugleich eine Nachricht fehlt, die beim Spiegeln scheiterte). Flags bleiben inkrementell (`CHANGEDSINCE`); IDLE-Läufe (nur INBOX) gleichen nie voll ab. Der einmalige `sync` ist davon nicht betroffen |
| `--jobs-interval` | nur mit `--watch` (seit S2.5): Abstand, in dem der Dauerläufer die Tabelle `jobs` auf wartende Entwürfe prüft (Job `draft_push`), auch zwischen zwei Läufen; Standard `10s`. Ein Entwurf, den `draft new` oder ein Agent über MCP anlegt oder ändert, liegt so spätestens nach diesem Abstand plus der Dauer des APPEND im Entwürfe-Ordner (gemessen gegen die Testserver: 10,0 bis 10,3 s im ungünstigsten Fall) |
| `--window` | Nachrichten je Fenster im Erstsync (Standard 200) |
| `--mailbox` | nur diese Ordner (mehrfach möglich) |
| `--dry-run` | meldet Ordner und lokalen Stand, schreibt nichts |

- Ausgabe ohne `--json`: Tabelle je Ordner (neu, geändert, entfernt, Fehler, gespiegelt, laut Server), am Ende genau die Zeilen `N messages mirrored` und `N errors`. Fortschritt laufender Erstsyncs steht auf stderr. `N messages mirrored` zählt die Nachrichten des Kontos, die der Server führt, ohne Dubletten über Ordner hinweg.
- Mit `--json`: ein Objekt (`account`, `mode`, `strategy`, `mailboxes[]`, `new`, `updated`, `removed`, `errors`, `messages_mirrored`, `attachments_extracted`, `messages_indexed`, `index_pending`, `duration_ms`, `dry_run`); im Dauerbetrieb zusätzlich je Lauf eine NDJSON-Zeile.
- **Nach dem Abgleich** arbeitet `sync` die Warteschlange des Kontos ab (seit S1.7, Tabelle `jobs`): Text aus Anhängen extrahieren (höchstens 60 s je Lauf, der Rest im nächsten) und neue oder geänderte Nachrichten in den Suchindex aufnehmen (bis alles drin ist; Fortschritt `search index: N/M` auf stderr). Ohne `--json` steht davor die Zeile `search index: N messages indexed` (mit `, M still pending …`, wenn ein Abbruch etwas übrig ließ). Ein Abbruch verliert nichts: Der nächste `sync` macht dort weiter.
- **Abbruch** (Strg+C, `kill`, Absturz): Gespeichert ist alles bis zum letzten fertigen Fenster; derselbe Befehl setzt dort fort, ohne Doppelte. Strg+C endet mit Exit 1, Code `interrupted`. Je Konto läuft höchstens **ein** schreibender Sync: Ein zweiter (z. B. ein manueller `sync` neben `sync --watch`) bricht sofort mit Exit 1, Code `sync_running` (`another sync is running: account …`) ab, ohne sich am Server anzumelden; `--dry-run` ist davon ausgenommen. Die Sperre ist eine Dateisperre des Betriebssystems (`sync-<konto-id>.lock` neben der Datenbank) und fällt mit dem Prozess, auch nach `kill -9`.
- Am Server ändert `sync` nichts (EXAMINE, `BODY.PEEK`) — mit einer Ausnahme seit S2.5: **Entwürfe ablegen**. Zu Beginn jedes Laufs (vor dem Spiegeln, damit der Spiegel die neue Kopie im selben Lauf sieht) und im Dauerbetrieb zusätzlich alle `--jobs-interval` legt `sync` jeden Entwurf, dessen Kopie aussteht (`remote: pending`), per IMAP APPEND im Entwürfe-Ordner ab: Ordner mit Rolle `drafts` (SPECIAL-USE, sonst Name wie `Drafts`/`Entwürfe`), `[draft] mailbox` der Konfiguration überschreibt; Flags `\Draft \Seen`, die beim Anlegen vergebene Message-ID, `Date` = Zeitpunkt der Ablage. `sync` sendet nie, löscht nie, verschiebt nie. In der Textausgabe (ohne `--json`) steht vor den Schlusszeilen eine Zeile `drafts: 1 put into Drafts (flags \Draft \Seen; nothing is sent)`, ggf. mit `N taken over in the mail client (not pushed)`, `N with their copy gone`, `N ready but no longer the approved message: back to draft, not pushed (see draft show)`, `N could not be built`, `N refused by the server (see draft show)` (seit der Gegenprüfung von S2.4/S2.5: nur dieser Entwurf, die übrigen werden abgelegt, der nächste `sync` versucht ihn erneut), `N found in the Drafts folder from an interrupted push (recorded, not put twice)` oder `error: no_drafts_mailbox` (kein Entwürfe-Ordner; der Lauf spiegelt trotzdem, Exit 0, der Entwurf bleibt `pending`, `draft show` nennt den Grund). Mit `--json` das Feld `drafts` (`pushed`, `taken_over`, `missing`, `approval_stale`, `failed`, `rejected`, `adopted`, `draft_ids`, `mailbox`, `error`). Im Dauerbetrieb meldet jede Ablage eine Zeile `<Zeit>  N drafts put into Drafts` auf stdout.
- **Ohne Verbindung** endet `sync` mit Exit 5 (`connection`); die Entwürfe bleiben `pending`, der Job ist sofort wieder fällig, und der nächste `sync` legt sie ab. Läuft für das Konto schon `sync --watch`, endet ein zweiter `sync` mit `sync_running` und überlässt dem Dauerläufer die Ablage (er hält die Kontosperre).
- **Erneute Ablage nach einer Änderung:** `draft edit` (oder `mail_update_draft`) setzt `remote` wieder auf `pending`; der nächste Push legt eine **neue** Kopie ab. Die alte bleibt im Entwürfe-Ordner (PostKauz löscht nie, kein Expunge im MVP); `draft show` nennt beide UIDs (`copies: uid 8, earlier uid 7`). Mailprogramme zeigen dann zwei Entwürfe mit derselben Message-ID; den alten löscht der Mensch im Mailprogramm.
- **Übernahme im Mailprogramm (E6):** Vor jeder Änderung und vor jeder erneuten Ablage prüft PostKauz anhand des Spiegels des Entwürfe-Ordners (vor einer erneuten Ablage frisch gespiegelt), ob die zuletzt abgelegte Kopie unverändert da ist (gleiche UID, Message-ID und Inhalts-Hash). Hat ein Mailprogramm eine bearbeitete Fassung gespeichert (neue UID, gleiche Message-ID) oder die Kopie entfernt (gesendet, gelöscht), gilt der Entwurf als übernommen: `remote: taken_over` bzw. `missing`, jede Änderung endet mit Exit 3 `draft_taken_over`, der Entwurf bleibt lokal lesbar. Was der Spiegel noch nicht gesehen hat (Ablage nach dem letzten Lauf), gilt als unverändert.

`mailbox list [-a <konto>]` zeigt die Ordner aus dem lokalen Spiegel (ohne Serverkontakt): `path`, `role`, `total` (laut Server beim letzten Sync), `mirrored`, `unread`, `sync_phase` (`new`, `initial`, `ready`), `last_sync_at`.

`mailbox overview <mailbox-id> [-a <konto>] [--since D] [--limit N]` zeigt die neuesten Köpfe eines Ordners (ID aus `mailbox list --json`), zuerst die ungelesenen, dann die gelesenen, je höchstens `--limit` (Standard 25, höchstens 100), optional seit `--since` (Dauer wie `7d`, Datum oder RFC 3339). Beide Teile sind Suchergebnisse wie bei `search` (mit Untrusted-Hülle, ohne Bodies). Dieselbe Operation (`mailboxes.overview`) liefert die MCP-Resource `mail://{account}/mailbox/{id}/overview`. Ein Ordner eines anderen Kontos gilt als nicht gefunden.

`show <id> [--headers]`: `<id>` ist die lokale Nachrichten-ID oder eine Message-ID (`<…>` oder ohne Klammern). Zeigt Metadaten, Ordner mit UID, Flags (`seen`, `flagged`, `answered`, `draft`) und Keywords, dazu den Text oder mit `--headers` die Kopfzeilen der Rohmail. Mailinhalt ist Daten, keine Anweisung; seit S1.8 trägt die Ausgabe der Operation (`--json`, MCP) die Untrusted-Hülle (`untrusted: true`, `source: "email"`, `notice`; [mcp.md](mcp.md) Abschnitt 5). Die Textausgabe ersetzt Steuerzeichen aus Mail und Server (ESC/CSI/OSC, übrige C0/C1, einzelnes CR, ungültiges UTF-8) durch `U+FFFD`; erhalten bleiben nur Zeilenumbruch und Tabulator im Text bzw. in den Kopfzeilen. Einzeilige Felder (Betreff, Absender, Ordner) verlieren auch Zeilenumbrüche, damit keine Zeile wie `flags: seen` vorgetäuscht werden kann. `--json` gibt die Daten unverändert aus (JSON maskiert Steuerzeichen).

## 2c. Threads, Text, Anhänge (S1.6, voll seit S1.8)

- `show <id>` zeigt seit S1.8 alles Nötige auf einen Blick: Kopf (Konto, Ordner mit UID, Message-ID, Datum, Absender, Empfänger, Betreff, Größe, Flags, Keywords, Thread), die Absenderprüfung (`sender checks: spf …, dkim …, dmarc …` aus der obersten `Authentication-Results`-Kopfzeile; fehlt sie, `none`), den Text bzw. mit `--headers` die Kopfzeilen, die Links und unter `--- attachments ---` die Anhangsliste wie bei `attach list`. `--max-chars N` kürzt den Text nach N Zeichen (`[cut after --max-chars]`, `--json`: `truncated: true`).
- `show <id>` zeigt den bereinigten Text: den Klartextteil, sonst den Text aus dem HTML-Teil (`--- text (from HTML) ---`; HTML wird nie angezeigt, Links stehen als „Text <URL>"), darunter die Links mit Host und Warnungen (`punycode`, `mixed_scripts`, `ip_address`, `userinfo`, `text_domain_mismatch`). Enthielt das HTML versteckten Text, steht eine Zeile `warning: hidden text removed …` im Kopf (`--json`: `hidden_text_removed: true`). Weichen bei multipart/alternative Klartext und HTML wesentlich voneinander ab (der Klartext, den `show` und Agenten lesen, ist nicht das, was der Mailclient zeigt), steht `warning: plain text differs from the HTML part …` (`--json`: `alternative_mismatch: true`; `thread --strip-quotes`: `(plain text differs from the HTML part)`). `--strip-quotes` zeigt nur den neuen Text einer Antwort (`text_source` `plain_unquoted`/`html_unquoted`). Die Zeile `thread:` nennt den Thread.
- `thread <thread-id>` zeigt einen Thread: Betreff der ersten Nachricht, Zahl, Zeitspanne, Teilnehmer und je Nachricht ID, Datum, Absender, Betreff, Flags. `--message` nimmt stattdessen eine Nachrichten-ID; `--strip-quotes` hängt den neuen Text jeder Nachricht an. Mit `-a <konto>` gilt ein Thread eines anderen Kontos als nicht gefunden. `--max-messages N` zeigt nur die neuesten N Nachrichten (`(K older messages not shown)`, `--json`: `messages_omitted`), `--max-chars N` kürzt jeden Text.
- `attach list <message-id>` listet die Anhänge: ID, MIME-Teil, Dateiname, **erkannter** Typ (Magic Bytes), Größe, Stand der Textextraktion (`pending`, `ok`, `failed`, `skipped`) und Hinweise (`TYPE MISMATCH: declared … content is …`, `EXECUTABLE`, `inline`, `truncated`, `hidden text removed`, Grund einer übersprungenen Extraktion).
- `attach text <attachment-id> [--max-chars N] [--message ID] [-a KONTO]` zeigt den extrahierten Text. `--message` und `-a` prüfen die Zugehörigkeit: Gehört der Anhang nicht zu dieser Nachricht bzw. diesem Konto, gilt er als nicht gefunden (so prüft auch die MCP-Resource). Steht die Extraktion noch aus, extrahiert der Befehl jetzt (Subprozess mit Grenzen, siehe [sicherheit.md](sicherheit.md) Abschnitt 7) und speichert nichts (`note: extracted now; the next sync stores it`); übersprungene oder gescheiterte Anhänge nennen den Grund. Den Text speichert der Sync am Ende jedes Laufs (Zeitbudget 60 s; Ausgabe `attachments_extracted` mit `--json`).
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
- **Mit `--json`: NDJSON, eine Zeile je Treffer** (`id`, `account`, `thread_id`, `date`, `from`, `subject`, `snippet`, `flags`, `has_attachments`, `deleted_remote`, `score`, `cursor`; seit S1.11 am Ende jeder Zeile die Kennzeichnung der Untrusted-Hülle `untrusted: true`, `source: "email"`, weil Betreff, Absender und Snippet aus Mails stammen; der Hinweis `notice` steht nur in der Textausgabe bzw. über MCP). Jeder Treffer trägt seinen Cursor: Mit `--cursor <cursor eines Treffers>` setzt dieselbe Anfrage genau dahinter fort. Mit `--explain` steht davor eine Zeile `{"explain":…,"total":…,"order":…,"index_pending":…,"next_cursor":…}`. Die Operation selbst (MCP, später REST) liefert ein Objekt mit `hits[]`, `total`, `next_cursor`, `order`, `index_pending` und optional `explain`.
- `--mailbox <ordner>` (seit S1.8) wirkt wie `in:<ordner>` in der Anfrage (Pfad samt Unterordnern oder Rolle); MCP nutzt dafür das Feld `mailbox`.
- `--fields id,subject,date` (seit S1.8, nur mit `--json`): je NDJSON-Zeile nur diese Felder und immer `untrusted`/`source` (erlaubt sind die Felder eines Treffers); unbekannte Felder sind ein Eingabefehler (Exit 2).
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
- `policy show` beschreibt die Richtlinie des (mit `--key` gewählten) Schlüssels: Rolle, Scopes, Trockenlauf-Standard, Ablauf, Zeichenbudget für Agenten (`[agent_defaults] max_chars` der Policy-Datei, Standard 20.000; seit S2.1) und die erlaubten Operationen mit Risikostufe. Ohne `--key` ist das der Mensch (`cli`, alle Rechte). Seit S2.3 (Entscheid 40) folgt für einen Schlüssel der Block `never with a key (human only):` mit je einer Zeile `  <operation>: human_only` (z. B. `drafts.approve: human_only`), `--json` im Feld `human_only` (für den Menschen leer): Diese Operationen darf kein Schlüssel, gleich welcher Rolle.

## 2f. Audit (S1.8)

- `audit [--since 24h] [--until …] [--actor human|agent|system] [--key-id key:<name>] [--operation <name>] [--result ok|denied|needs_approval|error] [--limit N] [--after-id N]` zeigt Einträge: ID, Zeit, Akteur, Schlüssel (`cli` für den Menschen, `key:<name>` für API-Schlüssel), Operation, Ergebnis (mit Grund und `dry-run`), Ziele (z. B. `message:42`) und Parameter **ohne Inhalte** (Feldnamen, Zähler; nie Suchtext oder Mailtext). Zeitangaben: Dauer rückwärts (`10m`, `24h`, `7d`), Datum (`2026-10-16`) oder RFC 3339; Standard die letzten 24 Stunden. `--json`: NDJSON, ein Eintrag je Zeile. Bei voller Seite `more: --after-id N`.
- `audit export [--to <ordner>]` schreibt das Audit tageweise (UTC) als JSONL-Dateien `postkauz-audit-JJJJ-MM-TT.jsonl` (0600) an einen **zweiten Ort** (anderer Datenträger oder Rechner, z. B. ein Google-Drive- oder Netzwerkordner) und hält je Tag Prüfsumme, erste/letzte Eintrags-ID und Ort in `audit_exports` fest. Ohne `--to` gilt `[audit] export_dir` (Abschnitt 5). Ein noch laufender Tag wird bei jedem Export neu geschrieben (`updated`). Passt eine früher exportierte Datei nicht mehr zu ihrer Prüfsumme, überschreibt der Export sie nicht (Exit 1, Code `mismatch`): Der Beleg einer Manipulation bleibt. **Format 2 (seit S1.11):** jede Zeile ist `{"event":{…},"prev":"…","chain":"…"}` mit `chain = SHA-256(prev + "\n" + event)`; die Kette läuft über alle Tage (erste Zeile eines Tages hängt an der letzten des Vortags, ganz am Anfang der feste Startwert `SHA-256("audit-chain-v1")`). Daneben schreibt der Export das **Manifest** `postkauz-audit-manifest.json` (je Datei Tag, Zahl der Einträge, IDs, Prüfsumme, Kettenende; dazu der Startwert und das Ende der ganzen Kette, keine Zeitangabe). Die Ausgabe nennt das Manifest und `chain head: …` (wer sich diesen Wert notiert, kann spätere Exporte dagegen prüfen). Exporte im Format 1 (S1.8 bis S1.10, eine Zeile je Eintrag ohne Kette) prüft `verify` weiter; der nächste Export schreibt sie im Format 2 neu.
- `audit verify` prüft jede exportierte Datei gegen ihre Prüfsumme (`export file was changed`), ihr Vorhandensein (`export file is missing`), die Hash-Kette in der Datei (`hash chain is broken`), das Manifest (`manifest is missing`, `manifest does not match the recorded exports`) und die Zeilen der Datenbank (`database rows differ from the export`); wo möglich mit der ersten betroffenen Zeile, z. B. `export file was changed (line 2)`. Ergebnis `ok: N exported days checked; …` mit `chain head: …` (Exit 0) oder `mismatch: …` (Exit 1, Code `mismatch`). Eine geänderte Datenbankzeile betrifft nur ihren Tag, nicht die späteren (deren Kette wird an der unveränderten Exportdatei fortgesetzt). Tage mit neueren, noch nicht exportierten Einträgen nennt die Zeile `not exported yet (new entries): …` (kein Fehler; `verify` selbst erzeugt einen Eintrag).
- **Täglich** (seit S1.11): Windows-Aufgabe über `deploy/windows/Register-AuditExport.ps1`, unter Linux der systemd-Timer `deploy/systemd/postkauz-audit-export.timer`; beide rufen `postkauz audit export --quiet` (Ziel aus `[audit] export_dir` bzw. `--to`). Einrichtung: README, „Daily audit export". `doctor` meldet den letzten Export (Zeile `audit:`). Seit S2.0/A7 trägt das Windows-Skript `--data-dir` und `--config` mit den Pfaden ein, die `doctor` beim Einrichten zeigt, und bricht ab, wenn Windows sie in den Ordner eines App-Pakets umleitet (Abschnitt 5).

## 2g. Erklären, leise, Profile (S1.8)

- **`--explain`** (global) schreibt nach jedem Aufruf eine Zeile auf **stderr**: Operation, Scope, Risiko, Schlüssel, Entscheidung der Policy mit Grund, Trockenlauf und die **Audit-ID** des Eintrags, z. B. `explain: messages.search (scope mail:search, risk R0) as key key:leser: allow (ok), dry run no; audit event 42`. Mit `--json` als Zeile `{"explain":{"operation":…,"scope":…,"risk":…,"key":…,"actor":…,"decision":…,"reason":…,"dry_run":…,"audit_id":…}}`. Auch verweigerte Aufrufe werden erklärt. Inhalte stehen nie darin; stdout bleibt die Ausgabe der Operation.
- **`--quiet`**, `-q`: keine Fortschrittszeilen (Sync, Suchindex) auf stderr; Ergebnis und Fehler bleiben.
- **`--profile <name>`**: getrennte Instanz mit eigener Konfiguration und eigenen Daten (Abschnitt 5). Gedacht für Probe- und Abnahmedaten neben dem Alltag.
- **`--config <datei>`** und **`--data-dir <ordner>`** (seit S2.0/A7): Konfigurationsdatei bzw. Datenverzeichnis für diesen Aufruf, mit Vorrang vor `POSTKAUZ_CONFIG`/`POSTKAUZ_DATA_DIR` und `data_dir` in der Datei. Relative Pfade gelten ab dem aktuellen Ordner, ein abschließender Trenner fällt weg; leere Werte, eine nicht vorhandene `--config`-Datei und `--config` zusammen mit `--profile` sind Eingabefehler (Exit 2; `--profile` nimmt `profiles/<name>/config.toml` neben der normalen Konfigurationsdatei). Gedacht für geplante Aufgaben, die genau die Pfade brauchen, die `doctor` im Fenster zeigt (`Register-AuditExport.ps1` schreibt sie in die Aufgabe).

## 2h. MCP-Server (S1.9)

`postkauz mcp` startet den lesenden MCP-Server über stdin/stdout für einen Agenten (Claude Code, Claude-App, andere MCP-Clients). Tools, Resources, Prompts und Hülle: [mcp.md](mcp.md); Einrichtung: README, „Set up MCP in Claude Code".

- **Schlüssel** nur aus der Umgebung: `POSTKAUZ_KEY` (der Schlüssel aus `key create`) oder `POSTKAUZ_KEY_FILE` (Pfad zu einer Datei mit dem Schlüssel; unter Linux/macOS nur für den Nutzer lesbar, sonst Exit 1). Nie als Argument; `--key` ist bei `mcp` ein Eingabefehler (Exit 2).
- Fehlt der Schlüssel oder ist er unbekannt, widerrufen oder abgelaufen, startet kein Server: Exit 3 (`denied`), Grund auf stderr, nichts auf stdout.
- Die Tool-Liste folgt den Scopes des Schlüssels (`reader`: 9 Tools; verengt auf `mail:search`: nur Suche und Richtlinie). Es gibt kein Tool zum Senden, Löschen, Verschieben oder Entwerfen.
- Jeder Tool-Aufruf und jedes Lesen einer Resource steht im Audit mit `key:<name>` und Akteur `agent`, ohne Mailtext (`postkauz audit --key-id key:<name>`).
- `--profile` und `POSTKAUZ_DATA_DIR` wählen die Datenbank wie bei jedem Kommando. Der Server synct nicht; aktuell hält die Daten `sync` bzw. `sync --watch`.
- Log: Start, Ende und je Aufruf Tool, Schlüssel und Audit-ID in `postkauz.log` (ohne Inhalte); stdout gehört allein dem Protokoll.

## 2i. Import aus mbox und Maildir (S1.10)

```
postkauz import mbox <datei|ordner> [-a KONTO] [--mailbox ZIEL] [--labels-from ORDNER]... [--rescan] [--dry-run]
postkauz import maildir <ordner>    [-a KONTO] [--mailbox ZIEL] [--labels-from ORDNER]... [--rescan] [--dry-run]
```

Liest einen Bestand in ein Konto ein, ohne den Server zu berühren (Operation `import.run`, Scope `admin`, R1; nur der Mensch an der CLI, kein MCP-Tool, weil sie beliebige lokale Dateien liest).

- **Abgleich über Identität, nicht über Datum** (ADR-003): Gibt es die Nachricht im Konto schon (gleiche Message-ID, sonst gleicher `content_hash`; dabei zählen auch Varianten mit einem Zeilenende mehr oder weniger am Schluss), wird sie nicht doppelt gespeichert; sie bekommt nur den Ordner des Imports zugeordnet (`existing`). Ihre Flags bleiben die des Servers. Sonst wird sie neu angelegt (`new`) und läuft durch dieselbe Aufbereitung wie beim Sync (Parser, Bereinigung, Anhänge mit Typprüfung, Threads); am Ende laufen Extraktion und Suchindex wie nach einem Sync. Eine nur importierte Nachricht zeigt `show` mit „deleted on server: not on the server (imported only)".
- **Ordner:** Importierte Ordner sind eigene, lokale Ordner neben denen des Servers (der Sync löscht sie nie). `show` nennt sie mit „(imported)", die Suche findet sie mit `in:<pfad>` (auch Unterordner) und `in:import` (alles Importierte). `mailbox list` zeigt nur die Ordner des Servers.
- **Zielordner:** Bei einer einzelnen Datei bzw. der Wurzel eines Maildirs ist `--mailbox` der Ordner (ohne Angabe: Dateiname bzw. `INBOX`); bei einem Verzeichnis ist es das Präfix der Ordner darunter (ohne Angabe: Pfade relativ zum Verzeichnis).
- **Thunderbird** (Profilordner `ImapMail/<server>` oder `Mail/Local Folders`, Ablage „mbox"): Jede Datei, die mit einer Trennzeile `From ` beginnt, ist ein Ordner; Unterordner liegen in `<ordner>.sbd/` und werden zu `<ordner>/<unterordner>`; Ordnernamen in Thunderbirds IMAP-UTF-7 werden dekodiert (`Entw&APw-rfe` → `Entwürfe`); `.msf`-Index- und andere Dateien werden übersprungen (`files ignored`). Aus `X-Mozilla-Status` werden Flags (gelesen, beantwortet, markiert), aus `X-Mozilla-Keys` Schlagwörter; **gelöscht markierte Nachrichten (Bit 0x0008) werden übersprungen** (`skipped`); die `X-Mozilla-*`-Zeilen und das `>From`-Quoting werden entfernt, damit die Nachricht der Kopie des Servers gleicht. Nur teilweise heruntergeladene Nachrichten (Bit 0x0400) werden mit Hinweis importiert. Andere mbox-Dateien (z. B. Gmail-Export) werden als mboxrd gelesen.
- **Maildir:** `cur/` und `new/`; Flags aus dem Dateinamen hinter `:2,` (unter Windows auch `!2,`/`;2,`): S, R, F, D; mit T markierte Dateien und Dateien in `tmp/` (laufende Zustellung) werden übersprungen. Unterordner im Maildir++-Stil (`.Archiv.2024` → `Archiv/2024`) und im Thunderbird-Stil (`<ordner>.sbd/`).
- **Labels (`--labels-from`)**: Nachrichten dieser Ordner werden **nur zugeordnet**, nie gespeichert; was dort nicht schon vorhanden ist, zählt als `skipped`. Gedacht für Gmail, wo „Alle Nachrichten" alles enthält und INBOX, Gesendet, Wichtig und jedes Label Kopien derselben Mails sind. Dateien, die schon als Inhalt gelesen werden, werden dabei übergangen. Beispiel (PowerShell, Thunderbird-Profil):

  ```powershell
  $tb = "$env:APPDATA\Thunderbird\Profiles\<profil>\ImapMail\imap.gmail.com"
  postkauz import mbox "$tb\[Gmail].sbd\Alle Nachrichten" -a gmail --mailbox "[Gmail]/Alle Nachrichten" --labels-from $tb --dry-run
  ```

- **Große Dateien, Wiederaufnahme:** Dateien werden gestreamt (im Speicher höchstens ein Kopf von 1 MiB und ein Stapel Metadaten; gemessen in [import_messung.md](import_messung.md)). Je Stapel (200 Nachrichten bzw. 32 MiB) wird die Position in der Datei mit den Nachrichten gespeichert; nach einem Abbruch (Strg+C, Absturz, `kill -9`) setzt derselbe Aufruf dort fort (`(resumed)`). Eine fertig importierte Quelle mit gleicher Größe wird beim nächsten Lauf nicht wieder gelesen (`unchanged`); eine gewachsene mbox-Datei wird ab der gespeicherten Position weitergelesen. `--rescan` liest alles neu (findet nichts doppelt, kostet nur Zeit).
- **Fortschritt** je Datei auf stderr (Prozent, Größe, MB/s; bei Maildir Dateien), höchstens einmal je Sekunde; `--quiet` schaltet ihn ab.
- **`--dry-run`** liest alles und zählt, schreibt aber nichts (keine Nachricht, keine Datei im Blob-Store, kein Stand).
- **Sperre:** Ein Import sperrt das Konto wie ein Sync; läuft `sync --watch` für dasselbe Konto, endet der Import mit `sync_running` (Exit 1), und umgekehrt.
- **Ausgabe:** je Quelle eine Zeile, danach Hinweise und als letzte Zeile die Zählung, z. B.

  ```
  import maildir into account demo (2.1s, 4.9 MB, 2.3 MB/s)
  SOURCE    FOLDER           MODE     NEW  EXISTING  SKIPPED  ERRORS
  Mail      Archive          content  700  300       3        0
  1 files ignored (not mbox, e.g. Thunderbird .msf index files)
  search index: 700 messages indexed
  new: 700, existing: 300, skipped: 3, errors: 0
  ```

  Ein zweiter Lauf zeigt `new: 0`. Mit `--json` ein Objekt nach dem `OutputSchema` von `import.run` (je Quelle `new`, `existing`, `skipped`, `errors`, `linked`, `bytes`, `resumed_at`, `unchanged`, `problems`; gesamt zusätzlich `linked`, `sources_unchanged`, `files_ignored`). `errors` zählt Nachrichten, die nicht gelesen oder gespeichert werden konnten (z. B. über 256 MiB); der Lauf geht weiter (Exit 0).
- **Eingabefehler** (unbekanntes Format, Pfad fehlt, kein mbox/Maildir gefunden): Exit 2. Im Audit stehen Konto und Feldnamen, nie Pfade.

## 2j. Entwürfe, Freigaben, Identitäten (S2.3, Grundform)

Draft-only (ADR-002): **PostKauz sendet nie.** Ein Entwurf wird lokal gespeichert; seine Kopie legt der Job `draft_push` in den Entwürfe-Ordner (seit S2.5: `sync` zu Beginn jedes Laufs, `sync --watch` alle `--jobs-interval`, Abschnitt 2b). Der Mensch sendet im Mailclient. S2.7 baut die Kommandos aus (u. a. `draft show --eml`); die Grundform hier deckt alle Felder der Operationen ab.

| Kommando | Optionen | Ausgabe (Text) |
|---|---|---|
| `draft new` | `-a <konto>`, `--to`/`--cc`/`--bcc` (je Adresse oder `"Name <adresse>"`, wiederholbar), `--subject`, `--body-file <datei>` (Markdown; `-` = Standardeingabe) oder `--html-file <datei>`, `--attach <anhang-id\|datei>` (wiederholbar; eine Zahl ist die ID aus `attach list`, sonst eine Datei unter `[paths] attach_dir`), `--identity <adresse\|id>`, `--no-signature` | `draft 1 created (status draft, remote pending)`, darunter Message-ID, Kopf, die ersten 500 Zeichen des Texts, Anhänge, `warning:` (Entscheid 39: Typ-Widerspruch oder ausführbarer Anhang, erlaubt), `note:`, `remote: …` und `nothing is sent: a human sends the draft in the mail client` |
| `draft reply <id\|Message-ID>` | wie `new`, dazu `--all` (Allen antworten), `--no-quote`; Empfänger, Betreff (`Re: …`), `In-Reply-To`, `References` und Zitat kommen aus der Ausgangsnachricht | wie `new` |
| `draft forward <id\|Message-ID>` | wie `new`, dazu `--no-attachments` (Anhänge der Ausgangsnachricht nicht übernehmen) | wie `new` |
| `draft edit <id>` | `--to`/`--cc`/`--bcc` (ersetzen die Liste; `--cc ""` leert sie), `--subject`, `--body-file`/`--html-file`, `--attach` (ersetzt alle Anhänge) oder `--no-attachments`, `--quote=true\|false`, `--signature=true\|false`; ohne Option Exit 2 | `draft 1 changed (status ready -> draft, remote pending)` und `approval 1 superseded: …` bzw. `draft 1 unchanged (status …)` |
| `draft show <id>` | – | Status, Freigabe, Hook-Ergebnis, Stand der Serverkopie (`remote: pending|pushed|taken_over|missing` mit Erklärung), seit S2.5 `copies: uid 8, earlier uid 7 (date of the copy: …)` (alle Kopien im Entwürfe-Ordner; frühere bleiben dort) und `push: …`, wenn der Push den Entwurf nicht ablegte (`approval_stale`, `no_drafts_mailbox`, `push_failed`, seit der Gegenprüfung von S2.4/S2.5 `push_rejected`: der Server hat ihn abgelehnt) oder die Kopie erst nach dem Abschluss ankam (`pushed_after_close`), Kopf, `references:`, ganzer Text, Anhänge mit Warnungen, `preview hash:`; mit `--json` dazu `remote_uid`, `remote_previous_uids`, `pushed_date`, `push_note`, `push_note_text` |
| `draft list` | `-a <konto>`, `--status <s>` (wiederholbar oder mit Komma), `--limit N` (Standard 100) | Tabelle `ID ACCOUNT KIND STATUS APPROVAL REMOTE TO ATT CHANGED SUBJECT` |
| `draft request <id>` | – | `hook: pass` und `draft 1: status pending_approval, approval 1 (until …); a human approves with: approve 1`; besteht der Prüf-Hook nicht (seit S2.4): `hook: fail` (bzw. `timeout`, `error`), darunter die Ausgabe des Hooks (höchstens 4 KB; länger: `(hook output cut after the first 4 KB)`), dann `draft 1: status stays draft; fix the draft and request again` (Exit 0: der Aufruf selbst ist gelungen). `--json`: `hook`, `hook_output`, `hook_output_truncated` |
| `approve <approval-id>` | – | `approval 1 approved by <name>: draft 1 is ready (valid until …); send it in your mail client, postkauz never sends` |
| `reject <approval-id>` | `--reason <text>` (steht nur in der Freigabe, nie im Audit) | `approval 2 rejected by <name>: draft 1 is draft` |
| `approval show <approval-id>` | – | Zustand (`pending`, `approved`, `rejected`, `superseded`, `expired`), Beschreibung ohne Mailinhalt, wer wann vorgelegt und entschieden hat |
| `draft abandon <id>`, `draft mark-sent <id>` | – | `draft 1: abandoned (was draft)` bzw. `draft 2: sent_by_human (was ready)`; die Kopie im Entwürfe-Ordner bleibt (kein Löschen im MVP) |
| `identity add` | `-a <konto>`, `--email` (Pflicht), `--name`, `--reply-to`, `--signature-file <datei>` (Text), `--signature-html-file <datei>` (HTML, bereinigt wie jeder Entwurf; versteckter Text → Exit 3 `hidden_text_in_draft`), `--style-file <datei>` (Stilhinweise für Agenten, ab S2.8 als Resource), `--reply-prefix` (Standard `Re: `), `--forward-prefix` (Standard `Fwd: `), `--default` | `identity 1 of account demo: Demo Büro <demo@example.org> (signature text, default)`; die erste Identität eines Kontos wird Standard |
| `identity list` | `-a <konto>` | Tabelle `ID ACCOUNT DEFAULT NAME EMAIL REPLY-TO SIGNATURE PREFIXES` |
| `identity set-default <id>`, `identity remove <id>` | – | wie `identity add`; ein Entwurf behält seinen Absender (Kopie), auch wenn die Identität entfernt wird (dann ohne Signatur beim nächsten Ändern) |

- **Wer darf was:** Anlegen, Ändern, Lesen und Vorlegen brauchen den Scope `mail:draft` (Rollen `assistant`, `operator`, `admin`; mit `--key` eines `reader`-Schlüssels Exit 3 `missing_scope`). `approve`, `reject`, `draft abandon`, `draft mark-sent` und `identity …` darf **nur der Mensch ohne `--key`**: mit jedem Schlüssel, auch `admin`, Exit 3 `human_only`, nichts ändert sich.
- **Freigabe:** `draft request` legt eine Freigabe an (gültig `[draft] approval_ttl`, Standard 7 Tage, danach fällt der Entwurf beim nächsten Lesen auf `draft` zurück). `approve` setzt `ready`, wenn sich der Entwurf seit der Vorlage nicht geändert hat; jede Änderung danach setzt ihn auf `draft`, die Freigabe auf `superseded`, und ein `approve` der alten Freigabe endet mit Exit 3 `approval_stale`. Seit der Gegenprüfung von S2.3 bindet die Freigabe auch die **fertig gebaute Nachricht** (`message_hash` in `draft request --json` und `approval show --json`): `approve` baut den Entwurf neu; hat sich seit der Vorlage etwas geändert, das erst beim Bau dazukommt (Signatur oder Reply-To der Identität, die zitierte Ausgangsnachricht, Anhänge), fällt der Entwurf auf `draft` zurück, die Freigabe wird `superseded`, Exit 3 `approval_stale`; dann `draft request` erneut. Ändert jemand den Entwurf, während `draft request` ihn prüft (Prüf-Hook), legt `draft request` nicht vor: Exit 3 `draft_changed`. `draft edit` ändert nur die genannten Felder auf dem jeweils aktuellen Stand (ändert ein anderer Prozess gleichzeitig, wird neu gelesen und zusammengeführt; nach drei vergeblichen Versuchen Exit 3 `draft_changed`).
- **Fachliche Ablehnungen** sind Exit 3 (`denied`) mit Grund in der Meldung, z. B. `denied (approval_stale): …`, `draft_closed` (Entwurf verworfen oder gesendet), `draft_taken_over`, `draft_changed`, `attach_outside_dir`, `attach_not_found`, `hidden_text_in_draft`, `invalid_address`, `too_large`, `too_many_recipients`; dazu ein Hinweis mit dem nächsten Schritt. Unbekannte Entwürfe, Freigaben und Identitäten: Exit 1 `not_found`. Widersprüchliche Eingaben (z. B. `--body-file` und `--html-file`): Exit 2.
- **Prüf-Hook** (seit S2.4): `draft request` startet vor der Vorlage das Programm aus `[draft] check_hook` der Policy-Datei (Abschnitt 5). Exit 0 = `pass`; sonst bleibt der Entwurf `draft`, und `draft request` zeigt die Ausgabe des Hooks. Sie stammt vom eigenen Programm des Betreibers, steht aber nie im Audit oder Log; dort nur `hook: pass|fail|timeout|error`. `draft show` zeigt das letzte Ergebnis (`hook:`).
- **Trockenlauf** (`--dry-run`): `draft new|reply|forward` zeigt die Vorschau ohne zu speichern (`dry run: would create a draft (reply) in account demo, status draft; nothing saved`), `draft request` prüft nur (der Prüf-Hook läuft dabei mit: `dry run: hook: …`), `approve` usw. melden, was geschähe.
- **Audit:** je Aufruf ein Eintrag des Executors und je Übergang ein Eintrag mit Grund `transition` (Draft-ID, Art, Zustand vorher/nachher, Zahl der Empfänger und Anhänge, Freigabe, Hook-Ergebnis); nie Betreff, Adressen, Text oder Dateinamen. `audit --since 10m --operation drafts.request_ready` zeigt sie.
- `--json` liefert die Ausgabe der Operation (`draft_id`, `status`, `preview`, `attachments`, `warnings`, `remote`, `remote_note`, `preview_hash`, …).
- **Ablage (S2.5):** `remote: pending` heißt: Die Kopie wartet auf den nächsten `sync` (ein laufender `sync --watch` legt sie nach höchstens `--jobs-interval` ab). Ein Entwurf in `ready` wird nur abgelegt, wenn die jetzt gebaute Nachricht (bis auf `Date`) die freigegebene ist; sonst kein Push, der Entwurf fällt auf `draft` zurück, die Freigabe wird `superseded`, und `draft show` erklärt es (`push: not pushed while ready …`); erneut vorlegen mit `draft request`. Danach wird er wie jeder Entwurf beim nächsten Push abgelegt. `draft abandon` und `draft mark-sent` lassen die Kopie im Entwürfe-Ordner (kein Löschen); abgeschlossene Entwürfe werden nie abgelegt. Seit der Gegenprüfung von S2.4/S2.5: `draft request` und `approve` prüfen, ob die Kopie im Entwürfe-Ordner noch genau die Nachricht ist, die vorgelegt bzw. freigegeben wird (z. B. nach dem Entfernen der Identität oder einer neuen `quote_line`); wenn nicht, wird eine neue Kopie eingereiht (`remote: pending`), die alte bleibt im Ordner. Lehnt der Server einen Entwurf ab, sagt `draft show` `push: not pushed: the mail server refused …` statt „within 10 s"; der nächste `sync` versucht es erneut, eine Änderung sofort. Wird ein Entwurf verworfen, während seine Kopie gerade abgelegt wird, bleibt die Kopie (`push: a copy (uid N) reached the Drafts folder just after the draft was closed …`); löschen kann sie nur der Mensch im Mailclient.
- **Noch nicht (S2.7):** `draft show --eml`, Alias-Optionen.

## 3. Maschinenlesbarkeit (`--json`)

- **Ein Ergebnis = ein JSON-Objekt** auf stdout, einzeilig. Listen, die streambar sein sollen (Suche, Audit), liefern **NDJSON** (ein Objekt je Zeile) ab S1.
- **Dieselben Felder wie MCP und REST.** Die Ausgabe ist die serialisierte Ausgabe der Operation (ihr `OutputSchema`); es gibt keine CLI-eigene Form. Die Registerversion steht in `postkauz version --json` (`schema_version`). Ausgaben mit Mailinhalt (`show`, `thread`, `search`, `attach list`, `attach text`) tragen oben die Untrusted-Hülle `untrusted`, `source`, `notice` (seit S1.8). Der Vertragstest `TestCLIContractRegistry` prüft jede Eingabe, die ein Kommando baut, gegen das `InputSchema` und jede `--json`-Ausgabe gegen das `OutputSchema` der Operation; `TestBindingsCoverCatalogAndSchemas`, dass jede Operation genau ein Kommando hat und jedes Feld des `InputSchema` über ein Argument oder eine Option erreichbar ist.
- **Fehler** als JSON auf stderr: `{"error":{"code":"…","message":"…","hint":"…"}}`. Codes: `usage`, `invalid_input`, `denied`, `needs_approval`, `unknown_operation`, `exists`, `not_found`, `auth`, `connection`, `secret_store_unavailable`, `no_secret`, `schema_too_new`, `policy_invalid`, `interrupted`, `sync_running`, `mismatch`, `internal`, `error`. `internal` heißt: Das Programm ist abgestürzt (Panic, ein Fehler im Programm); die Meldung nennt nur die Meldung der Go-Laufzeit oder den Typ des Panic-Werts, nie dessen Inhalt; ein bereinigter Stack (Funktionen und Datei:Zeile, ohne Argumente) steht auf stderr bzw. im Log (Exit 1).
- **Keine interaktiven Rückfragen im `--json`-Modus.** Fehlt eine nötige Bestätigung, endet der Aufruf mit Exit-Code 2 oder 4; er blockiert nie. Das ist wichtig für Agenten, die die CLI statt MCP nutzen.
- **Keine Geheimnisse in der Ausgabe** (Test mit Kanarienwerten ab S1).
- `--cursor` gibt es seit S1.7 bei `search` (Abschnitt 2d), `--fields id,subject,date` seit S1.8 (nur `search`); `--explain` ist seit S1.8 global (Abschnitt 2g).
- **Eingaben prüft der Executor** gegen das `InputSchema` der Operation (seit S1.8, für CLI und MCP gleich): falscher Typ, Wert außerhalb der Grenzen, unbekanntes Feld → Exit 2, Code `invalid_input`; der Aufruf steht trotzdem im Audit (`error`, Grund `invalid_input`). Die Meldung nennt nur Feld und Regel (z. B. `invalid input: account: does not have the expected form`, `kind: not an allowed value (one of: …)`), **nie den eingegebenen Wert** und nie den Namen eines unbekannten Felds; Zahlen und erlaubte Werte stammen aus dem Schema (seit der Gegenprüfung von S2.3). Grenzen der Entwurfsfelder (Betreff 500, `body_markdown`/`body_html` 200.000, Anzeigename 200, Begründung von `reject` 2.000 Zeichen) prüft der Handler: Überschreitung ist `denied` mit Grund `too_large` (Exit 3), wie A02 es vorgibt. Ins Log (`postkauz.log`) schreibt ein gescheiterter Aufruf nur Operation, Code und Grund, nie die Meldung.

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
  max_message_bytes = 268435456 # größte Rohmail in Bytes (Standard und Höchstwert 256 MiB, Mindestwert 1 MiB = 1048576, seit S2.0); größere (laut Server) fordert der Sync nicht an: einmal Fehler dieser Nachricht, danach gemerkt bis zum nächsten `sync --full`; der Rest läuft weiter
  [audit]
  export_dir = ""           # Standardziel von `audit export`: ein zweiter Ort, nicht das Datenverzeichnis
  [paths]                   # seit S2.1
  attach_dir = ""           # Dateianhänge von Entwürfen nur von hier (A02 E10); leer = <Datenverzeichnis>/attach, relativ = zum Datenverzeichnis
  [operator]                # seit S2.1
  name = ""                 # wer freigibt (approvals.decided_by); leer = Benutzername des Betriebssystems
  [draft]                   # seit S2.1 (gewirkt ab S2.2–S2.6); nicht zu verwechseln mit [draft] der Policy-Datei
  approval_ttl = "168h"     # so lange gilt eine offene oder erteilte Freigabe (1m bis 8760h)
  max_attachment_bytes = 26214400  # Summe der Anhänge eines Entwurfs (Standard 25 MiB, höchstens 256 MiB)
  mailbox = ""              # Ordner für die Entwurfskopien; leer = Ordner mit Rolle drafts
  quote_line = "On {date}, {from} wrote:"  # Einleitung des Zitats; nur {date}, {date_numeric} und {from}, eine Zeile
  ```

- **Zitatzeile auf Deutsch** (`[draft] quote_line`, Platzhalter `{date_numeric}` seit S2.4): Programmausgaben sind Englisch, deshalb beginnt ein Zitat standardmäßig mit `On Wed, 21 Oct 2026 09:15 +0200, Erika Beispiel <erika@example.org> wrote:`. Die Vorlage kennt drei Platzhalter: `{date}` (Datum der Ausgangsnachricht englisch, wie oben), `{date_numeric}` (dasselbe Datum als `21.10.2026 09:15`, in derselben Zeitzone, der des Rechners; fehlt das Datum, steht `??.??.???? ??:??`) und `{from}` (Absender als `Name <adresse>`). Für Deutsch:

  ```toml
  [draft]
  quote_line = "Am {date_numeric} schrieb {from}:"
  ```

  ergibt `Am 21.10.2026 09:15 schrieb Erika Beispiel <erika@example.org>:`. Ein anderer Platzhalter (z. B. `{datum}`) oder ein Zeilenumbruch ist ein Fehler beim Start (Exit 1, Meldung ohne den Wert). Die Zeile wirkt, sobald PostKauz die Nachricht baut (neuer Entwurf, `draft edit`, `draft request`, `approve`); ändert man sie zwischen Vorlage und Freigabe, passt die Freigabe nicht mehr (`approval_stale`, dann `draft request` erneut).

- **Policy-Datei** (seit S2.1, A02 E8): `policy.toml` im selben Ordner wie die Konfigurationsdatei (mit `--profile` im Profilordner), `POSTKAUZ_POLICY` überschreibt den Pfad (dann muss die Datei existieren). Aufbau und Wirkung: [sicherheit.md](sicherheit.md), Abschnitt 5. Jedes Kommando, das die Datenbank öffnet (auch `mcp`), lädt und prüft sie; eine ungültige Datei (unbekannter Schlüssel, ungültiger Wert, Syntaxfehler) hält es auf: Exit 1, Code `policy_invalid`, die Meldung nennt Datei, Zeile und Schlüssel, nie Werte. Ändert sich ihr Inhalt, trägt das nächste Kommando eine neue Version in die Tabelle `policies` ein; die Version steht in jedem Audit-Eintrag (`policy_version` in `audit --json`).

- **Prüf-Hook in der Policy-Datei** (seit S2.4, A02 E4; Regeln: [sicherheit.md](sicherheit.md), Abschnitt 5): `[draft] check_hook` ist eine **Liste** aus Programm und Argumenten, kein Shell-Befehl; das Programm als absoluter Pfad oder als Name auf `PATH`. In den Argumenten stehen Platzhalter für Dateien, die PostKauz je Lauf in einem frischen Temp-Ordner anlegt und danach löscht: `{eml}` (die fertige Nachricht), `{body_html}`, `{body_text}` (Text der Nachricht, UTF-8), `{meta_json}` (Betreff, Absender, Empfänger, Message-ID, Anhangsnamen und -größen, Konto, Identität). Dieselben Pfade stehen in `POSTKAUZ_DRAFT_EML`, `POSTKAUZ_DRAFT_BODY_HTML`, `POSTKAUZ_DRAFT_BODY_TEXT`, `POSTKAUZ_DRAFT_META_JSON`. Exit 0 = bestanden. `check_hook_timeout` (Standard `"30s"`) begrenzt die Laufzeit; danach wird der Hook samt Kindprozessen beendet (`hook: timeout`). Fehlt das Programm: `hook: error` (nie `pass`). Leere Liste (Standard) = kein Hook. Beispiele im Repository unter `deploy/hooks/` (scheitern, wenn der Betreff `TODO` enthält oder die Anrede fehlt: Die erste nicht leere Zeile des Texts muss, Leerzeichen am Anfang nicht gezählt, mit `Hallo`, `Guten Tag`, `Sehr geehrte`, `Liebe`, `Lieber`, `Dear` oder `Hi` beginnen; einfacher Vergleich des Zeilenanfangs mit genauer Groß- und Kleinschreibung, „Sehr geehrter" zählt mit, „Hilfe" leider auch, „hallo" nicht):

  ```toml
  # Windows (Windows PowerShell 5.1); einfache Anführungszeichen lassen die Backslashes stehen
  [draft]
  check_hook = ['powershell.exe', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
                '-File', 'C:\PostKauz\hooks\check-example.ps1', '{meta_json}', '{body_text}']
  check_hook_timeout = "30s"

  # Linux, macOS (anstelle des Windows-Eintrags)
  [draft]
  check_hook = ["sh", "/opt/postkauz/hooks/check-example.sh", "{meta_json}", "{body_text}"]
  ```

  Ein eigener Linter steht nur in der eigenen Policy-Datei, nie im Repository. Unter Windows startet der Hook ohne Konsolenfenster; Stapeldateien (`.bat`, `.cmd`) lehnt PostKauz ab (sie liefen über `cmd.exe`), ein PowerShell-Skript läuft über `powershell.exe -File`.

- **Profile** (`--profile <name>`, seit S1.8): Konfiguration und Daten liegen in einem Unterordner `profiles/<name>` des normalen Konfigurations- bzw. Datenordners (auch unter `POSTKAUZ_CONFIG` und `POSTKAUZ_DATA_DIR`), z. B. `%LOCALAPPDATA%\postkauz\profiles\test\postkauz.db`. Setzt die Profil-Konfiguration `data_dir`, gilt genau dieser Ordner. Namen wie Kontonamen. Seit S2.0 (A2) hängt der Eintrag im Schlüsselbund am Datenverzeichnis (`account/<datadir-id>/<name>`); zwei Profile mit gleichem Kontonamen haben getrennte Einträge.
- **Umgeleitete Pfade unter Windows** (seit S2.0/A7, Befund D3.3c): Läuft PostKauz in einem App-Paket (MSIX, z. B. gestartet aus der Claude-Desktop-App), legt Windows neue Ordner unter `%LOCALAPPDATA%` und `%APPDATA%` in Wahrheit unter `…\Packages\<Paket>\LocalCache\…` an; die Aufgabenplanung und jedes andere Programm sehen dort einen anderen, leeren Ordner. `doctor` erkennt das an der Datei-Identität (Windows zeigt die Umleitung sonst nirgends) und meldet den echten Pfad (Zeile `really:`, JSON `data_dir_real` bzw. `config_path_real`), den Paketnamen (`package:`, JSON `app_package`) und eine Warnung; liegt das Datenverzeichnis noch nicht vor, warnt er vorab. Umgekehrt meldet er in einem normalen Fenster eine Kopie gleichen Namens mit Datenbank im Paketordner, die er nicht sieht (JSON `data_dir_shadow`). Vorhandene Daten erst in den neuen Ordner kopieren, dann umstellen. Abhilfe: Datenverzeichnis außerhalb dieser Ordner, z. B. `--data-dir C:\PostKauz\data` oder `POSTKAUZ_DATA_DIR`. Auf Linux und macOS gibt es das nicht.

- **Kommandozeile** schlägt Umgebung schlägt Datei: `--config` und `--data-dir` (seit S2.0/A7) vor `POSTKAUZ_CONFIG`/`POSTKAUZ_DATA_DIR`; außerdem `POSTKAUZ_SECRETS_BACKEND`, `POSTKAUZ_PASSWORD_<NAME>` (Passwort eines Kontos, nur lesen) und seit S2.1 `POSTKAUZ_POLICY` (Pfad der Policy-Datei).
- **Daten:** `postkauz.db` (SQLite, 0600), `postkauz.log` (JSON-Zeilen, 0600, ohne Inhalte und Geheimnisse), später `blobs/`. Standard: Linux `$XDG_DATA_HOME/postkauz` bzw. `~/.local/share/postkauz`, Windows `%LOCALAPPDATA%\postkauz` (lokal, nicht „Roaming"), macOS `~/Library/Application Support/postkauz`. Verzeichnis 0700 (ein vorhandenes mit weiteren Rechten verschärft seit S2.0 jedes Kommando, das es öffnet, samt Log auf 0600, Sonderbits bleiben; ein Symlink oder ein Pfad eines anderen Nutzers bleibt unverändert; Fehler und Ausnahmen nur als Warnung im Log), **nie** in einem Cloud-synchronisierten Ordner (`doctor` warnt bei OneDrive, Dropbox, iCloud u. Ä.). Angelegt wird es erst beim ersten Kommando, das es braucht; `version` und `doctor` legen nichts an.
- **Geheimnisse:** Schlüsselbund (Windows Credential Manager, macOS Keychain, Secret Service) oder Umgebungsvariable; die Datenbank enthält nur den Verweis `account/<datadir-id>/<name>` (seit S2.0; `<datadir-id>` sind 8 Hex aus der Kennung des Datenverzeichnisses, [datenmodell.md](datenmodell.md), Tabelle `meta`). Einträge mit dem alten Namen `account/<name>` zieht das erste Kommando ab Schema 8 um (außer `mcp`, der keine Passwörter anfasst); scheitert dabei nur das Löschen des alten Eintrags, warnt das Kommando auf stderr und `doctor`, bis er gelöscht ist; ist unter dem alten Namen nichts lesbar und `POSTKAUZ_PASSWORD_<NAME>` nicht gesetzt, bleibt der alte Verweis, und `doctor` nennt, worauf der Umzug wartet. Ohne Schlüsselbund gibt es keinen stillen Rückfall (siehe [sicherheit.md](sicherheit.md), Abschnitt 4).
- **`doctor`** meldet Version, Konfiguration (Pfad, vorhanden, Fehler), Datenverzeichnis, Log, Datenbank (`missing`, `ok`, `needs_migration`, `too_new`, `error`), Schemaversion (vorhanden und bekannt), Schlüsselbund (`available`, `unavailable`, `insecure`) und je Konto, woher das Passwort käme (`stored`, `environment`, `missing`) samt Status der letzten Prüfung; bei `needs_migration` (älterer Bestand, z. B. Schema 7 aus 0.1.0-rc1) liest er die Konten trotzdem (nur Name, Verweis, Status) und weist darauf hin, dass das nächste Kommando migriert; seit S2.0 Warnungen für Konten mit altem Schlüsselbund-Verweis (ohne erreichbaren Schlüsselbund mit dem Hinweis, dass der Umzug wartet und `POSTKAUZ_PASSWORD_<NAME>` weiter gilt) und für alte Einträge, die beim Umzug nicht gelöscht werden konnten oder (je umgezogenem Konto nur lesend geprüft) noch bestehen, etwa nach einem Abbruch zwischen Umstellen und Löschen; seit S1.11 den Audit-Export (Zeile `audit:` mit letztem Export, Ort, Zahl der nicht exportierten Tage und `[audit] export_dir`; Warnung, wenn ein Tag älter als zwei Tage fehlt); seit S2.1 die Policy-Datei (Zeile `policy:` mit Pfad, `found` bzw. `not found, built-in defaults` und der eingetragenen Version, oder `not recorded yet`, wenn die Datei seit dem letzten Kommando geändert wurde; `INVALID: …` mit dem Fehler) und den Ordner für Dateianhänge (Zeile `attach:`); dazu Warnungen. `doctor` trägt selbst nichts ein. Exit 1 nur bei ungültiger Konfiguration oder Policy-Datei. `--json` liefert alles als ein Objekt (u. a. `database`, `schema_version`, `keyring`, `index_pending`, `extract_pending`, `audit_last_export`, `audit_days_not_exported`, `policy_path`, `policy` (`ok`\|`default`\|`invalid`), `policy_sha256`, `policy_version` (0 = noch nicht eingetragen), `policy_error`, `attach_dir`, `accounts`, `warnings`).
- **`doctor --secrets`** (S1.11) prüft zusätzlich Dateirechte und sucht gespeicherte Passwörter, einen gesetzten `POSTKAUZ_KEY` und jedes API-Schlüssel-Muster in Log, Konfiguration, Tabellen ohne Mailinhalt und Audit-Exporten ([sicherheit.md](sicherheit.md), Abschnitt 4). Ausgabe: Abschnitt `secrets:` mit einer Zeile je Prüfpunkt (`ok`, `warn`, `fail`, `skipped`) und `result: ok` bzw. `result: N problems`; ein Fund nennt Datei und Zeile, nie den Wert. Mit Funden Exit 1 (`secret check failed`).
- **Sprache:** Ausgaben und Fehlermeldungen sind Englisch (siehe CLAUDE.md); Mehrsprachigkeit ist kein Ziel des MVP.
