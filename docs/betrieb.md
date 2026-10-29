# Betrieb von PostKauz

Für: den Betreiber. Diese Anleitung erklärt, wie PostKauz im Alltag läuft: Schritt für Schritt, je Schritt ein Kommando und die Ausgabe, die du erwarten kannst. Programmierkenntnisse brauchst du nicht. Stand: T2 (0.3.0-alpha.3, 28.10.2026); Abschnitt 2 seit T3.

Die Beispiele nehmen an:

- Das Programm liegt in `C:\PostKauz\postkauz.exe`.
- Die Daten liegen in `C:\PostKauz\data` (nicht unter `%LOCALAPPDATA%`; warum, steht in Schritt 1.2).
- Die Konfigurationsdatei ist `C:\PostKauz\config.toml` (wenn du keine hast, lass `-ConfigFile` und `--config` einfach weg).
- Die Konten heißen `privat` und `verein`. Setze deine eigenen Namen ein.

Alle Kommandos tippst du in ein **PowerShell-Fenster** (Startmenü, „Windows PowerShell“). Administratorrechte brauchst du nie.

## 1. Windows-Dauerbetrieb

### 1.1 Was läuft wo

PostKauz spiegelt deine Postfächer nur, solange ein Sync läuft. Im Dauerbetrieb laufen drei Dinge nebeneinander:

| Was | Wie gestartet | Kennt das Mailpasswort? |
|---|---|---|
| **Dauer-Sync** `postkauz.exe sync --watch --all` (alle Konten in einem Programm) | Aufgabe „PostKauz sync watch“ in der Aufgabenplanung, startet bei deiner Anmeldung über `Run-SyncWatch.ps1` | ja, liest es aus der Windows-Anmeldeinformationsverwaltung |
| **Audit-Export** `postkauz.exe audit export` (täglich) | Aufgabe „PostKauz audit export“ (`Register-AuditExport.ps1`, README „Daily audit export“) | nein |
| **MCP-Server** `postkauz.exe mcp` für Claude | startet die Claude-App selbst | **nein** (nur sein API-Schlüssel) |

Nur der Dauer-Sync spricht mit dem Mailserver. Er spiegelt neue Mails, legt Entwürfe im Entwürfe-Ordner ab und führt angeforderte Syncs aus (Abschnitt 1.9). Das Passwort steht nirgends in einem Skript oder in der Aufgabe.

`Run-SyncWatch.ps1` passt auf den Dauer-Sync auf:

- Endet `postkauz.exe` (Absturz, Netz weg beim Start, Update), startet es ihn neu: erst nach 60 Sekunden, dann nach 2, 4, 8 … Minuten, höchstens alle 30 Minuten. Lief er mindestens 30 Minuten, beginnt die Wartezeit wieder bei 60 Sekunden.
- Lehnt der Mailserver die Anmeldung ab oder fehlt ein Passwort, versucht `postkauz.exe` (mit `--all`, wie die Aufgabe es startet) **dieses Konto** alle 30 Minuten erneut und schreibt das ins Log; das Programm selbst läuft weiter (seit der Nachbesserung T2). So sperrt der Anbieter dein Konto nicht wegen vieler Fehlversuche. `doctor` zeigt das Konto solange mit `status error` und Grund.
- Endet `postkauz.exe` doch mit Exit-Code 6 (Anmeldung abgelehnt; das kommt nur noch aus Läufen mit einem einzelnen Konto), wartet `Run-SyncWatch.ps1` immer 30 Minuten.
- Ein Konto mit Problem (Anmeldung, fehlendes Passwort, anderer Fehler) hält die anderen Konten nicht an.

### 1.2 Voraussetzungen prüfen

**Schritt 1: Programmversion.**

```powershell
C:\PostKauz\postkauz.exe version
```

Erwartet: `postkauz 0.3.0-alpha.3 (go1.27.3 windows/amd64)` oder neuer. Ältere Versionen kennen `sync --watch --all` nicht.

**Schritt 2: Datenverzeichnis und Konten.**

```powershell
C:\PostKauz\postkauz.exe doctor --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
```

Erwartet (Auszug):

```
data dir:  C:\PostKauz\data
database:  ok (C:\PostKauz\data\postkauz.db)
accounts:
  privat: password stored, status ok (env override: POSTKAUZ_PASSWORD_PRIVAT)
  verein: password stored, status ok (env override: POSTKAUZ_PASSWORD_VEREIN)
```

- Steht bei einem Konto `password missing`, speichere es: `C:\PostKauz\postkauz.exe account set privat --password-stdin --data-dir C:\PostKauz\data` (verdeckte Eingabe).
- Steht unter dem Datenverzeichnis eine Zeile `really: …`, leitet Windows den Ordner um (passiert, wenn PostKauz aus einer App wie Claude gestartet wurde und die Daten unter `%LOCALAPPDATA%` liegen). Die Aufgabenplanung sähe dann einen anderen Ordner. Nimm ein Datenverzeichnis wie `C:\PostKauz\data` und kopiere den Ordner hinter `really:` dorthin.

**Schritt 3: Ein einmaliger Sync geht.**

```powershell
C:\PostKauz\postkauz.exe sync -a privat --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
```

Erwartet: eine Tabelle der Ordner, am Ende `… messages mirrored` und `0 errors`. Geht das nicht, hilft die Aufgabe auch nicht; erst den Fehler beheben (README, „Troubleshooting“).

### 1.3 Aufgabe registrieren

Lege `Register-SyncWatch.ps1` und `Run-SyncWatch.ps1` (beide im Release-Paket) **nebeneinander** nach `C:\PostKauz`. Dann:

```powershell
cd C:\PostKauz
powershell -ExecutionPolicy Bypass -File .\Register-SyncWatch.ps1 -DataDir C:\PostKauz\data -ConfigFile C:\PostKauz\config.toml
```

Erwartet (Auszug):

