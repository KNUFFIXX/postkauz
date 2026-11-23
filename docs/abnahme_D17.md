# Abnahme D17: Umstellung auf 0.3.0-rc9 (A11 „Alltag 5“)

Stand: 23.11.2026 (Auftrag [A11](../auftraege/A11_alltag5.md), Teil 4). Skript: [abnahme_D17.ps1](abnahme_D17.ps1) (Tausch, zwei Läufe und Rückweg nach dem Muster von [D16](abnahme_D16.md), Teil B neu zu eigenen Entwurfskopien, `drafts.discard` und dem wiederholten Textauszug, dazu die Punkte K1 bis K7 aus der Gegenprüfung von D16, Abschnitt 9). Programm: Build **0.3.0-rc9**, `postkauz-windows-amd64.exe`.

**SHA256: siehe CHANGELOG, Abschnitt 0.3.0-rc9** (für `-ExpectSha256`; steht nach dem Build auch in `SHA256SUMS` neben dem Programm).

> **Hinweis:** Das Skript ist auf dem Entwicklungsrechner (Container) **nicht** mit PowerShell gelaufen; dort gibt es kein `pwsh`. Geprüft sind dort nur: reines ASCII, UTF-8 mit BOM, CRLF und ausgeglichene Klammern und Anführungszeichen. Die **Parser-Probe** (Abschnitt 2, Schritt 2) und der **Lesetest** (Abschnitt 2, Schritt 5) auf dem Büro-PC sind deshalb Pflicht, bevor etwas getauscht wird.

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft mit 0.3.0-rc8 (seit D16). D17 stellt ihn auf 0.3.0-rc9 um und prüft A11 am echten Postfach:

1. **Teil A:** Tausch ohne Migration (Schema bleibt 22), zwei vollständige Läufe, Liste aller Abschlüsse. Neu: Zählung der früheren Kopien offener Entwürfe und der gescheiterten Textauszüge vor dem Tausch und nach dem ersten Lauf.
2. **Teil B** im eigenen Konto mit einem Agentenschlüssel: Entwürfe nur an die eigene Adresse, **nichts wird gesendet**. Vier Schritte laufen ohne dich (B.1, B.2, B.3, B.5), einer braucht dich im Webmail (B.4).
3. **Rückweg** (nur bei Problemen) mit neuer Prüfung offener Aktionen vor dem Tausch.

### Ankündigung: Was der erste Lauf mit rc9 im Postfach tut

- **Frühere Kopien offener Entwürfe gehen in den Papierkorb.** rc8 und älter ließen nach jeder Änderung eines Entwurfs die alte Fassung im Entwürfe-Ordner liegen. Der erste vollständige Lauf mit rc9 legt für jede frühere, **eigene, unveränderte** Kopie eines offenen Entwurfs (Status `draft` oder `pending_approval`, vom Anleger zuletzt geändert, nicht geplant) eine Papierkorb-Aktion an (Herkunft `draft_update`, ohne Antrag); der zweite Lauf führt sie aus. Kopien, die du im Mailprogramm geändert hast, und Kopien von Entwürfen mit Status `ready` bleiben liegen. Nichts wird endgültig gelöscht; der Papierkorb ist umkehrbar. Das Skript nennt die Zahl vor dem Tausch (0.9 und 1.5, gezählt mit rc8) und danach erneut (3.1b nach dem ersten, 3.3b nach dem zweiten Lauf). Dazu kommen Kopien von Entwürfen, die in den letzten 7 Tagen per `supersede` ersetzt wurden und deren Papierkorb-Aktion damals scheiterte; die zählt das Skript nicht vorher (Grenze, Abschnitt 9).
- **Vorübergehend gescheiterte Textauszüge werden wiederholt.** Der erste Lauf je Konto setzt Anhänge mit `extract_status: failed` wieder auf `pending`, deren `extract_error` mit „extractor failed: exit status 0xc…“ beginnt oder „fork/exec“, „exec:“ oder „signal:“ enthält (etwa „exit status 0xc0000142“: das Hilfsprogramm konnte unter Windows nicht starten). Danach höchstens fünf Versuche im Abstand von mindestens 10 Minuten.

### `[draft] previous_copies` während der Abnahme nicht setzen

rc9 kennt den neuen Schlüssel `[draft] previous_copies` (`"trash"`, der Standard ohne Schlüssel, oder `"keep"` wie bisher). **Während der Abnahme nicht setzen:** 0.3.0-rc8 und älter **starten mit dem Schlüssel nicht** (unbekannter Schlüssel), der Rückweg ginge dann nicht. Das Skript prüft das in 0.7b (Teil A), BV.6 (Teil B) und R.0g (Rückweg); jedes Mal FAIL, wenn er gesetzt ist.

**Dauer:** Teil A etwa 20 bis 40 Minuten (die beiden Läufe, die Zählung der Anhänge vorher und nach dem ersten Lauf, 75 Sekunden Wartezeit in 1.1b; mit `-BackupTo` länger). Teil B etwa 30 bis 50 Minuten, davon höchstens 20 Minuten für deinen Handgriff in B.4.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, macht Parser-Probe und Lesetest, startet das Skript und wertet das Protokoll aus. In Teil B sagt Claude dir bei der Zeile „JETZT …“, was im Webmail zu tun ist.
- **[Du]** gibst den Tausch frei, bestätigst die Liste der Abschlüsse (3.5), startest danach die Claude-App neu und machst in Teil B den Handgriff B.4 im Webmail.

## 1. Was das Skript tut und was nicht

