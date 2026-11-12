# Abnahme D14: Umstellung auf 0.3.0-rc6 (A08 „Anhang-Sperre“)

Stand: 12.11.2026 (Auftrag [A08](../auftraege/A08_anhang_sperre.md), Teil 3). Skript: [abnahme_D14.ps1](abnahme_D14.ps1) (Muster D13, nur das Nötige). Programm: Build **0.3.0-rc6** an `a711d93`, `postkauz-windows-amd64.exe` SHA256 `71077bea2556df444f3cbca1030d25fb8a482b8cb47e9f4ce1db881223f69a6c` (für `-ExpectSha256`; Nachbau und übrige Prüfsummen in [roadmap.md](roadmap.md), „Stand 12.11.2026“, und in `SHA256SUMS` neben dem Programm).

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft seit 10.11.2026 mit 0.3.0-rc5. D14 stellt ihn auf 0.3.0-rc6 um und zeigt an fünf Proben, dass die neue **Anhang-Sperre** wirkt: Eine harmlose Datei unter `attach_dir` lässt sich anhängen; eine Passwort-Datenbank (`probe.kdbx`), eine Datei in einem Ordner `_secrets`, eine Token-Datei (`probe-token.json`) und ein alternativer Datenstrom (`probe.txt:stream`) nicht. Alle Proben laufen mit `draft new --dry-run`: **keine Mail, kein Entwurf, kein APPEND.** Regeln und Grenzen: [sicherheit.md](sicherheit.md) Abschnitt 7, „Anhang-Sperre“.

**Dauer:** etwa 5 Minuten (mit `-BackupTo` länger, je nach Sicherung).

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, prüft die Syntax, startet das Skript und wertet das Protokoll aus.
- **[Du]** gibst den Tausch frei und startest danach die Claude-App neu.

## 1. Was das Skript tut und was nicht

