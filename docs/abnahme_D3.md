# Abnahme D3: Gesamtabnahme Schnitt 1 (0.1.0-rc1)

Stand: 16.10.2026. Gehört zu `auftraege/A01_phase0.md`, Teil D, Abschnitt D3. Geprüft wird der Release-Kandidat **0.1.0-rc1** (Schnitt 1 komplett).

**Was du prüfst:** Das fertige Programm läuft auf Windows als einzelne Datei ohne weitere Software (D3.3), richtet den täglichen Audit-Export als Windows-Aufgabe ein (D3.3, neu in S1.11) und importiert einen Thunderbird-Bestand ohne Doppelte (D3.4). Die Punkte, die Docker und Go brauchen (D3.1 zweiter/dritter Server, D3.2 `make check && make cross`), hat Claude im Container ausgeführt; die Protokolle liegen bei, du liest sie nur gegen. Am Ende entscheidest du (D3.5). Dauer: rund 30 Minuten. Du musst nichts programmieren: Kommando eingeben, Ausgabe mit „Erwartet" vergleichen, abhaken. Bei einer Abweichung: Kommando und vollständige Ausgabe an Claude geben, nichts reparieren.

**Voraussetzung:** Abnahme D2 ist durch (oder du machst sie vorher mit dem neuen Programm; die Ergebnisse von D2.1 bis D2.9 sind mit 0.1.0-rc1 gleich, siehe Hinweis in [abnahme_D2.md](abnahme_D2.md), Abschnitt D2.9).

Du brauchst (alles von Claude bereitgestellt, keine echten Daten):