- **Wie D16:** Live-Datenverzeichnis, Aufgaben aus `-Tasks` (**Pflicht**, Namen nicht raten) angehalten und am Ende die vorher eingeschalteten wieder an, **Umbenennen statt Überschreiben** mit `-KeepStdioMcp`: Das alte Programm wird `postkauz-rc8.exe`. Das geht, weil rc9 **keine Migration** hat (Schema 22, `doctor` vorher und nachher).
- **`-Exe`, `-DataDir` und `-Config` sind Pflicht (neu, K3).** Sie haben keinen Standardwert mehr. Fehlt eine Angabe, beendet sich das Skript sofort mit Exit 2 und ändert nichts; fehlt die Datei oder der Ordner, ist 0.0a FAIL und es ändert ebenfalls nichts.
- **Welche Aufgaben in `-Tasks` gehören:** alle Aufgaben der Aufgabenplanung, die PostKauz dauerhaft betreiben: je Konto die Aufgabe mit `sync --watch` (auf dem Büro-PC über `run-sync-watch.ps1 -Konto <name>`), die Aufgabe mit `mcp --http` (eigenes Skript mit Neustart-Schleife), die Aufgabe mit `audit export` (wird nur wieder eingeschaltet). **Nicht** die Sicherungsaufgabe (0.6b und R.0f prüfen das).
- **Anhalten (1.1, neu K1):** Das Skript schaltet jede Aufgabe ab und beendet danach **jedes PowerShell, dessen Befehlszeile ein `.ps1` aus einer Aktion dieser Aufgaben nennt** (voller Pfad, oder der Dateiname ohne Ordner bzw. mit `.\`), nicht nur `run-sync-watch.ps1`. 0.6c nennt vorher die Zahl dieser Skripte. Danach beendet es `postkauz` (sync, `--http`, audit) aus dem Ordner von `-Exe` und prüft, dass **kein** postkauz-Prozess mehr läuft (mit `-KeepStdioMcp` außer dem stdio-MCP der Claude-App). **1.1b** wartet 75 Sekunden (die Neustart-Schleife des Hilfsskripts für `mcp --http` wartet 60 s) und prüft erneut: kein Hilfsskript, kein neu gestarteter postkauz.
- **Konfiguration:** nichts zu ändern. `[draft] attach_deny_dirs`/`attach_deny_files` dürfen stehen bleiben (0.7 nennt sie). `[draft] previous_copies` darf **nicht** gesetzt sein (0.7b).
- **Zählungen vorher (neu):**
  - **0.9 und 1.5 frühere Kopien:** mit rc8 `draft list --json` über die offenen Entwürfe, Summe von `older_copies` (Kopien mit derselben Message-ID, die laut Spiegel noch im Entwürfe-Ordner liegen), aufgeteilt nach Status (`draft`/`pending_approval` sind Kandidaten, `ready` bleibt liegen), dazu die Summe von `remote_previous_uids` aus `draft show --json` (mit rc8 alle vermerkten früheren Kopien). 1.5 zählt nach dem Anhalten noch einmal, das ist die Zahl vor dem Tausch.
  - **0.10 gescheiterte Textauszüge je Konto:** Es gibt **kein Kommando, das gescheiterte Auszüge je Konto zählt** (Zähler in `doctor` folgen mit A12). Das Skript sucht deshalb je Konto `search "has:attachment after:<Datum>" -a <Konto> --limit 500 --json --fields id,cursor` (die letzten `-ExtractDays` Tage, Standard 30; seitenweise mit `--cursor`; höchstens `-ExtractMaxMessages` Nachrichten, Standard 1000) und liest für jede Nachricht `attach list <id> --json` (`extract_status`, `extract_error`). Es nennt je Konto (als „Konto <n>“) Anhänge, `failed` und davon mit vorübergehendem Grund. Mit `-ExtractMessages <IDs>` kommen Nachrichten dazu, deren gescheiterte Anhänge aus dem Betrieb bekannt sind (auch wenn sie älter sind). 0.10b nennt die IDs der vorübergehend gescheiterten Anhänge (höchstens 50), 1.6 schreibt sie als `D17_anhaenge.txt` neben das Programm (nur Zahlen) für B.5.
- **Zwei vollständige Läufe** nach dem Tausch (3.1 und 3.3). Nach dem ersten Lauf: 3.1b frühere Kopien (Aktionen, die der Nachlauf angelegt hat, `cleanup_actions`/`cleanup_copies` aus `sync --json`, und `older_copies` jetzt), 3.1c Stand der vorher vorübergehend gescheiterten Anhänge, 3.1d Zählung je Konto wie 0.10. Nach dem zweiten Lauf: 3.3b Papierkorb-Aktionen `draft_update` seit dem Tausch mit Zustand und Ergebnis je Kopie. Nach einem FAIL im ersten Lauf folgt **kein zweiter**.
- **Sicherung nur mit `-BackupTo`** (optional, **empfohlen**). Abschlüsse und Verschiebungen in den Papierkorb in Teil 3 bleiben auch nach dem Rückweg; rückgängig nur über eine Sicherung ([betrieb.md](betrieb.md) 2.5) bzw. im Papierkorb des Mailprogramms.
- **Ein FAIL in Teil 0 ändert nichts:** Abbruch vor Teil 1 mit den Zeilen `Abbruch: Voraussetzungen fehlen; nichts geaendert` und `Ergebnis: nichts geaendert (…)`. Darauf beruht der Lesetest.
- **Rückweg bei jedem FAIL nach dem Tausch** (E.0): Zuerst führt **E.0d** offene Aktionen der Herkunft `draft_update`/`draft_discard` mit rc9 aus (je Konto ein `sync`), denn rc8 kennt diese Herkünfte nicht (Abschnitt 6). Erst dann kommt rc8 zurück (E.0), E.0c prüft die Datenbank, und die Aufgaben laufen mit rc8 an. Bleibt eine solche Aktion offen, bleibt rc9 an seinem Platz und die Aufgaben bleiben aus (Abschnitt 8).
- **Strg+C im Abschluss abgeschaltet** (E.4).
- **Protokoll** `D17_windows.txt` (Teil B: `D17_teilB.txt`) neben dem Programm, oder die Datei aus `-LogFile`; ein vorhandenes wird mit Zeitstempel beiseitegelegt. Ins Protokoll kommen nur Prüfnummern, IDs, Zustände, Gründe, Zähler, Rollen und Zeiten, **nie** Betreff, Adressen, Mailtext, Ordnernamen, Dateinamen, Schlüssel, Konto- oder Aufgabennamen. Konto- und Aufgabennamen ersetzt das Skript durch „Konto 1“, „Aufgabe 3“ usw., **seit D17 auch nach „-“, „_“, „.“ und mitten in Pfaden** (K5): Heißt ein Konto wie ein Teil des Datenpfads, steht in der Kopfzeile statt des Namens „Konto <n>“. Bei sehr kurzen Kontonamen (ein oder zwei Buchstaben) werden Pfade dadurch stark unkenntlich; das ist gewollt.

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d17` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc9, nicht umbenennen), `SHA256SUMS` und `abnahme_D17.ps1`.
2. **[Claude] Parser-Probe** (Pflicht, das Skript ist nie mit PowerShell gelaufen); erwartet ist eine **leere** Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d17\abnahme_D17.ps1', [ref]$null, [ref]$e); $e
   ```

   Steht dort etwas, nicht weitermachen; Claude meldet Zeile und Meldung.
3. **[Claude]** Konten und Aufgaben nennen lassen: die Konten aus `postkauz.exe accounts list`, die Aufgaben aus der Aufgabenplanung nach Abschnitt 1 (je Konto `sync --watch`, `mcp --http`, `audit export`; **ohne** die Sicherungsaufgabe). Claude zeigt dir die Liste vorher. Außerdem prüft Claude in `config.toml`, dass keine Zeile `previous_copies` steht.
4. **[Claude], optional:** die lokale ID der Nachricht mit den vier PDF-Anhängen, deren Auszug am 19.11.2026 mit „exit status 0xc0000142“ scheiterte (mit rc8 `postkauz search … --json` und `postkauz attach list <id> --json`), als `-ExtractMessages <id>` vormerken. Ohne Angabe findet 0.10 sie nur, wenn sie in den letzten 30 Tagen liegt.
5. **[Claude] Lesetest (fester Schritt vor dem Tausch):** ein echter Aufruf mit allen Angaben wie in Abschnitt 3, aber mit `-OldVersion 0.0.0-probe` und eigener Protokolldatei. Er prüft Parameter, Pfade (0.0a), Prüfsumme, Konten, Aufgaben (0.6, 0.6b, 0.6c), `previous_copies` (0.7b), Ordnerrollen (0.8) und die beiden Zählungen (0.9, 0.10) und **ändert nichts**, weil 0.4 (altes Programm meldet nicht `0.0.0-probe`) FAIL ist:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d17\abnahme_D17.ps1 -Exe '<Ordner des Programms>\postkauz.exe' -DataDir '<Datenordner>' -Config '<Datenordner>\config.toml' -NewExe C:\PostKauz\d17\postkauz-windows-amd64.exe -ExpectSha256 '<SHA256 aus CHANGELOG, Abschnitt 0.3.0-rc9>' -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp -OldVersion 0.0.0-probe -LogFile C:\PostKauz\d17\D17_lesetest.txt
   ```

   Erwartet in `C:\PostKauz\d17\D17_lesetest.txt`: **nur `0.4` und `E.2` als FAIL**, die Zeilen `Abbruch: Voraussetzungen fehlen; nichts geaendert` und `Ergebnis: nichts geaendert (…)`, `Stand: … meldet 0.3.0-rc8; postkauz-rc8.exe: fehlt`. WARN in 0.6b, 0.8, 0.9 oder 0.10 sind erlaubt; Claude nennt sie dir. INFO 0.9 nennt die Zahl der früheren Kopien, INFO 0.10 je Konto die gescheiterten Anhänge. Jeder andere FAIL (etwa 0.0a Pfad fehlt, 0.2b falsche Prüfsumme, 0.5 Konten, 0.6 Aufgabe fehlt, 0.6b Sicherungsaufgabe, **0.7b `previous_copies` gesetzt**) wird vor dem Tausch behoben. Danach `postkauz.exe version` ansehen: weiter rc8, die Aufgaben laufen unverändert.
