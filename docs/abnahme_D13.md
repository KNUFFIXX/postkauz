# Abnahme D13: Umstellung auf 0.3.0-rc5 (A07 „Alltag 3: Versanderkennung schließen, Kopien aufräumen“)

Stand: 10.11.2026 (Auftrag [A07](../auftraege/A07_alltag3.md), Teil 7). Skript: [abnahme_D13.ps1](abnahme_D13.ps1) (Muster D12, mit den Verbesserungen aus Sammelpunkt 0e). Programm: Build **0.3.0-rc5** an `69117c5`, `postkauz-windows-amd64.exe` SHA256 `97ab3d84573ca1a5efc7e3b41000601da5929e5bdb6ca41441b2769048231a78` (für `-ExpectSha256`; Prüfsummen und Prüfläufe in [protokolle/A07_stand.md](protokolle/A07_stand.md) Abschnitt 5).

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft seit 05.11.2026 mit 0.3.0-rc4. D13 stellt ihn auf 0.3.0-rc5 um und prüft A07 **am echten Bestand**:

1. Ein Antwort-Entwurf ohne Empfänger (`derive_recipients: false`), den du in Gmail adressiert und gesendet hast, wird über den Verlauf geschlossen (Teil 1).
2. Von zwei Fassungen derselben Antwort bekommt bei gleichem Text die übernommene bzw. jüngere die gesendete Nachricht; eine übernommene Fassung, deren Kopie ganz weg ist, wird `missing` und im **nächsten** Lauf ersetzt (Teil 2).
3. Eine neue Mail, die du in Gmail um einen Satz ergänzt hast, wird über den Betreff erkannt (Teil 3, Grund `subject`).
4. Ein offener Entwurf, dessen Kopie seit mehr als 24 Stunden fehlt und den kein Weg erkennt, wird `abandoned` mit Grund `copy_gone` (Teil 4).
5. **Teil B (Mitmach):** `drafts.supersede` an zwei eigenen Probe-Entwürfen an dich selbst.

**Dauer:** Teil A etwa 25 Minuten (meist die Sicherung), Teil B etwa 10 Minuten.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, ermittelt vorher die Erwartung (Abschnitt 2), startet das Skript und wertet das Protokoll aus. In Teil B sendet Claude den zweiten Probe-Entwurf in Gmail im Browser auf deine Anweisung (wie D5, D12).
- **[Du]** bestätigst die Erwartung (welche offenen Entwürfe gesendet, ersetzt oder `copy_gone` werden), am Ende die Liste der Abschlüsse, und gibst Teil B frei.

## 1. Was das Skript tut und was nicht

