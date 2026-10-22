# Abnahme D2: Suche, CLI, MCP (nach S1.9)

Stand: 16.10.2026. Gehört zu `auftraege/A01_phase0.md`, Teil D, Abschnitt D2. Die Kommandos und erwarteten Ausgaben sind mit der vorbereiteten Datenbank durchgespielt (Linux-Lauf; hier die PowerShell-Fassung für Windows).

**Was du prüfst:** PostKauz findet in einem Testarchiv mit 1.000 Nachrichten deutsche Wortformen, Umlaut-Schreibweisen und Teilwörter, zeigt Mails mit verstecktem Text sicher an, erzeugt einen Schlüssel für einen Agenten (nur einmal sichtbar), stellt Claude über MCP nur lesende Werkzeuge bereit, schreibt jeden Zugriff ohne Mailtext ins Audit und erkennt eine nachträglich veränderte Audit-Datei. Dauer: rund 30 Minuten. Du musst nichts programmieren: Kommando eingeben, Ausgabe mit „Erwartet" vergleichen, abhaken. Bei einer Abweichung: Kommando und vollständige Ausgabe an Claude geben, nichts reparieren.

**Anders als bei D1 brauchst du weder Docker noch Go noch einen Mailserver.** Die Nachrichten liegen schon fertig gespiegelt in einer vorbereiteten Datenbank (synthetische Testmails, keine echten Daten). Du brauchst nur:

