# Betrieb von PostKauz

Für: den Betreiber. Diese Anleitung erklärt, wie PostKauz im Alltag läuft: Schritt für Schritt, je Schritt ein Kommando und die Ausgabe, die du erwarten kannst. Programmierkenntnisse brauchst du nicht. Stand: S3.8 (0.3.0-rc1, 29.10.2026; Schnitt 3 komplett). Abschnitte 1 bis 1.7 seit T2, 2, 5 und 6 seit T3, 4 seit T5 (zuletzt zweite Nachbesserung: Passwort auf dem VPS als systemd-Credential, Empfehlung systemd ≥ 257), Microsoft-Konten in [betrieb_microsoft.md](betrieb_microsoft.md).

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

**Abgebrochener Export (seit 0.3.0-alpha.7):** Bricht ein Export ab (Strg+C, Rechner aus, Google Drive nicht erreichbar, Zeitlimit der Aufgabe), merkt sich PostKauz die halb fertige Tagesdatei; das Manifest passt seit 0.3.0-alpha.7b nach jedem fertigen Tag zum erreichten Stand. `audit verify` zeigt dann `ok` und eine Zeile `<tag>: not exported completely (…)` (endete der Prozess hart genau zwischen dem Nachweis eines Tages und dem neuen Manifest, statt `ok` einmal `manifest does not match the recorded exports`); der nächste `audit export` (spätestens die tägliche Aufgabe) schließt ihn ab. Laufen zwei Exporte zugleich (Aufgabe und Hand) oder ein Export und `audit rotate`, endet der zweite mit `error: another audit export or audit rotate of this data directory is running`; einfach später noch einmal.

**Altfall (Abbruch mit einer Version vor 0.3.0-alpha.7):** `audit export` endet mit `audit export file does not match its recorded checksum` oder `the target directory holds a file this program did not write`, und `audit verify` meldet `export file was changed` für den jüngsten Tag. Reparatur:

1. `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit verify` — merke dir die Datei, die in der Zeile mit `export file was changed` steht (die jüngste Tagesdatei).
2. Im Exportordner einen Unterordner anlegen, z. B. `abgebrochen-2026-10-29`, und genau diese Datei hineinschieben (nicht löschen; sie bleibt als Beleg).
3. `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit export` — erwartet: die Liste der Tage, der jüngste mit `updated` oder `written`.
4. `audit verify` wie in Schritt 1 — erwartet eine Zeile, die mit `ok:` beginnt.

Meldet `verify` mehr als einen Tag als verändert, ist es kein Abbruch: nicht reparieren, sondern klären (Abschnitt 1.8 oben, `docs/sicherheit.md` Abschnitt 6).

**Erster Export nach dem Update (seit 0.3.0-alpha.7b):** Der Exportordner darf anders geschrieben sein als bisher (z. B. `g:\my drive\…` statt `G:\My Drive\…` oder ein neuer Laufwerksbuchstabe von Google Drive), solange das Manifest dort zu den Nachweisen passt. Endet der Export trotzdem mit `the target directory belongs to another data directory: … last recorded at <ort>`, einmal genau mit diesem Ort exportieren: `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit export --to "<ort>"`, danach läuft die Aufgabe wieder. Bricht dieser erste Export unter der neuen Schreibweise ab, erkennt der nächste Lauf die schon vorhandenen Tagesdateien an ihrer Prüfsumme als eigene (seit 0.3.0-alpha.7c) und schließt ab; bis dahin kann `audit verify` für den alten und den neuen Ort `manifest does not match the recorded exports` melden.

**Export endet mit `audit_export_source_changed` (seit 0.3.0-alpha.7c):** Der Export hat nichts geschrieben. Ein abgeschlossener Tag liefert aus der Datenbank nicht mehr die Einträge seines Nachweises, die Datenbank wurde also verändert. Nicht erneut exportieren und nichts im Exportordner löschen oder verschieben:

1. `C:\PostKauz\postkauz.exe --config C:\PostKauz\config.toml audit verify` — erwartet eine Zeile mit `database_changed` und dem Tag.
2. Den ganzen Exportordner (mit Manifest) an einen zweiten Ort kopieren, z. B. auf einen USB-Stick; bei Google Drive zusätzlich nicht auf ältere Versionen zurücksetzen (sie sind der Beleg, Google hält sie nur begrenzt).
3. Klären, wer oder was die Datenbank geändert hat (`docs/sicherheit.md`, Abschnitt 6). Bis dahin bleibt der tägliche Export gesperrt; der Dauer-Sync läuft weiter, `doctor` warnt nach zwei Tagen.

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
- die Audit-Archive (`audit-archive-….db`), falls es welche gibt; nur die, die die Datenbank kennt und die zu ihrer Prüfsumme passen. Fehlt eines oder wurde es verändert, kommt es nicht mit, die Sicherung warnt (`warning: audit archive … is not in this backup …`) und `audit verify` zeigt denselben Befund. Eine Archivdatei, die die Datenbank nicht kennt (Rest eines abgebrochenen `audit rotate`), nennt `doctor`; verschiebe sie aus dem Datenverzeichnis. Meldet `doctor` stattdessen `an audit rotate of this data directory is running right now`, läuft die Rotation noch: nichts verschieben, später `doctor` noch einmal,
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

