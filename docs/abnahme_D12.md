# Abnahme D12: Umstellung auf 0.3.0-rc4 (A06 „Alltag 2: Adressen, Zitat, Erkennung“)

Stand: 05.11.2026 (Auftrag [A06](../auftraege/A06_alltag2.md), Teil 6). Skript: [abnahme_D12.ps1](abnahme_D12.ps1). Programm: Build **0.3.0-rc4** (Prüfsummen in [protokolle/A06_stand.md](protokolle/A06_stand.md)).

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft seit 03.11.2026 mit 0.3.0-rc3. D12 stellt ihn auf 0.3.0-rc4 um und prüft dabei A06 **am echten Bestand**:

1. Antwort-Entwürfe an Absender mit Komma oder @ im Namen lassen sich wieder ändern und vorlegen (Teil 1).
2. Antworten, die du in Gmail im Browser unverändert gesendet hast, erkennt PostKauz auch dann, wenn Gmail innere Zitatzeilen neu umbricht (Teil 3). Offene Entwürfe von vor der Umstellung prüft der erste Lauf einmal neu.
3. Entwürfe, die du in Gmail fast ganz neu geschrieben und gesendet hast, schließt der Sync als „ersetzt“ (`abandoned`, Grund `replaced`; Teil 4).
4. **Teil B (Mitmach):** Ob Gmail mit der neuen Form des Zitats (Teil 2) „>“ vor jede Zitatzeile schreibt, misst eine Antwort auf eine eigene Mail.

**Dauer:** Teil A etwa 20 Minuten (meist die Sicherung), Teil B etwa 10 Minuten.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, ermittelt vorher die Erwartung (Abschnitt 2), startet das Skript und wertet das Protokoll aus. In Teil B sendet Claude den Entwurf in Gmail im Browser auf deine Anweisung (wie D5).
- **[Du]** bestätigst die Erwartung (welche offenen Entwürfe gesendet bzw. ersetzt sind), am Ende die Liste der Abschlüsse, und gibst Teil B frei.

## 1. Was das Skript tut und was nicht

- Teil A arbeitet am **Live-Datenverzeichnis** wie D11: Sicherung nach [betrieb.md](betrieb.md) 2.2 vorher, ohne Sicherung kein Tausch. Es hält die Aufgaben aus `-Tasks` an (**Pflicht**, Namen nicht raten: der Büro-PC hat drei Aufgaben „sync watch“ je Konto, „audit export“ und „mcp http“) und schaltet am Ende die vorher eingeschalteten wieder ein.
- **Umbenennen statt Überschreiben** mit `-KeepStdioMcp` wie D11 Abschnitt 6: Das alte Programm wird `postkauz-rc3.exe`, das neue kommt an seine Stelle. Der MCP der Claude-App läuft bis zu ihrem nächsten Neustart mit dem alten Programm weiter. Das geht nur, weil rc4 **keine Migration** hat (Schema 22; das Skript prüft das vorher und nachher mit `doctor`).
- **Erwartung statt fester IDs:** `-ExpectSent` (z. B. `3,15`) sind die offenen Entwürfe, die der erste Lauf als `sent_by_human` schließen soll, `-ExpectReplaced` (z. B. `4`) die, die er als ersetzt schließen soll. Das Skript listet **alle** im ersten Lauf geschlossenen Entwürfe (ID, Status, Grund aus dem Audit). Ein Abschluss, der nicht erwartet war, ist FAIL (3.2b), ebenso ein erwarteter, der fehlt (3.2c).
- **Adressen:** `-CommaDraft` ist ein **bestehender** offener Antwort-Entwurf, dessen Empfängername ein Komma oder ein @ enthält. Das Skript prüft `draft edit <id> --dry-run` ohne Änderung (mit dem gespeicherten Wert von `--quote`) und `draft request <id> --dry-run`, beide ohne `invalid_address`. Es legt **keinen** neuen Entwurf an.
- **Rückweg bei jedem FAIL** (seit Nachbesserung 1): Gibt es nach dem Tausch ein FAIL oder eine Ausnahme, legt das Skript **vor** dem Neustart der Aufgaben das alte Programm zurück (E.0): Das neue wird in `postkauz-rc4.exe` umbenannt (nie gelöscht; eine laufende Exe lässt sich umbenennen), das alte kommt zurück; das Ergebnis ist eine eigene Prüfung. Die Aufgaben laufen nie mit dem neuen Programm an, wenn die Abnahme nicht bestanden ist; misslingt die Rückkopie, bleiben sie aus (E.1 FAIL, dann von Hand nach Abschnitt 5). Im ersten Lauf geschlossene Entwürfe bleiben geschlossen (Abschnitt 4a).
- **Prozesse:** Beendet werden nur `postkauz.exe`, deren Programm im Ordner von `-Exe` liegt; andere nennt das Skript mit PID und Pfad, beendet sie aber nicht (dann bricht 1.1 ab).
- Ins Protokoll kommen nur IDs, Zustände, Gründe und Zähler, nie Betreff, Adressen oder Mailtext. Von `sync` stehen nur Exit-Code und die **Zahl** der stderr-Zeilen drin.

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d12` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc4, nicht umbenennen), `SHA256SUMS` und `abnahme_D12.ps1`.
2. **[Claude]** Das Skript ist auf dem Entwicklungsrechner nicht mit PowerShell geprüft worden (dort gibt es keine). Vor dem ersten Lauf die Syntax prüfen; erwartet ist eine leere Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d12\abnahme_D12.ps1', [ref]$null, [ref]$e); $e
   ```