| Datei | Herkunft (Repository) | Ablage bei dir |
|---|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` (0.1.0-rc1), umbenannt | `C:\PostKauz\postkauz.exe` (ersetzt die Datei aus D2) |
| `SHA256SUMS` | `dist/SHA256SUMS` | `C:\PostKauz\SHA256SUMS` |
| `abnahme-d2-daten.zip` | `dist/abnahme-d2-daten.zip` (dieselbe Datei wie in D2) | `C:\PostKauz\abnahme-d2-daten.zip` |
| `abnahme-d3-import.zip` | `dist/abnahme-d3-import.zip` (Thunderbird-Testprofil) | `C:\PostKauz\abnahme-d3-import.zip` |
| `Register-AuditExport.ps1` | `dist/Register-AuditExport.ps1` (= `deploy/windows/`, Zeilenenden CRLF) | `C:\PostKauz\Register-AuditExport.ps1` |

Alle Kommandos sind für **PowerShell** und laufen in **einem** Fenster nacheinander (Ausnahme: D3.3c, dort steht es).

## 0. Vorbereitung

```powershell
cd C:\PostKauz
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Get-FileHash .\postkauz.exe, .\abnahme-d2-daten.zip, .\abnahme-d3-import.zip, .\Register-AuditExport.ps1 -Algorithm SHA256 | Format-Table Hash, Path -AutoSize
Unblock-File .\postkauz.exe, .\Register-AuditExport.ps1
```

Erwartet (die Hashes sind die Prüfsummen der bereitgestellten Dateien; Groß-/Kleinschreibung egal):

| Datei | SHA-256 |
|---|---|
| `postkauz.exe` | `5430D3C973995F7F1A7653E037BCA96896857D0FF1C97DE01EB0737BB8A55541` (steht auch in `SHA256SUMS` bei `postkauz-windows-amd64.exe`) |
| `abnahme-d2-daten.zip` | `329F6D9E0C28CCE98A387BF0F42DEA81DC836995204939D9DEF362B43AC03AD4` |
| `abnahme-d3-import.zip` | `390E26342EEACE3A4CF6294AFE3464AF0726D7F2AEE2E98097826D63F6C71212` |
| `Register-AuditExport.ps1` | `85DB55BD4F65783B944F77B229C69D149AAB496B9B08333851B71E5C1D5415B1` |

Weicht ein Hash ab, ist die Datei nicht so angekommen, wie Claude sie gebaut hat: nicht weitermachen, Claude Bescheid geben. `Unblock-File` entfernt die Markierung „aus dem Internet geladen", damit SmartScreen und PowerShell die Dateien ohne Rückfrage starten.

## D3.1 Zweiter und dritter Server (Claude, im Container)

A01 verlangt die Punkte D0 bis D2 zusätzlich gegen Stalwart 0.11.8 (nur IMAPS, Port 12993) und GreenMail (Port 13143). Das braucht Docker; Claude hat es mit derselben Programmversion ausgeführt (`sh scripts/abnahme-d3-server.sh stalwart|greenmail` nach frischem Start der Testserver). Protokolle:

- [protokolle/D3.1_stalwart.txt](protokolle/D3.1_stalwart.txt)
- [protokolle/D3.1_greenmail.txt](protokolle/D3.1_greenmail.txt)

Die Protokolle D3.1, D3.2 und D3.4 entstanden aus Commit `efef34e`; der Release-Commit `64e516c` ändert danach nur `make release` (Windows-Skript in `dist/`), nicht das Programm. Du musst nichts ausführen. Zum Gegenlesen die wichtigsten Zeilen (in beiden Protokollen gleich, wo nicht anders genannt):

| Punkt | Stalwart (IMAPS 12993) | GreenMail (13143) |
|---|---|---|
| D0.2–D0.6 | `version`, `No accounts configured yet.`, `{"accounts":[]}`, `doctor`, `bogus` → Exit 2 | gleich |
| D1.2 `account test` | `login: ok`, Strategie `condstore`, Ordner mit Rollen (`Sent Items` → `sent`, `Deleted Items` → `trash` …) | `login: ok`, Strategie `uid-scan` (GreenMail kann kein CONDSTORE), nur `INBOX` vor dem Laden |
| D1.3/D1.4 Sync mit Abbruch | Abbruch nach 2 s: `error: interrupted`, Exit 1; danach `1000 messages mirrored`, `0 errors`; erneut: dieselbe Zahl | gleich |
| D1.5 `mailbox list --json` | INBOX 230, Archiv/2024 379, Archiv/2025 371, Drafts 20 (`total` = `mirrored`) | gleich (GreenMail trennt mit `.`: `Archiv.2024`) |
| D1.6 Flag von außen | `flags: seen` nach `sync` | gleich |
| D2.1–D2.4 Suche | `5 of 453 results`; „prüfen" = „pruefen" (262); „Vollstreck" → „Zwangsvollstreckung …" (68); drei JSON-Zeilen mit Anhang | gleich |
| D2.5 versteckter Text | `warning: hidden text removed …` | gleich |
| D2.6 Schlüssel | einmal sichtbar, `key list` ohne Schlüssel | gleich |
| D2.7 MCP | 9 lesende Tools; `mail_search` in der Hülle (`untrusted=True`) | gleich |
| D2.8 Audit | Aufruf mit `agent`, `key:mcp-agent`, ohne Suchtext | gleich |
| D2.9 Export und Prüfung | `ok` (Exit 0); nach veränderter Zeile 2: `mismatch`, `export file was changed (line 2)`, `hash chain is broken (line 2)`, Exit 1 | gleich |
| `doctor --secrets` | `result: ok` | gleich |

Ein dritter, **echter** Anbieter (z. B. dein Gmail-Konto) gehört laut Roadmap nur in deine manuelle Prüfung (P1/P9), nie in die Abnahme durch Claude.

## D3.2 `make check && make cross` (Claude, im Container)

Protokoll: [protokolle/D3.2_make_check_cross.txt](protokolle/D3.2_make_check_cross.txt) (alle Pakete `ok`, `guard: ok`, `pii-check: ok`, drei Programme). Danach hat Claude den Release-Kandidaten gebaut (`make release VERSION=0.1.0-rc1`: ohne Symboltabelle, `-trimpath`, Prüfsummen):

| Datei in `dist/` | Größe | SHA-256 |
|---|---|---|
| `postkauz-windows-amd64.exe` | 17.424.896 Bytes (16,6 MiB) | `5430d3c973995f7f1a7653e037bca96896857d0ff1c97de01eb0737bb8a55541` |
| `postkauz-linux-amd64` | 17.506.464 Bytes (16,7 MiB) | `f41c5d654f96a66020554dbc1e23e749543a91a41af153e0da5ed57b995a86a7` |
| `postkauz-darwin-arm64` | 16.399.794 Bytes (15,6 MiB) | `7076166322bc0b2edaeb8e140990731c0b0526dc1f3aa608f40c2b25cab1f17a` |
| `Register-AuditExport.ps1` | 9.414 Bytes | `85db55bd4f65783b944f77b229c69d149aab496b9b08333851b71e5c1d5415b1` |
| `VERSION` | 147 Bytes | `ba52eb63ba1f3b3264c888820c3b3ea6c2961f03ae5e330d40960057a93b1548` |
| `SHA256SUMS` | 433 Bytes | `fd7ab43ff6915fdc52681c9aa1d9b6e00a84a277abd6e121dd3685097a186ff9` |
| `abnahme-d3-import.zip` | 473.946 Bytes | `390e26342eeace3a4cf6294afe3464af0726d7f2aee2e98097826d63f6c71212` |
| `abnahme-d2-daten.zip` | 3.614.993 Bytes (3,4 MiB) | `329f6d9e0c28cce98a387bf0f42dea81dc836995204939d9def362b43ac03ad4` |

`dist/VERSION`:

```text
postkauz 0.1.0-rc1
commit 64e516c5e75cbf6b8f2cb37b6c56bcc5cb42e17f
go1.27.2, CGO_ENABLED=0, -trimpath, -ldflags "-s -w"
built 2026-10-16T13:35:17Z
```

## D3.3 Installation auf Windows (du)

### D3.3a Programm ohne Installation

```powershell
.\postkauz.exe version
.\postkauz.exe doctor
```

Erwartet:

- `postkauz 0.1.0-rc1 (go1.27.2 windows/amd64)`
- `doctor` läuft ohne Fehlermeldung und ohne dass etwas installiert wurde. Es zeigt dein normales Datenverzeichnis (`C:\Users\…\AppData\Local\postkauz`, aus D1) und unter anderem:

  ```text
  database:  ok (C:\Users\…\AppData\Local\postkauz\postkauz.db)
  schema:    7 (program knows 7)
  keyring:   available (Windows Credential Manager) [secrets.backend = auto]
  pdftotext: not found (optional; without it PDF attachments get no text, see README)
  audit:     never exported; 2 days not exported yet [audit.export_dir = not set]
  ```

  Steht bei `database` `needs_migration` und bei `schema` eine kleinere Zahl vor `(program knows 7)`, ist das richtig: Deine Datenbank aus D1 ist älter; das erste Kommando, das sie öffnet (z. B. `.\postkauz.exe accounts list`), hebt sie an (mit Sicherungskopie daneben). Die Zahl der Tage bei `audit` hängt davon ab, an wie vielen Tagen du PostKauz benutzt hast; eine Warnung `audit: days older than two days are not exported …` ist an dieser Stelle **richtig** (der tägliche Export kommt in D3.3c).

### D3.3b Geheimnisse und Dateirechte

```powershell
.\postkauz.exe doctor --secrets
$LASTEXITCODE
```

Erwartet: am Ende ein Abschnitt `secrets:` mit Zeilen `skipped  permissions …: Windows: the access rights of the user profile apply`, `ok  secrets in log …`, `ok  secrets in database table …` und als letzte Zeile `result: ok (no secret found where it must not be, permissions fine)`; `$LASTEXITCODE` ist `0`. Eine Zeile `warn  search for password of account demo: shorter than 8 characters, too short to search for` ist richtig (das Testpasswort `pass` aus D1 ist zu kurz zum Suchen). Kein Passwort steht im Klartext in der Ausgabe.

### D3.3c Täglicher Audit-Export als Windows-Aufgabe

Der Audit-Export ist der eigentliche Schutz des Audit-Logs: einmal täglich an einen zweiten Ort. Als Ziel schlägt Claude einen Ordner in deinem Google Drive vor (Standard von Google Drive für Desktop: Laufwerk `G:\Meine Ablage`); jeder andere Ordner außerhalb von `AppData\Local\postkauz` geht auch.

**Neues PowerShell-Fenster** öffnen (ohne `POSTKAUZ_DATA_DIR`, die Aufgabe arbeitet mit deinem normalen Datenverzeichnis), dann:

```powershell
cd C:\PostKauz
powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -ExePath C:\PostKauz\postkauz.exe -ExportDir "G:\Meine Ablage\PostKauz-Audit"
```

Erwartet (rund 10 Sekunden; ein kurzes Konsolenfenster erscheint **nicht**):

```text
== Program: C:\PostKauz\postkauz.exe
== Export folder: G:\Meine Ablage\PostKauz-Audit
== Task 'PostKauz audit export' registered: daily at 12:30 for <RECHNER>\<du> (missed runs start as soon as possible)
== Starting the task once to check it ...
== OK: export written to G:\Meine Ablage\PostKauz-Audit (task result 0)
== Check it any time with: C:\PostKauz\postkauz.exe audit verify
```

Dann:

```powershell
Get-ScheduledTask "PostKauz audit export" | Format-List TaskName, State
Get-ChildItem "G:\Meine Ablage\PostKauz-Audit"
C:\PostKauz\postkauz.exe audit verify
$LASTEXITCODE
```

Erwartet: `State : Ready`; im Ordner je Tag eine Datei `postkauz-audit-JJJJ-MM-TT.jsonl` und `postkauz-audit-manifest.json`; `audit verify` beginnt mit `ok: N exported days checked; the export files match their checksums, the hash chain, the manifest and the database`, dazu `chain head: …` und meist `not exported yet (new entries): <heute>` (das ist der Eintrag von `verify` selbst, kein Fehler); `$LASTEXITCODE` ist `0`. In der „Aufgabenplanung" von Windows steht die Aufgabe unter „Aufgabenplanungsbibliothek".

Endet das Skript stattdessen mit `WARNING: The task ran … but no new export was found.`: die dort genannte Zeile `C:\PostKauz\postkauz.exe audit export --to …` von Hand ausführen und die Ausgabe an Claude geben; klappt sie, das Skript mit `-ShowWindow` wiederholen (dann blitzt beim täglichen Lauf kurz ein Fenster auf).

Die Aufgabe darf bleiben (gewollt für den Alltag). Entfernen: `powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -Unregister`.

## D3.4 Importprobe (du)

Ein synthetisches Thunderbird-Profil (derselbe Standardkorpus wie in D2, als Thunderbird-Ordner mit `.msf`-Dateien, Unterordnern, einem Ordner „Entwürfe" in Thunderbirds Schreibweise, Kopien in „Wichtig" und drei in Thunderbird gelöscht markierten Mails) wird in eine **frische Kopie** der D2-Datenbank importiert. Dort liegen die 1.000 Nachrichten schon (vom Testserver gespiegelt). Erwartet: Nichts wird doppelt angelegt; nur fünf Sonderfälle, die es auf dem Server nicht gab, kommen neu dazu.

Im **ersten** PowerShell-Fenster (aus Abschnitt 0):

```powershell
$env:POSTKAUZ_DATA_DIR = "$env:LOCALAPPDATA\postkauz-abnahme-d3"
Expand-Archive .\abnahme-d2-daten.zip -DestinationPath $env:POSTKAUZ_DATA_DIR
Expand-Archive .\abnahme-d3-import.zip -DestinationPath .\abnahme-d3-import
.\postkauz.exe import mbox .\abnahme-d3-import\Thunderbird -a demo --mailbox Archive
```

Erwartet (Zeitangaben weichen ab):

```text
import mbox into account demo (0.7s, 2.3 MB, 3.5 MB/s)
SOURCE           FOLDER               MODE     NEW  EXISTING  SKIPPED  ERRORS
Archiv.sbd/2024  Archive/Archiv/2024  content  0    379       0        0
Archiv.sbd/2025  Archive/Archiv/2025  content  0    371       0        0
Drafts           Archive/Drafts       content  0    20        0        0
Entw&APw-rfe     Archive/Entwürfe     content  5    0         0        0
INBOX            Archive/INBOX        content  0    230       3        0
Wichtig          Archive/Wichtig      content  0    100       0        0
7 files ignored (not mbox, e.g. Thunderbird .msf index files)
1100 folder assignments added to existing messages
search index: 5 messages indexed
new: 5, existing: 1100, skipped: 3, errors: 0
```

Wichtig ist die letzte Zeile: **`new: 5, existing: 1100, skipped: 3, errors: 0`**. Erklärung: 1.000 Nachrichten gab es schon (gespiegelt vom Server), dazu 100 Kopien im Ordner „Wichtig" (wie Gmail-Labels): zusammen `existing: 1100`, nichts doppelt. Neu sind nur die fünf Sonderfälle im Ordner „Entwürfe" (Nachricht nur aus Kopf, sehr lange Zeile, ohne Zeilenende am Schluss, mit Schlagwörtern, ein Entwurf). **`skipped: 3`** sind die drei Mails, die in Thunderbird als gelöscht markiert sind; PostKauz übernimmt sie bewusst nicht. (A01 nennt hier `skipped: 0` für einen Bestand ohne gelöscht markierte Mails; dieses Testprofil enthält sie absichtlich.) Am Ende `Archive/Entwürfe`: Thunderbird speichert den Ordnernamen als `Entw&APw-rfe`, PostKauz zeigt ihn richtig.

Zweiter Lauf:

```powershell
.\postkauz.exe import mbox .\abnahme-d3-import\Thunderbird -a demo --mailbox Archive
```

Erwartet: sechs Zeilen mit `unchanged`, `6 sources unchanged since the last import, not read again (--rescan reads them)` und als letzte Zeile **`new: 0, existing: 0, skipped: 0, errors: 0`**.

Gegenprobe (alles noch einmal lesen, trotzdem nichts doppelt):

```powershell
.\postkauz.exe import mbox .\abnahme-d3-import\Thunderbird -a demo --mailbox Archive --rescan
.\postkauz.exe search "Rechnung" -a demo --limit 1
.\postkauz.exe search "in:Archive/Entwürfe" -a demo
```

Erwartet: `new: 0, existing: 1105, skipped: 3, errors: 0`; bei „Rechnung" weiter **`1 of 453 results`** (dieselbe Zahl wie in D2.1, also keine Dubletten im Suchindex); `in:Archive/Entwürfe` listet genau fünf Nachrichten (`5 of 5 results (newest first)`) mit den Betreffen „Ohne Zeilenende", „Sehr lange Zeile", „Mit Schlagwörtern", „Nur ein Kopf", „Entwurf: Aushang Treppenhaus".

Claude hat denselben Ablauf mit der Linux-Fassung gegen dieselbe D2-Datei geprüft (`sh scripts/abnahme-d3-import.sh`, Protokoll [protokolle/D3.4_import.txt](protokolle/D3.4_import.txt)).

## D3.5 Entscheidung (du)

Bitte eine der drei Antworten an Claude:

1. **S1 angenommen.** Dann setzt du (oder Claude auf deinen Auftrag) den Git-Tag `v0.1.0-rc1` auf den Stand von `main` nach dieser Sitzung (die Programme sind aus Commit `64e516c` gebaut, siehe `dist/VERSION`; danach kamen nur noch Dokumentation und Protokolle) und lädst die Dateien aus `dist/` als GitHub-Release hoch (drei Programme, `Register-AuditExport.ps1`, `SHA256SUMS`, `VERSION`; die Abnahme-Zips nicht). Danach beginnt Schnitt 2 (Entwürfe, draft-only).
2. **Nachbesserung**, mit Liste der Abweichungen (Punkt, Kommando, Ausgabe).
3. **S2 starten** und die offenen Punkte parallel nachbessern.

Die offenen Fragen dazu stehen nummeriert in [roadmap.md](roadmap.md), Abschnitt „Offene Fragen"; Claude schlägt zu jeder eine Standardantwort vor.

## Aufräumen

```powershell
Remove-Item -Recurse -Force $env:POSTKAUZ_DATA_DIR, .\abnahme-d3-import
Remove-Item Env:\POSTKAUZ_DATA_DIR
```

Die Windows-Aufgabe aus D3.3c und der Exportordner bleiben (gewollt). `postkauz.exe` in `C:\PostKauz` ist ab jetzt die Version für den Alltag.

## Rückmeldung an Claude

Je Punkt „ok" oder „Abweichung":

| Punkt | Inhalt | Ergebnis |
|---|---|---|
| 0 | Hashes wie in der Tabelle | |
| D3.1 | Protokolle Stalwart/GreenMail gelesen (optional) | |
| D3.2 | Protokoll `make check && make cross` gelesen (optional) | |
| D3.3a | `version` zeigt `0.1.0-rc1 … windows/amd64`; `doctor` ohne Fehler, keine Installation nötig | |
| D3.3b | `doctor --secrets`: `result: ok`, Exit 0 | |
| D3.3c | Aufgabe angelegt, `OK: export written …`, `audit verify` `ok` | |
| D3.4 | erster Lauf `new: 5, existing: 1100, skipped: 3, errors: 0`; zweiter Lauf `new: 0`; Gegenprobe ohne Dubletten | |
| D3.5 | Entscheidung 1, 2 oder 3 | |