Verschwindet **während** der Sicherung eine Mail-Datei, auf die die Datenbank zeigt (etwa weil gleichzeitig `purge` und `blob gc` liefen), bricht die Sicherung ab: `error: backup: incomplete: … blob(s) the database refers to disappeared from the blob store during the backup …` (Code `backup_incomplete`). Eine frühere Sicherung im Ordner bleibt dann gültig. Starte die Sicherung einfach noch einmal, in denselben Ordner. Das gilt auch, wenn schon die **erste** Sicherung in einen neuen Ordner abbrach (Fehler, Strg+C): Liegen dort nur `ENCRYPT-ME.txt` und Reste des abgebrochenen Laufs (Temporärdateien `.…tmp`, ein Datenbank-Schnappschuss `postkauz-….db`, der Ordner `blobs`, Audit-Archive `audit-archive-….db`), gilt der Ordner als leer, und der nächste Lauf räumt die Reste ab. Woran PostKauz erkennt, dass die Reste von **diesem** Datenverzeichnis stammen: Die erste Sicherung schreibt als erste Datei die versteckte Laufmarke `.backup-run.tmp` mit der Kennung des Datenverzeichnisses und löscht sie erst, wenn `manifest.json` steht (seit 0.3.0-alpha.7c). Fehlt die Marke oder stammt sie von einem anderen Datenverzeichnis (etwa die Sicherung eines anderen Rechners, deren `manifest.json` verloren ging), weist PostKauz den Ordner ab (`error: backup: target not allowed: … is not empty and holds no backup of postkauz …`), statt hineinzuschreiben und den fremden Schnappschuss zu löschen. Nimm dann einen neuen Ordner. Das gilt auch für Reste einer ersten Sicherung, die mit 0.3.0-alpha.7b oder älter abbrach (damals ohne Marke).

Fehlte eine Mail-Datei **schon vor** der Sicherung (zum Beispiel, weil ein Virenscanner eine Mail mit Schadanhang in die Quarantäne verschoben hat), läuft die Sicherung trotzdem durch, damit eine einzige verlorene Datei nicht jede weitere Sicherung blockiert. Sie nennt die Datei dann nur mit ihrer Prüfsumme (Hash), nie mit Inhalt, zum Beispiel:

```
  missing:   1 blob already missing before the backup (see the warning)
warning: 1 blob the database refers to was already missing from the blob store before this backup and is not in it (listed in the manifest as missing_blobs): 3f6c…; check the data directory with doctor --blobs=all
```

Die Liste steht auch im Manifest (`missing_blobs`) und als Zahl im Audit (`backup.done`). Prüfe dann das Datenverzeichnis mit `doctor --blobs=all` (die Zeile `rows without file` nennt denselben Hash) und hole die Datei, wenn möglich, aus der Quarantäne zurück; die nächste Sicherung nimmt sie dann wieder mit. Eine Wiederherstellung aus so einer Sicherung übernimmt den Zustand: `restore` nennt die fehlenden Dateien (`missing: … already missing when the backup was made`), `doctor --blobs` in der Kopie ebenso, und `restore` endet deshalb mit Exit 1 (`blob check failed: … already missing when the backup was made`); die übrigen Daten sind vollständig wiederhergestellt.

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

   `restore` arbeitet in einem Ordner neben dem Ziel (`C:\PostKauz\data-neu.restoring-…`) und benennt ihn erst ganz am Ende in `data-neu` um. Bricht der Rechner mittendrin ab (Stromausfall), bleibt dieser Ordner liegen; der nächste `restore` und `doctor` nennen ihn (`… is left from an interrupted restore …`). Lösche ihn und starte `restore` neu; benutze ihn nie als Datenverzeichnis (einzige Ausnahme: der Fall im nächsten Absatz). Hält ein anderes Programm (unter Windows oft der Virenscanner, der die eben geschriebenen Dateien prüft) den Ordner gerade fest, versucht `restore` das Umbenennen bis zu fünfmal mit kurzer Pause (zusammen gut 3 Sekunden).

   Klappt es dann nicht, ist die Kopie trotzdem fertig, und `restore` **behält** den Arbeitsordner (seit 0.3.0-alpha.7c; vorher löschte es ihn, und alles musste neu kopiert werden). Die Meldung nennt den echten Grund, den Ordner und den Befehl, der ihn an seinen Platz bringt, unter Windows für PowerShell, zum Beispiel:

   ```
   error: restore: cannot move the restored copy into place (is another program using it?): … being used by another process; the restored copy is complete and was kept in "C:\PostKauz\data-neu.restoring-1a2b3c4d" (do not delete it); when no other program uses it any more, put it in place with: [System.IO.Directory]::Delete('C:\PostKauz\data-neu'); Rename-Item -LiteralPath 'C:\PostKauz\data-neu.restoring-1a2b3c4d' -NewName 'data-neu'
   ```

   Warte kurz und gib den Befehl hinter `put it in place with:` in PowerShell ein (unter Linux und macOS steht dort `rmdir '…' && mv '…' '…'` bzw. nur `mv '…' '…'` für die Shell). Der erste Teil (`[System.IO.Directory]::Delete(…)` bzw. `rmdir`) entfernt nur den leeren Zielordner; ist er nicht leer, scheitert er, `Rename-Item` bzw. `mv` läuft dann nicht oder scheitert ebenfalls, und nichts wird überschrieben. Nimm dafür nicht `Remove-Item`: Es fragt bei einem nicht leeren Ordner nach und löscht auf „Ja“ alles darin. Scheitert der Befehl, weil der Ordner noch belegt ist, warte länger oder starte den Rechner neu und versuche es noch einmal. Danach weiter mit Schritt 3. Die Zeilen `accounts:` und `style:` und den Bericht von `doctor` zeigt `restore` in diesem Fall nicht: Prüfe die Kopie mit Schritt 4, und wer die Server der Konten sehen will, bevor er ein Passwort eingibt, löscht den Arbeitsordner und startet `restore` noch einmal. Hat sich der Zielordner während des Laufs gefüllt (`error: backup: target not allowed: … is no longer empty`), entfernt `restore` den Arbeitsordner wie bisher.

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
- **Audit:** Das Audit läuft weiter; neu ist das Ereignis `backup.restore` mit der Prüfsumme des Manifests. Die Orte früherer Audit-Exporte gehören zum Original: `audit verify` prüft diese Tage in der Kopie nur gegen die Datenbank (`… days were exported by the data directory this one was restored from …`). Gib der Kopie einen **eigenen** Exportordner (`audit export --to …` bzw. `export_dir` in ihrer Konfiguration, 2.5 Schritt 2), nie den des Originals, auch mit `--keep-identity` nicht. Ohne `--keep-identity` verweigert der Export den Ordner des Originals seit 0.3.0-alpha.7 selbst (das Manifest dort trägt die Kennung des Originals), mit `--keep-identity` seit 0.3.0-alpha.7b, solange die Kopie dort noch nichts exportiert hat.