6. **[Claude]** `-BackupTo` vorschlagen (empfohlen): ein Ordner auf einem verschlüsselten Datenträger wie in [betrieb.md](betrieb.md) 2.2. Default: mit Sicherung, einverstanden?

## 3. Lauf Teil A

**Als welcher Benutzer?** Wie D16: derselbe Benutzer wie der Dauerbetrieb; erhöht nur, wenn die Aufgaben „Mit höchsten Privilegien ausführen“ haben (0.0 nennt es).

**Im Hintergrund starten**, danach das Protokoll mitlesen, bis die Schlusszeile `D17: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d17\abnahme_D17.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d17\postkauz-windows-amd64.exe','-ExpectSha256','<SHA256 aus CHANGELOG, Abschnitt 0.3.0-rc9>','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-KeepStdioMcp','-BackupTo','<Sicherungsordner>'
```

Ohne Sicherung `'-BackupTo','<Sicherungsordner>'` weglassen (dann INFO 1.4). Mit Schritt 4 aus Abschnitt 2 zusätzlich `'-ExtractMessages','<id>'`.

```powershell
Get-Content <Ordner des Programms>\D17_windows.txt -Wait
```

Bestanden ist Teil A nur mit der Schlusszeile `D17: <n> OK, 0 FAIL, <w> WARN, <s> SKIP`. Jedes `WARN` nennt Claude im Bericht.

**Harter Abbruch** (Fenster geschlossen, PC neu gestartet): erst `postkauz.exe version` ansehen. Meldet es rc8, die Aufgaben von Hand einschalten (Aufgabenplanung → Aufgabe → Rechtsklick → „Aktivieren“, dann „Ausführen“; für „audit export“ nur „Aktivieren“). Meldet es rc9 oder fehlt `postkauz.exe`, den Rückweg nach Abschnitt 6 laufen lassen.

## 4. Erwartet (Teil A)

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Rechte | INFO: läuft die PowerShell erhöht |
| 0.0a | **Pfade (neu, K3)** | OK: `-Exe` ist eine Datei, `-DataDir` ein Ordner, `-Config` eine Datei (ohne Angabe: Exit 2 vor dem Protokoll) |
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc9 passt zu `SHA256SUMS` und `-ExpectSha256`; das laufende Programm ist rc8 |
| 0.5, 0.6 | Konten, Aufgaben | `-Accounts` genau die Konten aus `accounts list` (mit `-DataDir`/`-Config`); jede Aufgabe vorhanden |
| 0.6b | keine Sicherungsaufgabe | OK; WARN, wenn nur der Name nach Sicherung klingt |
| 0.6c | **Skripte der Aufgaben (neu, K1)** | INFO: Zahl der `.ps1` in den Aktionen der Aufgaben |
| 0.7 | `attach_deny_*` | INFO ja/nein |
| 0.7b | **`[draft] previous_copies` (neu)** | OK: nicht gesetzt. FAIL, wenn gesetzt (rc8 startet damit nicht) |
| 0.8 | Ordnerrollen je Konto | INFO „Rollen drafts ja, trash ja (in Ordnung)“; WARN bei `drafts` ohne `trash` (frühere Kopien bleiben liegen, `no_trash_mailbox`) oder `all` ohne `sent` (wie D16) |
| 0.9 | **frühere Kopien vorher (neu)** | INFO mit rc8: offene Entwürfe, Summe `older_copies`, davon Kandidaten (`draft`/`pending_approval`) und `ready`, Summe `remote_previous_uids` |
| 0.10, 0.10b | **gescheiterte Anhänge je Konto (neu)** | INFO je Konto: Nachrichten mit Anhang der letzten 30 Tage, Anhänge, `failed`, davon mit vorübergehendem Grund; 0.10b die IDs der vorübergehend gescheiterten. WARN, wenn die Suche nicht lesbar ist |
| 1.1 | **Aufgaben anhalten (K1)** | INFO: beendete PowerShell mit einem Skript der Aufgaben, beendete postkauz; OK: außer dem stdio-MCP läuft kein postkauz. Ein FAIL nennt den wahrscheinlichen Bedienfehler („eine Aufgabe fehlt in `-Tasks`“) |
| 1.1b | **nach 75 s (neu, K1)** | OK: kein Hilfsskript der Aufgaben und kein postkauz neu gestartet |
| 1.2 | `doctor` vorher | `database ok`, Schema 22 |
| 1.3, 1.3a | offene Entwürfe, Marke im Audit | lesbar, unter 500; INFO mit der höchsten Audit-ID |
| 1.4 | Sicherung | mit `-BackupTo`: Exit 0 und `backup written to`; sonst INFO |
| 1.5 | **frühere Kopien vor dem Tausch** | INFO wie 0.9, nach dem Anhalten; dazu die Ankündigung für den ersten Lauf |
| 1.6 | **Liste für B.5** | INFO: `D17_anhaenge.txt` neben dem Programm geschrieben (nur IDs) |
| 2.1 | Tausch | rc9 läuft, `postkauz-rc8.exe` daneben |
| 2.2, 2.3 | `doctor` nachher | `database ok`, Schema unverändert (22), keine neue Warnung; Zeile `attach_guard` da |
| 3.1, 3.3 | `sync -a <Konto> --json` je Konto, erster und zweiter Lauf | Exit 0 und `errors` 0 (OK) oder nach einer sofortigen Wiederholung ohne Fehler (WARN). Die Zeile nennt neu `cleanup_actions`, `cleanup_copies` (Nachlauf), ausgeführte Aktionen (ok, failed, stale) und Auszüge |
| 3.1b | **frühere Kopien nach dem ersten Lauf** | INFO: Aktionen und Kopien aus dem Nachlauf (Herkunft `draft_update`), Vergleich mit 1.5, `older_copies` jetzt (noch etwa wie vorher; die Aktionen laufen im zweiten Lauf) |
| 3.1c | **Anhänge nach dem ersten Lauf** | INFO: von den vorher vorübergehend gescheiterten jetzt `ok`, `pending`, `failed` mit neuem Grund, `failed` mit altem Grund. WARN, wenn noch welche mit altem vorübergehendem Grund `failed` sind |
| 3.1d | Zählung je Konto nach dem ersten Lauf | INFO wie 0.10 (mit rc9) |
| 3.2, 3.4 | Abschlüsse seit der Marke | jeder mit passendem Grund aus dem Audit (wie D16; neu möglich `abandoned` mit `discarded`). Nach einem FAIL in 3.1/3.2 **kein zweiter Lauf** |
| 3.3b | **Papierkorb-Aktionen nach dem zweiten Lauf** | INFO: Aktionen `draft_update` seit dem Tausch nach Zustand (`done` …) und Ergebnis je Kopie (`ok`, `stale`, `not_found`), `older_copies` jetzt (kleiner als vorher). WARN bei `failed`/`partial` (etwa `stale`: Kopie im Mailprogramm geändert; der Nachlauf versucht es bis zu dreimal) |
| 3.4c, 3.4d | Summe, geplante Entwürfe | INFO (wie D16, dazu `discarded`) |
| 3.5 | **[Du]** | Stimmen die Abschlüsse? Claude zeigt dir je ID `draft show <id>` (Betreff nur am Bildschirm) |
| 3.6, 3.7 | `doctor`, `audit verify` | `database ok`, Schema 22, keine neue Warnung; `ok` |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.4 | Abschluss | INFO: Strg+C bis zum Ende abgeschaltet |
| E.0d, E.0, E.0c | nach FAIL | zuerst keine offene Aktion `draft_update`/`draft_discard` (sonst je Konto ein `sync` mit rc9, E.0da/E.0db), dann altes Programm zurück (`postkauz.exe` meldet rc8), Datenbank mit rc8 in Ordnung; sonst bleiben die Aufgaben aus |
| 4.1 bzw. E.1 | Aufgaben wieder an | je Aufgabe gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); je Konto ein `sync --watch` genau mit `-Exe`; `postkauz.exe` meldet rc9 (bzw. rc8 nach E.0) |
| Stand | letzte Zeilen | welche Version an `postkauz.exe` liegt und welche als `postkauz-rc8.exe` |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt.

