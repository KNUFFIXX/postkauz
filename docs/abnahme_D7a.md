# Abnahme D7a: Tags, Markieren, Verschieben, Papierkorb

Stand: 28.10.2026 (A03 Teil T2, Build `0.3.0-alpha.5` nach der Nachbesserung T2). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Teil T2, Haltepunkt „Abnahme D7a“, und zu [auftraege/A03_s3.md](../auftraege/A03_s3.md), Teil D, Abschnitt D7 (Schritte 1 bis 3 und 6). Skript: [abnahme_D7a.ps1](abnahme_D7a.ps1) mit der MCP-Probe [abnahme_D7a_mcp.py](abnahme_D7a_mcp.py). Windows PowerShell 5.1, ohne Go und Docker, nur mit dem Programm aus `dist/`. Ausgeführt von der Prüfsitzung auf dem Büro-PC.

## Zweck

T2 bringt Tags (lokale Etiketten), Markieren (gelesen, markiert), Verschieben (auch „Label entfernen“ in Gmail) und den Papierkorb mit Freigabe durch den Menschen. D7a prüft das in zwei Teilen:

- **Teil A (immer, offline):** ein frisches Datenverzeichnis mit zwei erfundenen Mails. Ohne Mailserver gibt es keine Serverkopien; geprüft wird deshalb alles, was ohne Server geht: Tags, Suche nach Tags, die Ablehnungen von `mark` und `trash` ohne Serverkopie, Freigabe nur durch den Menschen, die Rechte je Rolle, die MCP-Werkzeuge je Rolle, das Audit.
- **Teil B (nur mit deiner Zustimmung je Schritt):** am echten Gmail-Konto, aber nie auf der Live-Datenbank, und nur an **einer Mail, die du dir selbst geschickt hast**: taggen, als gelesen markieren, in einen Label-Ordner verschieben, das Label wieder entfernen, in den Papierkorb mit Freigabe. Nichts wird endgültig gelöscht. Diese Schreibvorgänge erlaubt Entschieden 48 ausdrücklich.

## Grundregeln

- **Nie die Live-Datenbank** (Rahmen 5): Der Build stammt nicht von `main`. `-DataDir` und `-Config` sind Pflicht und gehören Teil A; das Skript legt beide neu an (`-DataDir` leer oder noch nicht vorhanden, `-Config` noch nicht vorhanden). Teil B arbeitet auf `-AccountDataDir` mit `-AccountConfig`: einer **Kopie** der Live-Daten (wie in Abnahme T1) oder einem eigenen Datenverzeichnis mit diesem Konto.
- **Abbruch ohne Änderung**, wenn `-DataDir`, `-Config`, `-WorkDir` oder `-AccountDataDir`/`-AccountConfig` im Standard-Datenverzeichnis (`%LOCALAPPDATA%\postkauz`), im Standard-Konfigurationsordner (`%APPDATA%\postkauz`) oder in einem mit `-LivePath` genannten Pfad liegen (oder umgekehrt). Weil Zeichenketten eine Junction oder ein `subst`-Laufwerk nicht erkennen, legt das Skript in `-DataDir`, `-WorkDir` und `-AccountDataDir` kurz eine Markerdatei an und bricht ab, wenn sie in einem dieser Pfade auftaucht.
- **Teil B nur mit** `-LiveAccount <konto>` **und** `-AllowRealMailbox`, dazu `-AccountDataDir`, `-AccountConfig`, `-Label` und `-LivePath` (ohne `-LivePath` greift der Schutz nicht; dann kein Teil B). Fehlt etwas, steht `SKIP B` mit Grund im Protokoll, Teil A läuft trotzdem.
- **Zugangsdaten nie im Skript.** Teil B nutzt das Passwort, das im Windows-Schlüsselbund für das Konto der Kopie liegt (wie der Dauerbetrieb und Abnahme T1, Schritt 3). OAuth2-Konten (Microsoft) überspringt Teil B: Ein Token-Refresh auf der Kopie würde unter dem Verweis des Dauerbetriebs speichern.
- **Protokoll** `D7a_windows.txt` in `-WorkDir`: Das Skript legt es exklusiv neu an und überschreibt nie eine vorhandene Datei (auch keinen Link); `-WorkDir` darf kein Reparse Point (Junction, Link) sein. Testdaten, Schlüsseldateien und den Audit-Export von Teil A legt es in einen neuen Laufordner `-WorkDir\lauf-<Datum-Uhrzeit>`. Schlüssel, Mailadressen, Pfade (als `<DataDir>`, `<AccountDataDir>`, `<Live1>` …), der Kontoname aus Teil B (`konto1`) und der Label-Name (`<Label>`) sind maskiert. Betreff und Absender der Testmail zeigt das Skript nur auf dem Bildschirm, nie im Protokoll; Ordner nennt es nach Rolle (`role:inbox`, `role:all`, `role:trash`) oder als `<Label>`/`<ordner>`.
- Jede Zeile ist `OK`, `FAIL` oder `SKIP` mit Nummer; letzte Zeile `D7a: <n> OK, <m> FAIL, <k> SKIP`. Exit 0 nur ohne `FAIL`, Exit 2 bei Abbruch vor dem ersten Schritt.

