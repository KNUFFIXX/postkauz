# Abnahme D15: Umstellung auf 0.3.0-rc7 (A09 „Alltag 4: Ergänzte Empfänger, Kopfvergleich, Restfunde“)

Stand: 17.11.2026 (Auftrag [A09](../auftraege/A09_alltag4.md), Teil 3). Skript: [abnahme_D15.ps1](abnahme_D15.ps1) (Tausch und Rückweg nach dem Muster von D14, zwei Läufe und Teil B nach dem Muster von D13, dazu die Restfunde aus Sammelpunkt 0g (f), Abschnitt 8). Programm: Build **0.3.0-rc7**; die SHA256 der `postkauz-windows-amd64.exe` (für `-ExpectSha256`) steht nach dem Build in [roadmap.md](roadmap.md) und in `SHA256SUMS` neben dem Programm.

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft mit 0.3.0-rc6 (seit D14). D15 stellt ihn auf 0.3.0-rc7 um und prüft A09 am echten Postfach:

1. **Teil A:** Tausch ohne Migration, zwei vollständige Läufe, Liste aller Abschlüsse. Neu in rc7: Eine Antwort, bei der du in Gmail **Empfänger ergänzt** und sie sonst unverändert gesendet hast, wird jetzt als gesendet erkannt (Grund `thread`, im Audit `recipients_added`).
2. **Teil B (Mitmach)** im eigenen Gmail-Konto:
   - **B.1** Ein Entwurf mit langem Betreff mit Umlauten wird durch eine neue Fassung ersetzt (`supersede`). Die alte Kopie landet im Papierkorb; die Aktion endet `ok` (vor A09 endete sie `stale`).
   - **B.2** Ein Antwort-Entwurf; du ergänzt in Gmail einen Empfänger und sendest. PostKauz erkennt ihn als gesendet und sagt „recipients added in the mail client (1)“.
   - **B.3** Geschlossene Entwürfe zeigen keinen veralteten Hinweis zur Kopie mehr (`remote_note` leer).

**Dauer:** Teil A etwa 15 Minuten (meist die beiden Läufe; mit `-BackupTo` länger), Teil B etwa 20 bis 30 Minuten.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, prüft die Syntax, startet das Skript und wertet das Protokoll aus. In Teil B sucht Claude mit dir die Nachricht für B.2 aus.
- **[Du]** gibst den Tausch frei, bestätigst am Ende die Liste der Abschlüsse (3.5), startest danach die Claude-App neu und machst in Teil B den Schritt in Gmail (B.2).

## 1. Was das Skript tut und was nicht