## 5. Teil B (Agentenschlüssel, eigenes Konto)

**Voraussetzungen:** Teil A bestanden, die Aufgaben laufen mit rc9 (`sync --watch` legt Entwürfe ab und führt Aktionen aus). **Nur im eigenen Konto**, nie im geteilten ohne deine Anweisung. Das Konto braucht einen Entwürfe-Ordner (Rolle `drafts`) und einen Papierkorb (Rolle `trash`, BV.3).

- **Agentenschlüssel (empfohlen):** `-AgentKeyFile <Datei>` mit dem API-Schlüssel eines Agenten (`pk_key_…`, Rolle `assistant`, etwa der der Claude-App). Dann laufen Anlegen (`mail_create_draft`), Ändern (`mail_update_draft`), `mail_supersede_draft`, `mail_discard_draft`, `mail_get_draft`, `mail_get_action`, `mail_list_drafts` und `mail_get_attachment_text` über den MCP-Server wie bei einem Agenten. **Schlüssel und Dateiname stehen nie im Protokoll.** Ohne die Datei läuft Teil B als Mensch an der CLI (`draft new`, `draft edit`, `draft supersede`, `draft discard`, `attach text`; die MCP-Prüfungen B.1i und B.3f stehen dann als INFO bzw. ohne MCP-Teil da).
- **Probe-Entwürfe:** neue Mails **an die eigene Adresse** des Kontos (aus `accounts list`, nie im Protokoll). Betreff „D17 Probe B.x <Zeitstempel>“; B.1 zusätzlich mit Umlauten. **Nichts wird gesendet.**
- **Ins Protokoll** kommen nur IDs, Zustände, Gründe, Zähler und Zeiten (nie Papierkorb- oder Ordnernamen, auch nicht `trash` oder `next_step` aus der Ausgabe von `discard`/`supersede`).

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d17\abnahme_D17.ps1','-TeilB','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-SelfAccount','<eigenes Konto>','-AgentKeyFile','<Datei mit dem Agentenschlüssel>'
```

Protokoll mitlesen: `Get-Content <Ordner des Programms>\D17_teilB.txt -Wait`. Nach einem Bedienfehler in B.4 nur diesen Schritt wiederholen: zusätzlich `'-Steps','B.4'`.

**Ablauf und Fristen** (jede Wartezeit hat einen eigenen Parameter, K2; die Zeile „INFO B.4c JETZT: …“ sagt, wann du dran bist):

| Schritt | Wer | Was | Frist (Parameter) |
|---|---|---|---|
| B.1a, B.1b | Skript | Entwurf mit Umlaut-Betreff anlegen, warten, bis die Kopie im Spiegel des Entwürfe-Ordners liegt | 12 Minuten (`-WaitPushMinutes`) |
| B.1c, B.1d | Skript | `drafts.update` (neuer Text); nach der Ruhezeit von 30 s und einem Job-Takt (10 s) liegt die neue Kopie im Entwürfe-Ordner | 12 Minuten (`-WaitPushMinutes`) |
| B.1e, B.1f | Skript | Papierkorb-Aktion für die alte Kopie erscheint in `cleanup_action_ids` und ist ausgeführt | **2 Minuten** nach der neuen Kopie (`-WaitCleanupMinutes`) |
| B.2a, B.2b | Skript | Entwurf anlegen, warten nur bis `remote: pushed` | 12 Minuten (`-WaitPushMinutes`) |
| B.2c, B.2d | Skript | sofort einen Nachfolger mit gleichem Betreff anlegen und `drafts.supersede`; bei `copy_not_mirrored` nach 10 s erneut | 30 Sekunden ab dem ersten Aufruf (fest) |
| B.2f | Skript | Aktion `draft_supersede` ausgeführt, alte Kopie im Papierkorb | 2 Minuten (`-WaitCleanupMinutes`) |
| B.3b, B.3c | Skript | den Nachfolger aus B.2 (sonst einen neuen Entwurf) im Spiegel abwarten, `drafts.discard` | 12 Minuten (`-WaitPushMinutes`), Wiederholung 30 s |
| B.3e | Skript | Aktion `draft_discard` ausgeführt | 2 Minuten (`-WaitCleanupMinutes`) |
| B.4a, B.4b | Skript | Entwurf „D17 Probe B.4 …“ anlegen, Kopie im Spiegel | 12 Minuten (`-WaitPushMinutes`) |
| **B.4c, B.4d** | **[Du]** | im Webmail den Entwurf „D17 Probe B.4 …“ öffnen, **den Text um einen Satz ergänzen** und den Entwurf schließen (Gmail speichert ihn). **Nicht senden, nicht planen, nicht löschen.** Das Skript wartet, bis ein vollständiger Lauf die Änderung erkennt (`remote: taken_over`) | **20 Minuten** (`-WaitEditMinutes`), darin enthalten der Lauf, der die Änderung erkennt |
| B.4e bis B.4g | Skript | erst jetzt `drafts.discard` und `drafts.update` (Trockenlauf) | sofort |
| B.4h | Skript | ein weiterer **vollständiger Lauf** des Kontos (K4) | 10 Minuten (`-WaitRunMinutes`) |
| B.4i | **[Du]**, ohne Frist | aufräumen: den geänderten Entwurf im Webmail löschen, danach `postkauz draft abandon <id>` | – |
| B.5 | Skript | Anhänge aus Teil A prüfen, Text eines Anhangs lesen | – |

**Erwartet:**

| Nr | Prüfung | Erwartet |
|---|---|---|
| BV.1 bis BV.7 | Version, Konto, Rollen, Schlüssel, `sync --watch`, Konfiguration, Marke | rc9; Konto konfiguriert, Adresse bekannt; Rollen `drafts` und `trash`; MCP-Sitzung angemeldet (oder INFO); ein `sync --watch` genau mit `-Exe` für das Konto (FAIL nennt den wahrscheinlichen Bedienfehler „Aufgaben nach Teil A nicht wieder an“); `previous_copies` nicht gesetzt; INFO Marke |
| B.1 | Betreff | INFO: Länge und geschätzte Länge als encoded-words (mehr als 78: True) |
| B.1a, B.1b | Entwurf, erste Kopie | angelegt; Kopie im Spiegel (UID) |
| B.1c, B.1d | Änderung, neue Kopie | `drafts.update` angenommen; neue UID im Spiegel, Sekunden seit der Änderung (etwa 30 bis 60) |
| B.1e | `draft show --json` | `cleanup_action_ids` nennt eine Aktion |
| B.1f, B.1fb | `action show <id> --json` | `kind trash`, `source draft_update`, `draft_id` des Entwurfs, `state done`, Ziel die alte UID mit `result ok`; die Kopie liegt laut `show` im Papierkorb (Rolle `trash`), nicht mehr im Entwürfe-Ordner. INFO mit den Sekunden von der Änderung bis zur ausgeführten Aktion; WARN über 120 s |
| B.1g | nur die neue Kopie | `remote pushed` mit neuer UID, `remote_previous_uids` leer, `older_copies` 0, neue Kopie im Spiegel |
| B.1h | Audit `drafts.cleanup_queued` | Akteur `system`, `source draft_update`, `action_id` wie B.1e, `copies 1`, `result ok` |
| B.1i | MCP (nur mit Schlüssel) | `mail_get_draft` nennt die Aktion in `cleanup_action_ids`; `mail_get_action` zeigt `source draft_update` und `draft_id` |
| B.2a bis B.2c | alter Entwurf, Nachfolger | angelegt; alter `remote pushed` (ohne auf den Spiegel zu warten) |
| B.2d | `drafts.supersede` | OK beim ersten Aufruf oder nach **einmal** `copy_not_mirrored` beim zweiten binnen 30 s; WARN nach mehr Aufrufen; FAIL bei `copy_missing` („Befund B2 noch da“) oder einem anderen Grund |
| B.2e | alter Entwurf | `abandoned`, `closed_reason superseded_by_draft`, `superseded_by_draft_id`, `supersede_action_id` |
| B.2f, B.2fb | Aktion | `source draft_supersede`, `done`, Ziel `ok`; alte Kopie im Papierkorb |
| B.3a bis B.3c | `drafts.discard` | Kopie im Spiegel; Ergebnis `status abandoned`, `copies` 1 oder mehr, `action_id` |
| B.3d | `draft show --json` | `abandoned`, `closed_reason discarded`, `closed_note` vorhanden |
| B.3e, B.3eb | Aktion | `source draft_discard`, `done`, Ziel `ok`; Kopie im Papierkorb |
| B.3f | offene Entwürfe | nicht mehr in `draft list --json` (offene Status) und, mit Schlüssel, nicht in `mail_list_drafts` |
| B.3g | Audit `drafts.discard` | Eintrag mit Grund `discarded` |
| B.4a, B.4b | Entwurf | angelegt; Kopie im Spiegel |
| B.4c | **[Du]** | INFO „JETZT …“: Text ergänzen, nicht senden |
| B.4d | Handgriff erkannt | `remote taken_over`, Status offen. Sonst FAIL mit dem erkannten **Bedienfehler** (Tabelle unten), und **kein** Aufruf von `discard` oder `update` |
| B.4e | `drafts.discard` | abgelehnt mit `copy_changed`. Angenommen wäre ein **Programmfehler** |
| B.4f | `drafts.update` (Trockenlauf) | abgelehnt mit `draft_taken_over` |
| B.4g | nichts verschoben | Entwurf offen und `taken_over`, keine `cleanup_action_ids`, keine Aktion mit diesem `draft_id` in `action list --json` |
| B.4h | nach einem weiteren vollständigen Lauf | unverändert (der Nachlauf lässt die geänderte Kopie liegen); WARN, wenn kein vollständiger Lauf erkennbar war (der Zustand wird trotzdem geprüft) |
| B.4i | **[Du]** | INFO: aufräumen (Abschnitt 7) |
| B.5a | Anhänge aus Teil A | jeder vorher vorübergehend gescheiterte Anhang ist `ok`, `pending` oder `failed` mit neuem Grund (endgültig, oder nach mehreren Versuchen mit „(<n> attempts)“). FAIL bei `failed` mit dem alten vorübergehenden Grund. Den Zähler der Versuche eines `pending`-Anhangs zeigt `attach list` nicht (er steht nur in `meta`) |
| B.5b | Text | `mail_get_attachment_text` (ohne Schlüssel `attach text <id> --json`) liefert für einen davon Text: `extract_status ok`, Textlänge größer 0 (nur die Länge steht im Protokoll) |
| B.9 | Aufräumen | INFO: übrige eigene Probe-Entwürfe (außer B.4) verwirft das Skript selbst mit `drafts.discard`; noch offene nennt es |
| E.2 | Abschlussmerker | FAIL, wenn Teil B vor dem Ende abbricht |

**Bedienfehler in B.4 (K7):** Das Skript erkennt den wahrscheinlichsten Bedienfehler und schreibt ihn ausdrücklich als „Bedienfehler (kein Programmfehler)“ ins Protokoll:

| Beobachtung | Text im Protokoll | Was tun |
|---|---|---|
| Entwurf wird `sent_by_human` | im Webmail gesendet statt nur geändert (eine echte Mail an die eigene Adresse, harmlos) | B.4 mit `-Steps B.4` wiederholen |
| Entwurf wird `abandoned` mit `discarded`, bevor `taken_over` erkannt ist | ein Agent hat `drafts.discard` **zu früh** aufgerufen (vor dem vollständigen Lauf, der `taken_over` setzt) | die geänderte Fassung im Webmail löschen; B.4 wiederholen; Agenten erst nach `remote: taken_over` rufen lassen |
| neue Fassung abgelegt, Aktion angelegt oder `remote pending`, bevor `taken_over` erkannt ist | ein Agent hat `drafts.update` **zu früh** aufgerufen | wie oben |
| `remote scheduled` | im Webmail geplant statt geändert | im Ordner „Geplant“ „Senden abbrechen“, dann B.4 wiederholen |
| `remote missing` nach der Frist | im Webmail gelöscht statt geändert (oder Gmail hat beim Ändern eine neue Message-ID vergeben) | Claude prüft im Webmail; B.4 wiederholen |
| keine Änderung in der Frist | nicht geändert, nicht gespeichert, anderer Entwurf geändert, oder kein vollständiger Lauf (dann `sync --watch` prüfen) | B.4 wiederholen |

Das Skript selbst ruft `discard` und `update` **nur** nach `remote: taken_over` auf; ein Aufruf „zu früh“ kann also nur von einem anderen Agenten (etwa der Claude-App) kommen. Während B.4 deshalb in der Claude-App keine Entwurfswerkzeuge für diesen Entwurf benutzen.

Bestanden ist Teil B mit `D17: <n> OK, 0 FAIL, <w> WARN, 0 SKIP`. Wird nach einem Bedienfehler nur B.4 wiederholt, gilt das erste Protokoll für B.1 bis B.3 und B.5 und das zweite für B.4.

## 6. Rückweg (nur bei Problemen)

A11 hat **keine Migration** (Schema 22). Zwei Dinge sind anders als bei D16 ([betrieb.md](betrieb.md) 1.7c):

1. **`[draft] previous_copies` darf nicht gesetzt sein** (rc8 startet damit nicht): R.0g prüft das vor jedem Schritt.
2. **rc8 kennt die Herkünfte `draft_update` und `draft_discard` nicht** und behandelte eine solche Aktion wie eine gewöhnliche Papierkorb-Aktion **ohne** die Grenze „nur Entwürfe-Ordner“. Deshalb prüft das Skript **nach** dem Anhalten der Aufgaben und **vor** dem Tausch mit dem **neuen** Programm (nur es zeigt `source`): `action list --state requested,queued,running --limit 500 --json`, für jede genannte Aktion `action show <id> --json`. Ist eine mit `source` `draft_update` oder `draft_discard` offen, läuft je Konto ein `sync` mit dem neuen Programm (führt sie aus), danach wird erneut geprüft. Legt der Nachlauf dieses Laufs eine **neue** Aktion an (Wiederholung einer Kopie), folgt höchstens noch ein Durchgang (insgesamt drei). **Bleibt dieselbe Aktion offen, bricht der Rückweg ab**, nichts wird getauscht, und die Aufgaben laufen mit dem neuen Programm wieder an (betrieb.md 1.7c).

Vollständig, im Hintergrund wie in Abschnitt 3:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d17\abnahme_D17.ps1','-Rueckweg','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d17\postkauz-windows-amd64.exe','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-KeepStdioMcp'
```