## Voraussetzungen

**Für beide Teile** (die Prüfsitzung legt sie in einen Ordner, z. B. `C:\PK\d7a`):

| Datei | Herkunft |
|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` dieses Zweigs, umbenannt; Prüfsumme gegen `dist/SHA256SUMS` |
| `abnahme_D7a.ps1` | `docs/abnahme_D7a.ps1` |
| `abnahme_D7a_mcp.py` | `docs/abnahme_D7a_mcp.py` (neben dem Skript) |

Python 3.8 oder neuer für die MCP-Probe (Schritt A3.9): Das Skript nimmt `-Python <python.exe>` oder sucht `python.exe`/`py.exe` im `PATH` (nicht den Platzhalter aus dem Microsoft Store). Ohne Python steht `SKIP A3.9` mit Grund im Protokoll.

**Nur für Teil B:**

1. **Kopie der Live-Daten** in einem eigenen Ordner, z. B. `C:\PK\d7a\konto-daten`, mit eigener Konfiguration `C:\PK\d7a\konto-config.toml`. Am einfachsten wie in [abnahme_T1.md](abnahme_T1.md) Schritte 0 und 1: Dauer-Sync, Audit-Export und Claude-App stoppen, `postkauz.db`, `postkauz.db-wal` und `postkauz.db-shm` **zusammen** kopieren, danach alles wieder starten (oder `abnahme_T1.ps1` mit neuem `-DataDir` laufen lassen und dessen Kopie nehmen). Konfiguration: Kopie der echten `config.toml`, darin `[audit] export_dir` auf einen Ordner unter `C:\PK\d7a` ändern; `[tags] sync_keywords` aus lassen (Standard). Das Skript migriert nur diese Kopie.
2. **Label in Gmail anlegen** (Browser: links „Neues Label erstellen“), z. B. `D7a-Test`. Der Pfad, den `mailbox list` zeigt, ist `-Label` (das Skript holt ihn mit dem ersten Sync in den Spiegel).
3. **Testmail:** Schick dir von deinem Gmail-Konto eine Mail an dieselbe Adresse, Betreff z. B. „D7a Test“. Lass sie ungelesen. Das Skript nimmt nur Mails, deren Absender die Adresse des Kontos (oder ein Alias) ist und die im Posteingang liegen.
4. **Kein wartender Entwurf** in der Kopie (`remote: pending`): Sonst legte jeder `sync` ihn im echten Entwürfe-Ordner ab. Das Skript prüft das (B0.3) und überspringt Teil B dann. Ebenso **keine offene Aktion** (`queued`/`running`, etwa aus einem abgebrochenen früheren Lauf mit derselben Kopie): Der lesende Sync B0.4 führte sie sonst am echten Postfach aus (B0.3a, Nachbesserung T2).
5. Gmail im Browser offen haben: An vier Stellen fragt das Skript, was du dort siehst.

## Aufruf

Nur Teil A (offline; darf ohne sichtbares Fenster laufen):

```powershell
$d='C:\PK\d7a'; Unblock-File "$d\postkauz.exe","$d\abnahme_D7a.ps1"
powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$d\abnahme_D7a.ps1" -ExePath "$d\postkauz.exe" -DataDir "$d\daten-a" -Config "$d\config-a.toml" -WorkDir $d -LivePath <Live-Datenverzeichnis>,<Live-config.toml>
```

Teil A und B (Fenster **sichtbar**, weil das Skript nach „ja“ fragt; `-WindowStyle Hidden` weglassen):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PK\d7a\abnahme_D7a.ps1 `
  -ExePath C:\PK\d7a\postkauz.exe -DataDir C:\PK\d7a\daten-a -Config C:\PK\d7a\config-a.toml -WorkDir C:\PK\d7a `
  -LivePath <Live-Datenverzeichnis>,<Live-config.toml> `
  -LiveAccount <konto> -AllowRealMailbox `
  -AccountDataDir C:\PK\d7a\konto-daten -AccountConfig C:\PK\d7a\konto-config.toml -Label D7a-Test
```

