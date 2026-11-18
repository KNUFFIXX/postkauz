# Abnahme D16: Umstellung auf 0.3.0-rc8 (A10 „Geplant ist nicht gesendet“)

Stand: 18.11.2026 (Auftrag [A10](../auftraege/A10_geplant.md), Teil 4). Skript: [abnahme_D16.ps1](abnahme_D16.ps1) (Tausch, zwei Läufe und Rückweg nach dem Muster von [D15](abnahme_D15.md), Teil B neu zum geplanten Senden, dazu die Punkte aus der Gegenprüfung von D15, Abschnitt 8). Programm: Build **0.3.0-rc8**; die SHA256 der `postkauz-windows-amd64.exe` (für `-ExpectSha256`) steht nach dem Build in [roadmap.md](roadmap.md) und in `SHA256SUMS` neben dem Programm.

> **Hinweis:** Das Skript ist auf dem Entwicklungsrechner (Container) **nicht** mit PowerShell gelaufen; dort gibt es kein `pwsh`. Die Parser-Probe (Abschnitt 2, Schritt 2) und der Lesetest (Schritt 5) auf dem Büro-PC sind deshalb Pflicht, bevor etwas getauscht wird.

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft mit 0.3.0-rc7 (seit D15). D16 stellt ihn auf 0.3.0-rc8 um und prüft A10 am echten Postfach:

1. **Teil A:** Tausch ohne Migration (Schema bleibt 22, keine neuen Konfigurationsschlüssel), zwei vollständige Läufe, Liste aller Abschlüsse. Neu in rc8: **Als gesendet zählt nur noch, was in Gesendet liegt** (Ordner mit Rolle `sent`), wenn das Konto einen Gesendet-Ordner hat. „Alle Nachrichten“ allein reicht nicht mehr. Teil A prüft außerdem je Konto, ob es zu „Alle Nachrichten“ (Rolle `all`) auch einen Gesendet-Ordner (Rolle `sent`) gibt.
2. **Teil B (Mitmach)** im eigenen Gmail-Konto, vier Schritte von dir im Webmail:
   - **B.0** Probe gewöhnlich senden: PostKauz erkennt sie als gesendet, und die gesendete Nachricht liegt in Gesendet.
   - **B.1** Probe in Gmail für **in 3 Tagen planen**: PostKauz zeigt sie als **geplant, nicht gesendet** (vorher, mit rc6/rc7, stand sie nach einer Minute auf „gesendet“, Fehler B4).
   - **B.2** Plan in Gmail **abbrechen**: Der Entwurf ist wieder offen (`taken_over`).
   - **B.3** Denselben Entwurf für **in 5 Minuten planen** und Gmail senden lassen: PostKauz erkennt ihn als gesendet.
   - **B.4** (läuft mit, ohne dich): Die Suche zeigt bei der geplanten Nachricht die Ordnerrollen und den Termin (`is:scheduled`), nach dem Abbruch findet `is:draft` sie im Entwürfe-Ordner.

**Dauer:** Teil A etwa 15 Minuten (meist die beiden Läufe; mit `-BackupTo` länger). Teil B etwa 45 bis 75 Minuten, davon viel Warten auf den Sync.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, macht Parser-Probe und Lesetest, startet das Skript und wertet das Protokoll aus. In Teil B sagt Claude dir bei jeder Zeile „JETZT …“, was im Webmail zu tun ist.
- **[Du]** gibst den Tausch frei, bestätigst am Ende die Liste der Abschlüsse (3.5), startest danach die Claude-App neu und machst in Teil B die vier Schritte im Webmail (B.0, B.1, B.2, B.3).

## 1. Was das Skript tut und was nicht

- **Wie D15:** Live-Datenverzeichnis, Aufgaben aus `-Tasks` (**Pflicht**, Namen nicht raten) angehalten und am Ende die vorher eingeschalteten wieder an, **Umbenennen statt Überschreiben** mit `-KeepStdioMcp`: Das alte Programm wird `postkauz-rc7.exe`. Das geht nur, weil rc8 **keine Migration** hat (Schema 22, `doctor` vorher und nachher).
- **Welche Aufgaben in `-Tasks` gehören:** alle Aufgaben der Aufgabenplanung, die PostKauz **dauerhaft betreiben**, also
  - je Konto die Aufgabe mit `sync --watch` (auf dem Büro-PC über das Hilfsskript `run-sync-watch.ps1 -Konto <name>`; oder eine einzige mit `sync --watch --all`),
  - die Aufgabe mit `mcp --http` (falls eingerichtet),
  - die Aufgabe mit `audit export` (wird nur wieder eingeschaltet, nicht gestartet).

  **Nicht** in `-Tasks` gehört die **Sicherungsaufgabe** (Aktion ruft `postkauz backup create …` auf, direkt oder über ein Skript). Das Skript startet am Ende jede Aufgabe aus `-Tasks`; eine Sicherung liefe dann außerhalb ihres Plans an. 0.6b (und im Rückweg R.0f) prüft das und ist sonst FAIL. Die Sicherungsaufgabe bleibt während der Abnahme, wie sie ist.