3. **[Claude]** Konten und Aufgaben des Dauerbetriebs nennen lassen:

   ```powershell
   C:\PostKauz\postkauz.exe accounts list --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
   ```

   ```powershell
   Get-ScheduledTask | Where-Object TaskName -match 'PostKauz' | Select-Object TaskName, State
   ```

4. **[Claude]** Die offenen Entwürfe mit rc3 ansehen (`draft list`, je Kandidat `draft show <id>`) und mit dem Betreiber festlegen: welche wurden unverändert gesendet (→ `-ExpectSent`), welche hat er umgeschrieben und gesendet (→ `-ExpectReplaced`). Grundlage sind die drei Fälle aus dem Dauerbetrieb 03.–05.11.2026.
5. **[Claude]** Einen offenen Antwort-Entwurf mit Komma oder @ im Empfängernamen suchen (`draft show <id> --json`, Feld `to`); seine ID ist `-CommaDraft`. Mit rc3 endet `draft edit <id> --quote=true --dry-run` dort mit `invalid_address`.
6. **[Claude]** Den Sicherungsordner wählen: der verschlüsselte Sicherungsdatenträger, z. B. `E:\PostKauz-Sicherung`.

## 3. Lauf Teil A

In einer PowerShell als **derselbe Benutzer**, unter dem der Dauerbetrieb läuft (nicht „Als Administrator“). Platzhalter in spitzen Klammern:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d12\abnahme_D12.ps1 `
  -Exe <Ordner des Programms>\postkauz.exe -DataDir <Datenordner> -Config <Datenordner>\config.toml `
  -NewExe C:\PostKauz\d12\postkauz-windows-amd64.exe -BackupTo <Sicherungsordner> `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" `
  -Accounts <konto1>,<konto2>,<konto3> -ExpectSent <ids> -ExpectReplaced <ids> -CommaDraft <id> -KeepStdioMcp