Weitere Schalter, nur falls nötig: `-ExpectVersion` (Standard `0.3.0-alpha.5`), `-Python`, `-Probe`. Ein Wiederholungslauf braucht ein neues `-DataDir`, ein neues `-Config` (Teil A beginnt immer frisch) und ein neues `-WorkDir` oder das Umbenennen des alten Protokolls `D7a_windows.txt` (sonst Abbruch: „Protokolldatei existiert schon (oder ist ein Link)“); `-AccountDataDir` darf derselbe bleiben.

## Teil A: offline (immer)

| Nr. | Was passiert | Erwartet |
|---|---|---|
| A0.1 | `version` | nennt `-ExpectVersion`; SHA-256 der Programmdatei im Protokoll |
| A0.2–A0.4 | Konfiguration angelegt, Konto `demo` ohne Server (`127.0.0.1:1`, Passwort nur aus der Umgebung), `import mbox` mit zwei Mails (Rechnung, Verein; Adressen nur `example.org`), lokale IDs | `account demo added`; `new: 2, existing: 0, skipped: 0, errors: 0`; zwei IDs |
| A0.5 | `key create` für `test-agent` (reader), `test-assistant` (assistant), `test-triage` (triage), `test-operator` (operator), Laufzeit 2 Tage; `key list` | vier Schlüssel; `test-triage` hat genau `mail:read,mail:search,mail:draft,mail:tag`; kein Schlüssel im Klartext in `key list` |
| A0.6 | `doctor` | `data dir:` ist `-DataDir` (keine Umleitung) |
| A1.1 | `tag add rechnung <id>` (D7.1) | `1 tags added, 0 removed in account demo`, darunter `<id>  rechnung` |
| A1.2 | `search tag:rechnung --json` | genau diese ID |
| A1.3 | `tag list` | `rechnung  1` |
| A1.4 | `show <id>` | `tags:  rechnung` |
| A1.5 | `mark <id> --seen` | **Exit 3**, `denied (no_server_copy)`: eine nur importierte Mail hat keine Kopie auf einem Server. Das ist offline das richtige Ergebnis; „`show` nennt `pending`“ (D7.1) prüft Teil B (B3.2) |
| A1.6 | `show <id> --json` | unverändert: nicht gelesen, `pending_flags` nicht gesetzt |
| A2.1 | `trash <id>` (D7.2) | **Exit 3**, `denied (no_trash_mailbox)`: ohne Sync kennt der Spiegel keinen Papierkorb; es entsteht kein Antrag. Antrag, `action approve` und Ausführung prüft Teil B (B6) |
| A2.2 | `action approve 999 --key test-agent` | **Exit 3**, `denied (human_only)`: die Policy lehnt vor jeder anderen Prüfung ab (auch für eine Aktion, die es nicht gibt) |
| A2.3 | `action reject 999 --key test-operator` | **Exit 3**, `denied (human_only)`: auch die Rolle operator darf nicht entscheiden |
| A2.4 | `action approve 999` (Mensch) | `error: action not found`, Exit 1 (Code `not_found`; Befund D7a-1, behoben) |
| A2.5 | `action list` | `no actions` |
| A3.1 | `policy show --key test-assistant` (D7.3) | `messages.tag`, `sync.request` erlaubt; **keine** Zeile `messages.mark`/`messages.move`/`messages.trash` (`mark` braucht seit der Nachbesserung T2 den Scope `mail:mark`, V20); `actions.approve: human_only` |
| A3.2 | `policy show --key test-operator` | `messages.trash  mail:trash  R3  needs_approval`, `messages.move`, `messages.mark  mail:mark`, `actions.approve: human_only` |
| A3.3 | `policy show --key test-triage` (Nacht-Triage, Vorgang 5) | `scopes: mail:draft, mail:read, mail:search, mail:tag`; `messages.tag`, `drafts.create` ja; kein `messages.move`, `messages.trash`, `sync.*` |
| A3.4 | `move <id> --role archive --key test-triage`, `trash <id> --key test-triage` | beide **Exit 3**, `denied (missing_scope)` |
| A3.5 | `tag add rechnung <id2> --key test-triage` | erlaubt, Exit 0, als Trockenlauf (`dry run: 1 tags added …`; Schlüssel laufen standardmäßig trocken) |
| A3.6 | `trash 1,2,…,51 --key test-operator` (ohne `--confirm-count`) | **Exit 3**, `denied (confirm_count_required)` (Schwelle 50) |
| A3.7 | `sync request -a demo` mit `--key test-triage` bzw. `--key test-assistant` | triage: Exit 3 `denied (missing_scope)`; assistant: `dry run: would queue a sync of account demo …` |
| A3.8 | `sync request -a demo` (Mensch) | `sync of account demo (all mailboxes) queued as job <n>`; der Job bleibt ohne Server liegen (erwartet) |
| A3.9–A3.12 | MCP-Probe (`abnahme_D7a_mcp.py`, spricht mit `postkauz mcp` wie Claude) | A3.10: `test-assistant` sieht `mail_tag`, `mail_sync_now`, aber weder `mail_mark` noch `mail_move` noch `mail_trash`; A3.11: `test-operator` sieht `mail_mark`, `mail_move` und `mail_trash`; A3.12: `mail_trash` mit 51 IDs ohne `confirm_count` → `status: denied`, `reason: confirm_count_required`, keine Aktion angelegt; A3.9: Probe vollständig (3 Prüfungen, Exit 0) |
| A6.1 | `audit --since 10m` (D7.6) | Exit 0, zeigt die Aufrufe dieses Laufs (u. a. `messages.tag`, `denied (human_only)`) |
| A6.2 | `audit --since 10m --json` | je Aufruf **eine** Zeile mit Endergebnis; außer dem gerade laufenden `audit.list` keine Zeile `started` |
| A6.3 | `audit --since 10m --raw --json` | je erlaubtem Aufruf ein Start `started` und ein Abschluss mit `ref_id` (jeder Abschluss hat seinen Start); die Policy-Ablehnung `human_only` ist **ein** Eintrag ohne Start |
| A6.4 | dieselbe Ausgabe | kein Betreff, Absender oder Text der Testmails im Audit |
| A6.5 | `audit export --to <WorkDir>\lauf-<Zeit>\audit-export` (neuer Ordner) | `written`; Tagesdatei `postkauz-audit-<Tag>.jsonl` und `postkauz-audit-manifest.json` |
| A6.6 | `audit verify --json` | `status: ok` |