- **Konfiguration:** nichts zu ändern. `[draft] attach_deny_dirs` und `attach_deny_files` dürfen stehen bleiben (rc7 und rc8 kennen sie; 0.7 nennt nur, ob sie gesetzt sind).
- **Rollen der Ordner (0.8, neu):** Je Konto liest das Skript `mailbox list -a <konto> --json`. Hat ein Konto „Alle Nachrichten“ (Rolle `all`), aber keinen Gesendet-Ordner (Rolle `sent`), steht ein **Hinweis (WARN)** im Protokoll: „Rolle all ohne Rolle sent: Gesendet ist für IMAP ausgeblendet; dort zählt weiter ‚Alle Nachrichten‘ (A10 Teil 1 c)“. Kein FAIL: Für so ein Konto bleibt die alte Regel, und das Rennen aus Befund B5 bleibt möglich. Abhilfe nur nach deinem Entscheid (in Gmail: Einstellungen → Labels → „Gesendet“ → „In IMAP anzeigen“).
- **Zwei vollständige Läufe** nach dem Tausch (3.1 und 3.3, wie D15). Nach jedem Lauf listet das Skript alle seit der Marke (1.3a) geschlossenen Entwürfe mit Status und Grund. Neu ist der Grund `scheduled` (eine geplante Nachricht liegt jetzt in Gesendet). 3.4d nennt die offenen Entwürfe, die rc8 als **im Mailprogramm geplant** erkennt (nur ID und Termin). Keine Erwartungslisten: Ob die Abschlüsse stimmen, bestätigst du in 3.5. Nach einem FAIL im ersten Lauf folgt **kein zweiter**.
- **Sicherung nur mit `-BackupTo`** (optional, **empfohlen**). Ein Abschluss in Teil 3 ist endgültig; rückgängig nur über eine Sicherung ([betrieb.md](betrieb.md) 2.5).
- **Rückweg bei jedem FAIL** wie D15: E.0 (altes Programm zurück), E.0c (Datenbank mit rc7 in Ordnung), erst dann laufen die Aufgaben mit rc7 an. Mit rc8 laufen sie nur an, wenn die Abnahme bestanden ist.
- **Ein FAIL in Teil 0 ändert nichts:** Das Skript bricht vor Teil 1 ab (keine Aufgabe angehalten, kein Tausch, keine Sicherung) und schreibt die Zeilen `Abbruch: Voraussetzungen fehlen; nichts geaendert` und `Ergebnis: nichts geaendert (…)`. Darauf beruht der Lesetest (Abschnitt 2, Schritt 5).
- **Strg+C im Abschluss abgeschaltet** (wie D15, E.4).
- **Protokoll** `D16_windows.txt` (Teil B: `D16_teilB.txt`) neben dem Programm, oder die Datei aus `-LogFile`; ein vorhandenes wird als `D16_windows-<Zeitstempel>.txt` beiseitegelegt. Ins Protokoll kommen nur Prüfnummern, IDs, Zustände, Gründe, Zähler, Ordnerrollen und Termine, **nie** Betreff, Adressen, Ordnernamen oder Mailtext. Konto- und Aufgabennamen ersetzt das Skript in jeder Zeile durch „Konto 1“, „Aufgabe 3“ usw.

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d16` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc8, nicht umbenennen), `SHA256SUMS` und `abnahme_D16.ps1`.
2. **[Claude] Parser-Probe** (Pflicht, das Skript ist nie mit PowerShell gelaufen); erwartet ist eine **leere** Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d16\abnahme_D16.ps1', [ref]$null, [ref]$e); $e
   ```