- **Wie D14:** Live-Datenverzeichnis, Aufgaben aus `-Tasks` (**Pflicht**, Namen nicht raten) angehalten und am Ende die vorher eingeschalteten wieder an, **Umbenennen statt Überschreiben** mit `-KeepStdioMcp`: Das alte Programm wird `postkauz-rc6.exe`. Das geht nur, weil rc7 **keine Migration** hat (Schema 22, `doctor` vorher und nachher).
- **Konfiguration:** `[draft] attach_deny_dirs` und `attach_deny_files` aus A08 **dürfen** in `config.toml` stehen bleiben. rc6 kennt sie; auch vor einem Rückweg musst du nichts entfernen (0.7 nennt nur, ob sie gesetzt sind).
- **Zwei vollständige Läufe** nach dem Tausch (3.1 und 3.3, wie D13). Nach jedem Lauf listet das Skript alle seit der Marke (1.3a) geschlossenen Entwürfe mit Status, Grund und, falls vorhanden, `recipients_added`. **Keine Erwartungslisten** (anders als D13): Ein Abschluss ist nur FAIL, wenn sein Grund im Audit fehlt oder nicht zum Status passt, ein Entwurf verschwunden ist oder das Audit einen Abschluss eines vorher nicht offenen Entwurfs nennt. Ob die Abschlüsse stimmen, bestätigst du in 3.5. Nach einem FAIL im ersten Lauf folgt **kein zweiter**.
- **Keine Anhang-Proben:** Die gehören zu A08 und waren Teil von D14. 2.3 prüft nur, dass `doctor` die Zeile `attach_guard` weiter zeigt.
- **Sicherung nur mit `-BackupTo`** (optional, wie D14). Gut zu wissen: Ein Abschluss in Teil 3 ist endgültig; rückgängig nur über eine Sicherung ([betrieb.md](betrieb.md) 2.5).
- **Rückweg bei jedem FAIL** wie D14: E.0 (altes Programm zurück), E.0c (Datenbank mit rc6 in Ordnung), erst dann laufen die Aufgaben mit rc6 an. Mit rc7 laufen sie nur an, wenn die Abnahme bestanden ist.
- **Strg+C im Abschluss abgeschaltet** (neu): Sobald das Skript in den Abschluss geht (Aufgaben wieder einschalten, Rückweg nach FAIL), bricht Strg+C es nicht mehr ab (E.4).
- **Protokoll** `D15_windows.txt` (Teil B: `D15_teilB.txt`) neben dem Programm; ein vorhandenes wird als `D15_windows-<Zeitstempel>.txt` beiseitegelegt. Ins Protokoll kommen nur Prüfnummern, IDs, Zustände, Gründe und Zähler, **nie** Betreff, Adressen oder Mailtext. **Konto- und Aufgabennamen** ersetzt das Skript in **jeder** Zeile durch „Konto 1“, „Aufgabe 3“ usw., auch in `doctor`-Warnungen und Fehlertexten. Heißt ein Konto wie ein gewöhnliches Wort, wird auch dieses Wort ersetzt (harmlos, nur schlechter lesbar).

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d15` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc7, nicht umbenennen), `SHA256SUMS` und `abnahme_D15.ps1`.
2. **[Claude]** Syntax prüfen (das Skript ist auf dem Entwicklungsrechner nicht mit PowerShell gelaufen; dort gibt es keine); erwartet ist eine leere Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d15\abnahme_D15.ps1', [ref]$null, [ref]$e); $e
   ```

3. **[Claude]** Konten und Aufgaben wie in D14 nennen lassen.
4. **[Claude]** Mit rc6 die offenen Antwort-Entwürfe mit `remote: missing` ansehen (`postkauz draft list --json`) und dich fragen, ob du eine davon in Gmail mit **zusätzlichem Empfänger** gesendet hast. Genau solche schließt rc7 im ersten Lauf (`sent_by_human`, Grund `thread`). Das dient nur dazu, die Liste in 3.5 zu verstehen; das Skript bricht deswegen nicht ab.

## 3. Lauf Teil A

**Als welcher Benutzer?** Wie D13/D14: derselbe Benutzer wie der Dauerbetrieb; erhöht nur, wenn die Aufgaben „Mit höchsten Privilegien ausführen“ haben.

**Im Hintergrund starten**, danach das Protokoll mitlesen, bis die Schlusszeile `D15: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d15\abnahme_D15.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d15\postkauz-windows-amd64.exe','-ExpectSha256','2647a49f793bd1507000e01c5d84a9990e0c4005a07947ba1129b1c2cc1f1630','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-KeepStdioMcp'
```

Optional `'-BackupTo','<Sicherungsordner>'` anhängen.

```powershell
Get-Content <Ordner des Programms>\D15_windows.txt -Wait
```

Bestanden ist Teil A nur mit der Schlusszeile `D15: <n> OK, 0 FAIL, <w> WARN, <s> SKIP`. Jedes `WARN` nennt Claude im Bericht.

**Harter Abbruch** (Fenster geschlossen, PC neu gestartet): erst `postkauz.exe version` ansehen. Meldet es rc6, die Aufgaben von Hand einschalten (Aufgabenplanung → Aufgabe → Rechtsklick → „Aktivieren“, dann „Ausführen“; für „audit export“ nur „Aktivieren“). Meldet es rc7 oder fehlt `postkauz.exe`, den Rückweg nach Abschnitt 5 laufen lassen.

