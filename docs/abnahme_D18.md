# Abnahme D18: Umstellung auf 0.3.0-rc10 (A12 „Alltag 6“)

Stand: 25.11.2026 (Auftrag [A12](../auftraege/A12_alltag6.md), Teil 4). Skript: [abnahme_D18.ps1](abnahme_D18.ps1) (Tausch, zwei Läufe und Rückweg nach dem Muster von [D17](abnahme_D17.md), dazu `[extract] pdftotext`, der PATH der geplanten Aufgaben und W1 bis W3 aus der Gegenprüfung des Skripts D17; Teil B neu). D17 bleibt als Protokoll unverändert. Programm: Build **0.3.0-rc10**, `postkauz-windows-amd64.exe`.

**SHA256 (postkauz-windows-amd64.exe): siehe CHANGELOG, Abschnitt 0.3.0-rc10** (für `-ExpectSha256`; steht nach dem Build auch in `SHA256SUMS` neben dem Programm).

> **Hinweis:** Das Skript ist auf dem Entwicklungsrechner (Container) **nicht** mit PowerShell gelaufen (kein `pwsh`; eine heruntergeladene PowerShell durfte dort nicht ausgeführt werden). Geprüft sind dort nur: reines ASCII, UTF-8 mit BOM, CRLF und ausgeglichene Klammern und Anführungszeichen. Die **Parser-Probe** (Abschnitt 2, Schritt 2) und der **Lesetest** (Abschnitt 2, Schritt 6) auf dem Büro-PC sind deshalb Pflicht, bevor etwas getauscht wird.

**Worum es geht:** Der Dauerbetrieb läuft mit 0.3.0-rc9 (seit D17). D18 stellt ihn auf 0.3.0-rc10 um und prüft A12 am echten Postfach:

1. **Teil A:** Tausch ohne Migration (Schema bleibt 22), sofort danach `[extract] pdftotext` in der Konfiguration, zwei vollständige Läufe. Neu gezählt: Anhänge `failed` je Konto, Entwürfe mit `taken_over` und `missing` vor dem Tausch und nach dem ersten Lauf.
2. **Teil B:** B.1 Bearbeitung im Webmail (dein Handgriff), B.2 großer Entwurf (wartet 15 Minuten, kein Handgriff), B.3 `attach retry` für die vier Anhänge aus D17, B.4 `doctor --json`. Entwürfe nur an die eigene Adresse, **nichts wird gesendet**.
3. **Rückweg** (nur bei Problemen): wie D17, dazu nimmt das Skript `[extract]` vor dem ersten Start von rc9 heraus.

### Ankündigung: Was der erste Lauf mit rc10 tut

- **`missing` kann `taken_over` werden.** Liegt zu einem Entwurf mit fehlender Kopie im Entwürfe-Ordner eine nach der Ablage datierte Fassung (Antwort auf dasselbe Original bzw. neue Mail mit gleichem Betreff), gilt der Entwurf ab jetzt als im Mailprogramm weiterbearbeitet: Ein Agent kann ihn nicht mehr ändern oder verwerfen, „Kopie weg“ schließt ihn nicht, solange die Fassung liegt. Das Skript nennt die Zahlen (0.12 vorher, 3.1f nachher), **wertet sie aber nicht als Fehler**.
- **`moved`:** Hat Gmail eine große Kopie mit denselben Bytes unter eine neue UID gelegt und ist die neue Kopie geladen, führt rc10 die neue UID nach (Audit `moved`). Entwürfe, die rc8/rc9 deshalb fälschlich auf `taken_over` gesetzt hatten, werden darüber wieder `pushed`, wenn die neue Kopie geladen ist; alte Verknüpfungen ohne Abruf bleiben „nicht prüfbar“.
- **PDF-Programm:** Ab dem Tausch gilt `[extract] pdftotext = '<-PdfToText>'`. Vor dem ersten Auszug prüft rc10 das Programm mit einer eingebetteten Test-PDF (2.5: `probe ok`). Die vier Anhänge aus D17 stehen endgültig auf `failed`; sie holt erst B.3 nach (`attach retry`).