```

Ohne abweichende Pfade entfallen `-Exe`, `-DataDir` und `-Config` (Standard `C:\PostKauz\…`). Das Protokoll steht danach in `D12_windows.txt` neben dem Programm (anders mit `-LogFile`). Letzte Zeile: `D12: <n> OK, 0 FAIL`.

## 4. Erwartet (Teil A)

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc4 passt zu `SHA256SUMS`, das laufende Programm ist rc3 |
| 0.5 bis 0.8 | Sicherungsordner, Konten, Aufgaben, Erwartung | angegeben; jedes Konto aus `-Accounts` steht in `accounts list`; jede Aufgabe aus `-Tasks` vorhanden; keine ID in beiden Listen |
| 1.1 | Aufgaben anhalten | außer dem stdio-MCP der Claude-App läuft kein `postkauz.exe` |
| 1.2, 1.2b | `draft list` vorher | lesbares JSON, weniger als 500 offene Entwürfe (sonst Abbruch vor dem Tausch); jeder erwartete Entwurf ist offen |
| 1.3 | `doctor` vorher | `database ok`, Schema 22 |
| 1.4, 1.4b | Entwurf `-CommaDraft` | offene Antwort, `remote` `pushed` oder `pending`, nicht in `-ExpectSent`/`-ExpectReplaced`, ein Empfängername mit Komma oder @ oder gleich der Adresse (welcher Fall, merkt sich das Skript für 3.4c); sonst Abbruch vor dem Tausch. Mit rc3 `invalid_address` (INFO) |
| 1.5 | Sicherung | Exit 0 **und** `backup written to …` |
| 2.1, 2.2 | Tausch, `doctor` | rc4 läuft, `postkauz-rc3.exe` liegt daneben; `database ok`, Schema 22 |
| 3.1 | `sync -a <Konto> --json` je Konto | Exit 0; die Zeile nennt `sent_by_*`, `replaced_in_thread`, `missing`, `taken_over` |
| 3.2 | alle Abschlüsse im ersten Lauf | jeder mit Grund im Audit: `sent_by_human` mit `message_id`, `fallback` oder `thread`, `abandoned` mit `replaced`; jeder vorher offene Entwurf, der nicht mehr offen ist, einzeln mit `draft show` gelesen (unabhängig von der Grenze von `draft list`); einer, der weder offen noch geschlossen lesbar ist, zählt als unerwartet |
| 3.2b, 3.2c | Vergleich mit der Erwartung | kein nicht erwarteter Abschluss; jeder erwartete wie erwartet |
| 3.3 | **[Du]** | Stimmen die Abschlüsse? (`draft show <id>` zeigt den Betreff am Bildschirm; ein ersetzter Entwurf zeigt `closed: …, abandoned: replaced by an own reply in the thread (message <id>)`) |
| 3.4 bis 3.4d | Adressen | `draft edit --dry-run` Exit 0, `draft request --dry-run` ohne `invalid_address`; 3.4c: ein Name gleich der Adresse erscheint als bloße Adresse, ein Name mit Komma oder @ in Anführungszeichen; kein neuer Entwurf |
| 3.5 | `doctor` nachher | keine neue Warnung, Schema 22 (Nachweis: keine Migration) |
| 3.6 | `audit verify` | `ok` |
| E.0 | nur nach einem FAIL | altes Programm zurück (`postkauz.exe` meldet rc3, das neue liegt als `postkauz-rc4.exe` daneben) |
| 4.1 bzw. E.1 | Aufgaben wieder an | die vorher eingeschalteten laufen wieder (4.1 ohne FAIL mit rc4, E.1 nach einem Abbruch oder FAIL mit rc3) |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt.

**Folge des Updates für offene Antwort-Entwürfe mit Zitat** (wie Entschiedene Fragen 70 (1), (2), (4)): rc4 baut das Zitat im HTML neu (Gmails Form, Teil 2); der Klartext bleibt gleich. Ein vorgelegter Entwurf (`pending_approval`) scheitert deshalb bei `approve` mit `approval_stale` (dann `draft request <id>` erneut), ein freigegebener (`ready`) fällt beim nächsten Ablegen auf `draft` zurück, und `draft show --eml` meldet `changed after the copy …`, bis der Entwurf neu abgelegt ist. `preview_hash` und die Übernahme-Erkennung bleiben stabil.

**Bis zum Neustart der Claude-App** läuft ihr MCP (`-KeepStdioMcp`) mit dem alten Programm (rc3) weiter. Legt Claude in dieser Zeit über die App einen Antwort-Entwurf an, hat er noch das alte Zitat-HTML; rc4 baut ihn bei `approve` neu, und die Freigabe endet mit `approval_stale` (dann `draft request <id>` erneut). Deshalb die App direkt nach D12 neu starten.

**Was „ersetzt“ heißt:** Ein offener Antwort-Entwurf, dessen Kopie schon in einem früheren Lauf als verschwunden festgestellt wurde (`remote: missing`), und zu dem es eine eigene, **nach** der letzten Ablage gesendete Antwort im selben Verlauf an dieselben Empfänger gibt, deren Text aber anders ist, wird `abandoned` (nicht „gesendet“: sein Text ging nicht hinaus). Liegt im Entwürfe-Ordner noch eine neuere Antwort auf dasselbe Original (in Gmail weiterbearbeitet), bleibt er offen. Regeln: [versanderkennung.md](versanderkennung.md) Abschnitt 3b. Ein Entwurf, dessen Kopie erst in diesem ersten Lauf als fehlend erkannt wird, kann frühestens im nächsten Lauf (im Dauerbetrieb wenige Minuten später) ersetzt werden; in D12 bleibt er dann offen, und die Erwartung für ihn gehört nicht in `-ExpectReplaced`.

## 4a. Wenn der erste Lauf einen Entwurf unerwartet schließt

Das Skript meldet das als FAIL (3.2b) und legt das alte Programm zurück; die Aufgaben laufen mit rc3 weiter. **Der Abschluss selbst bleibt:** Auch der Rückweg ändert ihn nicht, denn rc3 öffnet keine geschlossenen Entwürfe. Rückgängig macht ihn nur das Zurückspielen der Sicherung von Schritt 1.5 nach [betrieb.md](betrieb.md) 2.5; dabei geht alles verloren, was seitdem gespiegelt oder geändert wurde (der Spiegel holt Mails beim nächsten Sync wieder, lokale Änderungen wie neue Entwürfe, Tags oder Freigaben seit der Sicherung nicht).

**Default:** so lassen. Der betroffene Entwurf ist nur ein Zustand in PostKauz (es wurde nichts gesendet und nichts im Postfach gelöscht); Claude nennt ID, Status und Grund im Bericht, und der Betreiber legt bei Bedarf einen neuen Entwurf an. Die Sicherung zurückspielen nur, wenn der Betreiber das ausdrücklich will.

## 5. Rückweg (nur bei Problemen)

A06 hat **keine Migration**. Das alte Programm läuft mit der Datenbank weiter (`doctor`: `database ok`, Schema 22); eine Wiederherstellung der Sicherung ist nicht nötig. Als ersetzt geschlossene Entwürfe bleiben geschlossen.

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d12\abnahme_D12.ps1 -Rueckweg `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp
```

(mit denselben `-Exe`/`-DataDir`/`-Config` wie in Teil A). Erwartet: `R.0` bis `R.0d`, `R.2` rc3 mit `database ok` und Schema 22, `R.3` je Konto ein `sync` ohne Fehler, `R.5` Aufgaben wieder an. Das neue Programm liegt danach als `postkauz-rc4.exe` daneben.

## 6. Teil B: Gmail-Zitat messen (Mitmach)

**Ziel:** Zeigen, ob Gmail im Browser mit der neuen Form des Zitats (`gmail_quote`, Teil 2) „> “ vor jede Zitatzeile schreibt. Bis dahin sichert Teil 3 die Erkennung ab.

1. **[Claude]** Eine eigene Mail an sich selbst wählen, die selbst ein „>“-Zitat enthält (sonst gibt es keine innere Ebene), z. B. eine frühere Antwort an sich selbst. Ihre lokale ID ist `-SelfMessage`. Die Aufgaben laufen (`sync --watch`).
2. **[Claude]** Teil B starten:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d12\abnahme_D12.ps1 -TeilB -SelfMessage <id>
   ```

   Das Skript legt den Antwort-Entwurf an (B.3), wartet, bis `sync --watch` ihn abgelegt hat (B.4), und schreibt dann `INFO B.5 JETZT: Entwurf <id> … UNVERAENDERT senden`. Weil es danach wartet, startet Claude es im Hintergrund (etwa `Start-Process powershell.exe -ArgumentList …`) und liest `D12_teilB.txt`, bis die Zeile B.5 dasteht.
3. **[Claude auf Anweisung des Betreibers]** In Gmail im Browser den Entwurf öffnen und **unverändert** senden.
4. Das Skript wartet bis zu 20 Minuten (`-WaitMinutes`) auf den Sync, findet die gesendete Nachricht über `sent_message_id` des Entwurfs (sonst über Betreff und Datum) und schreibt ohne Inhalte:
   - `B.6`: Status und Grund, mit dem der Entwurf geschlossen wurde (erwartet `sent_by_human`, meist `fallback`).
   - `B.8`: Zahl der Zeilen ab der Einleitungszeile **mit** und **ohne** „>“ (leere Zeilen nicht gezählt). Mit der neuen Form erwartet: alle mit „>“. Stehen dort noch Zeilen ohne „>“, schreibt Gmail das Zitat weiter ohne „>“; dann bleibt Teil 3 die Absicherung.

Das Protokoll steht in `D12_teilB.txt` neben dem Programm.