Protokoll `D17_windows.txt`. Erwartet, in dieser Reihenfolge:

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0a | Pfade (K3) | `-DataDir` ein Ordner, `-Config` eine Datei (`-Exe` darf nach einem harten Abbruch fehlen) |
| R.0 | altes Programm | `postkauz-rc8.exe` vorhanden |
| R.0b | laufendes Programm | meldet rc9 (oder INFO, wenn `postkauz.exe` fehlt) |
| R.0c | `postkauz-rc8.exe` | meldet rc8 |
| R.0d, R.0e, R.0f | Angaben, Aufgaben | `-Tasks` und `-Accounts` angegeben, jede Aufgabe vorhanden, keine Sicherungsaufgabe |
| R.0g | **`previous_copies`** | nicht gesetzt |
| R.0h | **Konten mit dem alten Programm (K3)** | `postkauz-rc8.exe accounts list --json` mit `-DataDir`/`-Config` nennt genau `-Accounts` (prüft zugleich, dass rc8 mit Datenordner und Konfiguration startet) |
| R.0i | Programm für die Prüfung der Aktionen | `postkauz.exe` meldet rc9, sonst `-NewExe` |
| R.1, R.1b | Aufgaben anhalten (K1) | wie 1.1 und 1.1b |
| R.2 (R.2a, R.2b) | **offene Aktionen `draft_update`/`draft_discard`** | OK: keine. Sonst INFO, je Konto ein `sync` mit rc9 (R.2a), dann OK „keine mehr offen“ (R.2b); FAIL „bleibt offen“ bricht ab |
| R.3 | Tausch | neues Programm als `postkauz-rc9.exe` beiseite, altes an seine Stelle |
| R.4 | rc8 mit der Datenbank | rc8, `database ok`, Schema 22 |
| R.5 | je Konto ein `sync` mit rc8 | Exit 0 und `errors` 0 (eine Wiederholung erlaubt, dann WARN) |
| R.6 | INFO zum Fall | siehe unten |
| R.7 | Aufgaben wieder an | mit rc8 |