**Dauer:** Teil A etwa 20 bis 40 Minuten. Teil B etwa 60 bis 90 Minuten (B.2 wartet allein 15 Minuten und zwei Läufe), davon höchstens zweimal 20 Minuten für deine Handgriffe in B.1.

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, macht Parser-Probe und Lesetest, startet das Skript und wertet das Protokoll aus. Bei jeder Zeile „JETZT …“ sagt Claude dir, was im Webmail zu tun ist.
- **[Du]** gibst den Tausch frei, nennst den Pfad zu Poppler (`-PdfToText`), Konto und Nachricht der vier Anhänge aus D17 (`-RetryAccount`, `-RetryMessage`), startest nach Teil A die Claude-App neu und machst in B.1 zwei Handgriffe im Webmail.

## 1. Was das Skript tut und was nicht (Unterschiede zu D17)

- **Versionen:** `-ExpectVersion 0.3.0-rc10`, `-OldVersion 0.3.0-rc9`; das alte Programm wird `postkauz-rc9.exe`.
- **`-PdfToText <Pfad>` ist in Teil A Pflicht:** absolut, ohne `'`, vorhanden, und `<Pfad> -v` muss Poppler melden (0.11). Sonst Abbruch **vor** dem Tausch. 0.11b nennt, welches `pdftotext` im PATH der geplanten Aufgaben zuerst steht.
- **PATH:** Alle Aufrufe des Skripts laufen mit dem PATH der geplanten Aufgaben (Machine und User aus der Registry), nicht mit dem der aufrufenden Hülle. In D17 fand die Hülle zuerst das `pdftotext` von Git für Windows (Xpdf 4.06).
- **Konfiguration:** Das Skript sichert `config.toml` als `config.toml.d18-vor-extract` und trägt **unmittelbar nach dem Tausch und vor dem ersten Aufruf von rc10** ein:

  ```toml
  [extract]
  pdftotext = 'C:\Tools\Poppler\bin\pdftotext.exe'
  ```

  (einfache Anführungszeichen: in `"…"` wäre `\P` ein Ladefehler). Steht `[extract]` schon da, ersetzt es nur den Schlüssel, nie eine zweite Tabelle. **Vor jedem Aufruf von rc9** nimmt es den Eintrag wieder heraus (Sicherung zurück): bei E.0/E.0c (Rückweg nach einem FAIL), im Pfad nach 4.1 und im Rückweg (R.2b). R.0h (`accounts list` mit rc9 vor dem Anhalten) liest eine temporäre Kopie ohne `[extract]`. 0.3.0-rc9 und älter starten mit `[extract]` nicht.