### 2.7 Original und Kopie auf demselben Rechner

Beide dürfen nebeneinander liegen. Danach zeigt `audit verify` in beiden `ok` (ohne `changed`):

```powershell
C:\PostKauz\postkauz.exe audit verify --data-dir C:\PostKauz\data
C:\PostKauz\postkauz.exe audit verify --data-dir C:\PostKauz\data-neu
```

Erwartet jeweils eine Zeile, die mit `ok:` beginnt.

## 3. Microsoft-Postfächer

Folgt beim Zusammenführen von Teil T4. Bis dahin: [betrieb_microsoft.md](betrieb_microsoft.md).

## 4. Fernzugriff über HTTP-MCP und Betrieb auf einem VPS

Seit T5 (S3.5, S3.6; ADR-010). Ziel: Ein Agent erreicht PostKauz auch von außerhalb des Rechners mit dem Datenverzeichnis, zuerst aus einer Claude-Code-Cloud-Sitzung, nur über dein privates Netz (Tailscale). Die Schritte zu Tailscale und Authelia sind **Stand Oktober 2026, ungeprüft**; die Abnahme D8 prüft sie.

### 4.1 Was läuft wo

- **Zwei Dienste** auf dem Dauerläufer (VPS oder PC):
  - `postkauz sync --watch` kennt die Mailpasswörter und spiegelt die Postfächer (wie in Abschnitt 1).
  - `postkauz mcp --http` kennt **kein** Mailpasswort. Er lauscht nur auf `127.0.0.1:8787` und beantwortet MCP-Anfragen über HTTP. Jeder MCP-Prozess (stdio und `--http`) bekommt einen Secrets-Speicher, der jeden Zugriff verweigert (Backend `denied`): Schlüsselbund, die Datei von `insecure-file`, `POSTKAUZ_PASSWORD_*` und systemd-Credentials fasst er nie an.
- **Tailscale** verbindet deine Geräte zu einem privaten Netz (Tailnet). `tailscale serve` macht aus `http://127.0.0.1:8787` die Adresse `https://<knoten>.<tailnet>.ts.net` mit gültigem Zertifikat, **nur im Tailnet**. Kein Funnel, kein Cloudflare-Tunnel, kein offener Port im Internet.
- **Anmeldung** an `mcp --http`, eine von zwei Arten (`[http] auth`):
  - `key`: Der Client schickt einen PostKauz-Schlüssel mit (`Authorization: Bearer pk_key_…`). Einfach, geht auch ohne Browser, also aus einer Cloud-Sitzung. Nur auf Loopback oder einer Tailnet-Adresse.
  - `oauth`: Der Client meldet sich bei einem **eigenen Anmeldedienst** (Authelia) an und schickt dessen kurzlebiges Token mit. Der Standard für Claude Code am PC (mit Browser).

Was ein Agent darf, bestimmt in beiden Fällen nur der PostKauz-Schlüssel (Rolle, Scopes). Jeder Aufruf steht im Audit mit `transport: http` und dem Schlüsselnamen, nie mit dem Token.

### 4.2 Erst lokal ausprobieren (Modus `key`, ohne Tailscale)

> **Achtung, nie die Live-Datenbank mit einem Testbau:** Gegen dein echtes Datenverzeichnis (`C:\PostKauz\data`, `C:\PostKauz\config.toml`) nur mit einem **Release von `main`**. Ein Testbau (zum Beispiel `0.3.0-alpha.8b` aus einem Arbeitszweig) stellt die Datenbank beim ersten Start auf ein neues Schema um; danach öffnet sie ein Programm von `main` nicht mehr. Testbauten deshalb nur mit einem **eigenen, neuen** `--data-dir` und `--config`, wie in den Beispielen unten (`C:\PK\http-test\…`, ein Ordner außerhalb von `C:\PostKauz`). Die Abnahme D8 macht das selbst ([abnahme_D8.md](abnahme_D8.md)).

1. Einen Schlüssel für den Test anlegen (nur lesen, einen Tag gültig; der Ordner `C:\PK\http-test` entsteht dabei neu):

   ```powershell
   C:\PostKauz\postkauz.exe key create --name http-test --role reader --expires 1d --data-dir C:\PK\http-test\data --config C:\PK\http-test\config.toml
   ```

   Erwartet: `key http-test created (…)` und darunter eingerückt der Schlüssel `pk_key_…`. Er erscheint nur dieses eine Mal; kopiere ihn.