- Wie D12: Live-Datenverzeichnis, Sicherung vorher (ohne Sicherung kein Tausch), Aufgaben aus `-Tasks` (**Pflicht**, Namen nicht raten) angehalten und am Ende die vorher eingeschalteten wieder an, **Umbenennen statt Überschreiben** mit `-KeepStdioMcp`: Das alte Programm wird `postkauz-rc4.exe`. Das geht nur, weil rc5 **keine Migration** hat (Schema 22, `doctor` vorher und nachher).
- **Zwei vollständige Läufe** nach dem Tausch (3.1 und 3.3): Eine übernommene Fassung, deren Kopie verschwunden ist, wird im ersten Lauf `missing` und erst im zweiten ersetzt (A07 Teil 2 b). Nach **jedem** Lauf listet das Skript alle seit dem Tausch geschlossenen Entwürfe (ID, Status, Grund aus dem Audit; 3.2, 3.4); ein nicht erwarteter Abschluss ist FAIL (3.2b, 3.4b). Nach dem zweiten Lauf vergleicht es die Summe mit der Erwartung (3.4c).
- **Erwartung statt fester IDs:** `-ExpectSent` (gesendet: Grund `message_id`, `fallback`, `thread` oder `subject`), `-ExpectReplaced` (ersetzt, Grund `replaced`; vorher `remote: missing` oder `taken_over`), `-ExpectCopyGone` (Auffangregel, Grund `copy_gone`; vorher `remote: missing`, und zwar seit mehr als 24 Stunden: Zeitpunkt aus dem Audit, siehe [versanderkennung.md](versanderkennung.md) Abschnitt 3d).
- **„errors 0“ mit einer Wiederholung** (Sammelpunkt 0e): Endet ein `sync -a <Konto>` mit einem Fehler, wiederholt das Skript ihn sofort einmal. Endet die Wiederholung ohne Fehler, ist das nur `WARN` (vorübergehender Serverfehler); sonst FAIL.
- **Rückweg bei jedem FAIL**: wie D12 (E.0 vor dem Neustart der Aufgaben; die Aufgaben laufen nie mit dem neuen Programm an, wenn die Abnahme nicht bestanden ist).
- **Konfiguration:** `[draft] copy_gone_after` darf **nicht** in `config.toml` stehen (0.9): rc4 kennt den Schlüssel nicht und startet mit ihm nicht (unbekannte Schlüssel sind ein Fehler). Der Standard von rc5 (24 Stunden) gilt ohne den Schlüssel. Im Dauerbetrieb also nicht setzen; vor einem Rücktausch entfernen.
- **Protokoll:** Ein vorhandenes `D13_windows.txt` wird nicht fortgeschrieben, sondern als `D13_windows-<Zeitstempel>.txt` beiseitegelegt. Schreibfehler am Protokoll brechen nichts ab (die Schlusszeile sagt es).
- Ins Protokoll kommen nur IDs, Zustände, Gründe und Zähler, nie Betreff, Adressen oder Mailtext.

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d13` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc5, nicht umbenennen), `SHA256SUMS` und `abnahme_D13.ps1`.
2. **[Claude]** Syntax prüfen (das Skript ist auf dem Entwicklungsrechner nicht mit PowerShell gelaufen; dort gibt es keine); erwartet ist eine leere Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d13\abnahme_D13.ps1', [ref]$null, [ref]$e); $e
   ```

3. **[Claude]** Konten und Aufgaben nennen lassen (wie D12 Abschnitt 2, Schritt 3).
4. **[Claude]** Die offenen Entwürfe mit rc4 ansehen (`draft list --json`, je Kandidat `draft show <id>`) und mit dem Betreiber festlegen:
   - unverändert oder leicht geändert gesendet, auch Antworten ohne Empfänger und neue Mails mit Nachsatz → `-ExpectSent`;
   - fast ganz neu geschrieben und gesendet, auch die ältere von zwei Fassungen, deren Kopie ganz weg ist → `-ExpectReplaced`;
   - Kopie seit mehr als 24 Stunden weg, sonst nichts passt → `-ExpectCopyGone`. Der Zeitpunkt ist der jüngste Audit-Eintrag `drafts.remote_check` mit Grund `missing`: `postkauz audit --operation drafts.remote_check --since 30d --json` zeigt ihn je `draft:<id>`.
5. **[Claude]** Sicherungsordner wählen (verschlüsselter Datenträger, z. B. `E:\PostKauz-Sicherung`).

## 3. Lauf Teil A

**Als welcher Benutzer?** (Sammelpunkt 0e, geklärt) Als **derselbe Benutzer**, unter dem der Dauerbetrieb läuft. Ob die PowerShell erhöht laufen muss, hängt davon ab, wie die Aufgaben angelegt sind: Sind sie mit „Mit höchsten Privilegien ausführen“ angelegt (Aufgabenplanung → Aufgabe → Eigenschaften → Reiter „Allgemein“), darf nur eine erhöhte PowerShell sie abschalten; dann „Als Administrator“ starten. Sonst eine gewöhnliche PowerShell. Das Skript schreibt in `0.0`, ob es erhöht läuft, und meldet in `1.1` ein FAIL je Aufgabe, die sich nicht abschalten ließ (dann wird nichts getauscht).