- **Zählungen:** 0.10 Anhänge `failed` je Konto (wie D17, mit rc9), 0.12 Entwürfe mit `taken_over`/`missing` (rc9); nach dem Tausch 2.6 `extract_failed` aus `doctor --json`; nach dem ersten Lauf 3.1e `extract_failed` und 3.1f `taken_over`/`missing` und die Zahl der Audit-Einträge `moved` und `taken_over` mit `edited_elsewhere`.
- **Unverändert aus D17:** `-Exe`, `-DataDir`, `-Config` Pflicht ohne Standard, `-Tasks` Pflicht (ohne Sicherungsaufgabe), Anhalten mit 75 s Wartezeit (1.1b), Umbenennen statt Überschreiben mit `-KeepStdioMcp`, Sicherung nur mit `-BackupTo` (empfohlen), offene Aktionen `draft_update`/`draft_discard` vor jedem Rücktausch ausführen (E.0d), Strg+C im Abschluss aus, Protokoll ohne Namen (Konten als „Konto <n>“).
- **W1 (neu):** Bleiben Aufgaben aus (rc9 bleibt im Ziel, Aktion offen, Zurücklegen gescheitert), steht im Protokoll eine Zeile `AUFGABEN AUS: … Grund: … Default (abnahme_D18.md Abschnitt 8): …`.
- **W2 (neu):** In Teil B liest das Skript vor der ersten Protokollzeile `accounts list` und maskiert alle Kontonamen, auch ohne `-Accounts`.
- **W3 (neu):** Scheitert im Rückweg das Anhalten (R.1), ist nichts getauscht; liegt rc10 noch an seinem Platz, schaltet das Skript die Aufgaben wie in Teil A 1.1 wieder ein.

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d18` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc10, nicht umbenennen), `SHA256SUMS` und `abnahme_D18.ps1`.
2. **[Claude] Parser-Probe** (Pflicht); erwartet ist eine **leere** Ausgabe:

   ```powershell
   $e = $null; [void][System.Management.Automation.Language.Parser]::ParseFile('C:\PostKauz\d18\abnahme_D18.ps1', [ref]$null, [ref]$e); $e
   ```

   Steht dort etwas, nicht weitermachen; Claude meldet Zeile und Meldung.
3. **[Du/Claude] Poppler finden:** den Ordner mit `pdftotext.exe` von Poppler (nicht `…\Git\…`). Probe:

   ```powershell
   & 'C:\Tools\Poppler\bin\pdftotext.exe' -v
   ```

   Erwartet: eine Zeile `pdftotext version 2…` und eine mit `Poppler`. **Wahrscheinlichster Bedienfehler:** der Pfad zeigt auf das `pdftotext.exe` von Git (`Xpdf`); dann FAIL 0.11 und Abbruch vor dem Tausch.
4. **[Claude]** Konten und Aufgaben wie in D17 (Abschnitt 1 dort) nennen lassen und zeigen.
5. **[Du]** für B.3: Konto und lokale ID der Nachricht mit den vier PDF-Anhängen aus D17 (Claude findet sie mit `postkauz attach list <id> --json`: vier Anhänge mit `extract_status: failed` und `extract_error` „pdftotext version 4.06 …“).
6. **[Claude] Lesetest (fester Schritt vor dem Tausch):** derselbe Aufruf wie in Abschnitt 3, aber mit `-OldVersion 0.0.0-probe` und eigener Protokolldatei:

   ```powershell
   powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -ExpectSha256 <sha256> -PdfToText C:\Tools\Poppler\bin\pdftotext.exe -Accounts <konten> -Tasks "<aufgaben>" -KeepStdioMcp -OldVersion 0.0.0-probe -LogFile C:\PostKauz\d18\D18_lesetest.txt
   ```

   Erwartet: genau zwei FAIL (0.4: altes Programm meldet nicht `0.0.0-probe`; E.2: Teil 3 lief nicht), die Zeilen `Abbruch: Voraussetzungen fehlen; nichts geaendert` und `Ergebnis: nichts geaendert (…)`, alle übrigen Zeilen OK oder INFO, darunter 0.11 OK mit einer Poppler-Zeile. **Ein weiterer FAIL:** nicht tauschen, Claude meldet ihn.

## 3. Lauf Teil A

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -ExpectSha256 <sha256> -PdfToText C:\Tools\Poppler\bin\pdftotext.exe -Accounts <konten> -Tasks "<aufgaben>" -KeepStdioMcp -BackupTo E:\Sicherung\d18
```

Aus einer PowerShell **mit denselben Rechten wie die Aufgaben** (wie D17). Danach die Claude-App neu starten (der stdio-MCP läuft sonst mit `postkauz-rc9.exe` weiter).

## 4. Erwartet (Teil A)

| Nr | erwartet |
|---|---|
| 0.0–0.10 | wie D17 (OK oder INFO); 0.7b ist jetzt INFO |
| 0.11 | OK: `-PdfToText` absolut, vorhanden, `-v` meldet Poppler |
| 0.11b, 0.11c, 0.12 | INFO: erstes `pdftotext` im PATH der Aufgaben, `[extract]` (vorher `False`), `taken_over`/`missing` vorher |
| 1.1–1.6 | wie D17 |
| 2.0b | OK: `[extract] pdftotext` eingetragen, Sicherung `config.toml.d18-vor-extract` |
| 2.1–2.3 | OK wie D17 (Schema 22, keine neue Warnung) |
| 2.5 | OK: `doctor --json` `pdftotext.path` = `-PdfToText`, `setting path`, `probe ok` |
| 2.6 | INFO: Anhänge `failed` je Konto (die vier aus D17 darunter) |
| 3.1–3.7 | wie D17; 3.1e INFO `extract_failed`, 3.1f INFO `taken_over`/`missing`/`moved` (kein Fehler, auch wenn Zahlen sich ändern) |
| 4.1 | OK: Aufgaben wieder an |
| Schluss | `D18: <n> OK, 0 FAIL, …` |

