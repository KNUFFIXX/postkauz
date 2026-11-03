# Abnahme D11: Umstellung auf 0.3.0-rc3 (A05 „Erkennung und Text“)

Stand: 03.11.2026 (Auftrag [A05](../auftraege/A05_erkennung.md), Teil 4). Skript: [abnahme_D11.ps1](abnahme_D11.ps1). Programm: Build **0.3.0-rc3** (Prüfsummen in [protokolle/A05_stand.md](protokolle/A05_stand.md)).

**Worum es geht:** Der Dauerbetrieb auf dem Büro-PC läuft seit 03.11.2026 mit 0.3.0-rc2. D11 stellt ihn auf 0.3.0-rc3 um und prüft dabei die drei Teile von A05 **am echten Bestand**:

1. Antwort-Entwürfe auf Mails mit Outlook-Verlauf, die in Gmail unverändert gesendet wurden, erkennt der Rückfall jetzt (Teil 1). Offene Entwürfe von vor der Umstellung prüft der erste Lauf einmal neu.
2. Antwort-Entwürfe, die du vor dem Senden geändert hast, erkennt der neue Weg über den Verlauf (Teil 2, Grund `thread`).
3. Mails mit HTML-Quelltext im Klartextteil zeigt PostKauz ohne HTML-Tags und ohne die Warnung `plain text differs from the HTML part` (Teil 3).

**Dauer:** etwa 20 Minuten, die meiste Zeit für die Sicherung. Du selbst brauchst etwa 5 Minuten (Claude-App beenden, am Ende die Liste der Entwürfe ansehen).

## Arbeitsteilung

- **[Claude]** kopiert Programm, `SHA256SUMS` und Skript auf den Büro-PC, sucht vorher eine betroffene Nachricht (Schritt 2.3), startet das Skript und wertet das Protokoll aus.
- **[Du]** beendest die Claude-App, bestätigst am Ende, ob die Entwürfe richtig erkannt sind, und startest die Claude-App wieder.

## 1. Was das Skript tut und was nicht

- Es arbeitet am **Live-Datenverzeichnis** (`C:\PostKauz\data`, `C:\PostKauz\config.toml`), weil genau dort geprüft werden soll. Vorher macht es eine **Sicherung** nach [betrieb.md](betrieb.md) 2.2/2.3; ohne Sicherung tauscht es nichts.
- Es hält die Aufgaben `PostKauz sync watch`, `PostKauz audit export` und (falls vorhanden) `PostKauz MCP HTTP` an und schaltet sie ab, wie beim Update in [betrieb.md](betrieb.md) 1.7. Läuft danach noch ein `postkauz.exe` (meist die Claude-App), bricht es ab, ohne etwas zu ändern.
- **Abbrüche:** Fehlt eine Voraussetzung (Teil 0), läuft noch ein `postkauz.exe` (1.1), ist der Stand vorher nicht lesbar (1.2, 1.3) oder fehlt die Sicherung (1.5), tauscht das Skript nichts. Scheitert 2.1 oder 2.2, kopiert es `postkauz-rc2.exe` zurück, bevor das neue Programm einen `sync` macht. In jedem Fall, auch bei einem unerwarteten Fehler, schaltet es die Aufgaben wieder ein, die es abgeschaltet hat (`E.1`), und schreibt die Schlusszeile `D11: … OK, … FAIL`.
- Das alte Programm bleibt als `C:\PostKauz\postkauz-rc2.exe` liegen (für den Rückweg).
- **Keine Migration:** A05 ändert das Schema nicht (Schema 22). Die Texte bekommen nur einen neuen Aufbereitungsstand (4); das alte Programm kann damit weiterarbeiten (Abschnitt 5).
- Ins Protokoll kommen nur IDs, Zustände, Gründe und Zähler, nie Betreff, Adressen oder Mailtext. Von `sync` stehen nur Exit-Code und die **Zahl** der Zeilen auf stderr drin (dort können Ordnernamen stehen; die Protokolle landen in `docs/protokolle`).

## 2. Vorbereitung