2. Den Server starten (das Fenster bleibt offen):

   ```powershell
   C:\PostKauz\postkauz.exe mcp --http --data-dir C:\PK\http-test\data --config C:\PK\http-test\config.toml
   ```

   Erwartet: `mcp --http: listening on 127.0.0.1:8787 (auth key, resource http://127.0.0.1:8787/mcp); stop with Ctrl+C`
3. In einem **zweiten** PowerShell-Fenster prüfen, dass er antwortet:

   ```powershell
   (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8787/healthz).StatusCode
   ```

   Erwartet: `204`
4. Ohne Schlüssel wird abgewiesen:

   ```powershell
   try { Invoke-WebRequest -UseBasicParsing -Method Post -ContentType application/json -Body '{}' http://127.0.0.1:8787/mcp } catch { $_.Exception.Response.StatusCode.value__ }
   ```

   Erwartet: `401`
5. Claude Code am selben PC anbinden. Den Schlüssel **nicht** in die Befehlszeile schreiben: Dort landet er im Befehlsverlauf von PowerShell und im Klartext in der Claude-Konfiguration (`~/.claude.json`). Stattdessen ihn in eine Umgebungsvariable dieses Fensters einfügen (was du bei `Read-Host` eintippst oder einfügst, kommt nicht in den Verlauf) und in der Konfiguration nur den Namen der Variablen eintragen:

   ```powershell
   $env:POSTKAUZ_KEY = Read-Host 'Schluessel einfuegen'
   claude mcp add --transport http postkauz-http http://127.0.0.1:8787/mcp --header 'Authorization: Bearer ${POSTKAUZ_KEY}'
   claude
   ```

   Wichtig sind die **einfachen** Anführungszeichen: Dann setzt PowerShell nichts ein, und Claude Code ersetzt `${POSTKAUZ_KEY}` erst beim Verbinden durch den Wert aus seiner eigenen Umgebung (so steht es in der Claude-Code-Doku, Abschnitt „Environment variable expansion“). Mit doppelten Anführungszeichen würde PowerShell `${POSTKAUZ_KEY}` als eigene, leere Variable lesen. Claude Code deshalb aus **demselben** Fenster starten (`claude`); fehlt die Variable, warnt `claude mcp list`. In Claude Code zeigt `/mcp` dann `postkauz-http` mit 12 Tools (Rolle `reader`; das Test-Datenverzeichnis hat noch keine Mails, die Suche bleibt also leer).
6. Beenden: im ersten Fenster Strg+C. Den Testschlüssel widerrufen und den Testordner löschen:

   ```powershell
   C:\PostKauz\postkauz.exe key revoke http-test --data-dir C:\PK\http-test\data --config C:\PK\http-test\config.toml
   claude mcp remove postkauz-http
   Remove-Item -Recurse C:\PK\http-test
   ```

Hinweise: `--listen 127.0.0.1:9000` wählt einen anderen Port. Eine andere Adresse als Loopback verweigert PostKauz ohne `[http] allow_non_loopback = true`; im Modus `key` sind dann nur Tailnet-Adressen erlaubt (100.64.0.0/10 und `fd7a:115c:a1e0::/48`). Achtung: 100.64.0.0/10 ist der allgemeine Bereich für Carrier-Grade-NAT, nicht nur Tailscale; auch dein Internetanbieter kann dir eine solche Adresse geben. Trage deshalb in `[http] listen` (oder `--listen`) nur die **eigene Tailscale-Adresse** dieses Rechners ein (`tailscale ip -4`), keine andere Adresse aus diesem Bereich. `doctor` zeigt die Einstellungen in der Zeile `http:`. Ein Fehler im Abschnitt `[http]` (etwa ein Tippfehler) hält nur `mcp --http` an; alle anderen Kommandos, auch der Dauerbetrieb `sync --watch`, laufen weiter. `doctor` zeigt den Fehler dann als Zeile `INVALID`.

### 4.3 VPS einrichten (Linux, einmalig)

Für einen kleinen EU-VPS (Debian oder Ubuntu). Empfohlen ist eine Version mit systemd 257 oder neuer (zum Beispiel Debian 13): Nur dann trennt `PrivatePIDs=` den HTTP-Dienst ganz vom Sync-Dienst (Restrisiko sonst in 4.4). Als `root`:

1. Updates und automatische Sicherheitsupdates:

   ```bash
   apt-get update && apt-get -y upgrade && apt-get -y install unattended-upgrades
   ```

2. Nutzer ohne Anmeldung für PostKauz:

   ```bash
   useradd --system --home-dir /var/lib/postkauz --shell /usr/sbin/nologin postkauz
   ```

3. Datenträgerverschlüsselung: Wähle beim Anbieter eine verschlüsselte Platte oder lege `/var/lib/postkauz` auf ein LUKS-Volume. Die Datenbank enthält deine Mails.
4. Keine offenen Ports außer SSH: Die Firewall des Anbieters lässt nur Port 22 zu (besser auch den nur aus dem Tailnet, sobald Tailscale läuft). PostKauz und Authelia lauschen nur auf `127.0.0.1`.
5. Programm aus `dist/` (Release) hochladen und einrichten:

   ```bash
   install -m 0755 postkauz-linux-amd64 /usr/local/bin/postkauz
   install -d -m 0750 -o root -g postkauz /etc/postkauz
   install -d -m 0700 -o root -g root /etc/postkauz/credentials
   ```

   In `/etc/postkauz/credentials` liegen später die Passwortdateien (4.4); nur `root` kommt hinein.