- Wie D13: Live-Datenverzeichnis, Aufgaben aus `-Tasks` (**Pflicht**, Namen nicht raten) angehalten und am Ende die vorher eingeschalteten wieder an, **Umbenennen statt Überschreiben** mit `-KeepStdioMcp`: Das alte Programm wird `postkauz-rc5.exe`. Das geht nur, weil rc6 **keine Migration** hat (Schema 22, `doctor` vorher und nachher).
- **Keine Erwartungslisten, keine Sync-Läufe, kein Teil B:** Die Proben schreiben nichts. Den ersten Lauf mit rc6 macht der Dauerbetrieb nach 4.1 selbst.
- **Sicherung nur mit `-BackupTo`** (optional): A08 ändert weder Schema noch Daten. Wer trotzdem sichern will, gibt den Ordner an (verschlüsselter Datenträger, [betrieb.md](betrieb.md) 2.2).
- **Prüfordner:** Das Skript liest `attach_dir` aus `doctor --json`, legt darunter `postkauz-d14-probe` an (mit `_secrets\` darin) und entfernt am Ende **nur die Dateien und Ordner, die es selbst angelegt hat**, auch nach einem Abbruch (3.8 bzw. E.3). Gibt es den Ordner schon, bricht es ab (3.0); dann von Hand ansehen und löschen.
- **Konfiguration:** `[draft] attach_deny_dirs` und `attach_deny_files` dürfen während D14 **nicht** in `config.toml` stehen (0.8): rc5 kennt die Schlüssel nicht und startet mit ihnen nicht (Rückweg). Die Standardlisten wirken ohne sie. Eigene Ergänzungen erst nach bestandenem D14 eintragen ([cli.md](cli.md) Abschnitt 5).
- **Rückweg bei jedem FAIL:** wie D13 (E.0 vor dem Neustart der Aufgaben; die Aufgaben laufen nie mit dem neuen Programm an, wenn die Abnahme nicht bestanden ist).
- **Protokoll** `D14_windows.txt` neben dem Programm; ein vorhandenes wird als `D14_windows-<Zeitstempel>.txt` beiseitegelegt. Ins Protokoll kommen nur Prüfnummern, Exit-Codes, Gründe und Regelarten, **nie Pfade unter `attach_dir`**, Adressen oder Aufgabennamen (Aufgaben als laufende Nummer).

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d14` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc6, nicht umbenennen), `SHA256SUMS` und `abnahme_D14.ps1`.
2. **[Claude]** Syntax prüfen (das Skript ist auf dem Entwicklungsrechner nicht mit PowerShell gelaufen; dort gibt es keine); erwartet ist eine leere Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d14\abnahme_D14.ps1', [ref]$null, [ref]$e); $e
   ```

3. **[Claude]** Konten und Aufgaben wie in D13 nennen lassen; Probe-Konto wählen (Standard: das erste aus `-Accounts`; es entsteht darin nichts).
4. **[Claude]** In `config.toml` nachsehen, dass `attach_deny_dirs`/`attach_deny_files` fehlen.

## 3. Lauf

**Als welcher Benutzer?** Wie D13: derselbe Benutzer wie der Dauerbetrieb; erhöht nur, wenn die Aufgaben „Mit höchsten Privilegien ausführen“ haben (Aufgabenplanung → Aufgabe → Eigenschaften → Reiter „Allgemein“).

**Im Hintergrund starten**, danach das Protokoll mitlesen, bis die Schlusszeile `D14: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d14\abnahme_D14.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d14\postkauz-windows-amd64.exe','-ExpectSha256','71077bea2556df444f3cbca1030d25fb8a482b8cb47e9f4ce1db881223f69a6c','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-ProbeAccount','<konto1>','-KeepStdioMcp'
```

```powershell
Get-Content <Ordner des Programms>\D14_windows.txt -Wait
```

Bestanden ist die Abnahme nur mit der Schlusszeile `D14: <n> OK, 0 FAIL, <w> WARN, <s> SKIP`. Ein `SKIP 3.5` (kein Datenstrom möglich) ist kein Fehler; Claude nennt es im Bericht.

**Harter Abbruch** (Fenster geschlossen, PC neu gestartet): erst `postkauz.exe version` ansehen. Meldet es rc5, die Aufgaben von Hand einschalten (Aufgabenplanung → Aufgabe → Rechtsklick → „Aktivieren“, dann „Ausführen“; für „audit export“ nur „Aktivieren“). Meldet es rc6 oder fehlt `postkauz.exe`, den Rückweg nach Abschnitt 5 laufen lassen. Bleibt `postkauz-d14-probe` unter `attach_dir` liegen, ihn von Hand löschen (darin nur Probedateien mit harmlosem Text).

## 4. Erwartet

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Rechte | INFO: läuft die PowerShell erhöht |
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc6 passt zu `SHA256SUMS` und `-ExpectSha256`; das laufende Programm ist rc5 |
| 0.5 bis 0.7 | Konten, Aufgaben, Probe-Konto | `-Accounts` genau die Konten aus `accounts list`; jede Aufgabe vorhanden; Probe-Konto aus `-Accounts` |
| 0.8 | Konfiguration | kein `attach_deny_dirs`/`attach_deny_files` in `config.toml` |
| 1.1 | Aufgaben anhalten | jede abgeschaltet; außer dem stdio-MCP läuft kein `postkauz.exe` |
| 1.2, 1.2b | `doctor` vorher | `database ok`, Schema 22; INFO: Zeile `attach_guard` fehlt (rc5) |
| 1.3 | offene Entwürfe vorher | lesbar (Zahl) |
| 1.4 | Sicherung | nur mit `-BackupTo`: Exit 0 und `backup written to …`; sonst INFO |
| 2.1 | Tausch | rc6 läuft, `postkauz-rc5.exe` daneben |
| 2.2 | `doctor` nachher | `database ok`, Schema unverändert (22), keine neue Warnung |
| 2.3 | Zeile `attach_guard` | da, z. B. `attach_guard: 19 dir rules, 34 file rules` |
| 2.4 | `attach_dir` | aus `doctor --json` gelesen, vorhanden |
| 3.0 | Prüfordner | `postkauz-d14-probe` gibt es vorher nicht |
| 3.1 | harmlose Datei | `draft new --dry-run` Exit 0 |
| 3.2 | `probe.kdbx` | Exit 3, `attach_denied`, Regelart `file`, kein Pfad in der Ausgabe |
| 3.3 | `_secrets\probe.txt` | Exit 3, `attach_denied`, Regelart `dir` |
| 3.4 | `probe-token.json` | Exit 3, `attach_denied`, Regelart `file` |
| 3.5 | `probe.txt:stream` | Exit 3, `attach_denied`, Regelart `stream`; SKIP, wenn das Dateisystem keinen Datenstrom zulässt |
| 3.6 | nichts angelegt | gleich viele offene Entwürfe wie in 1.3 |
| 3.7 | Audit | so viele `drafts.create` mit `denied`/`attach_denied` seit dem Start wie Ablehnungs-Proben (3 oder 4) |
| 3.8 | Aufräumen | Prüfdateien und -ordner entfernt (nur die eigenen) |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.0 | nach FAIL | altes Programm zurück (`postkauz.exe` meldet rc5) |
| 4.1 bzw. E.1 | Aufgaben wieder an | wie D13: je Aufgabe gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); je Konto ein `sync --watch` genau mit `-Exe`; `postkauz.exe` meldet rc6 |
| Stand | letzte Zeilen | welche Version an `postkauz.exe` liegt und welche als `postkauz-rc5.exe` |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt. Erst dann gilt die Sperre auch für Entwürfe, die Claude über MCP anlegt.

**Nach bestandenem D14 (optional):** eigene Ergänzungen in `config.toml` unter `[draft]` eintragen, z. B. `attach_deny_files = ["*.xlsm"]`, und mit `postkauz doctor` prüfen (die Zahl der Regeln steigt). Vor einem späteren Rücktausch auf rc5 die Zeilen wieder entfernen.

## 5. Rückweg (nur bei Problemen)

A08 hat **keine Migration**. Vorher `attach_deny_dirs`/`attach_deny_files` aus `config.toml` entfernen, falls gesetzt (R.0e prüft das).

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d14\abnahme_D14.ps1 -Rueckweg `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp
```

(mit denselben `-Exe`/`-DataDir`/`-Config` wie oben, im Hintergrund wie in Abschnitt 3). Erwartet: `R.0` bis `R.0e`, `R.1b` (neues Programm unter freiem Namen beiseite, meist `postkauz-rc6.exe`; fehlt `postkauz.exe`, nur das alte an seine Stelle), `R.2` rc5 mit Schema 22, `R.3` je Konto ein `sync` mit Exit 0 **und** `errors` 0 (eine Wiederholung erlaubt, dann WARN), `R.5` alle Aufgaben aus `-Tasks` wieder an. Mit rc5 gilt die Anhang-Sperre nicht mehr (nur die Betriebsregel).