## 4. Erwartet (Teil A)

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Rechte | INFO: läuft die PowerShell erhöht |
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc7 passt zu `SHA256SUMS` und `-ExpectSha256`; das laufende Programm ist rc6 |
| 0.5, 0.6 | Konten, Aufgaben | `-Accounts` genau die Konten aus `accounts list`; jede Aufgabe vorhanden |
| 0.7 | Konfiguration | INFO: `attach_deny_*` gesetzt ja/nein (beides in Ordnung) |
| 1.1 | Aufgaben anhalten | jede abgeschaltet; außer dem stdio-MCP läuft kein `postkauz.exe` |
| 1.2 | `doctor` vorher | `database ok`, Schema 22 |
| 1.3, 1.3a | offene Entwürfe, Marke im Audit | lesbar, unter 500; INFO mit der höchsten Audit-ID (WARN, wenn keine lesbar ist: dann gilt die Uhrzeit) |
| 1.4 | Sicherung | nur mit `-BackupTo`: Exit 0 und `backup written to …`; sonst INFO |
| 2.1 | Tausch | rc7 läuft, `postkauz-rc6.exe` daneben |
| 2.2 | `doctor` nachher | `database ok`, Schema unverändert (22), keine neue Warnung |
| 2.3 | Zeile `attach_guard` | da |
| 3.1, 3.3 | `sync -a <Konto> --json` je Konto, erster und zweiter Lauf | Exit 0 und `errors` 0 (OK) oder nach einer sofortigen Wiederholung ohne Fehler (WARN) |
| 3.2, 3.4 | Abschlüsse seit der Marke, nach jedem Lauf | jeder mit passendem Grund aus dem Audit: `sent_by_human` mit `message_id`, `fallback`, `thread` oder `subject` (bei ergänzten Empfängern zusätzlich `recipients_added n`); `abandoned` mit `replaced`, `copy_gone` oder `superseded_by_draft`. Nach einem FAIL in 3.1/3.2 **kein zweiter Lauf** |
| 3.4c | Summe | INFO: gesendet (davon mit ergänzten Empfängern), ersetzt, `copy_gone`, `supersede` |
| 3.5 | **[Du]** | Stimmen die Abschlüsse? Claude zeigt dir je ID `draft show <id>` (Betreff nur am Bildschirm). Bei ergänzten Empfängern steht dort `closed: …, sent: recipients added in the mail client (n)` |
| 3.6, 3.7 | `doctor` nach den Läufen, `audit verify` | `database ok`, Schema 22, keine neue Warnung; `ok` |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.4 | Abschluss | INFO: Strg+C bis zum Ende abgeschaltet (fehlt nur, wenn es keine Konsole gibt) |
| E.0, E.0c | nach FAIL | altes Programm zurück (`postkauz.exe` meldet rc6), Datenbank mit rc6 in Ordnung; sonst bleiben die Aufgaben aus |
| 4.1 bzw. E.1 | Aufgaben wieder an | wie D14: je Aufgabe gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); je Konto ein `sync --watch` genau mit `-Exe`; `postkauz.exe` meldet rc7 (bzw. rc6 nach E.0) |
| Stand | letzte Zeilen | welche Version an `postkauz.exe` liegt und welche als `postkauz-rc6.exe` |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt.

**Wenn ein Lauf einen Entwurf unerwartet schließt:** Der Abschluss bleibt (auch der Rückweg öffnet ihn nicht); rückgängig nur über eine Sicherung. Default: so lassen, Claude nennt ID, Status und Grund im Bericht.

## 5. Rückweg (nur bei Problemen)

A09 hat **keine Migration**. An `config.toml` ist nichts zu ändern.

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d15\abnahme_D15.ps1 -Rueckweg `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp
```

(mit denselben `-Exe`/`-DataDir`/`-Config` wie oben, im Hintergrund wie in Abschnitt 3; Protokoll `D15_windows.txt`). Erwartet: `R.0` bis `R.0d` (wie D14), **`R.0e` jede Aufgabe aus `-Tasks` vorhanden** (neu), `R.1` Aufgaben angehalten, `R.1b` neues Programm unter freiem Namen beiseite (meist `postkauz-rc7.exe`; fehlt `postkauz.exe`, nur das alte an seine Stelle), `R.2` rc6 mit Schema 22, `R.3` je Konto ein `sync` mit Exit 0 **und** `errors` 0 (eine Wiederholung erlaubt, dann WARN), `R.5` alle Aufgaben aus `-Tasks` wieder an.