**Abweichungen vom Wortlaut von D7** (A03, Teil D), mit Grund:

- D7.1 „`mark <id> --seen` → `pending` in `show`“ und D7.2 „`trash` → `needs_approval`, `action approve <n>` → `queued`“ gehen offline nicht: Importierte Mails haben keine Serverkopie, und ohne Sync gibt es keinen Papierkorb. Teil A prüft die Ablehnungen, Teil B den ganzen Ablauf.
- D7.3 „`policy show --key test-assistant` zeigt `messages.trash: needs_approval`“: Die Rolle assistant hat seit T2 weder `mail:move` noch `mail:trash` (MCP-Teil von D7.3: „assistant sieht kein `mail_trash`“). `messages.trash … needs_approval` zeigt deshalb `policy show --key test-operator` (A3.2); A3.1 prüft, dass assistant es **nicht** hat.
- `action approve <n> --key test-agent` prüft Teil A mit einer nicht vorhandenen Aktion (999): Die Policy lehnt `human_only` vor dem Handler ab. Mit einer echten Aktion prüft es Teil B (B6.3).

## Teil B: am echten Konto (nur mit Zustimmung)

Vor jedem Schritt fragt das Skript: **„ja“** eingeben, sonst wird der Schritt übersprungen (`SKIP`). Wo es fragt, was du in Gmail siehst, antwortest du „ja“ oder „nein“ (leer = nicht nachgesehen, `SKIP`).

