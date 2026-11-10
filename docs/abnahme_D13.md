# Abnahme D13: Umstellung auf 0.3.0-rc5 (A07 „Alltag 3: Versanderkennung schließen, Kopien aufräumen“)

Stand: 10.11.2026 (Auftrag [A07](../auftraege/A07_alltag3.md), Teil 7, mit Nachtrag 1 nach der Gegenprüfung: Audit-Grenze 1000, `Stop-D13`, Abbruch nach FAIL im ersten Lauf, Erwartung der Auffangregel vor dem Tausch, `sync --watch` je Konto, Rückweg, Pfadgrenzen, `-AgentKeyFile`; und mit Nachtrag 2, nur das Skript: Erwartungs-IDs als Zahlen, Marke im Audit, `-ExpectStaysOpen`, Auffangregel direkt vor dem Tausch noch einmal, Zeitpunkt „Kopie fehlt“ je Entwurf, `sync --watch` genau mit `-Exe`, Rückweg mit `-Accounts`, keine Aufgabennamen im Protokoll, MCP-Fehler mit Status und Grund). Skript: [abnahme_D13.ps1](abnahme_D13.ps1) (Muster D12, mit den Verbesserungen aus Sammelpunkt 0e). Programm: Build **0.3.0-rc5** an `1fdd000` (Nachtrag 1; ersetzt den Build an `69117c5`), `postkauz-windows-amd64.exe` SHA256 `d001341b2fa85a6928d127bc98e34d874ce13edca17def89b43ddbe99de0d619` (für `-ExpectSha256`; Prüfsummen und Prüfläufe in [protokolle/A07_stand.md](protokolle/A07_stand.md) Abschnitt 7.4).

