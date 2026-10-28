# Abnahme T1: Härtung, Audit, Jobs, Policy

Stand: 28.10.2026 (T1b nach der Nachbesserung und dem Merge mit `main`, Build 0.3.0-alpha.3; Vorfassung 27.10.2026 für 0.3.0-alpha.2). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Haltepunkt von T1b; ausgeführt von der Prüfsitzung auf dem Büro-PC. Skript: [abnahme_T1.ps1](abnahme_T1.ps1) (Windows PowerShell 5.1, ohne Go und Docker, nur mit den Programmen aus `dist/`). Schritt 0 stammt aus T1a und gilt für jeden Test eines neuen Builds an einer Kopie und vor dem Update der echten Datenbank.

**Grundregeln:** Das Skript arbeitet nur an einer **Kopie** (Rahmen 5). `-DataDir` und `-Config` sind Pflicht; es bricht ab, wenn `-DataDir` das Standard-Datenverzeichnis (`%LOCALAPPDATA%\postkauz`) oder das Live-Verzeichnis ist oder darin liegt, und wenn `-Config` die Standard-Konfiguration (`%APPDATA%\postkauz\config.toml`) oder die Live-Konfiguration ist. Weil Zeichenketten allein eine Junction oder ein `subst`-Laufwerk nicht erkennen, legt das Skript in `-DataDir` und `-WorkDir` kurz eine Markerdatei an und bricht ab, wenn sie im Live- oder Standard-Datenverzeichnis auftaucht. Die Live-Datenbank wird nur kopiert, nie mit PostKauz geöffnet (für die Prüfung auf andere Prozesse öffnet das Skript sie einen Augenblick exklusiv zum Lesen). Das Protokoll (`T1_windows.txt` in `-WorkDir`) maskiert Mailadressen, ersetzt Kontonamen durch `konto1`, `konto2` … und die Pfade (`USERPROFILE`, Live-Datenverzeichnis, `-DataDir`, `-WorkDir`, Konfigurationen) durch Platzhalter wie `<DataDir>` und nennt die Suchanfragen nicht. Die Umstellung des Dauerbetriebs (Schritt 4) macht das Skript nie.

## 0. Vor dem Update: alle Prozesse von 0.2.0-rc8 stoppen (Pflicht)

**Warum:** Ab 0.3.0-alpha.1b hält jeder PostKauz-Prozess eine gemeinsame Sperre neben der Datenbank (`postkauz.db.lock`), und eine neue Version migriert nur, wenn kein anderer Prozess die Datenbank offen hat (sonst `migration_blocked`, nichts geändert). **0.2.0-rc8 kennt diese Sperre nicht.** Ein laufender MCP-Server oder Audit-Export von rc8 würde die Migration nicht verhindern und danach mit dem neuen Schema weiterarbeiten. Für die Abnahme gilt zusätzlich: Kopiert wird nur bei gestopptem Dauerläufer (sonst ist die Kopie halb geschrieben). Deshalb vor Schritt 1 von Hand stoppen:

| Nr. | Was | Wie (Windows) | Erwartet |
|---|---|---|---|
| 0.1 | Aufgabe `sync --watch` | Start → „Aufgabenplanung“ öffnen → links „Aufgabenplanungsbibliothek“ → Aufgabe des Watchers anklicken → rechts „Beenden“, dann „Deaktivieren“ | Spalte „Status“: „Deaktiviert“ |
| 0.2 | Aufgabe Audit-Export | dort Aufgabe „PostKauz audit export“ → rechts „Deaktivieren“ | Spalte „Status“: „Deaktiviert“ |
| 0.3 | Claude-App (startet den MCP-Server `postkauz mcp`) | Claude-App schließen, dann im Infobereich unten rechts (Pfeil „^“) Rechtsklick auf das Claude-Symbol → „Beenden“ | kein Claude-Symbol mehr im Infobereich |
| 0.4 | Kontrolle: kein PostKauz-Prozess mehr | PowerShell: `Get-Process postkauz -ErrorAction SilentlyContinue` (das Skript prüft das auch, Zeile `0.4`, zusätzlich über Pfad und Kommandozeile, also auch eine umbenannte `postkauz-rc8.exe`, und Zeile `0.5`: die Live-Datenbank lässt sich exklusiv öffnen) | keine Ausgabe |