6. Konfiguration `/etc/postkauz/config.toml` (Rechte 0640, root:postkauz), Beispiel:

   ```toml
   [http]
   listen = "127.0.0.1:8787"
   auth = "key"
   resource = "https://postkauz.<tailnet>.ts.net/mcp"
   ```

   `resource` ist die Adresse, die die Clients benutzen (aus 4.5). Ohne sie nimmt PostKauz nur Anfragen an `127.0.0.1:8787` an (Host-Prüfung, sonst 421).

### 4.4 Konten und Dienste (systemd)

Auf dem VPS gibt es keinen Schlüsselbund. Das Passwort eines Kontos steht in einer Datei, die nur `root` lesen kann. systemd liest sie beim Start des Sync-Dienstes selbst (als `root`, bevor es zum Nutzer `postkauz` wechselt) und gibt sie nur diesem Dienst als **Credential**: eine Datei im Arbeitsspeicher unter `/run/credentials/<dienst>/`, deren Ort PostKauz in `$CREDENTIALS_DIRECTORY` findet (`LoadCredential=`, Rückfrage R2; Microsoft-Konten mit OAuth2 bleiben auf den Windows-PCs). Das Passwort steht **nicht in der Umgebung** des Prozesses (früher `EnvironmentFile=`; die Umgebung war bis zum Selbstschutz des Programms über `/proc/<pid>/environ` lesbar). Nötig ist systemd 251 oder neuer (Ubuntu 24.04, Debian 12); für ältere siehe unten.

1. Je Konto ein Ordner mit einer Datei, die nur das Passwort enthält. Der Dateiname ist `PASSWORD_` plus der Kontoname in Großbuchstaben, `-` wird `_` (Konto `privat` → `PASSWORD_PRIVAT`, PostKauz liest daraus `POSTKAUZ_PASSWORD_PRIVAT`):

   ```bash
   install -d -m 0700 -o root -g root /etc/postkauz/credentials/sync-privat
   install -m 0600 -o root -g root /dev/null /etc/postkauz/credentials/sync-privat/PASSWORD_PRIVAT
   ```

   Als `root` mit einem Editor nur das Passwort hineinschreiben (ein Zeilenumbruch am Ende zählt nicht mit). Keine weiteren Dateien in diesen Ordner legen: systemd gibt jede Datei darin dem Dienst.
2. Konto anlegen (liest das Passwort aus dem Credential, speichert es nicht). Als `root`, über `systemd-run`: systemd liest die Passwortdatei selbst, wie später beim Sync-Dienst, und startet das Programm als Nutzer `postkauz`. So steht das Passwort nie in einer Befehlszeile (`ps`, Befehlsverlauf, das Protokoll von `sudo`). `StateDirectory=` legt dabei `/var/lib/postkauz` für den Nutzer `postkauz` an, wie es die Units später auch tun:

   ```bash
   systemd-run --pipe --wait --quiet --uid=postkauz --gid=postkauz \
     -p LoadCredential=POSTKAUZ:/etc/postkauz/credentials/sync-privat \
     -p StateDirectory=postkauz -p StateDirectoryMode=0700 -p UMask=0077 \
     -E POSTKAUZ_CONFIG=/etc/postkauz/config.toml -E POSTKAUZ_DATA_DIR=/var/lib/postkauz \
     /usr/local/bin/postkauz account add privat --imap imap.gmail.com:993 --user <adresse> --password-from-env
   ```

   Erwartet: eine Zeile, die mit `account privat added (imap.gmail.com:993, tls …` beginnt.
3. Units aus `deploy/systemd/` (im Release-Paket neben den Programmen) installieren und starten (statt `<konto>` den Kontonamen, z. B. `privat`; je Konto ein `postkauz-sync@<konto>.service`):

   ```bash
   install -m 0644 postkauz-sync@.service postkauz-mcp-http.service /etc/systemd/system/
   systemctl daemon-reload
   systemctl enable --now postkauz-sync@<konto>.service postkauz-mcp-http.service
   ```

4. Prüfen:

   ```bash
   systemctl --no-pager status postkauz-sync@<konto>.service postkauz-mcp-http.service
   curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8787/healthz
   ```

   Erwartet: beide `active (running)`, dann `204`. Log: `journalctl -u postkauz-mcp-http.service` und `/var/lib/postkauz/postkauz.log`. Abgewiesene Anfragen (falscher Schlüssel, zu viele Anfragen, fremder Host) stehen dort höchstens einmal je 10 Sekunden als eine Zeile `http requests rejected` mit Zählern; viele davon heißen: Jemand im Tailnet probiert etwas, oder ein Client hat einen alten Schlüssel.

Der MCP-Dienst hat keine `LoadCredential`-Zeile, sieht `/etc/postkauz/credentials` und `/run/credentials` gar nicht (`InaccessiblePaths=`) und darf nichts außer seinem Datenverzeichnis schreiben (`ProtectSystem=strict`, `NoNewPrivileges`). Ändere das nicht.

**Ältere systemd (249 oder 250, etwa Ubuntu 22.04):** Dort kennt `LoadCredential=` keinen Ordner, und der Sync-Dienst startet nicht (`Is a directory`). Dann je Konto die Zeile der Unit durch die Einzeldatei ersetzen:

```bash
systemctl edit postkauz-sync@<konto>.service   # für das Konto privat: privat statt <konto>
```

In den Editor (zwischen die Kommentarzeilen):

```ini
[Service]
LoadCredential=
LoadCredential=POSTKAUZ_PASSWORD_PRIVAT:/etc/postkauz/credentials/sync-privat/PASSWORD_PRIVAT
```

Die leere erste Zeile löscht die Ordner-Zeile der Vorlage. Beim Kontoanlegen in Schritt 2 dann `-p LoadCredential=POSTKAUZ_PASSWORD_PRIVAT:/etc/postkauz/credentials/sync-privat/PASSWORD_PRIVAT` statt der Ordner-Zeile.

**Grenze, offen gesagt:** Beide Dienste laufen als derselbe Nutzer `postkauz`, weil sie dieselbe Datenbank und dieselben Mail-Dateien brauchen und PostKauz diese bei jedem Start auf „nur der Besitzer“ setzt (0600 und 0700). Normalerweise darf ein Prozess Umgebung und Speicher eines anderen Prozesses desselben Nutzers lesen, und die Credentials eines Dienstes sind für dessen Nutzer lesbar. Davor schützen drei Dinge:

- **Kein Passwort in der Umgebung:** Es kommt als Credential, nicht über `/proc/<pid>/environ`.
- **Im Programm (jede systemd-Version):** Jeder PostKauz-Prozess sperrt sich unter Linux als **erste Anweisung** gegen das Auslesen (`PR_SET_DUMPABLE=0`); dann können nur noch Prozesse von `root` seinen Speicher lesen oder ihn untersuchen, und es entsteht kein Speicherabbild mit dem Passwort. Vor dieser ersten Anweisung bleibt ein Fenster von wenigen Millisekunden nach dem Start (gemessen in der zweiten Nachbesserung T5); weil das Passwort nicht in der Umgebung steht, ist dort nichts zu holen.
- **In der Unit:** Der MCP-Dienst sieht `/run/credentials` und die Passwortdateien nicht (`InaccessiblePaths=`). Ab systemd 257 (zum Beispiel Debian 13) hat er zusätzlich einen eigenen Prozessraum (`PrivatePIDs=yes`) und sieht den Sync-Dienst gar nicht. Ältere Versionen melden `Unknown key name 'PrivatePIDs'` und lassen nur diese Zeile weg; dann kann der MCP-Dienst den Sync-Dienst sehen und beenden (der startet nach 60 s neu); Umgebung und Speicher des Sync-Prozesses selbst bleiben gesperrt, aber nicht ganz alles (Restrisiko unten).

**Restrisiko ohne `PrivatePIDs` (systemd älter als 257, etwa Ubuntu 24.04 mit 255, Debian 12 mit 252):** Der MCP-Dienst sieht dann die Prozesse des Sync-Dienstes. Gesperrt ist nur der Sync-Prozess selbst, nicht seine Kindprozesse (`pdftotext`, der DOCX-Extraktor): Über deren `/proc/<pid>/root` sähe ein übernommener MCP-Dienst das Dateisystem des Sync-Dienstes und damit dessen Credential-Datei. Dazu bleibt ein `/proc/<pid>/mem`, das in den ersten Millisekunden nach dem Start (vor der ersten Anweisung) geöffnet wurde, für denselben Nutzer erreichbar. **Empfehlung für den VPS:** systemd 257 oder neuer (zum Beispiel Debian 13), oder zwei Systemnutzer, sobald das Programm sie kann (Roadmap).

**Restrisiko:** Wer den MCP-Dienst übernimmt (Fehler im Programm), schreibt mit denselben Rechten in dieselbe Datenbank. Er könnte dort den Server eines Kontos (`accounts.host`) auf einen eigenen ändern; der Sync-Dienst meldete sich beim nächsten Lauf dort mit dem Passwort an. Dagegen hilft erst die Trennung in zwei Systemnutzer (Roadmap, braucht ein Rechtemodell für Datenbank und Mail-Dateien). Bis dahin: `accounts list` zeigt den Server je Konto; ein unerwarteter Name dort heißt sofort Passwort ändern.

Prüfen:

```bash
systemctl --version | head -n 1
systemctl show -p PrivatePIDs -p InaccessiblePaths postkauz-mcp-http.service
PID=$(systemctl show -p MainPID --value postkauz-sync@<konto>.service)
grep -c POSTKAUZ_PASSWORD /proc/$PID/environ
sudo -u postkauz cat /proc/$PID/environ
```

Erwartet: die Version; `PrivatePIDs=yes` (bei systemd 257 oder höher) und `InaccessiblePaths=` mit `/etc/postkauz/credentials` und `/run/credentials`; beim dritten Befehl `0` (kein Passwort in der Umgebung, auch für `root` nicht); beim vierten `Permission denied` (der Sync-Prozess ist für den eigenen Nutzer gesperrt). Zwei getrennte Nutzer mit gemeinsamer Gruppe gehen erst nach einer Änderung im Programm (Rechte des Datenverzeichnisses, in der Roadmap vorgemerkt). Beide Units haben außerdem `ProtectProc=invisible` (Prozesse anderer Nutzer bleiben unsichtbar).

### 4.5 Tailscale und `tailscale serve`