3. **[Claude]** Konten und Aufgaben nennen lassen: die Konten aus `postkauz.exe accounts list`, die Aufgaben aus der Aufgabenplanung nach Abschnitt 1 (je Konto `sync --watch`, `mcp --http`, `audit export`; **ohne** die Sicherungsaufgabe). Claude zeigt dir die Liste vorher.
4. **[Claude]** Mit rc7 die offenen Entwürfe mit `remote: missing` ansehen (`postkauz draft list --json`) und dich fragen, ob du davon einen in Gmail **zum Senden geplant** hast (Gmail-Ordner „Geplant“). Genau solche zeigt rc8 nach dem ersten Lauf als `remote: scheduled` (3.4d). Entwürfe, die rc6/rc7 schon fälschlich als gesendet geschlossen haben, bleiben geschlossen (eine Zuordnung ist endgültig). Das dient nur dazu, 3.4d und 3.5 zu verstehen.
5. **[Claude] Lesetest (fester Schritt vor dem Tausch):** ein echter Aufruf mit allen Angaben wie in Abschnitt 3, aber mit `-OldVersion 0.0.0-probe` und eigener Protokolldatei. Er prüft Parameter, Prüfsumme, Konten, Aufgaben (auch 0.6b) und Ordnerrollen (0.8) und **ändert nichts**, weil 0.4 (altes Programm meldet nicht `0.0.0-probe`) FAIL ist:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d16\abnahme_D16.ps1 -Exe '<Ordner des Programms>\postkauz.exe' -DataDir '<Datenordner>' -Config '<Datenordner>\config.toml' -NewExe C:\PostKauz\d16\postkauz-windows-amd64.exe -ExpectSha256 'bf2a9452b04f3838ac9ba1a903bf1f1e427456fbf098ce8ba88ddbd591388fba' -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp -OldVersion 0.0.0-probe -LogFile C:\PostKauz\d16\D16_lesetest.txt
   ```

   Erwartet in `C:\PostKauz\d16\D16_lesetest.txt`: **nur `0.4` und `E.2` als FAIL**, die Zeilen `Abbruch: Voraussetzungen fehlen; nichts geaendert` und `Ergebnis: nichts geaendert (…)`, `Stand: … meldet 0.3.0-rc7`. WARN in 0.6b oder 0.8 sind erlaubt; Claude nennt sie dir. Jeder andere FAIL (etwa 0.2b falsche Prüfsumme, 0.5 Konten, 0.6 Aufgabe fehlt, **0.6b Sicherungsaufgabe in `-Tasks`**) wird vor dem Tausch behoben. Danach `postkauz.exe version` ansehen: weiter rc7, die Aufgaben laufen unverändert.
6. **[Claude]** `-BackupTo` vorschlagen (empfohlen): ein Ordner auf einem verschlüsselten Datenträger wie in [betrieb.md](betrieb.md) 2.2. Default: mit Sicherung, einverstanden?

## 3. Lauf Teil A

**Als welcher Benutzer?** Wie D15: derselbe Benutzer wie der Dauerbetrieb; erhöht nur, wenn die Aufgaben „Mit höchsten Privilegien ausführen“ haben.

**Im Hintergrund starten**, danach das Protokoll mitlesen, bis die Schlusszeile `D16: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d16\abnahme_D16.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d16\postkauz-windows-amd64.exe','-ExpectSha256','bf2a9452b04f3838ac9ba1a903bf1f1e427456fbf098ce8ba88ddbd591388fba','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-KeepStdioMcp','-BackupTo','<Sicherungsordner>'
```

Ohne Sicherung `'-BackupTo','<Sicherungsordner>'` weglassen (dann INFO 1.4).

```powershell
Get-Content <Ordner des Programms>\D16_windows.txt -Wait
```

Bestanden ist Teil A nur mit der Schlusszeile `D16: <n> OK, 0 FAIL, <w> WARN, <s> SKIP`. Jedes `WARN` nennt Claude im Bericht.

**Harter Abbruch** (Fenster geschlossen, PC neu gestartet): erst `postkauz.exe version` ansehen. Meldet es rc7, die Aufgaben von Hand einschalten (Aufgabenplanung → Aufgabe → Rechtsklick → „Aktivieren“, dann „Ausführen“; für „audit export“ nur „Aktivieren“). Meldet es rc8 oder fehlt `postkauz.exe`, den Rückweg nach Abschnitt 5 laufen lassen.

## 4. Erwartet (Teil A)

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Rechte | INFO: läuft die PowerShell erhöht |
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc8 passt zu `SHA256SUMS` und `-ExpectSha256`; das laufende Programm ist rc7 |
| 0.5, 0.6 | Konten, Aufgaben | `-Accounts` genau die Konten aus `accounts list`; jede Aufgabe vorhanden |
| 0.6b | **keine Sicherungsaufgabe** (neu) | OK: keine Aufgabe in `-Tasks` ruft `postkauz backup` auf (Aktion oder ein dort genanntes Skript); sonst FAIL. WARN, wenn nur der Name nach Sicherung klingt |
| 0.7 | Konfiguration | INFO: `attach_deny_*` gesetzt ja/nein (beides in Ordnung) |
| 0.8 | **Ordnerrollen je Konto** (neu) | INFO je Konto „Rolle all ja/nein, Rolle sent ja/nein (in Ordnung)“; WARN „Rolle all ohne Rolle sent …“ (Hinweis, kein FAIL) oder WARN, wenn `mailbox list` nicht lesbar ist |
| 1.1 | Aufgaben anhalten | jede abgeschaltet; außer dem stdio-MCP läuft kein `postkauz.exe` |
| 1.2 | `doctor` vorher | `database ok`, Schema 22 |
| 1.3, 1.3a | offene Entwürfe, Marke im Audit | lesbar, unter 500; INFO mit der höchsten Audit-ID |
| 1.4 | Sicherung | mit `-BackupTo`: Exit 0 und `backup written to …`; sonst INFO |
| 2.1 | Tausch | rc8 läuft, `postkauz-rc7.exe` daneben |
| 2.2 | `doctor` nachher | `database ok`, Schema unverändert (22), keine neue Warnung |
| 2.3 | Zeile `attach_guard` | da |
| 3.1, 3.3 | `sync -a <Konto> --json` je Konto, erster und zweiter Lauf | Exit 0 und `errors` 0 (OK) oder nach einer sofortigen Wiederholung ohne Fehler (WARN). Die Zeile nennt neu `scheduled`, `sent_by_scheduled`, `schedule_cancelled`, `scheduled_gone` und die IDs geplanter Entwürfe |
| 3.2, 3.4 | Abschlüsse seit der Marke, nach jedem Lauf | jeder mit passendem Grund aus dem Audit: `sent_by_human` mit `message_id`, `fallback`, `thread`, `subject` oder (neu) `scheduled`; `abandoned` mit `replaced`, `copy_gone` oder `superseded_by_draft`. Nach einem FAIL in 3.1/3.2 **kein zweiter Lauf** |
| 3.4c | Summe | INFO: gesendet (davon Grund `scheduled`, davon mit ergänzten Empfängern), ersetzt, `copy_gone`, `supersede` |
| 3.4d | geplante Entwürfe (neu) | INFO: offene Entwürfe mit `remote scheduled` und Termin (passt zu deiner Antwort aus Abschnitt 2, Schritt 4) |
| 3.5 | **[Du]** | Stimmen die Abschlüsse? Claude zeigt dir je ID `draft show <id>` (Betreff nur am Bildschirm) |
| 3.6, 3.7 | `doctor` nach den Läufen, `audit verify` | `database ok`, Schema 22, keine neue Warnung; `ok` |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.4 | Abschluss | INFO: Strg+C bis zum Ende abgeschaltet |
| E.0, E.0c | nach FAIL | altes Programm zurück (`postkauz.exe` meldet rc7), Datenbank mit rc7 in Ordnung; sonst bleiben die Aufgaben aus |
| 4.1 bzw. E.1 | Aufgaben wieder an | je Aufgabe gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); je Konto ein `sync --watch` genau mit `-Exe`; `postkauz.exe` meldet rc8 (bzw. rc7 nach E.0) |
| Stand | letzte Zeilen | welche Version an `postkauz.exe` liegt und welche als `postkauz-rc7.exe` |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt.

**Wenn ein Lauf einen Entwurf unerwartet schließt:** Der Abschluss bleibt (auch der Rückweg öffnet ihn nicht); rückgängig nur über eine Sicherung. Default: so lassen, Claude nennt ID, Status und Grund im Bericht.

## 5. Rückweg (nur bei Problemen)

A10 hat **keine Migration**. An `config.toml` ist nichts zu ändern. Vollständig, im Hintergrund wie in Abschnitt 3:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d16\abnahme_D16.ps1','-Rueckweg','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-KeepStdioMcp'
```