**Im Hintergrund starten** (wie D12), danach das Protokoll mitlesen, bis die Schlusszeile `D13: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d13\abnahme_D13.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d13\postkauz-windows-amd64.exe','-ExpectSha256','97ab3d84573ca1a5efc7e3b41000601da5929e5bdb6ca41441b2769048231a78','-BackupTo','<Sicherungsordner>','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-ExpectSent','<ids>','-ExpectReplaced','<ids>','-ExpectCopyGone','<ids>','-KeepStdioMcp'
```

```powershell
Get-Content <Ordner des Programms>\D13_windows.txt -Wait
```

Bestanden ist die Abnahme nur mit der Schlusszeile `D13: <n> OK, 0 FAIL, <w> WARN`. Jedes `WARN` nennt Claude im Bericht.

**Harter Abbruch** (Fenster geschlossen, PC neu gestartet): erst `postkauz.exe version` ansehen. Meldet es rc4, die Aufgaben von Hand einschalten (Aufgabenplanung → Aufgabe → Rechtsklick → „Aktivieren“, dann „Ausführen“; für „audit export“ nur „Aktivieren“). Meldet es rc5 oder fehlt `postkauz.exe`, den Rückweg nach Abschnitt 5 laufen lassen (er kommt seit D13 auch ohne `postkauz.exe` zurecht).

## 4. Erwartet (Teil A)

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Rechte | INFO: läuft die PowerShell erhöht |
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc5 passt zu `SHA256SUMS` und `-ExpectSha256`; das laufende Programm ist rc4 |
| 0.5 bis 0.8 | Sicherungsordner, Konten, Aufgaben, Erwartung | angegeben; `-Accounts` genau die Konten aus `accounts list`; jede Aufgabe vorhanden; keine ID in zwei Listen |
| 0.9 | Konfiguration | kein `copy_gone_after` in `config.toml` |
| 1.1 | Aufgaben anhalten | jede abgeschaltet; außer dem stdio-MCP läuft kein `postkauz.exe`. Ist die Prozessliste nicht lesbar, Abbruch (nicht „0 Prozesse“) |
| 1.2 bis 1.2c | `draft list` vorher | lesbar, unter 500 offen; jeder erwartete offen; `-ExpectReplaced` mit `remote` `missing` oder `taken_over`, `-ExpectCopyGone` mit `missing` |
| 1.3 | `doctor` vorher | `database ok`, Schema 22 |
| 1.5 | Sicherung | Exit 0 und `backup written to …` |
| 2.1, 2.2 | Tausch, `doctor` | rc5 läuft, `postkauz-rc4.exe` daneben; Schema 22 |
| 3.1, 3.3 | `sync -a <Konto> --json` je Konto, erster und zweiter Lauf | Exit 0 und `errors` 0 (OK) oder nach einer sofortigen Wiederholung ohne Fehler (WARN); die Zeile nennt `sent_by_*` (auch `sent_by_subject`), `replaced_in_thread`, `copy_gone`, `missing`, `taken_over` |
| 3.2, 3.4 | alle Abschlüsse seit dem Tausch, nach jedem Lauf | jeder mit Grund im Audit: `sent_by_human` mit `message_id`, `fallback`, `thread` oder `subject`; `abandoned` mit `replaced` oder `copy_gone` |
| 3.2b, 3.4b | nicht erwartet | keiner |
| 3.4c | Summe nach zwei Läufen | jeder erwartete Abschluss wie erwartet |
| 3.5 | **[Du]** | Stimmen die Abschlüsse? `draft show <id>` zeigt bei `copy_gone`: `closed: …, abandoned: copy gone from the Drafts folder since … (copy_gone); no sent message was detected, it may or may not have been sent` |
| 3.6, 3.7 | `doctor` nachher, `audit verify` | keine neue Warnung, Schema 22; `ok` |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.0 | nach FAIL oder gescheitertem 4.1 | altes Programm zurück (`postkauz.exe` meldet rc4) |
| 4.1 bzw. E.1 | Aufgaben wieder an | je Aufgabe: gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); dazu läuft `sync --watch` aus dem Ordner von `-Exe`, und `postkauz.exe` meldet rc5. Scheitert 4.1, hält das Skript die Aufgaben an; nur wenn das gelang (sonst E.0b FAIL, Aufgaben bleiben aus), legt es rc4 zurück und startet mit rc4 |
| Stand | letzte Zeilen | welche Version wirklich an `postkauz.exe` liegt und welche als `postkauz-rc4.exe` (auch nach einem gescheiterten E.0) |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt; erst dann kennt Claude `mail_supersede_draft`.