```
== Program: C:\PostKauz\postkauz.exe
== Restart script: C:\PostKauz\Run-SyncWatch.ps1
== Data directory: C:\PostKauz\data
== Configuration: C:\PostKauz\config.toml
== Account privat: password in the Windows Credential Manager
== Account verein: password in the Windows Credential Manager
== Task 'PostKauz sync watch' registered: starts at logon of PC\du
== Log: C:\PostKauz\data\logs\sync-watch.log
== Starting the task to check it ...
== Watching the log for 15 seconds ...
== OK: the task is running, postkauz.exe runs and writes C:\PostKauz\data\logs\sync-watch.log
```

- Weigert sich Windows, das Skript auszuführen („nicht digital signiert“), einmal `Unblock-File .\Register-SyncWatch.ps1` und `Unblock-File .\Run-SyncWatch.ps1`, dann noch einmal.
- Eine Warnung `Account …: password missing` heißt: Dieses Konto wird erst beobachtet, wenn du das Passwort gespeichert hast (Schritt 2). Die anderen laufen schon.
- Endet es mit `postkauz.exe started but ended within 15 seconds` oder `no postkauz.exe from this path is running` (Exit-Code 1, seit der Nachbesserung T2), stehen darunter die letzten Zeilen des Logs mit dem Grund (z. B. falsches Datenverzeichnis, Datenbank braucht ein Update). Die Aufgabe bleibt registriert und versucht es mit wachsender Pause weiter; behebe den Grund und registriere noch einmal.
- Endet es mit `The task did not start as expected`, steht darunter ein Kommando, mit dem du das Hilfsskript von Hand startest und die Fehlermeldung siehst. Klappt es von Hand, registriere noch einmal mit `-ShowWindow` (dann läuft die Aufgabe mit einem minimierten Fenster statt unsichtbar).

Die Aufgabe startet ab jetzt bei jeder Anmeldung an Windows von selbst. Sie läuft nur, solange du angemeldet bist.

### 1.4 Prüfen, ob sie läuft

```powershell
(Get-ScheduledTask -TaskName "PostKauz sync watch").State
```

Erwartet: `Running`. (`Ready` heißt: registriert, läuft aber gerade nicht; `Disabled`: abgeschaltet.)

Welche PostKauz-Programme gerade laufen:

```powershell
Get-CimInstance Win32_Process -Filter "Name='postkauz.exe'" | Select-Object ProcessId, CommandLine
```

Erwartet: eine Zeile mit `sync --watch --all --data-dir C:\PostKauz\data …`. Eine Zeile mit `mcp` ist der MCP-Server der Claude-App (gehört nicht zur Aufgabe).

### 1.5 Log lesen

```powershell
Get-Content -LiteralPath C:\PostKauz\data\logs\sync-watch.log -Tail 20
```

Erwartet etwa:

```
2026-10-28 09:00:01  Run-SyncWatch started (process 4711); log C:\PostKauz\data\logs\sync-watch.log
2026-10-28 09:00:01  starting: C:\PostKauz\postkauz.exe sync --watch --all --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
2026-10-28 09:00:09    2026-10-28T08:00:09Z  [privat] 3 new, 0 updated, 0 removed, 0 errors; 52311 messages mirrored
2026-10-28 09:00:12    2026-10-28T08:00:12Z  [verein] 0 new, 1 updated, 0 removed, 0 errors; 1840 messages mirrored
```

- Vorne steht die Ortszeit des Skripts, in der Zeile die Zeit von PostKauz in UTC (`Z`). In eckigen Klammern steht das Konto.
- Alle 5 Minuten kommt je Konto eine Zeile; neue Mails in der INBOX lösen sofort eine Zeile aus.
- Mitlesen, während es läuft: dasselbe Kommando mit `-Wait` am Ende (beenden mit Strg+C; die Aufgabe läuft weiter).
- Das Log wird bei 10 MB zu `sync-watch.1.log`; es bleiben fünf alte Dateien (`.1` bis `.5`). Es enthält nie ein Passwort.
- Mehr Einzelheiten (je Ordner, Warnungen) stehen im Programm-Log `C:\PostKauz\data\postkauz.log` (Pfad zeigt `doctor`, Zeile `log:`).

Wichtige Zeilen und was sie bedeuten:

| Zeile im Log | Bedeutung | Was tun |
|---|---|---|
| `[verein] another sync is running: account verein; trying again in 1m0s` | Für dieses Konto läuft gerade ein anderer Sync (z. B. von Hand gestartet). | Nichts; es versucht es von selbst wieder. |
| `[verein] provider: authentication failed …; retrying in 30m (login errors are not retried sooner, …; the other accounts go on)` | Der Server hat die Anmeldung dieses Kontos abgelehnt (auch bei einer kurzen Störung beim Anbieter, `NO [UNAVAILABLE]`). Die anderen Konten laufen weiter; dieses versucht es alle 30 Minuten erneut (seit der Nachbesserung T2; vorher endete es bis zum nächsten Neustart). `doctor` zeigt solange `status error` und `last error: sync --watch --all: login rejected by the server at …; retrying every 30m` samt Warnung. | Passwort prüfen und neu speichern (`account set verein --password-stdin`). Spätestens nach 30 Minuten läuft es wieder; sofort mit Neustart der Aufgabe (1.6). Der Vermerk verschwindet nach der ersten erfolgreichen Runde, auch nach einem Neustart der Aufgabe. |
| `[verein] stopped: …; the other accounts go on` | Dieses Konto endete an einem anderen Fehler (z. B. unvollständige Antwort des Servers). Die anderen laufen weiter; dieses erst nach einem Neustart der Aufgabe wieder. `doctor` zeigt `last error: sync --watch --all: stopped (…)` mit Warnung. | Grund im Log nachsehen (`postkauz.log`), dann Aufgabe neu starten (1.6). |
| `[verein] no password stored for this account; retrying in 30m (the other accounts go on)` | Für dieses Konto ist kein Passwort gespeichert (bzw. kein OAuth2-Token). `doctor` zeigt es wie oben. | Passwort speichern (Schritt 2) bzw. `account login verein`; es läuft beim nächsten Versuch. |
| `postkauz.exe ended with exit code 6 … Not retrying quickly` | Das Programm endete mit einem Anmeldefehler (seit der Nachbesserung T2 unter `--all` nur noch, wenn es nicht mehrere Konten beobachtet, z. B. von Hand gestartet). Das Skript wartet 30 Minuten. | Wie oben; danach Aufgabe neu starten. |
| `postkauz.exe ended with exit code 5 …; restarting in 60 s` | Kein Netz oder Server nicht erreichbar beim Start. | Nichts; es startet von selbst neu. |
| `[privat] requested sync (job 12): running` | Ein Agent hat über Claude einen Sync angefordert (1.9). | Nichts. |
| `[privat] requested sync (job 12): refused (key_revoked)` | Der Schlüssel, der den Sync anforderte, ist widerrufen. Es lief nichts. | Nichts, oder den Schlüssel prüfen (`key list`). |