Protokoll `D16_windows.txt` (wie oben mitlesen). Erwartet: `R.0` (`postkauz-rc7.exe` vorhanden), `R.0b` (laufendes Programm rc8, oder INFO, wenn `postkauz.exe` fehlt), `R.0c` (`postkauz-rc7.exe` meldet rc7), `R.0d` (`-Tasks` und `-Accounts` angegeben), `R.0e` jede Aufgabe aus `-Tasks` vorhanden, **`R.0f` keine Sicherungsaufgabe in `-Tasks`** (neu), `R.1` Aufgaben angehalten, `R.1b` neues Programm unter freiem Namen beiseite (meist `postkauz-rc8.exe`; fehlt `postkauz.exe`, nur das alte an seine Stelle), `R.2` rc7 mit Schema 22, `R.3` je Konto ein `sync` mit Exit 0 **und** `errors` 0 (eine Wiederholung erlaubt, dann WARN), `R.4` INFO zum Fall, `R.5` alle Aufgaben aus `-Tasks` wieder an.

**Bleibt der Rückweg irgendwo stehen** (FAIL in R.0 bis R.3, Abbruch in R.1, auch bei fehlender `postkauz.exe`, oder Strg+C), bleiben die Aufgaben **aus** (Schlusszeile mit `E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“). Dann `postkauz.exe version` und `postkauz.exe doctor` von Hand ansehen und die Aufgaben erst einschalten, wenn beide rc7 und `database: ok` zeigen.

**Mit rc7 gilt:** Abschlüsse aus rc8 bleiben. rc7 kennt kein geplantes Senden: Ein in Gmail geplanter Entwurf steht wieder auf `remote: missing` und kann mit rc7 wieder fälschlich als gesendet schließen (Fehler B4) oder nach `[draft] copy_gone_after` als `copy_gone`. Die Vermerke zu geplanten Entwürfen bleiben in der Datenbank (`meta`) und wirken erst wieder mit rc8. Suchtreffer haben keine `roles` und kein `scheduled_for`, `is:scheduled` ist unbekannt.

## 6. Teil B (Mitmach)

**Voraussetzungen:** Teil A bestanden, die Aufgaben laufen mit rc8 (`sync --watch` legt ab und spiegelt Entwürfe, Gesendet und „Alle Nachrichten“). **Nur im eigenen Gmail-Konto**, nie im geteilten ohne deine Anweisung. Das Konto muss Gesendet über IMAP zeigen (BV.3).

- **Agentenschlüssel (empfohlen):** `-AgentKeyFile <Datei>` mit dem API-Schlüssel eines Agenten (`pk_key_…`, etwa der der Claude-App; Rolle `assistant`). Dann laufen Anlegen, `mail_update_draft` (Trockenlauf, B.1h) und `mail_search` (B.4c, B.4e) über den MCP-Server wie bei einem Agenten. **Schlüssel und Dateiname stehen nie im Protokoll.** Die Datei danach löschen oder nur für dich lesbar lassen. Ohne die Datei läuft Teil B als Mensch an der CLI (dann INFO statt B.1h, B.4c, B.4e).
- **Probe-Entwürfe:** neue Mails an die eigene Adresse des Kontos (das Skript nimmt sie aus `accounts list`, nie ins Protokoll). Betreff „D16 Probe B.0 <Zeitstempel>“ bzw. „D16 Probe B.1 <Zeitstempel>“. **B.0 und B.3 sind echte Mails an dich selbst.**
- **Ins Protokoll** kommen nur IDs, Zustände, Gründe, Zähler, Ordnerrollen und Termine.

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d16\abnahme_D16.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-TeilB','-SelfAccount','<eigenes Gmail-Konto>','-AgentKeyFile','<Datei mit dem Agentenschlüssel>'
```

Protokoll mitlesen: `Get-Content <Ordner des Programms>\D16_teilB.txt -Wait`. `-WaitMinutes <n>` ändert die Frist für deine Schritte (Standard 20 Minuten).

**Ablauf und Fristen** (die Zeile „INFO B.x JETZT: …“ sagt, wann du dran bist):

| Schritt | Wer | Was | Frist |
|---|---|---|---|
| B.0a, B.0b | Skript | Probe B.0 anlegen, warten, bis die Kopie im Spiegel des Entwürfe-Ordners liegt | 12 Minuten |
| B.0c | **[Du]** | Entwurf „D16 Probe B.0 …“ im Webmail öffnen und **unverändert gewöhnlich senden** (nicht planen) | 20 Minuten (`-WaitMinutes`) |
| B.1a, B.1b | Skript | Probe B.1 anlegen, Kopie im Spiegel | 12 Minuten |
| B.1c | **[Du]** | Entwurf „D16 Probe B.1 …“ öffnen, beim Pfeil neben „Senden“ **„Senden planen“** → „Datum und Uhrzeit auswählen“: **in 3 Tagen**, Uhrzeit beliebig. Nicht sofort senden | 20 Minuten |
| B.1i | Skript | ein weiterer Lauf (sync request), Entwurf bleibt geplant | 10 Minuten |
| B.4a–c | Skript | Suche nach der geplanten Nachricht (der Suchindex darf bis zu 5 Minuten hinterher sein) | 5 Minuten |
| B.2a | **[Du]** | im Webmail den Ordner **„Geplant“** öffnen, die Nachricht „D16 Probe B.1 …“ öffnen, **„Senden abbrechen“**. Danach nicht senden, nicht löschen, nicht ändern | 20 Minuten |
| B.2d | Skript | ein weiterer Lauf, Entwurf bleibt `taken_over` | 10 Minuten |
| B.4d, B.4e | Skript | Suche `is:draft` | 5 Minuten |
| B.3a | **[Du]** | den Entwurf „D16 Probe B.1 …“ (wieder unter „Entwürfe“) öffnen, unverändert **„Senden planen“** → „Datum und Uhrzeit auswählen“: **heute, in 5 Minuten**. Danach nichts mehr tun; Gmail sendet zum Termin | 20 Minuten für den Plan, danach bis **Termin + 20 Minuten** (höchstens 2 Stunden nach B.3a) |

**Erwartet:**

| Nr | Prüfung | Erwartet |
|---|---|---|
| BV.1 bis BV.5 | Version, Konto, Ordnerrollen, Agentenschlüssel, `sync --watch` | rc8; Konto konfiguriert, Adresse bekannt; **Ordner mit Rolle `all`, `sent` und `drafts`**; MCP-Sitzung angemeldet (oder INFO ohne Schlüssel); ein `sync --watch` genau mit `-Exe` für das Konto |
| BV.6 | Marke im Audit | INFO |
| B.0a, B.0b | Probe B.0 | angelegt (mit Schlüssel über MCP); Kopie im Spiegel |
| B.0c | **[Du]** | INFO „JETZT …“: gewöhnlich senden |
| B.0d | erkannt | `sent_by_human` |
| B.0e | Audit `drafts.sent_detected` | Grund `message_id`, `fallback`, `thread` oder `subject` |
| B.0f | gesendete Nachricht | `show <sent_message_id> --json` nennt einen Ordner mit Rolle **`sent`**, kein `scheduled_for` (prüft A10 Teil 1) |
| B.1a, B.1b | Probe B.1 | angelegt; Kopie im Spiegel |
| B.1c | **[Du]** | INFO „JETZT …“: für in 3 Tagen planen |
| B.1d | `draft show --json` | `remote: scheduled`, `scheduled_for` mehr als 1 Stunde voraus (INFO B.1d2, wenn nicht etwa 72 Stunden), `scheduled_message_id`, `remote_note` „scheduled in the mail client for <Termin>; not sent yet“, Status offen (`draft`, `pending_approval` oder `ready`). **FAIL „Fehler B4 ist noch da“**, wenn er `sent_by_human` wird |
| B.1e | `draft list --json` | derselbe Entwurf mit `remote: scheduled` und `scheduled_for` |
| B.1f | Audit `drafts.scheduled_detected` | Akteur `system`, Grund `scheduled`, `message_id` und `scheduled_for` wie `draft show` |
| B.1g | `draft edit <id> --quote=true --dry-run --json` | Exit 3, `denied (draft_scheduled)` (Trockenlauf: ändert auch ohne Ablehnung nichts) |
| B.1h | `mail_update_draft` (Trockenlauf, nur mit Schlüssel) | abgelehnt mit `draft_scheduled` |
| B.1i | nach einem weiteren Lauf | weiter offen und `remote: scheduled`, nie `sent_by_human` (WARN, wenn kein weiterer Lauf erkennbar war) |
| B.4a | `search "is:scheduled" -a <Konto> --json` | trifft die Nachricht `scheduled_message_id` aus B.1d; `roles` mit `all` (ohne `inbox`, `sent`, `drafts`, `trash`, `junk`; bei Gmail genau `["all"]`), `scheduled_for` wie `draft show` |
| B.4b | `show <scheduled_message_id> --json` | `mailboxes[].role` mit `all`, nicht `sent`/`drafts`; `scheduled_for` gesetzt |
| B.4c | `mail_search` „is:scheduled“ (nur mit Schlüssel) | dieselbe Nachricht mit `roles` und `scheduled_for` |
| B.2a | **[Du]** | INFO „JETZT …“: Plan abbrechen |
| B.2b | `draft show --json` | `remote: taken_over`, Status offen, kein `scheduled_for` |
| B.2c | Audit `drafts.remote_check` | Grund `schedule_cancelled` |
| B.2d | nach einem weiteren Lauf | weiter offen und `taken_over` |
| B.4d | `search "is:draft" -a <Konto> --json` | trifft die Nachricht im Entwürfe-Ordner (dieselbe ID wie in B.4a, oder eine neue mit demselben Betreff): `roles` mit `drafts`, kein `scheduled_for` (Gmail setzt dort kein `\Draft`; `is:draft` trifft sie trotzdem) |
| B.4e | `mail_search` „is:draft“ (nur mit Schlüssel) | dieselbe Nachricht mit Rolle `drafts` |
| B.3a | **[Du]** | INFO „JETZT …“: für in 5 Minuten planen |
| B.3b | erneut geplant | INFO mit Termin, sobald `draft show` wieder `remote: scheduled` zeigt (oder „nicht beobachtet“, wenn Plan und Versand zwischen zwei Läufe fielen) |
| B.3c | erkannt | `sent_by_human` |
| B.3d | Audit `drafts.sent_detected` | **OK mit Grund `scheduled`**. Grund `fallback`, `thread` oder `subject` (auch `message_id`) ist **INFO, kein FAIL**: „Gmail hat beim Versand eine neue Nachricht angelegt“. Das Ergebnis steht im Protokoll; Claude nennt es im Bericht (A10 Teil 2 (iii) ist am echten Server bisher nicht geprüft) |
| B.3e | gesendete Nachricht | Rolle `sent`, kein `scheduled_for` |
| B.3f | INFO | ob `sent_message_id` die zuletzt geplante Nachricht ist |
| B.9 | INFO | noch offene Probe-Entwürfe (nach einem vollständigen Lauf keine) |
| E.2 | Abschlussmerker | FAIL, wenn Teil B vor dem Ende von B.3 abbricht |

**Nach einem unerwarteten Status** bricht der laufende Schritt ab (Zeile „Schritt B.x abgebrochen: …“), und **kein Schritt, der davon abhängt, läuft noch**: B.4a–c, B.2 und B.3 brauchen einen geplanten Entwurf aus B.1 (B.1d und B.1i); B.4d–e und B.3 brauchen den Abbruch aus B.2 (B.2b und B.2d). Sie stehen dann als `SKIP … haengt von B.x ab` im Protokoll. So fordert das Skript dich nach einem Fehler nie zu einem weiteren Schritt im Webmail auf. B.0 und B.1 sind unabhängig voneinander. Antwortet der MCP-Server 60 s nicht, endet Teil B ganz (FAIL mit Grund, E.2).

Bestanden ist Teil B mit `D16: <n> OK, 0 FAIL, <w> WARN, 0 SKIP`.

**Danach aufräumen ([Claude] auf deine Anweisung):**

- **Nach einem vollständigen Lauf:** nichts. B.0 und B.3 sind echte Mails an dich selbst (in Gesendet und im Posteingang); sie dürfen liegen bleiben oder in Gmail gelöscht werden. Beide Entwürfe sind geschlossen (`sent_by_human`).
- **Nach einem Abbruch:** Zuerst im Webmail im Ordner **„Geplant“** nachsehen. Liegt dort noch „D16 Probe B.1 …“, **„Senden abbrechen“** (sonst sendet Gmail sie zum Termin), dann den Entwurf in Gmail löschen. Danach jeden noch offenen Probe-Entwurf (IDs aus B.9) schließen: `postkauz draft abandon <id>`. Ein offener Entwurf aus B.0 ebenso (vorher in Gmail löschen, wenn er dort noch liegt).

## 7. Wenn etwas FAIL ist

- **Lesetest:** Jeder FAIL außer 0.4 und E.2 wird vor dem Tausch behoben (Parameter, Aufgabenliste, Prüfsumme). Fehlt die Zeile „nichts geaendert“, nicht weitermachen und Claude fragen.
- **0.6b FAIL:** Eine Sicherungsaufgabe steht in `-Tasks`. Default: aus `-Tasks` nehmen und neu starten; die Sicherungsaufgabe bleibt unverändert.
- **Teil A:** Nichts von Hand tun. Das Skript legt rc7 zurück (E.0, E.0c) und startet die Aufgaben mit rc7. Claude schickt dir die Zeilen mit FAIL und einen Vorschlag. Abschlüsse aus Teil 3 bleiben.
- **`E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“:** `postkauz.exe version` und `postkauz.exe doctor` ansehen; zeigen sie rc7 und `database: ok`, die Aufgaben von Hand einschalten (Abschnitt 5). Sonst Claude fragen.
- **B.0d/B.0f FAIL** (nicht erkannt oder nicht in Gesendet): Default: Claude prüft mit `draft show <id>` und `show <id>`, ob die Mail in Gesendet liegt; danach schließt du den Entwurf mit `postkauz draft mark-sent <id>`.
- **B.1d FAIL „Fehler B4 ist noch da“** (geplant, aber `sent_by_human`): Die Korrektur aus A10 wirkt nicht. Sofort im Webmail den Plan abbrechen und den Entwurf löschen; Claude meldet es. Der Abschluss bleibt.
- **B.1d FAIL** (nicht als geplant erkannt, etwa `remote: missing`): im Webmail den Plan abbrechen und löschen, dann `postkauz draft abandon <id>`; Claude meldet Zustand und Termin.
- **B.1g/B.1h FAIL** (Änderung nicht abgelehnt): Es war ein Trockenlauf; nichts geändert. Claude meldet es.
- **B.4 FAIL:** nur Anzeige, nichts im Postfach betroffen; Claude meldet Rollen und Termin.
- **B.2b FAIL:** Im Webmail nachsehen, ob die Nachricht noch unter „Geplant“ liegt; wenn ja, Plan abbrechen. Danach aufräumen wie oben.
- **B.3c FAIL** (nicht als gesendet erkannt): Default: Claude prüft mit `draft show <id>`, ob Gmail die Mail gesendet hat (Gesendet); danach schließt du den Entwurf mit `postkauz draft mark-sent <id>`.
- **B.3d FAIL** (Grund fehlt oder ist ein anderer als `scheduled`, `fallback`, `thread`, `subject`, `message_id`): Der Entwurf ist geschlossen; Claude nennt Grund und Audit im Bericht.

## 8. Was aus der Gegenprüfung von D15 umgesetzt ist

Die Punkte aus [A10](../auftraege/A10_geplant.md), Teil 0 (c) (D15 selbst bleibt als Protokoll unverändert):

| Punkt | In D16 |
|---|---|
| Nach 0.6 ein Prüfschritt „keine Sicherungsaufgabe in `-Tasks`“ | **umgesetzt:** 0.6b (und im Rückweg R.0f). Sicherungsaufgabe = Aufgabe, deren Aktion `postkauz backup` aufruft: das Wort `backup` allein in Programm und Argumenten der Aktion, oder in einem dort genannten Skript (`.ps1`, `.cmd`, `.bat`) eine Zeile mit `postkauz` und `backup` (Kommentare zählen nicht). FAIL bei einem Treffer; WARN, wenn nur der Name nach Sicherung klingt |
| In Teil B folgt nach einem unerwarteten Status kein abhängiger Schritt (auch nicht nach dem Supersede) | **umgesetzt:** Abbruch des laufenden Schritts wie D15 (`Stop-Step`), dazu Merker für die Abhängigkeiten (B.1 → B.4a–c, B.2, B.3; B.2 → B.4d–e, B.3); abhängige Schritte stehen als SKIP im Protokoll und fordern dich zu nichts mehr auf. Einen Supersede gibt es in D16 nicht mehr |
| Rückweg-Beispiel und `.EXAMPLE` nennen `-Exe`, `-DataDir`, `-Config` | **umgesetzt:** Abschnitt 5 und jedes `.EXAMPLE` im Skript (Umstellung, Lesetest, Rückweg, Teil B) |
| Die Anleitung nennt die Aufgaben ausdrücklich (ohne die Sicherungsaufgabe) | **umgesetzt:** Abschnitt 1 („Welche Aufgaben in `-Tasks` gehören“) und Abschnitt 2, Schritt 3 |
| Lesetest als fester Schritt vor dem Tausch (echter Aufruf mit `-OldVersion 0.0.0-probe -LogFile …`; erwartet nur 0.4 und E.2 als FAIL und „nichts geaendert“) | **umgesetzt:** Abschnitt 2, Schritt 5. `-LogFile` gab es schon in D15. Ein FAIL in Teil 0 bricht vor Teil 1 ab (`Abbruch: Voraussetzungen fehlen; nichts geaendert`); neu schreibt das Skript zusätzlich `Ergebnis: nichts geaendert (…)`, wenn Teil 1 nicht begonnen hat. Auch 0.6b und 0.8 laufen im Lesetest mit |
| `-BackupTo` empfehlen | **umgesetzt:** Abschnitt 2, Schritt 6, Beispiel in Abschnitt 3 mit `-BackupTo`; INFO 1.4 nennt die Empfehlung |
| Schritte mit Wartezeit auf den Menschen stehen in der Anleitung mit der Frist | **umgesetzt:** Tabelle „Ablauf und Fristen“ in Abschnitt 6 (B.0c, B.1c, B.2a je 20 Minuten, B.3a 20 Minuten plus Termin + 20 Minuten; dazu die Wartezeiten des Skripts) |

**Grenzen:** Ruft eine Sicherungsaufgabe `postkauz backup` über einen Umweg auf, den 0.6b nicht liest (etwa ein Skript, das ein weiteres Skript startet), erkennt 0.6b sie nicht; Claude prüft die Aufgabenliste deshalb vorher mit dir (Abschnitt 2, Schritt 3). „Ein weiterer Lauf“ (B.1i, B.2d) erkennt das Skript an `last_sync_at` des Entwürfe-Ordners in `mailbox list`; ist das nicht erkennbar, steht ein WARN da, und der Zustand wird trotzdem geprüft. Nicht am Gerät geprüft (kein PowerShell im Container): die genaue Form der MCP-Ablehnung von `mail_update_draft` (das Skript nimmt `draft_scheduled` als Code oder Grund an).