| Nr. | Was passiert | Erwartet |
|---|---|---|
| B0.1 | `accounts list --json` auf der Kopie (das erste Kommando migriert die Kopie auf das Schema dieses Builds) | Konto `-LiveAccount` vorhanden |
| B0.2 | `doctor --json` | Datenbank `ok`; Konto mit Passwort (OAuth2 → `SKIP B`) |
| B0.3 | `draft list` des Kontos | kein Entwurf mit `remote: pending` (sonst `SKIP B`); bei 500 Entwürfen (Obergrenze) ebenfalls `SKIP B` |
| B0.3a | `action list --account <konto> --state queued --state running --json` | keine offene Aktion (sonst `SKIP B`: ein `sync` führte sie am echten Postfach aus; seit der Nachbesserung T2, V2) |
| B0.4 | **[ja]** `sync -a <konto>` | Exit 0 (liest nur; holt die Testmail und das Label in den Spiegel) |
| B0.5 | `mailbox list` | Posteingang vorhanden; dazu Rolle `all` („Alle Nachrichten“), Papierkorb und das Label (fehlt eins, werden B4/B5 bzw. B6 übersprungen) |
| B1 | Du gibst die **lokale ID** der Testmail ein (finden mit `search "D7a Test" -a <konto>`); das Skript zeigt Datum, Absender, Empfänger, Betreff und Ordner auf dem Bildschirm; **[ja]** | nur eine Mail mit Absender = Adresse des Kontos (oder Alias), im Posteingang; sonst fragt es erneut (höchstens dreimal) |
| B2.1–B2.2 | **[ja]** `tag add d7a-test <id>`; `search tag:d7a-test` | Tag an der Mail, Suche findet sie (nur lokal in der Kopie) |
| B3.1–B3.2 | **[ja]** `mark <id> --seen` | `mark of 1 messages queued (action <n>, …)`; `show`: gelesen, **pending** (noch nicht am Server) |
| B3.3–B3.5 | `sync -a <konto>`, dann `show`, `action show <n>` | Exit 0; `pending` verschwunden; Aktion `done`, jede Kopie `ok`. Im Dauerbetrieb erledigt das ein laufendes `sync --watch` binnen 10 s (`--jobs-interval`); die Kopie hat keinen, deshalb ruft das Skript `sync` |
| B3.6 | **[Du]** Gmail neu laden | Testmail als gelesen markiert |
| B4.1–B4.4 | **[ja]** `move <id> --from INBOX --to <Label>`, `sync`, `action show`, `show` | Aktion `done`/`ok`; die Mail liegt im Label-Ordner und nicht mehr im Posteingang |
| B4.5 | **[Du]** Gmail | Mail hat das Label und ist archiviert (nicht mehr im Posteingang) |
| B5.1–B5.4 | **[ja]** `move <id> --from <Label> --role all` (Gmail „Label entfernen“, Vorgang 1), `sync`, `action show`, `show` | Aktion `done`/`ok`; die Mail liegt nicht mehr im Label-Ordner |
| B5.5 | **[Du]** Gmail | Label an der Mail weg; sie liegt unter „Alle Nachrichten“ |
| B6.1 | **[ja]** `trash <id>` | **Exit 4**, `trash of 1 messages to <Papierkorb> requested (action <n>, approval <m>)`, Ausgabe nennt `action approve <n>`, stderr `needs_approval` |
| B6.2–B6.3 | `action show <n>`; Probe-Schlüssel (Rolle reader, 1 Tag, nur in der Kopie) und `action approve <n> --key <probe>` | `requested`; **Exit 3** `denied (human_only)`, Aktion bleibt `requested`; der Probe-Schlüssel wird danach widerrufen |
| B6.4 | **[ja]** `action approve <n>` | `action <n>: queued (approval <m> by …)`. Ohne „ja“: `action reject <n>` → `rejected`, am Server passiert nichts |
| B6.5–B6.7 | `sync`, `action show <n>`, `show` | Aktion `done`/`ok`; die Mail liegt im Papierkorb |
| B6.8 | **[Du]** Gmail | Mail im Papierkorb, dort wiederherstellbar |
| B7.1–B7.2 | `audit --since 2h --json`, `audit verify --json` auf der Kopie | die Aufrufe aus Teil B mit Endergebnis; `status: ok` |

**Gmail-Hinweise:** Eine Mail an dich selbst liegt in Gmail im Posteingang, unter „Gesendet“ und unter „Alle Nachrichten“; der Spiegel führt sie als **eine** Nachricht mit drei Ordnern. B4 nennt deshalb den Quellordner (`--from INBOX`). Verschieben aus dem Posteingang in ein Label heißt in Gmail: Label setzen und aus dem Posteingang nehmen (archivieren). Der Papierkorb in B6 nimmt die Kopie aus „Gesendet“ (sonst aus „Alle Nachrichten“); Gmail legt die ganze Nachricht in den Papierkorb. PostKauz löscht nie endgültig; Gmail leert den Papierkorb nach 30 Tagen selbst.