### 1.6 Stoppen, wieder starten, entfernen

Stoppen (z. B. vor einem Update oder einer Sicherung):

```powershell
Stop-ScheduledTask -TaskName "PostKauz sync watch"
(Get-ScheduledTask -TaskName "PostKauz sync watch").State
```

Erwartet: `Ready`. Prüfe danach mit dem Kommando aus 1.4, dass keine Zeile mit `sync --watch` mehr da ist. Steht doch noch eine da, beende sie: `Stop-Process -Id <ProcessId>` (die Nummer aus der Liste).

Wieder starten:

```powershell
Start-ScheduledTask -TaskName "PostKauz sync watch"
```

Erwartet: kein Text; danach zeigt 1.4 wieder `Running`.

Abschalten, bis du sie wieder einschaltest (startet dann auch nicht bei der Anmeldung):

```powershell
Disable-ScheduledTask -TaskName "PostKauz sync watch"
Enable-ScheduledTask -TaskName "PostKauz sync watch"
```

Ganz entfernen (Daten und Logs bleiben):

```powershell
powershell -ExecutionPolicy Bypass -File C:\PostKauz\Register-SyncWatch.ps1 -Unregister
```

Erwartet: `== Task 'PostKauz sync watch' stopped and removed. Data and logs stay where they are.`

Gut zu wissen: Das Stoppen beendet `postkauz.exe` hart. Im Audit bleibt dann ein Eintrag „gestartet“ ohne Abschluss. Der nächste Lauf schließt ihn ab, sobald er zehn Minuten alt ist (`error`/`process_ended`). Bis dahin kann `doctor` warnen: `audit: 1 call started more than 10 minutes ago without an end entry`. Das verschwindet von selbst.

### 1.7 Update der Programmdatei

Zwei Versionen dürfen nie gleichzeitig auf dieselbe Datenbank zugreifen (README, „Updating“).

1. Dauer-Sync anhalten und abschalten:
   `Stop-ScheduledTask -TaskName "PostKauz sync watch"` und `Disable-ScheduledTask -TaskName "PostKauz sync watch"`.
   Erwartet: 1.4 zeigt `Disabled` und keine Zeile `sync --watch`.
2. Audit-Export abschalten: `Disable-ScheduledTask -TaskName "PostKauz audit export"`.
3. Die Claude-App ganz beenden (auch im Infobereich unten rechts). Danach zeigt 1.4 auch keine Zeile `mcp` mehr.
4. `C:\PostKauz\postkauz.exe` durch die neue Datei ersetzen. Die neuen `Register-SyncWatch.ps1` und `Run-SyncWatch.ps1` aus dem Paket ebenfalls nach `C:\PostKauz` kopieren.
5. `C:\PostKauz\postkauz.exe doctor --data-dir C:\PostKauz\data` — erwartet `database: needs_migration` (oder `ok`, wenn sich das Schema nicht ändert), dann `C:\PostKauz\postkauz.exe accounts list --data-dir C:\PostKauz\data` (führt die Migration aus; erwartet: deine Konten).
6. Aufgabe neu registrieren (übernimmt neue Einstellungen der Skripte) und starten: das Kommando aus 1.3. Erwartet: `== OK: the task is running, postkauz.exe runs …`.
7. Audit-Export wieder einschalten: `Enable-ScheduledTask -TaskName "PostKauz audit export"`; Claude-App wieder starten.

### 1.7a Nach dem Update auf T2 (0.3.0-alpha.5 und später)

Was sich für vorhandene API-Schlüssel ändert (Nachbesserung T2, V20):

1. **`assistant`-Schlüssel ändern am Server weiterhin nichts.** Sie sehen neu die Tools `mail_tag` (lokale Tags), `mail_list_tags`, `mail_get_action`, `mail_list_actions` und `mail_sync_now`, aber nicht `mail_mark`, `mail_move` und `mail_trash`. Markieren (gelesen, markiert) braucht den neuen Scope `mail:mark`, den nur die Rollen `operator` und `admin` haben. Grund: Markieren wirkt auf alle Kopien am Server ohne Freigabe; eine Mail, die per Prompt-Injection „alles als gelesen markieren“ verlangt, versteckte sonst neue Mails in einem geteilten Posteingang.
2. **Tags erreichen den Server nur mit `[tags] sync_keywords = true`** (Standard aus). Dann schreibt auch ein `assistant`-Schlüssel IMAP-Keywords; das bleibt eine bewusste Entscheidung in `config.toml`.
3. **Soll ein Agent markieren dürfen**, bekommt er einen eigenen Schlüssel der Rolle `operator` (der kann dann auch verschieben und den Papierkorb beantragen; mit `--scopes` verengen, z. B. `key create --name markierer --role operator --scopes mail:read,mail:search,mail:mark`). Ein `operator`- oder `admin`-Schlüssel, der vor diesem Stand angelegt wurde, hat `mail:mark` nicht (die Scopes stehen je Schlüssel in der Datenbank); neu anlegen.
4. Prüfen: `postkauz policy show --key <name>` zeigt `messages.mark  mail:mark` nur bei Schlüsseln, die es dürfen.

### 1.7b `policy.toml` ändern (seit T3)