## 5. Teil B (eigenes Konto)

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -TeilB -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -SelfAccount <dein-konto> -AgentKeyFile C:\PostKauz\d18\agent.key -RetryAccount <konto-der-anhaenge> -RetryMessage <nachricht-id>
```

B.1 und B.2 laufen mit dem Agentenschlüssel über MCP (ohne `-AgentKeyFile`: SKIP), B.3 und B.4 als Mensch an der CLI. Fristen wie D17: Ablage 12 min (`-WaitPushMinutes`), Handgriff samt Lauf 20 min (`-WaitEditMinutes`), Lauf über alle Ordner höchstens 5 min (`-WaitRunMinutes` 10 als Puffer). Einzelne Schritte wiederholen: `-Steps B.1`.

**B.1 Bearbeitung im Webmail (zwei Handgriffe):**

1. Das Skript legt „D18 Probe B.1 …“ an und wartet auf die Kopie (B.1a, B.1b).
2. **JETZT (B.1c):** im Webmail den Entwurf öffnen, **einen Satz** ergänzen, schließen (Gmail speichert ihn). Nicht senden, nicht planen, nicht löschen. Erwartet nach einem vollständigen Lauf: B.1d OK `remote taken_over`, `edited_message_id` gesetzt. **Wahrscheinlichster Bedienfehler:** gesendet statt geändert (B.1d FAIL mit „Bedienfehler … gesendet“; harmlos, eine Mail an dich selbst) oder einen anderen Entwurf geändert (FAIL „keine Aenderung erkannt“).
3. B.1e OK `drafts.update` → `draft_taken_over`; B.1e2 OK `drafts.discard` → `copy_changed`; B.1e3 OK nichts verschoben.
4. **JETZT (B.1e4):** im Webmail die geänderte Fassung **verwerfen** (Entwurf öffnen, Papierkorb-Symbol „Entwurf verwerfen“). Erwartet nach einem weiteren Lauf: B.1e5 OK `remote missing`, `edited_message_id` leer. **Wahrscheinlichster Bedienfehler:** die Fassung nur geschlossen statt verworfen (B.1e5 FAIL „noch taken_over“).
5. B.1f OK: `drafts.discard` schließt ohne Aktion (`copies 0`).

**B.2 Großer Entwurf (kein Handgriff):** Nur wenn `[imap] max_message_bytes` größer als die Nachricht ist (Standard 256 MiB; sonst SKIP mit Grund). Das Skript erzeugt im `attach_dir` eine Datei mit `-BigMB` (11) MB Zufallsdaten (ohne personenbezogene Daten; danach gelöscht), legt damit einen Entwurf an (Nachricht über 13 MB) und wartet auf die Ablage (B.2b; der APPEND dauert 60–100 s). Dann **15 Minuten nichts tun** (`-QuietMinutes`; den Entwurf im Webmail nicht öffnen) und zwei vollständige Läufe. B.2c OK: nie `taken_over`, `remote pushed`; das Detail nennt, ob unverändert `pushed` oder `moved` im Audit (beides OK). B.2d OK: `drafts.update` (ein Satz); B.2e neue Kopie; B.2f OK: danach nur die neue Kopie im Entwürfe-Ordner, keine wartende Aktion (zog eine Kopie um, erst nach dem nächsten Lauf); B.2g/B.2h OK: `drafts.discard`, Kopie im Papierkorb. **Wahrscheinlichster Bedienfehler:** den Entwurf im Webmail geöffnet (dann kann Gmail ihn neu speichern: B.2c FAIL mit `taken_over`).

**B.3 `attach retry` (kein Handgriff):** B.3a nennt die Zahl der Anhänge `failed` der Nachricht (erwartet 4), B.3b OK Trockenlauf nennt dieselbe Zahl, B.3c OK zurückgesetzt, B.3d OK nach dem nächsten Lauf alle `ok`, B.3e OK `attach text` liefert Text. **Wahrscheinlichster Bedienfehler:** falsches Konto oder falsche Nachricht (B.3a nennt 0, B.3d FAIL).

**B.4 `doctor --json`:** B.4a OK `pdftotext.probe ok`, B.4b OK Felder je Konto und `notes`, B.4c INFO Zähler, B.4d OK Exit-Code 0.

## 6. Rückweg (nur bei Problemen)

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d18\abnahme_D18.ps1 -Rueckweg -Exe C:\PostKauz\postkauz.exe -DataDir C:\PostKauz\data -Config C:\PostKauz\config.toml -NewExe C:\PostKauz\d18\postkauz-windows-amd64.exe -Accounts <konten> -Tasks "<aufgaben>" -KeepStdioMcp
```