**Bleibt der Rückweg irgendwo stehen** (FAIL in R.0 bis R.3, Abbruch in R.1, auch bei fehlender `postkauz.exe`, oder Strg+C), bleiben die Aufgaben **aus** (Schlusszeile mit `E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“). Dann `postkauz.exe version` und `postkauz.exe doctor` von Hand ansehen und die Aufgaben erst einschalten, wenn beide rc6 und `database: ok` zeigen. Mit rc6 gilt: Abschlüsse aus rc7 bleiben; ergänzte Empfänger erkennt rc6 nicht; geschlossene Entwürfe zeigen wieder alte Hinweise zur Kopie; eine Papierkorb-Aktion mit langem Betreff endet wieder `stale`.

## 6. Teil B (Mitmach)

**Voraussetzungen:** Teil A bestanden, die Aufgaben laufen mit rc7 (`sync --watch` legt ab, führt die Aktion aus und erkennt den Versand). **Nur im eigenen Gmail-Konto**, nie im geteilten ohne deine Anweisung.

- **Agentenschlüssel (empfohlen):** `-AgentKeyFile <Datei>` mit dem API-Schlüssel eines Agenten (`pk_key_…`, etwa der der Claude-App; Rolle `assistant`). Dann laufen Anlegen, `supersede` und `mail_get_draft` über den MCP-Server wie bei einem Agenten. **Schlüssel und Dateiname stehen nie im Protokoll.** Die Datei danach löschen oder nur für dich lesbar lassen. Ohne die Datei läuft Teil B als Mensch an der CLI.
- **Nachricht für B.2:** eine Mail im Gmail-Konto, die du dir **selbst von einer anderen eigenen Adresse** geschickt hast (dann geht die Antwort nur an dich). Kommt sie von der Adresse des Gmail-Kontos selbst, hat die Antwort keinen Empfänger, und B.2b ist FAIL. **[Claude]** findet ihre lokale ID mit `postkauz search` und `postkauz show <id>` und zeigt sie dir vorher.
- **Ergänzter Empfänger:** eine zweite eigene Testadresse von dir (nicht die, an die die Antwort ohnehin geht).

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d15\abnahme_D15.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-TeilB','-SelfAccount','<eigenes Gmail-Konto>','-ReplyToMessage','<lokale ID der Nachricht>','-AgentKeyFile','<Datei mit dem Agentenschlüssel>'
```

`-SelfAddress` ist nicht nötig: Das Skript nimmt die Adresse des Kontos aus `accounts list` (nie ins Protokoll). Ohne `-ReplyToMessage` laufen nur B.1 und B.3 (SKIP B.2, dann ist Teil B nicht vollständig geprüft). Protokoll mitlesen: `Get-Content <Ordner des Programms>\D15_teilB.txt -Wait`.

**Ablauf:**

1. **B.1** läuft allein: zwei Fassungen einer neuen Mail an dich mit dem Betreff „D15 Prüfung: Rückfrage zur Nebenkostenabrechnung für das Jahr mit Belegen und Erläuterungen <Zeitstempel>“. Vor jedem Schritt wartet das Skript, bis die Kopie **im Spiegel** des Entwürfe-Ordners liegt (nicht nur `remote: pushed`; dafür bittet es `sync --watch` mit `sync request` um einen Lauf). Danach Trockenlauf, `supersede`, Warten auf die Papierkorb-Aktion.
2. Bei `INFO B.2d JETZT: Entwurf <id> (Antwort) im Webmail im Browser oeffnen …` **[Du]** (oder Claude auf deine Anweisung) in Gmail im Browser den Entwurf öffnen, **genau einen** Empfänger ergänzen (deine zweite Testadresse), den Text **nicht** ändern und senden. Das Skript wartet bis zu 20 Minuten (`-WaitMinutes`).
3. **B.3** läuft danach allein.

| Nr | Prüfung | Erwartet |
|---|---|---|
| B.0 bis B.0d | Version, Konto, Agentenschlüssel, `sync --watch` | rc7; Konto konfiguriert, Adresse bekannt; MCP-Sitzung angemeldet (oder INFO ohne Schlüssel); ein `sync --watch` genau mit `-Exe` für das Konto |
| B.0e | Marke im Audit | INFO |
| B.1a, B.1d | zwei Probe-Entwürfe | angelegt (über MCP mit dem Agentenschlüssel) |
| B.1b | Voraussetzung | Betreff als **eine** Zeile über 78 Zeichen aus mehreren encoded-words (nur Länge und Zahlen im Protokoll). WARN, wenn nicht: dann prüft B.1 nicht den Fall aus A09 Teil 1b |
| B.1c, B.1e | Kopie im Spiegel | je Fassung innerhalb von 12 Minuten |
| B.1f | Trockenlauf | nichts geändert, mindestens 1 Kopie würde in den Papierkorb gehen (meldet er kurz `copy_missing`, wiederholt das Skript bis zu 3 Minuten) |
| B.1g | `supersede` | alter Entwurf `abandoned`, Aktion angelegt |
| B.1h | Papierkorb-Aktion | Zustand `done`, **jedes Ziel `ok`** (vor A09: `stale`) |
| B.1i | alte Kopie | laut Spiegel im Papierkorb, nicht mehr im Entwürfe-Ordner |
| B.1j | alter Entwurf | `abandoned`, `closed_reason` `superseded_by_draft`, Nachfolger und `supersede_action_id` stimmen |
| B.1k | zweite Fassung | INFO: bleibt offen (siehe unten) |
| B.2a, B.2b | Antwort-Entwurf | angelegt; `kind` `reply`, offen, `In-Reply-To` gesetzt, mindestens ein Empfänger |
| B.2c | Kopie im Spiegel | innerhalb von 12 Minuten (dann in Gmail sichtbar) |
| B.2d | **[Du]** | INFO „JETZT …“: Empfänger ergänzen, unverändert senden |
| B.2e | erkannt | `sent_by_human` |
| B.2f | Audit | `drafts.sent_detected` mit Grund `thread` und `recipients_added` 1 |
| B.2g | `draft show --json` | `closed_note` „recipients added in the mail client (1)“, `closed_reason` leer |
| B.3a, B.3c | geschlossene Entwürfe aus B.1 und B.2 | kein `remote_note` (nur bei `push_note` `pushed_after_close` erlaubt, dann WARN) |
| B.3b | alter Entwurf aus B.1 | `closed_note` nennt „queued for the trash“, nicht „did not move it“. Ist B.1h FAIL, nur INFO, ob dort „copy still in the Drafts folder; action N did not move it (remove with drafts.cleanup_copies)“ steht |
| B.3d | `mail_get_draft` (nur mit Agentenschlüssel) | je Entwurf kein `remote_note`, `closed_note` und `closed_reason` wie `draft show` (WARN, wenn der Schlüssel den Entwurf nicht lesen darf) |
| E.2 | Abschlussmerker | FAIL, wenn Teil B vor dem Ende von B.3 abbricht |

**Nach einem FAIL** bricht nur der laufende Schritt ab (Zeile „Schritt B.x abgebrochen: …“); was davon abhängt, läuft nicht mehr. B.2 läuft auch nach einem FAIL in B.1. Antwortet der MCP-Server 60 s nicht, endet Teil B ganz (FAIL mit Grund).

Bestanden ist Teil B mit `D15: <n> OK, 0 FAIL, <w> WARN, 0 SKIP` (ein `SKIP B.2` heißt: B.2 fehlt noch).

**Danach aufräumen ([Claude] auf deine Anweisung):** Die zweite Fassung aus B.1 bleibt offen. In Gmail löschen und `postkauz draft abandon <id>` (ID aus B.1k). Die alte Kopie im Papierkorb von Gmail kannst du löschen oder liegen lassen (PostKauz löscht nie endgültig).

## 7. Wenn etwas FAIL ist

- **Teil A:** Nichts von Hand tun. Das Skript legt rc6 zurück (E.0, E.0c) und startet die Aufgaben mit rc6. Claude schickt dir die Zeilen mit FAIL und einen Vorschlag. Abschlüsse aus Teil 3 bleiben.
- **`E.1` FAIL „Aufgaben NICHT wieder eingeschaltet“:** `postkauz.exe version` und `postkauz.exe doctor` ansehen; zeigen sie rc6 und `database: ok`, die Aufgaben von Hand einschalten (Abschnitt 5). Sonst Claude fragen.
- **B.1h FAIL** (Aktion `stale` oder `failed`): Der Kopfvergleich aus A09 Teil 1b wirkt nicht. Die alte Kopie liegt noch im Entwürfe-Ordner. Default: liegen lassen, Claude meldet es; entfernen später mit `postkauz draft cleanup <id>` nach deinem Entscheid.
- **B.2e FAIL** (nicht erkannt): Der Entwurf bleibt offen. Default: Claude prüft mit `draft show <id>`, ob die Antwort wirklich mit **einem** zusätzlichen Empfänger und unverändertem Text hinausging; danach schließt du ihn mit `postkauz draft mark-sent <id>`.
- **B.2f/B.2g FAIL** (anderer Grund als `thread` oder andere Zahl): Der Entwurf ist geschlossen; Claude nennt Grund und Zahl im Bericht.
- **B.3 FAIL:** nur Anzeige, nichts im Postfach betroffen; Claude meldet es.

## 8. Restfunde aus Sammelpunkt 0g (f)

Alle Punkte aus [roadmap.md](roadmap.md), „Als Nächstes“, 0g (f), sind in D15 umgesetzt:

| Punkt aus 0g (f) | In D15 |
|---|---|
| Teil B wartet auf die Kopie im Spiegel, nicht nur auf `remote: pushed` (D13: B.6/B.7 `copy_missing`); nach einem FAIL in B.6 kein B.7 | **umgesetzt:** B.1c, B.1e, B.2c warten auf die Kopie im Spiegel (`draft show` plus `show <Message-ID> --json`, Ordnerkopie mit der UID der Ablage); der Trockenlauf wiederholt bei `copy_missing` bis zu 3 Minuten; nach jedem FAIL bricht der laufende Schritt ab |
| Rückweg bei fehlender `postkauz.exe`: nach einem Abbruch in R.1 die Aufgaben aus lassen | **umgesetzt:** jeder unvollständige Rückweg lässt die Aufgaben aus (E.1 FAIL) |
| R.5 nach einem FAIL in R.2/R.3 nicht starten | **umgesetzt** (seit D14, übernommen) |
| Der Rückweg prüft vor R.1, dass alle Aufgaben aus `-Tasks` vorhanden sind | **umgesetzt:** R.0e |
| Strg+C während 4.1 (inneres `finally`) absichern | **umgesetzt:** Strg+C ist im ganzen Abschluss abgeschaltet (E.4). Grenze: wirkt nur mit Konsole; Fenster schließen oder Neustart bleibt ein harter Abbruch (Abschnitt 3) |
| Abschlussmerker für Teil B | **umgesetzt:** E.2 FAIL, wenn Teil B nicht bis zum Ende von B.3 kommt |
| `needs_approval` ohne `structuredContent`: `content[].text` auswerten | **umgesetzt:** Status und Grund werden aus `content[].text` gelesen (Zeile „Fehler needs_approval, Status needs_approval, Grund …“) |
| `doctor`-Warnungen und Ausnahmetexte (`X`, 4.1, E.1) ohne Konto- und Aufgabennamen | **umgesetzt:** jede Protokollzeile ersetzt bekannte Konto- und Aufgabennamen (aus `-Accounts`, `-SelfAccount`, `accounts list`, `-Tasks`) |

**Offen aus 0g (f):** nichts. Grenzen: Namen, die das Skript nicht kennt (etwa ein Konto, das erst nach dem Start angelegt wird), ersetzt es nicht; die Prüfung „Kopie im Spiegel“ nimmt an, dass `show <Message-ID>` die gespiegelte Kopie findet (bei mehreren Nachrichten mit derselben Message-ID prüft es jede einzeln).