Eine geänderte `policy.toml` (z. B. `[move] agent_may = "none"` oder ein neuer Abschnitt `[keys.<name>]`) gilt

- **für Kommandos sofort:** Jedes Kommando liest sie beim Start neu;
- **für den MCP-Server erst nach seinem Neustart:** Er liest die Datei nur beim Start (Betreiber-Entscheid 29.10.2026: so gewollt). Also nach der Änderung die Claude-App ganz beenden und neu starten;
- **für schon eingereihte Aktionen erst nach einem Neustart des Dauer-Syncs:** `sync --watch` liest die Datei nur beim Start und prüft jede wartende Aktion eines Schlüssels vor der Ausführung gegen die Regeln, die er geladen hat (V3). Was die neue Datei verbietet, scheitert dann mit dem Grund (z. B. `policy_forbids`, in `action show <nr>`), ohne den Server zu berühren.

Also nach jeder Änderung: Dauer-Sync neu starten (1.6, `Stop-ScheduledTask …` und `Start-ScheduledTask …`) und die Claude-App neu starten (MCP-Server). Prüfen: `C:\PostKauz\postkauz.exe doctor --data-dir C:\PostKauz\data` zeigt die neue Policy-Version.

Gut zu wissen (V8, Betreiber 28.10.2026): In Ordner mit Rolle Spam (`junk`), Entwürfe (`drafts`) oder Gesendet (`sent`) verschiebt nur der Mensch an der Kommandozeile ohne `--key`. Ein Agent bekommt `denied (target_role_human_only)`, auch wenn er den Ordner per Namen nennt.

### 1.8 Zusammenspiel mit dem Audit-Export

Beide Aufgaben vertragen sich: Der Audit-Export liest nur das Audit und schreibt Dateien an den zweiten Ort; der Dauer-Sync hält nur die Sperren der Konten. Richte den Export ein, wie im README („Daily audit export“) beschrieben, mit **denselben** Pfaden:

```powershell
powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -DataDir C:\PostKauz\data -ExportDir "G:\My Drive\PostKauz-Audit"
```

Erwartet am Ende: `== OK: export written to G:\My Drive\PostKauz-Audit …`. Ein `sync --watch`, der über Mitternacht (UTC) läuft, schreibt seinen Abschluss erst am Ende; der Export ordnet ihn dem nächsten offenen Tag zu (`docs/cli.md`, Abschnitt 2f). `audit verify` bleibt dabei `ok`.

**Abgebrochener Export (seit 0.3.0-alpha.7):** Bricht ein Export ab (Strg+C, Rechner aus), merkt sich PostKauz die halb fertige Tagesdatei. `audit verify` zeigt dann `ok` und eine Zeile `<tag>: not exported completely (…)`; der nächste `audit export` (spätestens die tägliche Aufgabe) schließt ihn ab. Laufen zwei Exporte zugleich (Aufgabe und Hand), endet der zweite mit `error: another audit export of this data directory is running`; einfach später noch einmal.

**Altfall (Abbruch mit einer Version vor 0.3.0-alpha.7):** `audit export` endet mit `audit export file does not match its recorded checksum` oder `the target directory holds a file this program did not write`, und `audit verify` meldet `export file was changed` für den jüngsten Tag. Reparatur:

1. `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit verify` — merke dir die Datei, die in der Zeile mit `export file was changed` steht (die jüngste Tagesdatei).
2. Im Exportordner einen Unterordner anlegen, z. B. `abgebrochen-2026-10-29`, und genau diese Datei hineinschieben (nicht löschen; sie bleibt als Beleg).
3. `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit export` — erwartet: die Liste der Tage, der jüngste mit `updated` oder `written`.
4. `audit verify` wie in Schritt 1 — erwartet eine Zeile, die mit `ok:` beginnt.

Meldet `verify` mehr als einen Tag als verändert, ist es kein Abbruch: nicht reparieren, sondern klären (Abschnitt 1.8 oben, `docs/sicherheit.md` Abschnitt 6).

### 1.9 MCP-Server ohne Passwort, Sync über Claude anstoßen

Der MCP-Server, den die Claude-App startet, kennt **kein** Mailpasswort (ADR-009). Er liest den Spiegel und schreibt Entwürfe nur lokal. Ein Agent kann deshalb nicht selbst synchronisieren. Er kann aber mit dem Tool `mail_sync_now` einen Sync **anfordern**: Das legt einen Auftrag (Job `sync`) in der Datenbank an, und der Dauer-Sync führt ihn binnen etwa 10 Sekunden aus.

- Dafür braucht der Schlüssel des Agenten den Scope `mail:sync` (Rollen `assistant`, `operator`, `admin`; `reader` nicht).
- Läuft gerade kein Dauer-Sync, wartet der Auftrag bis zum nächsten `postkauz.exe sync` dieses Kontos.
- Wird der Schlüssel inzwischen widerrufen, läuft der Auftrag nicht (Logzeile `refused (key_revoked)`).
- Höchstens ein Sync je Minute auf Anforderung (seit T3): Fragt der Agent binnen 60 Sekunden noch einmal, antwortet das Tool `status: too_soon` und nennt mit `retry_after`, ab wann es wieder geht. Das hält einen Agenten in einer Schleife davon ab, das Postfach alle 10 Sekunden abzufragen.

Ausprobieren ohne Claude, von Hand:

```powershell
C:\PostKauz\postkauz.exe sync request -a privat --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
```

Erwartet:

```
sync of account privat (all mailboxes) queued as job 1
note: the sync is queued: a running sync --watch of this account runs it within its jobs interval (10 s by default); without one, the next postkauz sync of the account does it
```

Nach höchstens etwa 10 Sekunden steht im Log (1.5): `[privat] requested sync (job 1): running` und danach eine Zeile mit den Zahlen.

In Claude genügt ein Satz wie „Synchronisiere bitte mein Postfach privat jetzt“. Das Tool antwortet mit `status: queued` und der Job-Nummer.

## 2. Sicherung und Wiederherstellung