1. **[Claude]** Ordner `C:\PostKauz\d11` anlegen, darin `postkauz-windows-amd64.exe` (Build 0.3.0-rc3, nicht umbenennen), `SHA256SUMS` und `abnahme_D11.ps1`.
2. **[Claude]** Die Konten des Dauerbetriebs nennen lassen:

   ```powershell
   C:\PostKauz\postkauz.exe accounts list --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
   ```

3. **[Claude]** Eine Nachricht mit HTML im Klartextteil suchen (noch mit rc2: Der Suchindex enthält den Quelltext):

   ```powershell
   C:\PostKauz\postkauz.exe search "xmlns" --limit 5 --data-dir C:\PostKauz\data --config C:\PostKauz\config.toml
   ```

   Eine der IDs mit `show <id> --json` prüfen: erwartet `"alternative_mismatch": true` und im Feld `text` Tags wie `<html` oder `<meta`. Diese ID ist `-HtmlMessage`.
4. **[Du]** Die Claude-App ganz beenden (auch im Infobereich unten rechts: Rechtsklick auf das Claude-Symbol → „Beenden“). Sonst läuft noch ein `postkauz.exe mcp`.
5. **[Claude]** Den Sicherungsordner wählen: dein verschlüsselter Sicherungsdatenträger, z. B. `E:\PostKauz-Sicherung` (eine frühere Sicherung dort wird ergänzt, [betrieb.md](betrieb.md) 2.3).

## 3. Lauf

In einer PowerShell als **derselbe Benutzer**, unter dem der Dauerbetrieb läuft (nicht „Als Administrator“, sonst ist der Schlüsselbund ein anderer):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d11\abnahme_D11.ps1 `
  -NewExe C:\PostKauz\d11\postkauz-windows-amd64.exe -BackupTo E:\PostKauz-Sicherung `
  -Accounts <konto1>,<konto2> -HtmlMessage <id>