**Ergebnis (10.11.2026):** Teil A bestanden, **D13: 41 OK, 0 FAIL, 0 WARN** (21:17–21:19, Build an `1fdd000`; gesendet 38 `thread`, 44 und 47 `subject`, ersetzt 45 im zweiten Lauf). Dauerbetrieb seit 21:19 auf 0.3.0-rc5. Teil B inhaltlich bestanden: B.6/B.7 scheiterten im Skript mit `copy_missing` (es wartet nur auf `pushed`, nicht auf den Spiegel; fail closed, nichts geändert), B.6 bis B.13 danach von Hand über den MCP-Server der Claude-App. Einzelheiten: [protokolle/A07_stand.md](protokolle/A07_stand.md) Abschnitt 9.

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
- **Erwartung statt fester IDs:** `-ExpectSent` (gesendet: Grund `message_id`, `fallback`, `thread` oder `subject`), `-ExpectReplaced` (ersetzt, Grund `replaced`; vorher `remote: missing` oder `taken_over`), `-ExpectCopyGone` (Auffangregel, Grund `copy_gone`; vorher `remote: missing`, und zwar seit mehr als 24 Stunden: Zeitpunkt aus dem Audit, siehe [versanderkennung.md](versanderkennung.md) Abschnitt 3d), `-ExpectStaysOpen` (laut Audit länger als 24 h `missing`, soll aber offen bleiben; Nachtrag 2). Die IDs sind Zahlen; was keine ist oder in zwei Listen steht, ist FAIL in 0.8 (Nachtrag 2: vorher blieben die Listen Text, und der Abgleich mit dem Stand lief ins Leere).
- **Marke im Audit** (Nachtrag 2): Nach der Vorher-Liste (1.2) merkt sich das Skript die höchste Audit-ID (1.2a). Als Abschluss zählt nur, was danach im Audit steht; ein Entwurf, den rc4 noch vor 1.1 geschlossen hat, ist kein fremder Abschluss mehr. Ist keine Marke lesbar, gilt der Zeitpunkt der Vorher-Liste (WARN 1.2a).
- **„errors 0“ mit einer Wiederholung** (Sammelpunkt 0e): Endet ein `sync -a <Konto>` mit einem Fehler, wiederholt das Skript ihn sofort einmal. Endet die Wiederholung ohne Fehler, ist das nur `WARN` (vorübergehender Serverfehler); sonst FAIL.
- **Rückweg bei jedem FAIL**: wie D12 (E.0 vor dem Neustart der Aufgaben; die Aufgaben laufen nie mit dem neuen Programm an, wenn die Abnahme nicht bestanden ist).
- **Konfiguration:** `[draft] copy_gone_after` darf **nicht** in `config.toml` stehen (0.9): rc4 kennt den Schlüssel nicht und startet mit ihm nicht (unbekannte Schlüssel sind ein Fehler). Der Standard von rc5 (24 Stunden) gilt ohne den Schlüssel. Im Dauerbetrieb also nicht setzen; vor einem Rücktausch entfernen.
- **Protokoll:** Ein vorhandenes `D13_windows.txt` wird nicht fortgeschrieben, sondern als `D13_windows-<Zeitstempel>.txt` beiseitegelegt. Schreibfehler am Protokoll brechen nichts ab (die Schlusszeile sagt es).
- Ins Protokoll kommen nur IDs, Zustände, Gründe und Zähler, nie Betreff, Adressen oder Mailtext; seit Nachtrag 1 bei 0.7 und B.0b auch keine Aufgaben- oder Kontonamen, nur Anzahlen. Seit Nachtrag 2 stehen Aufgaben auch in 1.1, 4.1, E.1, R.5 und in Fehlerzeilen nur als laufende Nummer in der Reihenfolge von `-Tasks` („Aufgabe 2“).
- **Audit-Abfragen** seitenweise: `audit` liefert höchstens 1000 Einträge je Seite; das Skript liest mit `--after-id` weiter, bis eine Seite kürzer ist. Endet die Abfrage nach 50 Seiten nicht, gilt sie als nicht lesbar (FAIL) (Nachtrag 1, seit Nachtrag 2 mit Seiten statt Abbruch bei 1000).

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
   - Kopie seit mehr als 24 Stunden weg, sonst nichts passt → `-ExpectCopyGone`. Der Zeitpunkt ist der jüngste Audit-Eintrag `drafts.remote_check` mit Grund `missing` seit der letzten Ablage (`pushed_date` in `draft show <id> --json`): `postkauz audit --operation drafts.remote_check --since 30d --json` zeigt ihn je `draft:<id>`;
   - laut Audit seit mehr als 24 Stunden `missing`, soll aber **offen bleiben** → `-ExpectStaysOpen` (Nachtrag 2). Der Fall: Im Entwürfe-Ordner liegt laut Spiegel wieder eine Fassung des Entwurfs, die rc4 als fehlend vermerkt hat: die alte Kopie unter ihrer UID (rc5 setzt den Entwurf auf `pushed` zurück, Audit `present`, Nachtrag 1 B2) oder eine weiterbearbeitete Fassung derselben Antwort bzw. eine neue Mail mit demselben Betreff (rc5 schließt dann nicht nach der Auffangregel). Ob rc5 eine in Gmail zurückgeholte Kopie unter einer **neuen** UID so erkennt, ist nicht sicher (Sammelpunkt in der Roadmap); vorher in Gmail nachsehen. 3.4c prüft, dass jeder Entwurf aus der Liste nach beiden Läufen offen ist. Im Zweifel nicht in diese Liste, sondern mit dem Betreiber klären.
   - Für **jeden** offenen `missing`-Entwurf muss bekannt sein, seit wann seine Kopie fehlt: Fehlt `pushed_date`, liefert `draft show` nichts oder steht seit der Ablage kein `drafts.remote_check missing` als jüngster Eintrag im Audit, bricht das Skript vor dem Tausch ab (1.2d mit der ID; Nachtrag 2). Dann mit dem Betreiber klären, bevor D13 noch einmal läuft.
5. **[Claude]** Sicherungsordner wählen (verschlüsselter Datenträger, z. B. `E:\PostKauz-Sicherung`).

## 3. Lauf Teil A

**Als welcher Benutzer?** (Sammelpunkt 0e, geklärt) Als **derselbe Benutzer**, unter dem der Dauerbetrieb läuft. Ob die PowerShell erhöht laufen muss, hängt davon ab, wie die Aufgaben angelegt sind: Sind sie mit „Mit höchsten Privilegien ausführen“ angelegt (Aufgabenplanung → Aufgabe → Eigenschaften → Reiter „Allgemein“), darf nur eine erhöhte PowerShell sie abschalten; dann „Als Administrator“ starten. Sonst eine gewöhnliche PowerShell. Das Skript schreibt in `0.0`, ob es erhöht läuft, und meldet in `1.1` ein FAIL je Aufgabe, die sich nicht abschalten ließ (dann wird nichts getauscht).

