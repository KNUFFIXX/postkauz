# Betrieb von PostKauz

Für: den Betreiber. Diese Anleitung erklärt, wie PostKauz im Alltag läuft: Schritt für Schritt, je Schritt ein Kommando und die Ausgabe, die du erwarten kannst. Programmierkenntnisse brauchst du nicht. Stand: T2 (0.3.0-alpha.3, 28.10.2026).

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
- Lehnt der Mailserver die Anmeldung ab (falsches Passwort, Exit-Code 6), wartet es immer 30 Minuten und schreibt das ins Log. So sperrt der Anbieter dein Konto nicht wegen vieler Fehlversuche.
- Ein Konto mit Problem (Anmeldung, fehlendes Passwort) hält die anderen Konten nicht an.

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
| `[verein] stopped: provider: authentication failed …; the other accounts go on` | Der Server hat die Anmeldung dieses Kontos abgelehnt. Die anderen Konten laufen weiter. | Passwort prüfen und neu speichern (`account set verein --password-stdin`), dann Aufgabe neu starten (1.6). |
| `postkauz.exe ended with exit code 6 … Not retrying quickly` | Alle Konten scheiterten an der Anmeldung. Das Skript wartet 30 Minuten. | Wie oben; danach Aufgabe neu starten. |
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

### 1.8 Zusammenspiel mit dem Audit-Export

Beide Aufgaben vertragen sich: Der Audit-Export liest nur das Audit und schreibt Dateien an den zweiten Ort; der Dauer-Sync hält nur die Sperren der Konten. Richte den Export ein, wie im README („Daily audit export“) beschrieben, mit **denselben** Pfaden:

```powershell
powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -DataDir C:\PostKauz\data -ExportDir "G:\My Drive\PostKauz-Audit"
```

Erwartet am Ende: `== OK: export written to G:\My Drive\PostKauz-Audit …`. Ein `sync --watch`, der über Mitternacht (UTC) läuft, schreibt seinen Abschluss erst am Ende; der Export ordnet ihn dem nächsten offenen Tag zu (`docs/cli.md`, Abschnitt 2f). `audit verify` bleibt dabei `ok`.

### 1.9 MCP-Server ohne Passwort, Sync über Claude anstoßen

Der MCP-Server, den die Claude-App startet, kennt **kein** Mailpasswort (ADR-009). Er liest den Spiegel und schreibt Entwürfe nur lokal. Ein Agent kann deshalb nicht selbst synchronisieren. Er kann aber mit dem Tool `mail_sync_now` einen Sync **anfordern**: Das legt einen Auftrag (Job `sync`) in der Datenbank an, und der Dauer-Sync führt ihn binnen etwa 10 Sekunden aus.

- Dafür braucht der Schlüssel des Agenten den Scope `mail:sync` (Rollen `assistant`, `operator`, `admin`; `reader` nicht).
- Läuft gerade kein Dauer-Sync, wartet der Auftrag bis zum nächsten `postkauz.exe sync` dieses Kontos.
- Wird der Schlüssel inzwischen widerrufen, läuft der Auftrag nicht (Logzeile `refused (key_revoked)`).

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

Folgt mit Teil T3 (`backup`, `restore`, Löschkonzept).

## 3. Microsoft-Postfächer

Folgt beim Zusammenführen von Teil T4. Bis dahin: [betrieb_microsoft.md](betrieb_microsoft.md).

## 4. Betrieb auf einem VPS

Folgt mit Teil T5 (Linux-Server, systemd, Fernzugriff über HTTP-MCP).