Wie D17 ([betrieb.md](betrieb.md) 1.7c und 1.7d): R.0h liest eine temporäre Konfiguration ohne `[extract]`; nach dem Anhalten prüft rc10, dass keine Aktion `draft_update`/`draft_discard` offen ist (R.2); R.2b nimmt `[extract]` heraus; dann rc9 an seinen Platz, `doctor`, ein Lauf, Aufgaben an. rc9 lässt die Vermerke `draft_edited/<id>` liegen und führt die nach `moved` nachgeführte UID wie jede abgelegte Kopie (es prüft sie aber wieder streng: ein von Gmail umgelegter großer Entwurf kann unter rc9 wieder `taken_over` werden).

Von Hand (ohne Skript): Aufgaben anhalten, `config.toml.d18-vor-extract` zurück nach `config.toml` kopieren (oder den Abschnitt `[extract]` löschen), `postkauz-rc9.exe` an die Stelle von `postkauz.exe`, `postkauz doctor`, Aufgaben starten.

## 7. Aufräumen nach Teil B

Offene Probe-Entwürfe mit `remote pushed` verwirft das Skript selbst (B.9). Bleibt ein Probe-Entwurf offen (etwa B.1 nach einem FAIL), im Webmail die Fassung „D18 Probe …“ löschen und `postkauz draft abandon <id>`.

## 8. Wenn etwas FAIL ist

- **Vor dem Tausch** (Teil 0, Teil 1): nichts geändert; Claude meldet die Zeile.
- **Nach dem Tausch:** E.0d führt offene Aktionen `draft_update`/`draft_discard` mit rc10 aus, E.0x nimmt `[extract]` heraus, E.0 legt rc9 zurück, E.0c prüft die Datenbank, die Aufgaben laufen mit rc9.
- **Aufgaben bleiben aus** (Zeile `AUFGABEN AUS: …`, W1): **Default:** nicht von Hand einschalten. Claude meldet den Stand; du entscheidest, ob rc10 bleibt (dann `[extract]` prüfen und die Aufgaben einschalten) oder der Rückweg nach Abschnitt 6 läuft.
- **Teil B:** ein FAIL in B.1 oder B.2 ist meist ein Bedienfehler (die Zeile sagt es); wiederholen mit `-Steps B.1` bzw. `-Steps B.2`. Ein FAIL in B.2c mit `taken_over` ohne Bedienfehler ist ein Programmfehler (Claude prüft das Audit `drafts.remote_check` des Entwurfs: `via`, `copy_uid`, `loaded`, `changed`).

## 9. Was aus der Gegenprüfung des Skripts D17 umgesetzt ist

- **W1:** Aufgaben bleiben in denselben Fällen aus wie in D17 (rc9 bleibt im Ziel, Aktion offen, Zurücklegen gescheitert); neu ist nur die klare Zeile mit Aufgaben, Grund und Default.
- **W2:** Teil B maskiert alle Kontonamen aus `accounts list` vor der ersten Protokollzeile.
- **W3:** Nach einem FAIL in R.1 (nichts getauscht) schaltet das Skript die Aufgaben wieder ein, wenn rc10 noch an seinem Platz liegt.
- **Offen:** die Hinweise K-a bis K-f der Gegenprüfung (Prüfbericht beim Betreiber), Sammelpunkt in [roadmap.md](roadmap.md).