1. Tailscale auf dem VPS installieren und anmelden (Anleitung auf tailscale.com, „Linux“); den Knoten `postkauz` nennen.
2. HTTPS im Tailnet einschalten (Admin-Konsole von Tailscale: DNS → MagicDNS an, „HTTPS Certificates“ an).
3. Weiterleitung einrichten (bleibt nach einem Neustart):

   ```bash
   tailscale serve --bg 8787
   ```

   Erwartet: eine Zeile mit `https://postkauz.<tailnet>.ts.net/` → `http://127.0.0.1:8787`. **Nie** `tailscale funnel`.
4. Von einem anderen Gerät im Tailnet:

   ```bash
   curl -sS https://postkauz.<tailnet>.ts.net/.well-known/oauth-protected-resource
   ```

   Erwartet: JSON mit `"resource":"https://postkauz.<tailnet>.ts.net/mcp"`.
5. ACL in der Admin-Konsole: Cloud-Sitzungen bekommen einen Tag (z. B. `tag:cloud`) und dürfen nur Port 443 des Knotens `postkauz` erreichen.

**Eine Cloud-Sitzung im Tailnet:** Sie tritt mit einem **ephemeren, zeitlich begrenzten Auth-Key** mit Tag bei (der Key kommt als Geheimnis in die Sitzung, nie ins Repository) und läuft ohne Root im Userspace-Modus (`tailscaled --tun=userspace-networking`); Programme in der Sitzung erreichen das Tailnet dann über den lokalen Proxy von `tailscaled`. Die Abnahme D8 (`docs/abnahme_D8.md`) zeigt die Kommandos. Claude Code bindet PostKauz dort im Modus `key` an: `claude mcp add --transport http postkauz https://postkauz.<tailnet>.ts.net/mcp --header 'Authorization: Bearer ${POSTKAUZ_KEY}'`. Die **einfachen** Anführungszeichen sind Pflicht: Mit doppelten setzt bash den Schlüssel sofort ein, und er stünde im Klartext in der Claude-Konfiguration; so trägt Claude Code nur `${POSTKAUZ_KEY}` ein und setzt den Wert erst beim Verbinden aus seiner Umgebung ein. Der Schlüssel (Rolle passend wählen, kurze Laufzeit) kommt als Geheimnis in die Sitzung, also als Umgebungsvariable `POSTKAUZ_KEY`.

### 4.6 Anmeldedienst Authelia (Modus `oauth`)

Für Claude Code am PC mit Browser. PostKauz selbst meldet niemanden an; das macht Authelia (Apache-2.0), PostKauz prüft nur das Token. Vorlagen: `deploy/docker/auth/` (Docker Compose, Konfiguration und Benutzerdatei mit Platzhaltern). Authelia läuft nur auf Linux (VPS, oder WSL2 auf einem Windows-PC).

1. Geheimnisse und Signaturschlüssel erzeugen (im Ordner mit den Vorlagen):

   ```bash
   openssl genrsa -out jwks.pem 2048
   for n in jwt session storage hmac; do echo "$n: $(openssl rand -hex 32)"; done
   ```

2. `configuration.example.yml` nach `configuration.yml` kopieren und die Platzhalter ersetzen: `<auth-host>` (z. B. `postkauz.<tailnet>.ts.net`), `<auth-url>` (z. B. `https://postkauz.<tailnet>.ts.net:8443`, das ist später `[http] issuer`), `<resource>` (genau `[http] resource`), `<callback-port>` (ein fester Port, z. B. `33418`), die vier Zufallswerte.
3. Ein Konto in `users.yml` (aus `users.example.yml`); den Passwort-Hash erzeugt:

   ```bash
   docker run --rm authelia/authelia:4.39.28 authelia crypto hash generate argon2
   ```

   **Empfohlen:** ein zweiter Faktor. Dafür in `configuration.yml` beim Client `authorization_policy: 'two_factor'` setzen und für das Konto in Authelia einen zweiten Faktor einrichten (TOTP-App oder WebAuthn). Die Vorlage steht auf `one_factor`, weil der Spike damit gemessen hat; für den ersten Test reicht das.

4. Starten und im Tailnet bereitstellen:

   ```bash
   docker compose -f deploy/docker/auth/docker-compose.yml up -d
   tailscale serve --bg --https=8443 9091
   ```

   Authelia ist dann unter `https://postkauz.<tailnet>.ts.net:8443` erreichbar, nur im Tailnet.
5. PostKauz umstellen, `/etc/postkauz/config.toml`:

   ```toml
   [http]
   listen = "127.0.0.1:8787"
   auth = "oauth"
   resource = "https://postkauz.<tailnet>.ts.net/mcp"
   issuer = "https://postkauz.<tailnet>.ts.net:8443"
   [http.principals]
   "platzhalter" = "cloud-reader"
   ```

   und `systemctl restart postkauz-mcp-http.service`.
6. Claude Code am PC:

   ```powershell
   claude mcp add --transport http --client-id claude-code --callback-port 33418 postkauz https://postkauz.<tailnet>.ts.net/mcp
   ```

   In Claude Code `/mcp` → `postkauz` → anmelden. Der Browser zeigt Authelia; anmelden und „Zustimmen“ klicken (einmal je Anmeldung, Befund B3).
7. **Subjekt zuordnen:** Beim ersten Mal antwortet PostKauz mit `403 forbidden`, weil das Subjekt noch unbekannt ist. Das Subjekt steht im Audit:

   ```bash
   sudo -u postkauz env POSTKAUZ_CONFIG=/etc/postkauz/config.toml POSTKAUZ_DATA_DIR=/var/lib/postkauz /usr/local/bin/postkauz audit --operation keys.authenticate --since 1h --json
   ```

   Den Wert von `params.via.sub` statt `platzhalter` in `[http.principals]` eintragen, Dienst neu starten, in Claude Code neu verbinden. Danach: `tools/list` nach der Rolle des Schlüssels.