**Wichtig vorab: Eine Sicherung ist dein ganzes Mailarchiv, unverschlüsselt.** PostKauz verschlüsselt sie nicht selbst. Lege sie deshalb nur auf ein **verschlüsseltes** Laufwerk (unter Windows BitLocker bzw. „BitLocker To Go“ für USB-Sticks, unter Linux LUKS, sonst ein VeraCrypt-Container) oder verschlüssele den Strom mit `age` oder `restic` (Schritt 2.4). Jede Sicherung legt dazu die Datei `ENCRYPT-ME.txt` in den Zielordner und warnt am Ende.

Die Beispiele nehmen an: Der verschlüsselte USB-Stick ist entsperrt und heißt `E:`.

### 2.1 Was in einer Sicherung steckt

- die Datenbank als Schnappschuss (`postkauz-<Zeit>.db`); der Dauer-Sync darf dabei weiterlaufen,
- alle Mails und Anhänge (Ordner `blobs`), auch die Anhänge deiner Entwürfe,
- die Audit-Archive (`audit-archive-….db`), falls es welche gibt; nur die, die die Datenbank kennt und die zu ihrer Prüfsumme passen. Fehlt eines oder wurde es verändert, kommt es nicht mit, die Sicherung warnt (`warning: audit archive … is not in this backup …`) und `audit verify` zeigt denselben Befund. Eine Archivdatei, die die Datenbank nicht kennt (Rest eines abgebrochenen `audit rotate`), nennt `doctor`; verschiebe sie aus dem Datenverzeichnis,
- `manifest.json`: Liste aller Dateien mit Größe und Prüfsumme (SHA-256),
- `ENCRYPT-ME.txt`: der Hinweis auf die Verschlüsselung.

**Nicht** darin: deine Passwörter (sie liegen im Schlüsselbund von Windows), das Log, `config.toml` und `policy.toml` (kopiere sie selbst dazu) und der Ordner des Audit-Exports (der ist schon dein zweiter Ort).

### 2.2 Erste Sicherung

```powershell
C:\PostKauz\postkauz.exe backup create --to E:\PostKauz-Sicherung --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
```

Erwartet (Zahlen und Zeit bei dir anders):

```
backup written to E:\PostKauz-Sicherung
  database:  postkauz-20261028T101500Z.db (schema 22)
  blobs:     52311 (copied 52311, already there 0; 3.1 GB copied)
  manifest:  E:\PostKauz-Sicherung\manifest.json, 52312 files (4.5 GB), sha256 4f0c…
  restore:   restore --from <backup folder> --data-dir <new folder>
warning: the backup is not encrypted; keep it only on an encrypted drive (BitLocker, LUKS, VeraCrypt) or encrypt it (age, restic); see ENCRYPT-ME.txt
```

Die Warnung kommt bei **jeder** Sicherung, auch wenn der Stick verschlüsselt ist: PostKauz kann das nicht prüfen. Der Zielordner muss neu, leer oder eine frühere Sicherung sein; einen Ordner im Datenverzeichnis lehnt das Programm ab (`error: backup: target not allowed: …`, nichts wird geschrieben).

### 2.3 Weitere Sicherungen in denselben Ordner

Dasselbe Kommando wie in 2.2. Kopiert werden nur Mails und Anhänge, die neu dazugekommen sind; die Datenbank wird jedes Mal neu gesichert und der alte Schnappschuss danach gelöscht. Erwartet zum Beispiel:

```
  blobs:     52340 (copied 29, already there 52311; 2.3 MB copied)
  replaced:  1 database snapshot of the earlier backup
```

Ohne neue Mails steht dort `copied 0`. Wer mehrere Stände aufheben will, nimmt mehrere Ordner (z. B. einen je Monat).

Es läuft immer nur **eine** Sicherung je Datenverzeichnis. Startest du eine zweite, während die erste noch kopiert (z. B. von Hand neben einer geplanten Aufgabe), bricht die zweite ab, ohne etwas zu schreiben: `error: backup: another backup of this data directory is running` (Code `backup_running`). Warte, bis die erste fertig ist, und starte dann neu.

Fehlt während der Sicherung eine Mail-Datei, auf die die Datenbank zeigt (etwa weil gleichzeitig `purge` und `blob gc` liefen), bricht die Sicherung ab: `error: backup: incomplete: … blob(s) the database refers to are missing from the blob store …` (Code `backup_incomplete`). Eine frühere Sicherung im Ordner bleibt dann gültig. Starte die Sicherung einfach noch einmal; kommt der Fehler wieder, prüfe das Datenverzeichnis mit `doctor --blobs=all`.

Ab und zu prüfen, ob die Sicherung noch vollständig und unverändert ist (liest alles, schreibt nichts):

```powershell
C:\PostKauz\postkauz.exe restore --from E:\PostKauz-Sicherung --data-dir C:\PostKauz\pruefung --dry-run
```

Erwartet: `dry run: the backup of … is complete and unchanged (…); … would be created; nothing was written`. Der Ordner `pruefung` entsteht dabei nicht.

### 2.4 Sicherung als verschlüsselter Strom (age, restic)

Statt eines Ordners schreibt `--tar -` einen Tar-Strom auf die Standardausgabe; die Zusammenfassung und die Warnung stehen dann auf dem Bildschirm (stderr). Das ist der Weg für Linux und macOS:

```sh
postkauz backup create --tar - | age -r age1… > postkauz-2026-10-28.tar.age
postkauz backup create --tar - | restic backup --stdin --stdin-filename postkauz.tar
```

Erwartet: `backup written as a tar stream to standard output` und die Warnung. Während des Laufs liegt der Datenbank-Schnappschuss kurz im Datenverzeichnis (Ordner `backup-tmp`, braucht so viel freien Platz wie die Datenbank) und wird danach gelöscht. **Unter Windows** in der alten PowerShell 5 nicht pipen (sie verändert Binärdaten); dort ist `--to` auf ein BitLocker-Laufwerk der einfache Weg.

### 2.5 Wiederherstellen

Wiederhergestellt wird **immer in einen neuen, leeren Ordner**. In das Datenverzeichnis, das gerade benutzt wird (aus `config.toml`, der Umgebung oder dem Standardort), schreibt `restore` nie.