**Im Hintergrund starten** (wie D12), danach das Protokoll mitlesen, bis die Schlusszeile `D13: …` dasteht:

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d13\abnahme_D13.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-NewExe','C:\PostKauz\d13\postkauz-windows-amd64.exe','-ExpectSha256','d001341b2fa85a6928d127bc98e34d874ce13edca17def89b43ddbe99de0d619','-BackupTo','<Sicherungsordner>','-Tasks','"<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>"','-Accounts','<konto1>,<konto2>,<konto3>','-ExpectSent','<ids>','-ExpectReplaced','<ids>','-ExpectCopyGone','<ids>','-ExpectStaysOpen','<ids>','-KeepStdioMcp'
```

Leere Listen (`-ExpectStaysOpen` meist) einfach weglassen.

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
| 0.5 bis 0.8 | Sicherungsordner, Konten, Aufgaben, Erwartung | angegeben; `-Accounts` genau die Konten aus `accounts list`; jede Aufgabe vorhanden; jede ID eine Zahl, keine ID in zwei der vier Listen (auch `-ExpectStaysOpen`) |
| 0.9 | Konfiguration | kein `copy_gone_after` in `config.toml` |
| 1.1 | Aufgaben anhalten | jede abgeschaltet (Aufgaben als Nummer, nie mit Namen); außer dem stdio-MCP läuft kein `postkauz.exe`. Ist die Prozessliste nicht lesbar, Abbruch (nicht „0 Prozesse“) |
| 1.2 | `draft list` vorher | lesbar, unter 500 offen |
| 1.2a | Marke im Audit | INFO mit der höchsten Audit-ID nach der Vorher-Liste; nur spätere Abschlüsse zählen (WARN, wenn keine lesbar ist: dann der Zeitpunkt der Vorher-Liste) |
| 1.2b, 1.2c | Erwartung zum Stand | jeder erwartete offen (auch `-ExpectStaysOpen`); `-ExpectReplaced` mit `remote` `missing` oder `taken_over`, `-ExpectCopyGone` mit `missing` |
| 1.2d | Auffangregel, nicht erwartet | jeder offene Entwurf, dessen Kopie bis zum Ende von Lauf 2 (jetzt + 3 h Sicherheitsabstand) länger als 24 h fehlt, steht in einer Erwartungsliste, auch `-ExpectStaysOpen` (Zeitpunkt: jüngster `drafts.remote_check` `missing` bzw. `present` seit der letzten Ablage, je Entwurf aus dem Audit ab seinem `pushed_date`; seit Nachtrag 1). Ist der Zeitpunkt für einen `missing`-Entwurf unbekannt oder das Audit nicht bis zum Ende lesbar: FAIL mit der ID, Abbruch vor dem Tausch (Nachtrag 2) |
| 1.2e | Auffangregel, erwartet | jeder Entwurf aus `-ExpectCopyGone` fehlt schon vor dem Tausch länger als 24 h |
| 1.2f | INFO | erwartet gesendet bzw. ersetzt, aber auch über der Grenze (ein anderer Weg muss im selben Lauf greifen) |
| 1.3 | `doctor` vorher | `database ok`, Schema 22 |
| 1.5 | Sicherung | Exit 0 und `backup written to …` |
| 1.6d bis 1.6f | Auffangregel direkt vor dem Tausch | dieselben Prüfungen wie 1.2d bis 1.2f mit der Uhrzeit nach der Sicherung (eine lange Sicherung kann einen Entwurf über die Grenze schieben); sonst Abbruch, nichts getauscht (Nachtrag 2) |
| 2.1, 2.2 | Tausch, `doctor` | rc5 läuft, `postkauz-rc4.exe` daneben; Schema 22 |
| 3.1, 3.3 | `sync -a <Konto> --json` je Konto, erster und zweiter Lauf | Exit 0 und `errors` 0 (OK) oder nach einer sofortigen Wiederholung ohne Fehler (WARN); die Zeile nennt `sent_by_*` (auch `sent_by_subject`), `replaced_in_thread`, `copy_gone`, `missing`, `taken_over` |
| 3.2, 3.4 | alle Abschlüsse seit der Marke 1.2a, nach jedem Lauf | jeder mit Grund im Audit: `sent_by_human` mit `message_id`, `fallback`, `thread` oder `subject`; `abandoned` mit `replaced` oder `copy_gone`. Ein Abschluss im Audit für einen Entwurf, der vorher nicht offen war, ist nicht erwartet. Nach einem FAIL bis 3.2b bricht das Skript ab: **kein zweiter Lauf** mit dem neuen Programm (Nachtrag 1) |
| 3.2b, 3.4b | nicht erwartet | keiner |
| 3.4c | Summe nach zwei Läufen | jeder erwartete Abschluss wie erwartet; jeder Entwurf aus `-ExpectStaysOpen` noch offen |
| 3.5 | **[Du]** | Stimmen die Abschlüsse? `draft show <id>` zeigt bei `copy_gone`: `closed: …, abandoned: copy gone from the Drafts folder since … (copy_gone); no sent message was detected, it may or may not have been sent` |
| 3.6, 3.7 | `doctor` nachher, `audit verify` | keine neue Warnung, Schema 22; `ok` |
| E.2 | Teil 3 nicht vollständig | FAIL |
| E.0 | nach FAIL oder gescheitertem 4.1 | altes Programm zurück (`postkauz.exe` meldet rc4) |
| 4.1 bzw. E.1 | Aufgaben wieder an | je Aufgabe: gestartet **und nach 20 s noch** `Running` (audit export nur eingeschaltet); dazu läuft **je Konto aus `-Accounts`** ein `sync --watch` **genau mit dem Programm `-Exe`** (ein `postkauz-rc4.exe` im selben Ordner zählt nicht), das Konto aus den Optionen `-a <konto>`, `--account <konto>` oder `--all` der Befehlszeile (kein Teilwort-Vergleich: `--mailbox shared` ist nicht das Konto `shared`; unabhängig von den Namen der Aufgaben), und `postkauz.exe` meldet rc5 (Nachtrag 1, genauer seit Nachtrag 2). Scheitert 4.1, hält das Skript die Aufgaben an; nur wenn das gelang (sonst E.0b FAIL, Aufgaben bleiben aus), legt es rc4 zurück und startet mit rc4 |
| Stand | letzte Zeilen | welche Version wirklich an `postkauz.exe` liegt und welche als `postkauz-rc4.exe` (auch nach einem gescheiterten E.0) |

Danach **[Du]** die Claude-App neu starten (Rechtsklick auf das Claude-Symbol unten rechts → „Beenden“, dann wieder öffnen), damit ihr MCP das neue Programm nimmt; erst dann kennt Claude `mail_supersede_draft`.

**Wenn ein Lauf einen Entwurf unerwartet schließt:** wie D12 Abschnitt 4a. Der Abschluss bleibt (auch der Rückweg öffnet ihn nicht); rückgängig nur über die Sicherung von 1.5 ([betrieb.md](betrieb.md) 2.5). Default: so lassen, Claude nennt ID, Status und Grund im Bericht.

## 5. Rückweg (nur bei Problemen)

A07 hat **keine Migration**. Vorher `copy_gone_after` aus `config.toml` entfernen, falls gesetzt (R.0e prüft das).

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d13\abnahme_D13.ps1 -Rueckweg `
  -Tasks "<Aufgabe 1>,<Aufgabe 2>,<Aufgabe 3>,<Aufgabe 4>,<Aufgabe 5>" -Accounts <konto1>,<konto2>,<konto3> -KeepStdioMcp