**Hart beendeter Watch (seit 0.3.0-alpha.3, A-F9):** Beendet die Aufgabenplanung `sync --watch` hart (Schritt 0.1 „Beenden“), bleibt im Audit ein Start ohne Abschluss. Bis 0.3.0-alpha.2 blieb er für immer offen (`doctor` warnte dauerhaft); ab 0.3.0-alpha.3 schließt der nächste Sync bzw. Import dieses Kontos ihn mit `error`/`process_ended` ab. rc8 schreibt keine Start-Einträge; an der Kopie (2b.5) gibt es deshalb keine offenen Starts aus dem Dauerbetrieb.

Nach der Abnahme an der Kopie die Aufgaben wieder aktivieren (Aufgabe anklicken → rechts „Aktivieren“) und die Claude-App starten: Der Dauerbetrieb läuft dann unverändert mit 0.2.0-rc8 weiter.

## Aufruf

Vorbereitung (Prüfsitzung): beide Programme in eigene Ordner legen (alt: 0.2.0-rc8 aus dem GitHub-Release, neu: 0.3.0-alpha.3 aus `dist/` dieses Zweigs, Prüfsumme gegen `SHA256SUMS`), einen leeren Arbeitsordner anlegen, darin eine **Konfiguration für die Kopie**: Kopie der echten `config.toml`, darin `[audit] export_dir` auf einen Ordner im Arbeitsordner ändern (der echte Export-Ordner wird nur von `audit verify` gelesen, nie beschrieben). Liegt neben der echten Konfiguration eine `policy.toml`, sie daneben kopieren. Optional eine Textdatei mit den festen Suchen des Betreibers (eine Anfrage je Zeile; sie bleibt auf dem Rechner, das Protokoll nennt nur „Suche 1, 2, …“).

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File abnahme_T1.ps1 `
  -OldExe <ordner>\rc8\postkauz.exe -NewExe <ordner>\alpha3\postkauz.exe [-NewVersion 0.3.0-alpha.3] `
  -LiveDataDir <Live-Datenverzeichnis> -LiveConfig <Live-config.toml> `
  -DataDir <arbeitsordner>\data -Config <arbeitsordner>\config.toml `
  -WorkDir <arbeitsordner> [-SearchFile <arbeitsordner>\suchen.txt]