**Stelle nur deine eigenen Sicherungen wieder her, die sicher (verschlüsselt, nur für dich zugänglich) lagen.** Eine Sicherung ist nicht signiert: Wer sie verändern konnte, kann darin den Server eines Kontos auf seinen eigenen umlenken; das Passwort, das du danach eintippst, ginge dann dorthin. Deshalb zeigt `restore` je Konto Server und Benutzer (Zeilen unter `accounts:`) und die Stildateien (`style:`). **Prüfe den Server, bevor du in Schritt 3 ein Passwort eingibst**; stimmt etwas nicht, lösche die Kopie und nimm eine andere Sicherung.

1. Aus einem Tar-Strom erst entpacken, z. B. `mkdir wieder && age -d -i schluessel.txt postkauz-2026-10-28.tar.age | tar -x -C wieder`. Ein Sicherungsordner aus 2.2 braucht das nicht.
2. Wiederherstellen (`--config` nennt hier die Live-Konfiguration nur, damit `restore` deren Datenverzeichnis sicher meidet):

   ```powershell
   C:\PostKauz\postkauz.exe restore --from E:\PostKauz-Sicherung --data-dir C:\PostKauz\data-neu --config C:\PostKauz\config.toml
   ```

   Erwartet (Auszug):

   ```
   restored:  backup of 2026-10-28T10:15:00Z (program 0.3.0-…, schema 22) from E:\PostKauz-Sicherung
   into:      C:\PostKauz\data-neu (schema 22)
   manifest:  sha256 4f0c…; 52312 files (4.5 GB) checked: database, 52311 blobs, 0 audit archives
   identity:  new data directory id; the keyring entries of the original are not used here
   …
   accounts:  check each server and user before you store a password (only restore your own backups):
              privat: imap.example.org:993 (tls), user ich@example.org, password
   …
   blobs:
     …
     result:  ok
   ```

   Vorher prüft `restore` jede Datei gegen das Manifest. Fehlt eine Datei oder wurde eine verändert, bricht es ab (`error: backup: invalid backup: …`) und entfernt, was es angelegt hatte. Ist der Zielordner nicht leer: `error: backup: target not allowed: … is not empty`.

   `restore` arbeitet in einem Ordner neben dem Ziel (`C:\PostKauz\data-neu.restoring-…`) und benennt ihn erst ganz am Ende in `data-neu` um. Bricht der Rechner mittendrin ab (Stromausfall), bleibt dieser Ordner liegen; der nächste `restore` und `doctor` nennen ihn (`… is left from an interrupted restore …`). Lösche ihn und starte `restore` neu; benutze ihn nie als Datenverzeichnis.

   Für alles Weitere bekommt die Kopie eine **eigene** Konfiguration `C:\PostKauz\config-neu.toml` (Kopie der Live-Konfiguration) mit eigenem Exportordner, nie dem des Originals:

   ```toml
   data_dir = 'C:\PostKauz\data-neu'   # oben in der Datei, vor dem ersten [Abschnitt]

   [audit]
   export_dir = 'G:\My Drive\PostKauz-Audit-neu'
   ```

   Exportiert die Kopie doch in den Ordner des Originals, bricht der Export seit 0.3.0-alpha.7 ab, ohne etwas zu schreiben (`error: audit export: the target directory belongs to another data directory`).
3. **Passwörter neu hinterlegen.** Die Kopie benutzt die gespeicherten Passwörter des Originals absichtlich nicht (`doctor` meldet `account privat has no password`):
   `C:\PostKauz\postkauz.exe account set privat --password-stdin --data-dir C:\PostKauz\data-neu` (bei Microsoft-Konten `account login privat`).
4. Prüfen: `C:\PostKauz\postkauz.exe doctor --blobs=all --data-dir C:\PostKauz\data-neu` — erwartet `result:  ok`; eine Suche, die du kennst, liefert dieselben Treffer wie vorher.
5. Soll die Kopie ab jetzt dein Datenverzeichnis sein: alles anhalten wie in 1.7 (Schritte 1 bis 3), dann die Aufgaben mit `-DataDir C:\PostKauz\data-neu` neu registrieren (1.3) und den Audit-Export mit dem **neuen** Exportordner der Kopie (1.8: `-DataDir C:\PostKauz\data-neu -ExportDir "G:\My Drive\PostKauz-Audit-neu"`), in der Claude-Konfiguration `--data-dir` anpassen. Der alte Exportordner bleibt als Beleg des Originals liegen. Das alte Verzeichnis erst löschen, wenn alles läuft.

### 2.6 Was die Kopie absichtlich anders macht

Eine Wiederherstellung ist eine **neue Instanz** (A10). Nichts darin schreibt von selbst auf einen Mailserver:

- **Neue Kennung:** Die Kopie sucht ihre Passwörter unter eigenen Namen im Schlüsselbund; die Einträge des Originals liest, verschiebt und löscht sie nie. Mit `--keep-identity` behält sie die Kennung des Originals und benutzt dessen Passwörter. Das ist nur gedacht, wenn das Original verloren ist und du auf demselben Rechner weiterarbeiten willst; Original und Kopie dürfen dann nicht beide laufen.
- **Offene Aufträge sind geschlossen:** Entwürfe ablegen, markieren, verschieben, angeforderte Syncs (`failed`, Grund `restored`); offene Anträge auf den Papierkorb sind abgelehnt. Entwürfe, die noch in den Entwürfe-Ordner sollten, hält die Kopie zurück: `draft show <id>` sagt warum; `draft edit` oder `draft request` gibt sie frei (vorher prüfen, ob das Original sie schon abgelegt hat).
- **Audit:** Das Audit läuft weiter; neu ist das Ereignis `backup.restore` mit der Prüfsumme des Manifests. Die Orte früherer Audit-Exporte gehören zum Original: `audit verify` prüft diese Tage in der Kopie nur gegen die Datenbank (`… days were exported by the data directory this one was restored from …`). Gib der Kopie einen **eigenen** Exportordner (`audit export --to …` bzw. `export_dir` in ihrer Konfiguration, 2.5 Schritt 2), nie den des Originals, auch mit `--keep-identity` nicht. Ohne `--keep-identity` verweigert der Export den Ordner des Originals seit 0.3.0-alpha.7 selbst (das Manifest dort trägt die Kennung des Originals).