**Wenn ein Lauf einen Entwurf unerwartet schließt:** wie D12 Abschnitt 4a. Der Abschluss bleibt (auch der Rückweg öffnet ihn nicht); rückgängig nur über die Sicherung von 1.5 ([betrieb.md](betrieb.md) 2.5). Default: so lassen, Claude nennt ID, Status und Grund im Bericht.

## 5. Rückweg (nur bei Problemen)

A07 hat **keine Migration**. Vorher `copy_gone_after` aus `config.toml` entfernen, falls gesetzt (R.0e prüft das).

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d13\abnahme_D13.ps1 -Rueckweg `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp
```

(mit denselben `-Exe`/`-DataDir`/`-Config` wie in Teil A, im Hintergrund wie in Abschnitt 3). Erwartet: `R.0` bis `R.0e`, `R.1b` (neues Programm unter freiem Namen beiseite, meist `postkauz-rc5.exe`; fehlt `postkauz.exe`, nur das alte an seine Stelle), `R.2` rc4 mit Schema 22, `R.3` je Konto ein `sync` mit Exit 0 **und** `errors` 0 (eine Wiederholung erlaubt, dann WARN), `R.5` **alle** Aufgaben aus `-Tasks` wieder an. Dass alle Aufgaben am Ende wieder an sein sollen, merkt sich das Skript **vor** R.1; auch ein Abbruch in R.1 schaltet sie im Abschluss wieder ein. Mit rc5 geschlossene Entwürfe (`subject`, `copy_gone`, `superseded_by_draft`) bleiben geschlossen.

## 6. Teil B: `drafts.supersede` (Mitmach)

**Ziel:** Zeigen, dass ein Entwurf durch eine neuere Fassung ersetzt wird, seine unveränderte Kopie ohne Antrag im Papierkorb landet und die neue Fassung danach normal als gesendet erkannt wird. **Nur im eigenen Postfach**, nie im geteilten ohne Anweisung des Betreibers.

1. **[Claude]** Teil B starten (die Aufgaben laufen, `sync --watch` legt ab und führt die Aktion aus):

   ```powershell
   Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d13\abnahme_D13.ps1','-TeilB','-SelfAccount','<eigenes Konto>','-SelfAddress','<eigene Adresse>'
   ```

   und `D13_teilB.txt` mitlesen. Das Skript legt zwei neue Mails an dich selbst mit gleichem Betreff an (B.2, B.4), wartet, bis beide abgelegt sind (B.3, B.5), prüft `draft supersede <erste> --by <zweite> --dry-run` (B.6: nichts geändert) und führt es aus (B.7). Dann wartet es, bis die Aktion `done` ist (B.8: Kopie der ersten Fassung im Papierkorb, ohne Antrag), und prüft `draft show` (B.9: `abandoned`, `closed_reason` `superseded_by_draft`, Nachfolger) und das Audit (B.10).
2. Bei `INFO B.11 JETZT: Entwurf <id> (zweite Fassung) … UNVERAENDERT senden` **[Claude auf Anweisung des Betreibers]** in Gmail im Browser die zweite Fassung öffnen und **unverändert** senden. Im Entwürfe-Ordner liegt die erste Fassung dann nicht mehr (sie ist im Papierkorb).
3. Das Skript wartet bis zu 20 Minuten (`-WaitMinutes`) und prüft: B.12 die zweite Fassung `sent_by_human` (meist Grund `fallback`), B.13 die erste bleibt `abandoned`.

Das Protokoll steht in `D13_teilB.txt` neben dem Programm. Die erste Fassung kannst du danach im Papierkorb von Gmail löschen oder liegen lassen (PostKauz löscht nie endgültig).