```

`-NewVersion` ist die erwartete Version des neuen Programms (Standard `0.3.0-alpha.3`; für einen späteren Build angeben). Schritt 3 nur mit dem Zusatz `-Sync -SyncAccount <konto>` und nach der Zustimmung des Betreibers (siehe dort). Läuft das Skript ein zweites Mal, braucht es einen neuen leeren `-DataDir` (eine schon migrierte Kopie lehnt es ab).

## Schritte und Erwartung

Jede Zeile des Protokolls ist `OK`, `FAIL` oder `SKIP` mit Nummer; am Ende `T1: <n> OK, 0 FAIL, <k> SKIP`, Exit 0. Bei `ABBRUCH` hat das Skript nichts an der Kopie geändert, was nicht zurückzunehmen wäre; die Live-Daten bleiben ohnehin unberührt.

| Nr. | Prüfung | Erwartet |
|---|---|---|
| 0.0 | Versionen der beiden Programme | alt `0.2.0-rc8`, neu `-NewVersion` (Standard `0.3.0-alpha.3`) |
| 0.4 | kein `postkauz`-Prozess (Name, Pfad, Kommandozeile) | OK; sonst Abbruch |
| 0.5 | Live-Datenbank lässt sich exklusiv öffnen (kein Prozess hält sie offen) | OK; sonst Abbruch |
| 1.1 | freier Platz auf dem Laufwerk von `-DataDir` | mindestens dreimal so groß wie `postkauz.db` samt `-wal`/`-shm` |
| 1.2 | Kopie von `postkauz.db`, `-wal` und `-shm` **zusammen**, gleiche Größen, kein Prozess dazwischen gestartet | OK |
| 2a.1–2a.4 | **altes Programm** auf der Kopie: `audit verify` ok; `accounts list`; Zähler je Ordner (gespiegelt, ungelesen) aller Konten; feste Suchen (je bis 100 Treffer) | alle OK |
| 2b.0 | **neues Programm**, erstes Kommando (`accounts list`): Migrationen 0017, 0018, 0019 laufen, Snapshot `postkauz.db.pre-0017-….bak` neben der Kopie | OK, Dauer in ms im Protokoll (Messung an 50.000 Nachrichten und 20.000 Audit-Zeilen: 0017 rund 0,4 s, 0018 + 0019 rund 0,3 s samt Snapshot; die echte Datenbank ist wegen Volltextindex und Texten größer, der Snapshot wächst mit der Dateigröße) |
| 2b.5 | `doctor --json` | Datenbank `ok`, `schema_version` = `schema_known` = 19, `audit_unfinished_calls` 0 |
| 2b.1–2b.4 | dieselben Kommandos wie 2a mit dem neuen Programm | alle OK |
| 2c.1 | `audit verify` alt und neu | gleiche Zahl geprüfter Tage, gleiches Kettenende (die Migration 0018 hat die Audit-Zeilen Wert für Wert übernommen; Pflichttest A1) |
| 2c.2–2c.4 | Konten, Zähler je Ordner, Treffer der Suchen alt und neu | gleich (Treffer gleich in gleicher Reihenfolge) |
| 2c.5 | neues Audit: `audit --operation accounts.list --raw` | je Aufruf des neuen Programms ein Start `started` und ein Abschluss mit `ref_id` (2 und 2) |
| 2d | Zeile `INFO 2d`: wie viele wartende Entwürfe der erste Push nach der Umstellung verwerfen bzw. zurücksetzen würde (Schlüssel widerrufen, abgelaufen oder unbekannt; A8, A-F3) | nur Zahl, kein OK/FAIL; ist sie größer als 0, vor Schritt 4 mit dem Betreiber klären (`draft list`, `key list`) |
| 3.1–3.3 | nur mit `-Sync`: kein Entwurf wartet auf seine Kopie (`draft list`, `remote: pending`); lesender `sync --account <konto> --mailbox INBOX`; danach `audit verify` ok | OK; wartet ein Entwurf, `SKIP 3` (ein Sync würde ihn im echten Postfach ablegen); liefert `draft list` genau 500 Entwürfe (Obergrenze), ebenfalls `SKIP 3` (nicht prüfbar) |

**Was die Zahlen bedeuten:** 2c vergleicht, ob die neue Version dieselbe Datenbank genauso liest wie die alte. Unterschiede in 2c.3 oder 2c.4 sind ein Befund (FAIL), außer sie gehen auf Schritt 3 zurück (der läuft erst danach). Neu ist in der Ausgabe nur, was T1 hinzufügt (`remote_status`, im Audit `started` und `ref_id`); das Skript vergleicht nur Felder, die beide Versionen kennen.

## 3. Lesender Sync der Kopie (nur mit Zustimmung)

Nur ein Konto, nur INBOX, nur nach dem Ja des Betreibers. Der Sync liest das echte Postfach mit den Zugangsdaten aus dem Schlüsselbund (dieselben wie im Dauerbetrieb) und ändert dort nichts, **außer** er findet Entwürfe, deren Kopie noch aussteht: die legte er per APPEND im Entwürfe-Ordner des echten Postfachs ab. Deshalb prüft das Skript vorher `draft list` und überspringt den Schritt, wenn ein offener Entwurf `remote: pending` hat. Gmail zählt den Sync zum IMAP-Tageslimit; bei großer INBOX kann er dauern.

## 4. Umstellung des Dauerbetriebs (nicht im Skript)

Erst nach dem Ja des Betreibers zur Abnahme, mit allen rc8-Prozessen gestoppt (Schritt 0). Alle Kommandos mit **genau** den Pfaden, die die Aufgaben des Dauerbetriebs nutzen (`--data-dir <Live-Datenverzeichnis> --config <Live-config.toml>`, abgekürzt `<pfade>`); ohne sie migriert das Programm das Standardverzeichnis, nicht die echte Datenbank.

1. **Vorher zeigen** (noch mit rc8, nur lesend): `postkauz-rc8.exe <pfade> draft list --json` und `postkauz-rc8.exe <pfade> key list`. Wartende Entwürfe (`remote: pending`), deren Schlüssel widerrufen oder abgelaufen ist, verwirft bzw. setzt der erste Push zurück (Zeile `INFO 2d` der Abnahme); Betreiber entscheidet, ob das so sein soll.
2. **Freier Platz:** auf dem Laufwerk der echten Datenbank mindestens **zweimal** die Größe von `postkauz.db` samt `-wal` (Snapshot plus Wachstum).
3. Neues Programm an die Stelle des alten legen (alte Datei vorher umbenennen, z. B. `postkauz-rc8.exe`).
4. `postkauz <pfade> doctor` (öffnet nur), dann `postkauz <pfade> accounts list`: Das zweite migriert und legt den Snapshot `postkauz.db.pre-0017-<Zeit>.bak` an. **Kontrolle:** Der Snapshot liegt im Live-Datenverzeichnis **neben** der echten `postkauz.db` (Explorer oder `Get-ChildItem <Live-Datenverzeichnis> -Filter 'postkauz.db.pre-*.bak'`); liegt er woanders, war `<pfade>` falsch: anhalten.
5. `postkauz <pfade> audit verify` → `ok`; `postkauz <pfade> doctor` ohne Warnung.
6. Aufgaben wieder aktivieren (Aufgabe anklicken → rechts „Aktivieren“), Claude-App starten.

**Rückweg** über den Snapshot (alte Programmdatei zurück, `postkauz.db` durch den Snapshot ersetzen, `-wal`/`-shm` entfernen) **nur bis zum ersten Audit-Export mit der neuen Version**; danach nur vorwärts: Der Export hat dann Tage mit Einträgen des neuen Formats (`started`, `ref_id`) an den zweiten Ort geschrieben, die eine zurückgesetzte Datenbank nicht mehr belegt (`audit verify` meldete `database_changed`). Alles, was die neue Version bis dahin lokal geschrieben hat (Entwürfe, Audit), geht beim Rückweg verloren.

## Bekannte Grenzen

- Das Skript ist im Container nicht ausgeführt worden (dort gibt es kein PowerShell); die Prüfsitzung führt es aus (Fassung für 0.3.0-alpha.3 zum ersten Mal). Bei einem Skriptfehler (nicht bei einer fachlichen Abweichung) das Protokoll und die Meldung an die Bausitzung zurückgeben.
- Blobs (`blobs\`) werden nicht kopiert: `audit verify`, Suchen, Konten und Zähler brauchen sie nicht. Kommandos, die den Inhalt einer Nachricht lesen (`show`, Anhänge), zeigen auf der Kopie deshalb nicht alles; das ist kein Befund.
- Zugangsdaten für Schritt 3 kommen aus dem Windows-Schlüsselbund (wie im Dauerbetrieb). Nutzt das Live-Verzeichnis den Notbehelf `insecure-file` (`<Live-Datenverzeichnis>\insecure-secrets`), hat die Kopie keine Passwörter, und Schritt 3 scheitert; dann Schritt 3 weglassen.
- Eine Suchanfrage darf mit `-` beginnen (Ausschluss, z. B. `-is:read`); das Skript übergibt sie hinter `--`.
- `audit verify` liest auf der Kopie die Dateien am echten Exportort (der Nachweis in der Kopie zeigt dorthin); geschrieben wird dort nichts, das Skript ruft `audit export` nie auf.