```

(mit denselben `-Exe`/`-DataDir`/`-Config` wie in Teil A, im Hintergrund wie in Abschnitt 3). `-Tasks` **und** `-Accounts` sind Pflicht (R.0d; Nachtrag 2). Erwartet: `R.0` bis `R.0e`, `R.1b` (neues Programm unter freiem Namen beiseite, meist `postkauz-rc5.exe`; fehlt `postkauz.exe`, nur das alte an seine Stelle), `R.2` rc4 mit Schema 22, `R.3` je Konto ein `sync` mit Exit 0 **und** `errors` 0 (eine Wiederholung erlaubt, dann WARN), `R.5` **alle** Aufgaben aus `-Tasks` wieder an. Dass alle Aufgaben am Ende wieder an sein sollen, merkt sich das Skript direkt **vor** R.1 (nach R.0 bis R.0e): Ein Abbruch in R.0x („nichts geändert“) schaltet keine Aufgabe ein, ein Abbruch in R.1 schaltet sie im Abschluss wieder ein (Nachtrag 1). Dann liegt unter `-Exe` noch rc5; E.1 prüft seit Nachtrag 2 die Version, die wirklich dort liegt (INFO E.1 nennt sie), statt falsch „nicht rc4“ zu melden. Mit rc5 geschlossene Entwürfe (`subject`, `copy_gone`, `superseded_by_draft`) bleiben geschlossen.

## 6. Teil B: `drafts.supersede` (Mitmach)

**Ziel:** Zeigen, dass ein Entwurf durch eine neuere Fassung ersetzt wird, seine unveränderte Kopie ohne Antrag im Papierkorb landet und die neue Fassung danach normal als gesendet erkannt wird. **Nur im eigenen Postfach**, nie im geteilten ohne Anweisung des Betreibers.

1. **[Claude]** Teil B starten (die Aufgaben laufen, `sync --watch` legt ab und führt die Aktion aus):

   ```powershell
   Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d13\abnahme_D13.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-TeilB','-SelfAccount','<eigenes Konto>','-SelfAddress','<eigene Adresse>'
   ```

   (`-Exe`, `-DataDir` und `-Config` wie in Teil A; Nachtrag 2.)

   und `D13_teilB.txt` mitlesen. B.1 verlangt ein `sync --watch` genau mit `-Exe` für `-SelfAccount` (oder `--all`), B.1b setzt die Marke im Audit. Das Skript legt zwei neue Mails an dich selbst mit gleichem Betreff an (B.2, B.4), wartet, bis beide abgelegt sind (B.3, B.5), prüft `draft supersede <erste> --by <zweite> --dry-run` (B.6: nichts geändert) und führt es aus (B.7). Dann wartet es, bis die Aktion `done` ist (B.8: Kopie der ersten Fassung im Papierkorb, ohne Antrag), und prüft `draft show` (B.9: `abandoned`, `closed_reason` `superseded_by_draft`, Nachfolger) und das Audit (B.10).
2. Bei `INFO B.11 JETZT: Entwurf <id> (zweite Fassung) … UNVERAENDERT senden` **[Claude auf Anweisung des Betreibers]** in Gmail im Browser die zweite Fassung öffnen und **unverändert** senden. Im Entwürfe-Ordner liegt die erste Fassung dann nicht mehr (sie ist im Papierkorb).
3. Das Skript wartet bis zu 20 Minuten (`-WaitMinutes`) und prüft: B.12 die zweite Fassung `sent_by_human` (meist Grund `fallback`), B.13 die erste bleibt `abandoned`.

**Mit Agentenschlüssel (empfohlen, Nachtrag 1):** Mit `-AgentKeyFile <Datei>` (eine Datei, die nur den API-Schlüssel eines Agenten enthält, `pk_key_…`, etwa den der Claude-App; Rolle `assistant` oder `triage`) legt das Skript beide Probe-Entwürfe und den Aufruf `supersede` über den MCP-Server (`postkauz mcp`, stdio, Umgebung `POSTKAUZ_KEY_FILE`) mit diesem Schlüssel an. Das ist der Weg eines Agenten: Eigentümerprüfung (`created_by`/`updated_by`) und die Ausnahme im Runner (`mail:draft` statt `mail:trash`, ohne Antrag). B.0c meldet die Anmeldung, B.8 erwartet `done`. Lehnt der Server einen Aufruf ab, nennt die Zeile Code, Status und Grund (etwa `Status needs_approval, Grund …`), nie die Meldung. Antwortet der Server 60 s lang nicht, endet Teil B sauber mit FAIL und Grund (kein zweiter Leseversuch). Das Skript schreibt an den Server UTF-8 ohne BOM (Nachtrag 2). **Der Schlüssel und der Dateiname stehen nie im Protokoll.** Die Datei danach löschen oder nur für den eigenen Benutzer lesbar lassen. Ohne `-AgentKeyFile` läuft die Probe als Mensch an der CLI (ohne Eigentümerprüfung).

```powershell
Start-Process powershell.exe -WindowStyle Hidden -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','C:\PostKauz\d13\abnahme_D13.ps1','-Exe','<Ordner des Programms>\postkauz.exe','-DataDir','<Datenordner>','-Config','<Datenordner>\config.toml','-TeilB','-SelfAccount','<eigenes Konto>','-SelfAddress','<eigene Adresse>','-AgentKeyFile','<Datei mit dem Agentenschlüssel>'
```

Das Protokoll steht in `D13_teilB.txt` neben dem Programm. Die erste Fassung kannst du danach im Papierkorb von Gmail löschen oder liegen lassen (PostKauz löscht nie endgültig).