```

Das Protokoll steht danach in `C:\PostKauz\D11_windows.txt`. Letzte Zeile: `D11: <n> OK, 0 FAIL`.

## 4. Erwartet

| Nr | Prüfung | Erwartet |
|---|---|---|
| 0.1 bis 0.4 | neues Programm, Prüfsumme, Versionen | rc3 passt zu `SHA256SUMS`, das laufende Programm ist rc2 |
| 0.5, 0.6 | Sicherungsordner, Konten | angegeben |
| 1.1 | Aufgaben anhalten | kein `postkauz.exe` läuft mehr |
| 1.2 | `draft list` vorher | Liste der offenen Entwürfe (ID, Status, Art, `remote`) |
| 1.3 | `doctor` vorher | `database ok`; die Warnungen werden für 3.5 gemerkt |
| 1.4, 1.4b | Nachricht vorher (INFO) | HTML-Tags im Text, meist `alternative_mismatch true`; Suchtreffer für „xmlns“ |
| 1.5 | Sicherung | Exit 0 **und** `backup written to …` (sonst Abbruch ohne Tausch) |
| 2.1 | Tausch | `C:\PostKauz\postkauz.exe` meldet 0.3.0-rc3, `postkauz-rc2.exe` liegt daneben |
| 2.2 | `doctor` | `database ok` (keine Migration); bei FAIL in 2.1 oder 2.2 kommt das alte Programm zurück (2.3), Abbruch vor dem ersten `sync` |
| 3.1 | `sync -a <Konto> --json` je Konto | Exit 0; die Zeile nennt `sent_by_message_id`, `sent_by_fallback`, `sent_by_thread` und die Zahl der stderr-Zeilen |
| 3.2 | `draft list` nachher | Offene Entwürfe von vorher, die gesendet waren, stehen auf `sent_by_human`; zu jedem gibt es einen Audit-Eintrag `drafts.sent_detected` mit Grund `message_id`, `fallback` oder `thread` |
| 3.3 | **[Du]** | Sind die geschlossenen Entwürfe wirklich gesendet und die offenen wirklich nicht? (`draft show <id>` zeigt den Betreff am Bildschirm) |
| 3.4 | `show <id>` | keine HTML-Tags, `text_source html`, `alternative_mismatch false` |
| 3.4b | Suche (INFO) | deutlich weniger Treffer für „xmlns“ als vorher |
| 3.5 | `doctor` nachher | keine Warnung, die vorher nicht da war |
| 3.6 | `audit verify` | `ok` |
| 4.1 | Aufgaben wieder an | die vorher eingeschalteten Aufgaben laufen wieder (nach einem Abbruch dasselbe als `E.1`) |

Danach **[Du]** die Claude-App wieder starten.

**Folge des Updates für offene Entwürfe (wie bei rc2, Entschiedene Fragen 70):** Antwort- und Weiterleitungs-Entwürfe, deren Ausgangsnachricht HTML-Quelltext im Klartextteil und **keinen** HTML-Teil hat (oder einen HTML-Teil ohne Text), baut rc3 mit bereinigtem Zitat neu. Ein freigegebener (`ready`) fällt deshalb beim nächsten Ablegen auf `draft` zurück (`approval_stale`), ein vorgelegter (`pending_approval`) scheitert bei `approve` mit `approval_stale`; dann `draft request <id>` erneut. Das betrifft nur solche offenen Entwürfe von vor der Umstellung.

**Was 3.2 bedeutet:** Der erste Lauf prüft jeden offenen Entwurf einmal nach den neuen Regeln. Entwürfe, die du in Gmail unverändert gesendet hast (auch auf Mails mit Outlook-Verlauf), schließt der Rückfall (`fallback`); solche, die du vor dem Senden geändert hast (etwa einen Satz ergänzt), schließt der Weg über den Verlauf (`thread`), wenn die Kopie aus dem Entwürfe-Ordner verschwunden ist und der eigene Text mindestens 8 Wörter hat. Bleibt ein gesendeter Entwurf offen, schließt du ihn wie bisher mit `draft mark-sent <id>` und sagst Claude die ID (dann prüfen wir, welche Bedingung fehlte; [versanderkennung.md](versanderkennung.md) Abschnitt 3a).

## 5. Rückweg (nur bei Problemen)

A05 hat **keine Migration**. Das alte Programm läuft deshalb mit der Datenbank weiter: Es kennt den Aufbereitungsstand 4 nicht, lässt solche Texte aber, wie sie sind (es bereitet nur Stände unter 3 neu auf), und `doctor` meldet `database ok`, nicht `too_new`. Eine Wiederherstellung der Sicherung ist **nicht** nötig. Prüfen und umstellen mit:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PostKauz\d11\abnahme_D11.ps1 -Rueckweg `
  -Accounts <konto1>,<konto2> -HtmlMessage <id>
```

Erwartet: `R.0` bis `R.0c` (`postkauz-rc2.exe` da und meldet rc2; `postkauz.exe` meldet rc3, `postkauz-rc3.exe` gibt es noch nicht, sonst kopiert das Skript nichts), `R.2` altes Programm mit `database ok`, `R.3` die Nachricht weiter ohne HTML-Tags, `R.4` je Konto ein `sync --json` ohne Fehler (nur Exit-Code und Zahl der stderr-Zeilen im Protokoll), `R.6` Aufgaben wieder an. Das neue Programm liegt danach als `postkauz-rc3.exe` daneben.

**Grenze nach einem Rückweg:** Wechselst du später wieder auf rc3, erfasst dessen erster Lauf die Nachrichten, die rc2 inzwischen mit Stand 3 gespiegelt hat, **nur für Teil 3** (HTML im Klartextteil). Die einmalige Neuprüfung des Rückfalls (Teil 1, `sent_check_rev`) ist dann schon vermerkt und läuft nicht noch einmal: Entwürfe aus der rc2-Zeit, die nur der Rückfall von Teil 1 erkannt hätte (Antwort auf eine Mail mit Outlook-Verlauf, unverändert gesendet), schließt der Weg über den Verlauf (Teil 2, wenn die Kopie fehlt und `In-Reply-To` passt) oder du selbst mit `draft mark-sent <id>`.

Nur wenn ein künftiger Build eine Migration hätte, verweigerte das alte Programm den Start (`ErrSchemaTooNew`, `doctor`: `database too_new`); dann hilft nur die Wiederherstellung der Sicherung nach [betrieb.md](betrieb.md) 2.5. **Für rc3 gilt dieser Fall nicht.**