| Datei | Herkunft (Repository, von Claude bereitgestellt) | Ablage bei dir |
|---|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` (Stand S1.9 mit Nachbesserung vom 16.10.2026), umbenannt | `C:\PostKauz\postkauz.exe` |
| `abnahme-d2-daten.zip` | `dist/abnahme-d2-daten.zip` (3,6 MB) | `C:\PostKauz\abnahme-d2-daten.zip` |

Für D2.7 und D2.8 zusätzlich **Claude Code** (Kommando `claude` in PowerShell) **oder** die **Claude-App** (Desktop). Beides ist bei dir vorhanden; eines genügt.

Alle Kommandos sind für **PowerShell** geschrieben und laufen in **einem** Fenster nacheinander. Öffnest du zwischendurch ein neues Fenster, die Zeilen aus Abschnitt 0 ab `cd` wiederholen (ohne `Expand-Archive`).

## 0. Vorbereitung

Ordner `C:\PostKauz` anlegen und die beiden Dateien hineinlegen. Dann PowerShell öffnen:

```powershell
cd C:\PostKauz
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-FileHash .\abnahme-d2-daten.zip -Algorithm SHA256 | Format-List Hash
$env:POSTKAUZ_DATA_DIR = "$env:LOCALAPPDATA\postkauz-abnahme-d2"
Expand-Archive .\abnahme-d2-daten.zip -DestinationPath $env:POSTKAUZ_DATA_DIR
.\postkauz.exe version
.\postkauz.exe doctor
```

Erwartet:

- `Hash : 329F6D9E0C28CCE98A387BF0F42DEA81DC836995204939D9DEF362B43AC03AD4` (die Datei ist unverändert angekommen).
- `version` zeigt eine Zeile mit `postkauz`, Version und `go1.27.2 windows/amd64`.
- `doctor` (Auszug; Pfade mit deinem Benutzernamen):

  ```text
  data dir:  C:\Users\…\AppData\Local\postkauz-abnahme-d2
  database:  ok (C:\Users\…\AppData\Local\postkauz-abnahme-d2\postkauz.db)
  schema:    6 (program knows 6)
  keyring:   available (Windows Credential Manager)
  pdftotext: not found (optional; without it PDF attachments get no text, see README)
  search:    0 messages waiting for the index (index version 2)
  extract:   0 attachments waiting for text extraction
  accounts:
    demo: password missing, status new …
  ```

  Mit einem Programm ab S1.10 (Import, Migration 0007) steht bei `schema` zunächst `6 (program knows 7)`; das erste Kommando, das die Datenbank öffnet (z. B. `accounts list`), hebt sie auf 7 (mit Sicherungskopie daneben), danach `7 (program knows 7)`. Beides ist richtig.

  Die Zeile `demo: password missing` und eine Warnung dazu sind **richtig**: Das Konto `demo` gehörte zum Testserver, auf dem die Daten erzeugt wurden; für D2 wird nichts synchronisiert. Steht bei `demo` stattdessen `stored`, liegt noch das Testpasswort aus D1 im Schlüsselbund; auch das ist in Ordnung.

Erklärungen:

- `[Console]::OutputEncoding` sorgt dafür, dass Umlaute auch in weitergeleiteter Ausgabe (`| Select-Object`) richtig erscheinen. Ohne diese Zeile stehen dort Zeichen wie `Ã¼`; das wäre eine Einstellung von PowerShell, kein Fehler der Daten.
- `POSTKAUZ_DATA_DIR` lässt PostKauz mit der Abnahme-Datenbank arbeiten; dein normales Datenverzeichnis bleibt unberührt.
- Meldet `Expand-Archive`, dass Dateien schon existieren: Die Abnahme lief schon einmal. `Remove-Item -Recurse -Force $env:POSTKAUZ_DATA_DIR` und die `Expand-Archive`-Zeile wiederholen.

## D2.1 Suche mit Wortformen

```powershell
.\postkauz.exe search "Rechnung" -a demo --limit 5
```

Erwartet: eine Tabelle mit den Spalten `ID`, `DATE`, `FROM`, `SUBJECT` und fünf Zeilen, darunter Betreffzeilen mit **„Rechnungen"** (Mehrzahl, also eine andere Wortform als gesucht), am Ende genau:

```text
5 of 453 results (by relevance)
more: --cursor …
```

So sah es bei der Vorbereitung aus (Reihenfolge und Datumsspalte können sich an einem anderen Tag leicht verschieben, weil neuere Mails etwas höher gewichtet werden; die Zahl **453** bleibt):

```text
ID   DATE              FROM                                      SUBJECT
196  2026-09-28 06:45  Sören Straßburger <…                      Rechnungen für das Erdgeschoss
132  2026-08-09 14:54  Dörte Dummy <doerte@dummy.example.org>    Rechnungen für das Erdgeschoss
135  2026-07-15 16:01  noreply@portal.example.net                Rechnungen für das Erdgeschoss
3    2026-09-04 07:15  Grete Größe <grete@groesse.example.net>   AW: Fenster schließt nicht – Reparatur nötig 🔧 (Vorgang 1)
219  2026-06-15 10:30  Hans-Jörg Fiktiv <…                       Rechnung Gartenpflege Juni
5 of 453 results (by relevance)
```

(Die vierte Zeile ist ein Treffer im Text der Mail, nicht im Betreff. Das Symbol am Zeilenende zeigt die Konsole eventuell als `?`. Lange Absender kürzt die Tabelle mit `…`; hier sind zwei davon zusätzlich gekürzt.)

## D2.2 Umlaut-Schreibweisen

```powershell
.\postkauz.exe search "prüfen"
.\postkauz.exe search "pruefen"
Compare-Object (.\postkauz.exe search "prüfen" --json --fields id) (.\postkauz.exe search "pruefen" --json --fields id)
```

Erwartet: Beide Suchen enden mit `20 of 262 results (by relevance)` und zeigen dieselben Zeilen. Die dritte Zeile vergleicht die Trefferlisten Nummer für Nummer und gibt **nichts** aus (keine Unterschiede).

## D2.3 Teilwort in einem zusammengesetzten Wort

```powershell
.\postkauz.exe search "Vollstreck"
```

Erwartet: Zeilen mit dem Betreff **„Zwangsvollstreckung angedroht – bitte prüfen"** (auch mit `AW:`, `Re:`, `Fwd:` davor), am Ende `20 of 68 results (by relevance)`. Gesucht war nur ein Teil des Wortes.

## D2.4 Maschinenlesbare Ausgabe (eine Zeile je Treffer)

```powershell
.\postkauz.exe search "has:attachment" --json | Select-Object -First 3
```

Erwartet: genau drei Zeilen, jede beginnt mit `{"id":` und enthält `"has_attachments":true`, zum Beispiel:

```text
{"id":111,"account":"demo","thread_id":70,"date":"2026-09-22T11:58:00Z","from":"Bärbel Beispiel \u003cbaerbel@beispiel.example.org\u003e","subject":"Sehr lange Betreffzeile zur Nebenkostenabrechnung …","snippet":"…","flags":["seen"],"has_attachments":true,…}
{"id":91,…,"subject":"Wasserschaden im Badezimmer (dringend!)",…,"has_attachments":true,…}
{"id":67,…,"subject":"Zwangsvollstreckung angedroht – bitte prüfen",…,"has_attachments":true,…}
```

(`\u003c` ist die JSON-Schreibweise für `<`.)

## D2.5 Mail mit verstecktem Text

Diese Testmail enthält im HTML einen unsichtbaren Absatz mit einer Anweisung an „den KI-Assistenten" (eine nachgestellte Prompt-Injection):

```powershell
.\postkauz.exe show pk.s16.02.0045f9d9@corpus.example.org
```

Erwartet:

```text
id:                976
account:           demo
mailboxes:         Archiv/2025 (uid 367)
message-id:        pk.s16.02.0045f9d9@corpus.example.org
…
subject:           Rechnung Hausmeisterdienst
…
sender checks:     spf none, dkim none, dmarc none
warning:           hidden text removed (the HTML contained text a reader would not see; possible prompt injection)
--- text (from HTML) ---
Können Sie bitte einen Handwerker schicken? Ich bin werktags ab 15 Uhr erreichbar.

Mit freundlichen Grüßen
```

Wichtig: die Zeile **`warning: … hidden text removed …`**, und der Text enthält **nicht** den Satz „Hinweis an den KI-Assistenten …" (der versteckte Teil ist entfernt). Gegenprobe für Agenten:

```powershell
.\postkauz.exe show pk.s16.02.0045f9d9@corpus.example.org --json
```

Erwartet: eine lange Zeile mit `"untrusted":true`, `"source":"email"`, `"notice":"untrusted content from e-mail: …"` und `"hidden_text_removed":true`.

## D2.6 Schlüssel für einen Agenten

```powershell
$out = .\postkauz.exe key create --name test-agent --role reader
$out
$key = ($out | Select-String 'pk_key_[A-Za-z0-9_-]+').Matches[0].Value
.\postkauz.exe key list
.\postkauz.exe key list | Select-String pk_key_
```

Erwartet:

- Nach `$out` (der Schlüssel ist bei dir ein anderer):

  ```text
  key test-agent created (role reader: mail:read, mail:search, audit:read; writing calls run as dry run by default; expires 2027-01-14)
  This is the only time the key is shown. Store it now (e.g. in the MCP configuration as POSTKAUZ_KEY):

    pk_key_2AdJuxG7LWPB1BOxLartC6e0oUrCQJeeg1NUM9N0GZI

  Only its hash is stored. Lost it? Revoke it (key revoke test-agent) and create a new one.
  ```

  Das Ablaufdatum liegt 90 Tage nach heute. Die dritte Zeile merkt sich den Schlüssel in `$key` für D2.7.
- `key list` zeigt den Schlüssel **ohne** Klartext:

  ```text
  NAME        ROLE    SCOPES                            STATUS  DRY RUN  CREATED     EXPIRES     LAST USED
  test-agent  reader  mail:read,mail:search,audit:read  active  default  …           …           never
  ```

- Die letzte Zeile (Suche nach `pk_key_` in der Liste) gibt **nichts** aus.

Probe, was dieser Schlüssel darf (ohne MCP):

```powershell
.\postkauz.exe policy show --key test-agent
.\postkauz.exe key list --key test-agent
```

Erwartet: `policy show` nennt `role: reader`, die drei Scopes und eine Liste lesender Operationen (alle `R0`). Der zweite Befehl endet mit `error: denied (missing_scope): …` (bis S2.6: `… deny (missing_scope)`) und Exit-Code 3 (`$LASTEXITCODE` zeigt `3`): Ein Leseschlüssel darf keine Schlüssel verwalten.

## D2.7 MCP: Claude sieht nur lesende Werkzeuge

Eine der beiden Varianten genügt. Beide geben PostKauz den Schlüssel aus D2.6 und die Abnahme-Datenbank mit; der Server heißt dort `postkauz-abnahme`.

### Variante A: Claude Code

Im selben PowerShell-Fenster (dort sind `$key` und `POSTKAUZ_DATA_DIR` gesetzt):

```powershell
claude mcp add --env "POSTKAUZ_KEY=$key" --env "POSTKAUZ_DATA_DIR=$env:POSTKAUZ_DATA_DIR" --scope user --transport stdio postkauz-abnahme -- C:\PostKauz\postkauz.exe mcp
claude mcp list
```

Erwartet: `Added stdio MCP server postkauz-abnahme …`, danach in `claude mcp list` eine Zeile `postkauz-abnahme: C:\PostKauz\postkauz.exe mcp - ✓ Connected`.

Gut zu wissen: `claude mcp add --env` speichert den Schlüssel **im Klartext** in der Datei `%USERPROFILE%\.claude.json` (Abschnitt `mcpServers`, Eintrag `postkauz-abnahme`). Er gilt nur für die Abnahme-Datenbank; die Schritte unter „Aufräumen" (`claude mcp remove …` und das Löschen der Abnahme-Datenbank) entfernen ihn wieder. Variante B legt ihn ebenso im Klartext in `claude_desktop_config.json` ab; dort entfernt ihn das Zurückkopieren der Sicherung.

Jetzt Claude Code starten (`claude`) und dort eingeben: `/mcp`, dann `postkauz-abnahme` auswählen und „View tools".

### Variante B: Claude-App

Die Konfiguration der App enthält bei dir schon andere Server; das folgende Skript **ergänzt** nur einen Eintrag und legt vorher eine Sicherung an (im selben PowerShell-Fenster):

```powershell
$cfg = "$env:APPDATA\Claude\claude_desktop_config.json"
Copy-Item $cfg "$cfg.vor-abnahme-d2"
$j = Get-Content $cfg -Raw -Encoding UTF8 | ConvertFrom-Json
if (-not $j.mcpServers) { $j | Add-Member -NotePropertyName mcpServers -NotePropertyValue ([pscustomobject]@{}) }
$srv = [pscustomobject]@{ command = "C:\PostKauz\postkauz.exe"; args = @("mcp"); env = [pscustomobject]@{ POSTKAUZ_KEY = $key; POSTKAUZ_DATA_DIR = $env:POSTKAUZ_DATA_DIR } }
$j.mcpServers | Add-Member -Force -NotePropertyName "postkauz-abnahme" -NotePropertyValue $srv
[IO.File]::WriteAllText($cfg, ($j | ConvertTo-Json -Depth 20), (New-Object Text.UTF8Encoding $false))
```

Dann die Claude-App **ganz** beenden (auch im Infobereich der Taskleiste: Rechtsklick auf das Symbol, „Beenden") und neu starten. In einem neuen Chat über das Werkzeug-Symbol (bzw. „+" → Konnektoren) den Server `postkauz-abnahme` aufklappen.

(Ohne Skript: Einstellungen → Entwickler → „Konfiguration bearbeiten" und unter `"mcpServers"` einen Eintrag `"postkauz-abnahme"` mit `"command": "C:\\PostKauz\\postkauz.exe"`, `"args": ["mcp"]` und `"env"` mit `POSTKAUZ_KEY` und `POSTKAUZ_DATA_DIR` ergänzen.)

### Erwartet (beide Varianten)

Der Server ist verbunden und bietet **genau diese 9 Werkzeuge**, alle nur lesend:

```text
mail_audit_log, mail_get_attachment_text, mail_get_message, mail_get_policy, mail_get_thread,
mail_list_accounts, mail_list_attachments, mail_list_mailboxes, mail_search
```

**Kein** Werkzeug zum Senden, Löschen, Verschieben oder Entwerfen (also nichts mit `send`, `delete`, `trash`, `move`, `draft`). Dazu gibt es Prompts (`inbox_triage`, `summarize_important`, `open_threads`, `extract_actions`) und Resources (`mail://…`); sie sind nur Vorlagen bzw. Lesequellen.

Zeigt Claude Code `✗ Failed to connect`: In PowerShell `$env:POSTKAUZ_KEY = $key; C:\PostKauz\postkauz.exe mcp` ausführen. Läuft der Server, wartet er still auf Eingaben (mit Strg+C beenden; dann liegt der Fehler in der Konfiguration). Meldet er `error: …`, diese Zeile an Claude geben. Danach `Remove-Item Env:\POSTKAUZ_KEY`.

## D2.8 Claude nutzt die Werkzeuge, das Audit zeigt es ohne Mailtext

Im Chat (Claude Code oder App) fragen:

> Suche Mails über Rechnungen und fasse sie zusammen.

Erwartet: Claude ruft `mail_search` auf (die App bzw. Claude Code fragt beim ersten Mal, ob das Werkzeug benutzt werden darf: erlauben) und meist danach `mail_get_message` oder `mail_get_thread`, und fasst einige Rechnungs-Mails zusammen. Dann zurück in PowerShell (Fenster aus Abschnitt 0):

```powershell
.\postkauz.exe audit --since 10m
.\postkauz.exe audit --since 10m --json | Select-String "Rechnung"
```

Erwartet:

- Eine Tabelle; für jeden Aufruf von Claude eine Zeile mit `ACTOR` **`agent`**, `KEY` **`key:test-agent`**, `OPERATION` z. B. `messages.search`, `messages.get` oder `threads.get`, `RESULT` `ok`. Beispiel:

  ```text
  ID  TIME                 ACTOR  KEY             OPERATION        RESULT  TARGETS     PARAMS
  12  2026-10-17 09:14:03  agent  key:test-agent  messages.search  ok      -           {"cursor":false,"explain":false,"limit":0,"mailbox":false,"operators":[],"terms":1}
  13  2026-10-17 09:14:09  agent  key:test-agent  messages.get     ok      message:196 {"fields":["id","max_chars","strip_quotes"]}
  ```

  In `PARAMS` stehen nur Feldnamen und Zähler (z. B. `"terms":1`), **nie** der Suchbegriff oder Mailtext.
- Die zweite Zeile (Suche nach „Rechnung" im Audit) gibt **nichts** aus.

Optional (zeigt den Schutz vor eingeschleusten Anweisungen): Claude bitten „Zeig mir die Mail pk.s16.02.0045f9d9@corpus.example.org und sag, ob etwas auffällig ist". Erwartet: Claude meldet, dass versteckter Text entfernt wurde (`hidden_text_removed`), und gibt keine Weiterleitung o. Ä. aus.

## D2.9 Audit-Export und Prüfung

```powershell
$exp = "$env:LOCALAPPDATA\postkauz-abnahme-d2-export"
.\postkauz.exe audit export --to $exp
.\postkauz.exe audit verify
$LASTEXITCODE
```

Erwartet:

- `audit export`: eine Tabelle `DAY  EVENTS  STATE  FILE` mit einer Zeile je Tag (16.10.2026 aus der Vorbereitung und, falls du an einem anderen Tag prüfst, heute), Zustand `written`, darunter `… in C:\Users\…\AppData\Local\postkauz-abnahme-d2-export; check with: audit verify`.
- `audit verify`: `ok: 1 exported day checked; …` bzw. `ok: 2 exported days checked; the export files match their checksums and the database`, dazu `not exported yet (new entries): …` (das ist der heutige Tag: `verify` schreibt selbst einen Audit-Eintrag; kein Fehler).
- `$LASTEXITCODE` zeigt `0`.

Jetzt **absichtlich** die Exportdatei vom 16.10.2026 verändern und erneut prüfen:

```powershell
Add-Content "$exp\postkauz-audit-2026-10-16.jsonl" '{"id":999999,"note":"absichtlich veraendert"}'
.\postkauz.exe audit verify
$LASTEXITCODE
```

Erwartet:

```text
mismatch: 1 problem in 2 exported days
  2026-10-16: export file was changed: C:\Users\…\AppData\Local\postkauz-abnahme-d2-export\postkauz-audit-2026-10-16.jsonl
not exported yet (new entries): …
error: audit export does not match
hint: the audit log or its export was changed; see audit verify
1
```

(Bei Prüfung am 16.10.2026 selbst steht `in 1 exported day`.)

**Mit dem Programm 0.1.0-rc1 (ab S1.11)** sehen D2.9 und `audit export` etwas anders aus, beides richtig: `audit export` nennt zusätzlich `manifest: …\postkauz-audit-manifest.json` und `chain head: …` (der Export hat jetzt eine Hash-Kette und ein Manifest); `audit verify` meldet `ok: … the export files match their checksums, the hash chain, the manifest and the database` und `chain head: …`; nach der Veränderung steht `mismatch: 2 problems …` mit den Zeilen `export file was changed (line N)` und `hash chain is broken (line N)` (N ist die angehängte Zeile). Bei `search --json` (D2.4) endet jede Zeile zusätzlich mit `"untrusted":true,"source":"email"`; sie beginnt weiter mit `{"id":`. Wichtig: **`mismatch`**, die Zeile mit `export file was changed` und der Exit-Code **`1`**. Ein weiterer `audit export` überschreibt die veränderte Datei nicht (er endet ebenfalls mit `mismatch`), damit der Beleg erhalten bleibt.

## Aufräumen

```powershell
claude mcp remove postkauz-abnahme --scope user          # nur Variante A
Copy-Item "$env:APPDATA\Claude\claude_desktop_config.json.vor-abnahme-d2" "$env:APPDATA\Claude\claude_desktop_config.json" -Force   # nur Variante B, dann App neu starten
Remove-Item -Recurse -Force $env:POSTKAUZ_DATA_DIR, "$env:LOCALAPPDATA\postkauz-abnahme-d2-export"
Remove-Item Env:\POSTKAUZ_DATA_DIR
```

Die Sicherung in Variante B nur zurückkopieren, wenn du die App-Konfiguration seit Abschnitt D2.7 nicht anders geändert hast; sonst den Eintrag `postkauz-abnahme` über „Konfiguration bearbeiten" löschen. Der Schlüssel `test-agent` verschwindet mit der Abnahme-Datenbank; er galt nur dort.

## Rückmeldung an Claude

Je Punkt „ok" oder „Abweichung":

| Punkt | Inhalt | Ergebnis |
|---|---|---|
| D2.1 | `search "Rechnung"`: fünf Zeilen, „Rechnungen" dabei, `5 of 453 results` | |
| D2.2 | „prüfen" und „pruefen": je `20 of 262 results`, `Compare-Object` ohne Ausgabe | |
| D2.3 | „Vollstreck" findet „Zwangsvollstreckung …", `20 of 68 results` | |
| D2.4 | drei JSON-Zeilen mit `"has_attachments":true` | |
| D2.5 | `warning: … hidden text removed`, versteckter Satz fehlt; `--json` mit `"untrusted":true` | |
| D2.6 | Schlüssel genau einmal sichtbar; `key list` ohne `pk_key_`; `key list --key test-agent` Exit 3 | |
| D2.7 | Claude (Code oder App) sieht 9 lesende Werkzeuge, keins zum Senden/Löschen/Entwerfen | |
| D2.8 | Claude nutzt `mail_search`/`mail_get_message`; Audit: `agent`, `key:test-agent`, kein Mailtext | |
| D2.9 | `audit verify` erst `ok` (Exit 0), nach Veränderung `mismatch` (Exit 1) | |

Bei einer Abweichung: das Kommando und die vollständige Ausgabe schicken. Danach entscheidest du: S1.9 angenommen, Nachbesserung (Liste) oder weiter mit S1.10.

## Anhang: Wie die Abnahme-Datenbank entsteht (für Claude und Entwickler)

Die Datei `dist/abnahme-d2-daten.zip` enthält ein Datenverzeichnis (`postkauz.db` und `blobs/`), erzeugt am 16.10.2026 auf Linux in der Entwicklungsumgebung:

```sh
make testmail-up                      # Dovecot auf 127.0.0.1:11143
sh scripts/abnahme-d2-db.sh           # Ergebnis: dist/abnahme-d2-daten.zip
```

Das Skript baut das Programm, lädt den Standardkorpus (1.000 synthetische Nachrichten, Samen 42) für einen frischen Nutzer in den Dovecot-Testserver, legt das Konto `demo` **ohne gespeichertes Passwort** an (`--password-from-env`), synct vollständig (Ergebnis: `1000 messages mirrored`, `0 errors`, `search index: 1000 messages indexed`; INBOX 230, Archiv/2024 379, Archiv/2025 371, Drafts 20) und packt Datenbank und Blobs (ohne Log und Sperrdateien) zu einer Zip-Datei. Größe 3.614.993 Bytes, SHA-256 `329f6d9e0c28cce98a387bf0f42dea81dc836995204939d9def362b43ac03ad4` (neu erzeugt am 16.10.2026 nach der Nachbesserung: Schema 6, Indexversion 2, Aufbereitung 3; die Ergebnisse von D2.1 bis D2.5 sind unverändert nachgeprüft). Vor dem Packen prüft das Skript per `doctor --json`, dass Index und Extraktion fertig sind. Ein erneuter Lauf erzeugt eine inhaltlich gleichwertige, aber nicht byte-gleiche Datei (Zeitstempel, Nutzername auf dem Testserver); dann den Hash in Abschnitt 0 anpassen.