### 2.7 Original und Kopie auf demselben Rechner

Beide dürfen nebeneinander liegen. Danach zeigt `audit verify` in beiden `ok` (ohne `changed`):

```powershell
C:\PostKauz\postkauz.exe audit verify --data-dir C:\PostKauz\data
C:\PostKauz\postkauz.exe audit verify --data-dir C:\PostKauz\data-neu
```

Erwartet jeweils eine Zeile, die mit `ok:` beginnt.

## 3. Microsoft-Postfächer

Folgt beim Zusammenführen von Teil T4. Bis dahin: [betrieb_microsoft.md](betrieb_microsoft.md).

## 4. Betrieb auf einem VPS

Folgt mit Teil T5 (Linux-Server, systemd, Fernzugriff über HTTP-MCP).

## 5. Löschkonzept

PostKauz löscht nie von selbst. Was auf dem Server gelöscht wurde, bleibt im lokalen Archiv, bis du es ausdrücklich entfernst (ADR-003). Dafür gibt es zwei Kommandos, dazu eines für das Audit (5.3); sie laufen nur ohne `--key` (ein Agent kann sie nie aufrufen) und nie, während ein Sync oder Import läuft. Probiere jedes zuerst mit `--dry-run`: Dann zählt es nur und ändert nichts.

### 5.1 Was auf dem Server gelöscht wurde: `purge`

1. Zählen, was älter als 180 Tage gelöscht ist:

   ```powershell
   C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml purge --older-than 180d --dry-run
   ```

   Erwartet (Zahlen je nach Postfach):

   ```
   all accounts, deleted on the server before 2026-05-01T10:00:00Z (older than 180d):
     gone from the server: 120
     kept (imported archive): 100
     kept (a draft refers to them): 1
   dry run: 19 messages would be deleted from the local archive; nothing changed
   ```

   `kept (imported archive)` sind Nachrichten aus `import` (z. B. dein Thunderbird-Archiv): Die bleiben immer. `kept (a draft refers to them)` sind Nachrichten, auf die ein Entwurf antwortet: Die bleiben ebenfalls.
2. Wirklich löschen: dasselbe Kommando ohne `--dry-run`. Erwartet zum Schluss:

   ```
   deleted: 19 messages, 7 attachment entries, 12 threads that became empty
   the stored files stay until: blob gc
   ```

   Nur ein Konto: `-a privat` anhängen. Statt `--older-than` kannst du die Frist auch fest in die Konfiguration schreiben (`[retention] deleted_remote_after = "180d"`), dann genügt `purge`.
3. Meldet es `sync_running`, läuft gerade ein Sync (z. B. die Aufgabe `sync --watch`). Warte, bis er fertig ist, oder stoppe die Aufgabe kurz (Abschnitt 1.6), und versuche es noch einmal.

### 5.2 Gespeicherte Dateien ohne Verweis: `blob gc`

Rohmails und Anhänge liegen als Dateien unter `blobs\`. Nach `purge` verweist auf manche nichts mehr. `blob gc` löscht solche Dateien, aber nur, wenn sie älter als 24 Stunden sind (`[retention] blob_gc_min_age`); Anhänge von Entwürfen bleiben immer.

```powershell
C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml blob gc --dry-run
```

Erwartet (Zahlen je nach Bestand):

```
blob store: 52000 files, 51990 rows in table blobs, 51975 referenced
  without reference: 30, of these younger than 1d (kept): 2