## Was du entscheidest

- **Alles `OK` (SKIP nur, wo gewollt):** D7a bestanden. Die Testmail liegt im Papierkorb; wiederherstellen kannst du sie in Gmail (Papierkorb → Mail öffnen → „In Posteingang verschieben“). Das Label `D7a-Test` kannst du in Gmail löschen.
- **`FAIL` in Teil A:** Claude schreibt je Abweichung, was erwartet war und was kam; du entscheidest: nachbessern lassen oder hinnehmen.
- **`FAIL` in Teil B oder „nein“ bei einer Gmail-Frage:** wichtigster Fall ist B5 (Label entfernen, Vorgang 1). Zeigt Gmail das Label noch, obwohl PostKauz `done` meldet, gilt „Label entfernen über `--role all`“ als nicht bestätigt; dann entscheidest du, ob T3 einen anderen Weg (z. B. Gmail-Labels per `X-GM-LABELS`) bekommt.
- **Kopie aufräumen:** Nach der Abnahme `-AccountDataDir` und `-DataDir` löschen (nur Kopie und Testdaten). Die Schlüssel aus Teil A gelten 2 Tage, der Probe-Schlüssel aus B6 einen Tag und ist schon widerrufen.

## Bekannte Grenzen

- **Gmail-Label-Verhalten am Testserver nicht prüfbar:** Dovecot, Stalwart und GreenMail kennen keine Labels; dort heißt `move --role all` nur „in den Ordner mit Rolle all verschieben“ (Dovecot legt dabei eine zweite Kopie unter „All Mail“ an). Ob Gmail dabei das Label entfernt, zeigt nur Teil B (B5.4 und deine Antwort in B5.5).
- **MOVE:** PostKauz verschiebt nur mit IMAP `MOVE`; ein Server ohne MOVE bekommt `unsupported`, und nichts passiert (E2, kein Ersatz über Kopieren und Löschen). Ob GreenMail 2.1 MOVE anbietet, ist im Container nicht gemessen (Docker Hub lehnte das Laden der Images ab, Fehler 429); Gmail und Dovecot bieten MOVE.
- **Nicht unter PowerShell ausgeführt:** Im Container gibt es kein PowerShell. Belegt ist vorab: Teil A als Python-Nachbau mit denselben Kommandos und Mustern gegen das Linux-Programm (alle 30 Prüfungen `OK`, MCP-Probe unverändert `3 OK`), Teil B als Python-Nachbau gegen einen lokalen Dovecot 2.3 mit Gmail-ähnlichen Ordnern (Posteingang, „Sent“, „All Mail“ mit `\All`, Label-Ordner, „Trash“; selbst geschickte Mail in drei Ordnern; alle 30 Prüfungen `OK`). Nur unter Windows prüfbar: PowerShell 5.1 selbst (Quoting, Kodierung, `Read-Host`), die Markerprüfung mit Junctions, Python unter Windows, der Schlüsselbund. Bei einem Skriptfehler (nicht bei einer fachlichen Abweichung) Protokoll und Meldung an die Bausitzung zurückgeben.
- **Befund D7a-1:** `action approve <n>` bzw. `action show <n>` für eine Aktion, die es nicht gibt, endet mit Exit 1 und `error: store: action not found: <n>` (Code `error`, Audit `error (handler_error)`), nicht mit Code `not_found` wie bei Entwürfen und Nachrichten (`internal/cli/exit.go` kennt `store.ErrActionNotFound` nicht). **Behoben in T2** (CLI und MCP melden `not_found`, ohne Text des Stores); A2.4 prüft das.
- **Befund D7a-2:** `key create --help` nennt bei `--role` nur `reader | assistant | operator | admin`; die Rolle `triage` (T2, Vorgang 5) fehlte im Hilfetext, funktionierte aber. **Behoben in T2.**
- `[tags] sync_keywords = true` in `-AccountConfig` schickte den Tag aus B2 beim nächsten Sync als IMAP-Keyword an Gmail; das Skript meldet es (Hinweis unter B2.1). Standard ist aus.