Kein ändernder Schritt vor R.1: Ein FAIL in R.0 bis R.0i endet mit `Abbruch: Rueckweg nicht moeglich; nichts geaendert`, ein FAIL in 0.0a mit `Abbruch: Pfade fehlen; nichts geaendert`. Fehlt `postkauz.exe` (harter Abbruch) und bricht R.2 ab, bleiben die Aufgaben aus (kein Programm an seinem Platz).

**Bleibt der Rückweg nach R.2 stehen** (FAIL in R.3 bis R.5 oder Strg+C), bleiben die Aufgaben **aus** (Schlusszeile mit `E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“). Dann `postkauz.exe version` und `postkauz.exe doctor` von Hand ansehen und die Aufgaben erst einschalten, wenn beide rc8 und `database: ok` zeigen. **Bricht er in R.2 ab**, laufen die Aufgaben mit rc9 weiter (E.1 mit rc9); Claude sieht sich die offene Aktion an (`action show <id>`), Default: Rückweg nach dem nächsten Lauf über alle Ordner wiederholen.

**Mit rc8 gilt:** Mit rc9 verschobene Kopien bleiben im Papierkorb, verworfene Entwürfe bleiben `abandoned` (rc8 zeigt bei ihnen keinen `closed_reason` `discarded`). rc8 räumt nach einer Änderung nicht auf: frühere Kopien bleiben wieder liegen. Die Vermerke in `meta` (`draft_copy_cleanup/…`, `extract_retry/…`, `extract_requeue/…`) bleiben liegen; ein Anhang, der auf einen Wiederholversuch wartet, bleibt `pending` und wird von rc8 ohne Wartezeit erneut versucht.

## 7. Aufräumen nach Teil B

- **Nach einem vollständigen Lauf:** B.9 hat die eigenen Probe-Entwürfe B.1 bis B.3 schon verworfen (Kopien im Papierkorb). **[Du]** löschst den geänderten Entwurf „D17 Probe B.4 …“ im Webmail und schließt ihn danach in PostKauz: `postkauz draft abandon <id>` (ID aus B.4a). Die Kopien im Papierkorb dürfen liegen bleiben (Gmail leert den Papierkorb nach 30 Tagen) oder im Webmail endgültig gelöscht werden.
- **Nach einem Abbruch:** B.9 nennt noch offene Probe-Entwürfe mit ID. Für jeden zuerst im Webmail nachsehen: Liegt er unter „Geplant“, „Senden abbrechen“. Einen im Webmail geänderten Entwurf löschst du dort und schließt ihn mit `postkauz draft abandon <id>`; einen unveränderten schließt Claude mit `postkauz draft discard <id>` (Kopie in den Papierkorb).

## 8. Wenn etwas FAIL ist

- **Lesetest:** Jeder FAIL außer 0.4 und E.2 wird vor dem Tausch behoben. Fehlt die Zeile „nichts geaendert“, nicht weitermachen und Claude fragen.
- **0.0a FAIL:** ein Pfad stimmt nicht. Default: Pfade aus den Aufgaben übernehmen (Claude zeigt sie dir), neu starten.
- **0.7b, BV.6 oder R.0g FAIL:** `previous_copies` steht in `config.toml`. Default: Zeile entfernen, neu starten.
- **1.1 oder 1.1b FAIL:** Es läuft noch ein postkauz oder ein Hilfsskript startet es neu. Wahrscheinlich fehlt eine Aufgabe in `-Tasks` (Bedienfehler). Das Skript tauscht nichts und schaltet die Aufgaben wieder ein. Default: Claude sucht mit dir die fehlende Aufgabe, dann neu.
- **Teil A nach dem Tausch:** Nichts von Hand tun. Das Skript führt offene Aktionen `draft_update` mit rc9 aus (E.0d), legt rc8 zurück (E.0, E.0c) und startet die Aufgaben mit rc8. Claude schickt dir die Zeilen mit FAIL und einen Vorschlag. **E.0 FAIL „Aktion bleibt offen“:** rc9 bleibt an seinem Platz, die Aufgaben bleiben aus. Default: Claude prüft `action show <id>`; danach entweder die Aufgaben mit rc9 von Hand einschalten (die Aktion läuft dann im Dauerbetrieb) oder später den Rückweg (Abschnitt 6).
- **`E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“:** `postkauz.exe version` und `postkauz.exe doctor` ansehen; zeigen sie rc8 (bzw. rc9 nach einem bestandenen Teil 3) und `database: ok`, die Aufgaben von Hand einschalten. Sonst Claude fragen.
- **3.1c WARN / B.5a FAIL** (Anhang mit altem vorübergehendem Grund `failed`): Das einmalige Zurücksetzen hat ihn nicht erfasst. Default: Claude meldet die ID; der Anhang lässt sich mit `postkauz attach text <id>` trotzdem lesen (rc9 versucht dort den Auszug neu).
- **B.1d bis B.1g FAIL** (keine neue Kopie, keine Aktion oder Aktion nicht `done`): Im Webmail nachsehen, ob zwei Fassungen „D17 Probe B.1 …“ liegen; nichts löschen. Claude meldet Aktion, Zustand und Ergebnis (`stale`, `not_found`).
- **B.2d FAIL `copy_missing`:** Befund B2 ist nicht behoben. Der alte Entwurf bleibt offen; Claude schließt ihn danach mit `postkauz draft discard <id>`.
- **B.3 FAIL:** Entwurf bleibt offen oder Kopie bleibt liegen; Claude meldet es, Aufräumen nach Abschnitt 7.
- **B.4d FAIL:** Bedienfehler nach der Tabelle in Abschnitt 5; B.4 mit `-Steps B.4` wiederholen.
- **B.4e FAIL „Programmfehler: discard trotz remote taken_over angenommen“:** Die geänderte Fassung im Webmail nicht löschen; Claude meldet es sofort.
- **B.4f/B.4g/B.4h FAIL:** Claude meldet Zustand und Aktionen; nichts im Webmail tun, bis Claude es prüft.

## 9. Was aus der Gegenprüfung von D16 umgesetzt ist

Die Punkte aus [A11](../auftraege/A11_alltag5.md), Teil 0 (c) (D16 selbst bleibt als Protokoll unverändert):

| Punkt | In D17 |
|---|---|
| **K1** `Stop-Tasks` beendete nur Hüllen mit `run-sync-watch.ps1`; die Hülle der Aufgabe für `mcp --http` startete das alte Programm neu | **umgesetzt:** `Get-TaskScripts` liest alle `.ps1` aus den Aktionen der Aufgaben aus `-Tasks` (in `"…"`, `'…'` oder ohne Anführungszeichen, relativ zum Arbeitsordner der Aktion); 1.1 (und R.1, E.0a) beendet **jedes** PowerShell, dessen Befehlszeile eines davon nennt (voller Pfad, oder Dateiname ohne Ordner bzw. mit `.\`), dazu wie D16 `run-sync-watch.ps1` dieses Dauerbetriebs. Danach Prüfung „kein postkauz außer stdio-MCP“ und **1.1b** nach 75 s: kein Hilfsskript, kein neu gestarteter postkauz. Die Prozessliste umfasst auch umbenannte Programme (`postkauz%`) |
| **K2** keine eigene Frist für den Handgriff, `WaitMinutes + 25` | **umgesetzt:** Der einzige Handgriff (B.4) hat `-WaitEditMinutes` (20); auch die Wartezeiten des Skripts haben je einen eigenen Parameter (`-WaitPushMinutes` 12, `-WaitCleanupMinutes` 2, `-WaitRunMinutes` 10). Alle stehen in der Tabelle „Ablauf und Fristen“ (Abschnitt 5) |
| **K3** Standardwerte `C:\PostKauz\…` für `-Exe`, `-DataDir`, `-Config`; der Rückweg prüfte die Pfade erst nach dem Anhalten und Tausch | **umgesetzt:** keine Standardwerte; ohne Angabe Exit 2 vor jedem Schritt, 0.0a prüft, dass sie vorhanden sind; jedes `.EXAMPLE` und jedes Beispiel hier nennt alle drei. Im Rückweg vor dem ersten ändernden Schritt (R.1) **R.0h** `accounts list` mit dem **alten** Programm gegen `-Accounts` und R.0g `previous_copies` |
| **K4** „weiterer Lauf“ galt als erkannt, sobald der Entwürfe-Ordner neu gespiegelt war, plus 30 s | **umgesetzt:** `Wait-FullRun` (B.4h) wartet auf einen neueren **abgeschlossenen Lauf des ganzen Kontos**: Jeder bisher gespiegelte Ordner des Kontos muss laut `mailbox list --json` (`last_sync_at`) nach dem Beginn des Wartens neu gespiegelt sein, danach 60 s für die Prüfung der Entwürfe nach dem letzten Ordner. Grund (aus dem Quelltext): Seit rc9 spiegelt die Ablage den Entwürfe-Ordner sofort (`SyncMailbox` nur für diesen Ordner), IDLE spiegelt nur den Posteingang; nur ein Lauf über alle Ordner (`SyncAll`) aktualisiert alle Ordner und prüft danach die Entwürfe samt Nachlauf. Ein eigenes Kennzeichen „Lauf fertig“ gibt es nicht (der Job von `sync request` ist von außen nicht lesbar, und eine weitere Anforderung während eines Laufs würde ihn erneut einreihen); `sync request` ohne Ordner kommt deshalb nur am Anfang und alle 4 Minuten. B.4d wartet direkt auf `remote: taken_over`, das erst ein vollständiger Lauf setzt |
| **K5** Maskierung ersetzte nicht nach „-“; ein Kontoname als Teil des Datenpfads stand in der Kopfzeile | **umgesetzt:** Namen gelten als Wort, wenn davor und danach kein Buchstabe und keine Ziffer steht (also auch nach „-“, „_“, „.“), und in jeder Zeichenfolge mit `\` oder `/` (Pfade) auch mitten im Wort. Die Namen werden intern durch Marken ohne Buchstaben und Ziffern ersetzt und erst beim Schreiben als „Konto <n>“/„Aufgabe <n>“ ausgegeben; das Kürzel am Zeilenanfang („OK 1.2“) wird nie maskiert |
| **K7** ein Bedienfehler bekam den Text eines Programmfehlers | **umgesetzt:** B.4 erkennt die wahrscheinlichsten Bedienfehler (gesendet, geplant, gelöscht, nicht geändert, Agent zu früh mit `discard` oder `update`) und nennt sie „Bedienfehler (kein Programmfehler)“ (Tabelle in Abschnitt 5); nach einem Bedienfehler ruft das Skript weder `discard` noch `update` auf, und `-Steps B.4` wiederholt nur diesen Schritt. Auch 1.1, 1.1b und BV.5 nennen den wahrscheinlichen Bedienfehler (Aufgabe fehlt in `-Tasks`, Aufgaben nicht wieder an) |
| K6 | nicht Teil dieses Auftrags (A11 Teil 0 (c) nennt ihn nicht) |

**Anforderungen aus A11 Teil 4 und wo sie im Skript stehen:**

| Anforderung | Schritt |
|---|---|
| `-ExpectVersion 0.3.0-rc9`, `-OldVersion 0.3.0-rc8`, altes Programm als `postkauz-rc8.exe` | Parameter; 0.3, 0.4, 2.1, R.0, R.0c |
| Lesetest vor dem Tausch als fester Schritt | Abschnitt 2, Schritt 5; Teil 0 ändert nichts |
| `doctor` vorher/nachher, Schema unverändert, `audit verify` ok, zwei vollständige Läufe | 1.2, 2.2, 3.6, 3.7, 3.1, 3.3 |
| `[draft] previous_copies` nicht gesetzt | 0.7b (FAIL), BV.6, R.0g |
| frühere Kopien offener Entwürfe vor dem Tausch zählen und nach dem ersten Lauf erneut nennen | 0.9, 1.5; 3.1b (und 3.3b nach dem zweiten Lauf) |
| Anhänge mit `failed` je Konto vorher und nach dem ersten Lauf | 0.10, 0.10b; 3.1c, 3.1d |
| Rückweg: nach dem Anhalten, vor dem Tausch mit dem neuen Programm keine offene Aktion `draft_update`/`draft_discard`; sonst `sync`, erneut prüfen, sonst Abbruch | R.1, R.2 (R.2a, R.2b); ebenso E.0d vor E.0 in Teil A |
| PowerShell 5.1: UTF-8 mit BOM, nur ASCII, CRLF; Parser-Probe und Lesetest als feste Schritte | Datei; Abschnitt 2, Schritte 2 und 5 |
| Teil B.1: Umlaut-Betreff über 78 Zeichen als encoded-words, Ablage, `drafts.update`, nach 30 s Ruhezeit, Job-Takt und Aktion (Frist 2 Minuten) nur die neue Kopie im Entwürfe-Ordner, die alte im Papierkorb; `cleanup_action_ids`, `source draft_update`, `done` | B.1 bis B.1i |
| Teil B.2: sofort nach `remote: pushed` Nachfolger und `drafts.supersede`: ok oder einmal `copy_not_mirrored` und ok binnen 30 s; alte Kopie im Papierkorb | B.2a bis B.2f |
| Teil B.3: `drafts.discard` → `abandoned` (`discarded`), Kopie im Papierkorb, nicht mehr offen | B.3a bis B.3g |
| Teil B.4: Mensch ändert die Kopie; erst nach `remote: taken_over` `discard` und `update` → `copy_changed` bzw. `draft_taken_over`, nichts verschoben; zu früh = Bedienfehler | B.4a bis B.4i |
| Teil B.5: vorübergehend gescheiterte Anhänge `ok`, `pending` oder mit neuem Grund `failed`; `mail_get_attachment_text` liefert Text | B.5a, B.5b |

**Quelltext, auf dem das Skript beruht** (Stand dieses Zweigs): `draft discard <draft-id>` und `drafts.discard` (internal/cli/action_bindings.go, internal/ops/draftdiscard.go: `dry_run`, `draft_id`, `status`, `copies`, `skipped`, `trash`, `action_id`, `next_step`); `action show`/`action list --state … --limit … --json` mit `source` und `draft_id` (internal/ops/actions.go, `ActionView`); `draft show --json` mit `cleanup_action_ids`, `remote_previous_uids`, `older_copies`, `closed_reason`, `closed_note`, `supersede_action_id` (internal/ops/drafts.go); `sync --json` mit `draft_check.cleanup_actions`/`cleanup_copies`, `actions`, `attachments_extracted` (internal/ops/sync.go, internal/drafts/detect.go); Nachlauf `RetryCleanup` nach der Prüfung der Entwürfe, Aktionen laufen am Anfang des nächsten Laufs (internal/mailsync/mailsync.go, internal/drafts/cleanup.go); `attach list`/`attach text` mit `extract_status`, `extract_error`, `extracted_now` und die vorübergehenden Muster (internal/ops/threads.go, internal/extract/extract.go, internal/store/extractretry.go); MCP-Werkzeuge `mail_discard_draft`, `mail_supersede_draft`, `mail_update_draft`, `mail_get_draft`, `mail_get_action`, `mail_list_drafts`, `mail_get_attachment_text` (internal/mcp/tools.go).

**Grenzen:**

- **Zählung der Anhänge:** Es gibt kein Kommando, das gescheiterte Auszüge je Konto zählt. 0.10 und 3.1d zählen nur Nachrichten mit Anhang aus den letzten `-ExtractDays` Tagen (höchstens `-ExtractMaxMessages` je Konto) und die aus `-ExtractMessages`; das Zurücksetzen in rc9 erfasst dagegen **alle** Anhänge des Kontos. Die Zahlen sind deshalb eine Stichprobe, keine Gesamtzahl. Den Zähler der Versuche eines `pending`-Anhangs zeigt keine Ausgabe (nur `meta`).
- **Zählung der früheren Kopien:** 0.9 und 1.5 zählen nur offene Entwürfe. Kopien von Entwürfen, die in den letzten 7 Tagen per `supersede` geschlossen wurden und deren Papierkorb-Aktion damals scheiterte, räumt der Nachlauf ebenfalls auf; sie stehen erst in 3.1b/3.3b.
- **Vollständiger Lauf (K4):** Erkannt an `last_sync_at` aller Ordner; dauert die Prüfung der Entwürfe nach dem letzten Ordner länger als 60 s (sehr großes Konto), kommt B.4h zu früh. Scheitert in jedem Lauf ein Ordner, ist kein Lauf erkennbar (WARN, der Zustand wird trotzdem geprüft).
- **Nicht am Gerät geprüft** (kein PowerShell im Container): die genaue Form der MCP-Ablehnungen (das Skript nimmt den Grund aus `reason`, sonst aus `code`), die Prozessliste mit `Name LIKE 'postkauz%'` und das Verhalten von `Get-TaskScripts` an den echten Aufgaben (1.1 und 0.6c zeigen es im Lesetest bzw. beim Lauf).
- **K1:** Ein Hilfsskript, das in **keiner** Aufgabe aus `-Tasks` steht und das Programm neu startet, beendet das Skript nicht; 1.1b erkennt es dann aber (FAIL, nichts getauscht).