Token leben 5 Minuten; Claude Code erneuert sie selbst mit dem Refresh-Token. Ein widerrufener Schlüssel (`key revoke cloud-reader`) sperrt den Zugang sofort, auch mit gültigem Token (`403 denied (key_revoked)`).

### 4.7 Docker statt systemd

`deploy/docker/` enthält ein statisches Image (nur die Programmdatei, Nutzer ohne Rechte) und eine Compose-Datei mit den beiden Diensten und einem gemeinsamen Volume:

```bash
make release VERSION=<version>
docker build -f deploy/docker/Dockerfile -t postkauz:<version> .
cd deploy/docker && mkdir -p config && install -m 0600 /dev/null sync.env
POSTKAUZ_VERSION=<version> docker compose up -d
```

`config/config.toml` wie in 4.3. `sync.env` enthält je Konto eine Zeile `POSTKAUZ_PASSWORD_<KONTO>=<passwort>` (Name in Großbuchstaben, `-` wird `_`); die Credential-Ordner aus 4.4 gelten nur für systemd, nicht für Docker (beide Dateien stehen in `.gitignore`). Im Container steht das Passwort in der Umgebung des Sync-Containers; der MCP-Container hat einen eigenen Prozessraum und sieht sie nicht. Ein Konto legst du mit `docker compose run --rm sync account add privat --imap … --user … --password-from-env` an; bis dahin startet der Dienst `sync` mit `no account configured yet` immer wieder neu. Die Wurzel der Container ist schreibgeschützt (nur `/data` und `/tmp`). Tailscale als Sidecar statt auf dem Host: zusätzlich `-f docker-compose.tailscale.yml` und eine Datei `tailscale.env` mit `TS_AUTHKEY=…`.

### 4.8 Updates und Sicherung auf dem VPS

- Update: neue Programmdatei nach `/usr/local/bin/postkauz`, dann `systemctl restart postkauz-sync@<konto>.service postkauz-mcp-http.service`. Vor Updates mit neuer Schemaversion eine Sicherung (Abschnitt 2).
- Sicherung: `postkauz backup create --to /pfad/auf/verschluesseltem/ziel` regelmäßig (Abschnitt 2), Audit-Export als Timer (`deploy/systemd/postkauz-audit-export.*`).

### 4.9 Vorläufig: ein Windows-PC als Knoten (bis zum VPS)

> **Achtung:** Dieser Abschnitt arbeitet mit deinem echten Datenverzeichnis. Das geht nur mit einem **Release von `main`**. Testbauten (Arbeitszweige, Abnahmen) laufen nur mit einem eigenen, neuen `--data-dir` und `--config` (siehe 4.2), sonst stellt der Testbau die Live-Datenbank auf ein neues Schema um.

Bis über einen VPS entschieden ist, kann ein PC des Betreibers der Knoten sein (Abnahme D8, vorläufig): Tailscale für Windows installieren, Dauer-Sync wie in Abschnitt 1, `mcp --http` in einem eigenen Fenster wie in 4.2 (eine geplante Aufgabe dafür kommt mit dem Betriebsentscheid), dann in einer PowerShell als normaler Nutzer `tailscale serve --bg 8787` und `[http] resource` auf den Namen des PCs im Tailnet setzen. Den Modus `oauth` braucht Authelia auf Linux (WSL2 oder VPS).

**Grenze unter Windows:** Sync und `mcp --http` laufen hier unter **demselben Windows-Benutzer**. Das Betriebssystem trennt sie nicht; der HTTP-Prozess kommt nur deshalb nicht an ein Passwort, weil PostKauz ihm keins gibt (Secrets-Speicher `denied`, siehe 4.1). Getrennte Benutzer für Sync und HTTP-MCP unter Windows sind für später vorgemerkt.

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
     kept (tagged): 2
   dry run: 17 messages would be deleted from the local archive; nothing changed
   ```

   `kept (imported archive)` sind Nachrichten aus `import` (z. B. dein Thunderbird-Archiv): Die bleiben immer. `kept (a draft refers to them)` sind Nachrichten, auf die ein Entwurf antwortet: Die bleiben ebenfalls. `kept (tagged)` sind Nachrichten, denen du (oder ein Agent) ein Tag gegeben hast, etwa `rechnung`: Ein Tag ist eine bewusste Markierung, deshalb löscht `purge` sie nicht (E5: „Tags überleben das Löschen auf dem Server“). Willst du eine davon doch loswerden, nimm erst das Tag ab (`tag remove rechnung <id>`) und lass `purge` danach noch einmal laufen.
2. Wirklich löschen: dasselbe Kommando ohne `--dry-run`. Erwartet zum Schluss:

   ```
   deleted: 17 messages, 7 attachment entries, 12 threads that became empty
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

Ohne `--dry-run` steht statt `would delete` die Zeile `deleted: …`. Danach meldet `doctor --blobs` bei `without reference` nur noch die frischen Dateien. Die Claude-App (MCP) darf dabei offen bleiben: Legt ein Agent gerade einen Entwurf mit Anhang an, wartet er kurz, und `blob gc` lässt dessen Datei stehen (Zeile `kept: … referenced or stored again during the run`).

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