would delete: 28 files (12.3 MB), 26 rows
dry run: nothing changed
```

Ohne `--dry-run` steht statt `would delete` die Zeile `deleted: …`. Danach meldet `doctor --blobs` bei `without reference` nur noch die frischen Dateien.

### 5.3 Altes Audit archivieren: `audit rotate`

Das Audit wächst mit jedem Aufruf. `audit rotate` verschiebt Tage, die älter als ein Jahr sind (`[audit] retain`, Standard `365d`), aus der Datenbank in eine eigene Archivdatei im Datenverzeichnis (`audit-archive-<von>-<bis>.db`). Es nimmt nur Tage, die schon exportiert sind und die `audit verify` bestätigt; sonst lehnt es ab. Die Prüfkette bleibt dabei ganz: `audit verify` und `audit export` lesen die alten Tage danach aus dem Archiv.

1. Erst exportieren und prüfen (das tut die tägliche Aufgabe ohnehin):

   ```powershell
   C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit export
   C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit verify
   ```

   Erwartet zum Schluss von `audit verify`: eine Zeile, die mit `ok:` beginnt.
2. Zählen:

   ```powershell
   C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit rotate --dry-run
   ```

   Erwartet (Daten und Zahlen je nach Bestand):

   ```
   dry run: would move 120 days (2025-06-01 to 2025-09-28, 8400 entries) into C:\PostKauz\data\audit-archive-2025-06-01-2025-09-28.db; nothing changed
   ```

   Ist nichts alt genug: `nothing to rotate: no exported day ended before … (retain 365d)`.
3. Wirklich archivieren: dasselbe ohne `--dry-run`. Erwartet:

   ```
   rotated 120 days (2025-06-01 to 2025-09-28, 8400 entries) into C:\PostKauz\data\audit-archive-2025-06-01-2025-09-28.db
   sha256: …
   chain anchor: …
   check with: audit verify
   ```

4. Danach `audit verify`: wieder `ok: …`. Die Archivdatei gehört zum Datenverzeichnis; sichere sie mit (das Backup tut das). **Lösche sie nie:** Ohne sie meldet `audit verify` `archive is missing`.

Meldungen: `error: another audit rotate of this data directory is running` heißt, eine zweite Rotation läuft oder hat gerade ein Archiv angelegt; nichts wurde verschoben: `audit verify`, dann noch einmal. `denied (not_exported)` heißt, ein alter Tag ist noch nicht (fertig) exportiert: `audit export`, dann `audit verify`, dann noch einmal. `denied (not_verified)` heißt, `audit verify` findet eine Abweichung; erst klären (Abschnitt 1.8), dann noch einmal.

## 6. Export

Seit T3 kann PostKauz einzelne Anhänge als Datei ablegen und das ganze Archiv eines Kontos als mbox oder als Ordner mit `.eml`-Dateien ausgeben. Der Export ist **nicht verschlüsselt**: Lege ihn nur auf einen Datenträger, den du selbst schützt (wie beim Backup).

### 6.1 Einen Anhang ablegen

```powershell
C:\PostKauz\postkauz.exe attach list 1201
C:\PostKauz\postkauz.exe attach save 7 --subdir rechnungen
```

Erwartet: `saved attachment 7 of message 1201 as rechnungen/Rechnung.pdf below export_dir (…)`. Die Datei liegt im Ordner, den `doctor` in der Zeile `export:` nennt (Standard `C:\PostKauz\data\export`). Gibt es den Namen schon, heißt die neue Datei `Rechnung-2.pdf`; nichts wird überschrieben. Programme und Skripte (z. B. `.exe`, `.bat`, `.js`) bekommen die Endung `.blocked`: Öffne sie nicht. Einen anderen Ordner stellst du in der Konfiguration ein (`[paths] export_dir`).

### 6.2 Das Archiv exportieren

Das Ziel muss ein **neuer oder leerer** Ordner sein.

```powershell
C:\PostKauz\postkauz.exe export mbox --to D:\Export\privat-mbox -a privat
C:\PostKauz\postkauz.exe export eml --to D:\Export\privat-eml -a privat
```

Erwartet: je Ordner eine Zeile `  INBOX: 230 of 230 messages`, am Ende eine Tabelle und `messages: …, skipped: …`. `skipped` mit `no_blob` sind Nachrichten, von denen nur der Kopf gespiegelt ist; sie fehlen im Export. Ist der Zielordner nicht leer, endet der Befehl mit `denied (target_not_fresh)` und ändert nichts. Nur einen Ordner: `--mailbox INBOX`; nur neuere Nachrichten: `--since 2026-01-01`.

- **mbox:** eine Datei je Ordner, Unterordner wie bei Thunderbird (`Archiv.sbd\2024`). Nachrichten, die nicht mehr auf dem Server liegen, stehen unter `_imported` (importiert) bzw. `_no_folder`.
- **eml:** je Ordner ein Unterordner mit einer Datei je Nachricht, dazu `index.csv` (Nummer, Message-ID, Datum, Ordner, Größe, Prüfsumme; ohne Betreff und Adressen).

### 6.3 Die mbox in Thunderbird öffnen (ungeprüft, Stand Oktober 2026)

Thunderbird liest mbox-Dateien nicht ohne Zusatz ein. Der übliche Weg ist das Add-on **ImportExportTools NG**:

1. Thunderbird: Menü **Extras → Add-ons und Themes**, nach „ImportExportTools NG“ suchen, **Zu Thunderbird hinzufügen**, Thunderbird neu starten.
2. Links unter **Lokale Ordner** einen neuen Ordner anlegen, z. B. `Export privat` (Rechtsklick → **Neuer Ordner**).
3. Rechtsklick auf diesen Ordner → **ImportExportTools NG → Importiere mbox-Datei** → **Einzelne mbox-Dateien importieren** (bzw. „alle mbox-Dateien aus einem Verzeichnis einschließlich Unterordnern“ für den ganzen Export) → die Datei (z. B. `INBOX`) im Exportordner wählen.
4. Ein paar Nachrichten öffnen: Betreff, Text und Anhänge müssen lesbar sein.

Die Menünamen können je nach Version abweichen; geprüft wird das in der Abnahme D7.

## 7. Release bauen und nachbauen

Ein Release (`make release VERSION=…`) ist erst belastbar, wenn ein zweiter Bau aus einem frischen Klon Byte für Byte dieselben Programmdateien ergibt. Die Programmdatei trägt die Modulversion von Go (`mod … v0.0.0-<Zeit>-<Hash>`, sichtbar mit `go version -m`). Diese **Pseudoversion hängt von den Tags im Klon ab**: In einem Klon mit dem Tag `v0.2.0-rc8` entsteht `v0.2.0-rc8.0.<Zeit>-<Hash>`, ohne Tags `v0.0.0-<Zeit>-<Hash>`; die Dateien unterscheiden sich dann (Prüfsitzung 29.10.2026: `0.3.0-alpha.6` war erst nach dem Löschen der Tags bytegleich).

Regel (seit der Nachbesserung T3):

1. Bauen und Nachbauen jeweils in einem frischen Klon **ohne Tags**, auf dem Release-Commit, mit sauberem Arbeitsbaum:

   ```bash
   git clone --no-tags --branch <zweig> https://github.com/KNUFFIXX/postkauz.git pk-release
   ```

   ```bash
   cd pk-release && git checkout <commit> && git status --short
   ```

   Erwartet: `git status --short` gibt nichts aus.
2. Bauen mit der Toolchain aus `go.mod` (go1.27.3):

   ```bash
   make release VERSION=<version>
   ```

3. Den Nachbau genauso in einem **zweiten** frischen Klon ohne Tags.
4. Vergleichen: `dist/SHA256SUMS` beider Klone gleich (die Zeile `VERSION` darf abweichen, sie enthält die Bauzeit), dazu je Programm

   ```bash
   go version -m dist/postkauz-linux-amd64
   ```

   Erwartet in beiden: dieselbe Zeile `mod github.com/KNUFFIXX/postkauz v0.0.0-<Zeit>-<Hash>` und dieselben `build`-Zeilen.

**Festlegung (Einspruch möglich):** Auch die Cloud-Sitzung baut Releases so, in einem Klon ohne Tags; Prüfsummen und `go version -m` stehen im Protokoll der Sitzung.
