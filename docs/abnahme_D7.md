# Abnahme D7: Anhänge, Export, Sicherung, Wiederherstellung, Löschkonzept

Stand: 29.10.2026 (A03 Teil T3 mit zweiter Nachbesserung, Build `0.3.0-alpha.7b`). Gehört zu [auftraege/A03_teile.md](../auftraege/A03_teile.md), Teil T3, Haltepunkt „Abnahme D7“, und zu [auftraege/A03_s3.md](../auftraege/A03_s3.md), Teil D, Abschnitt D7 (Schritte 4, 5 und 7; die Schritte 1 bis 3 und 6 prüft [abnahme_D7a.md](abnahme_D7a.md)). Skripte: [abnahme_D7.ps1](abnahme_D7.ps1) (Windows PowerShell 5.1, ohne Go und Docker, nur mit dem Programm aus `dist/`) und [abnahme_D7_restore.sh](abnahme_D7_restore.sh) (Restore im Linux-Container). Ausgeführt von der Prüfsitzung.

## Zweck

T3 bringt Anhänge als Datei ablegen, den Export des Archivs (mbox, EML), Sicherung und Wiederherstellung und das Löschkonzept. D7 prüft das **offline**: ohne Mailserver, ohne echtes Postfach, ohne Zugangsdaten, mit drei erfundenen Mails. Nichts davon berührt die Live-Daten.

## Grundregeln

- **Nie die Live-Datenbank** (Rahmen 5): `-DataDir`, `-Config` und seit der Nachbesserung T3 auch `-LivePath` sind Pflicht; das Skript legt `-DataDir` und `-Config` neu an (`-DataDir` leer oder noch nicht vorhanden, `-Config` noch nicht vorhanden). Abbruch ohne Änderung, wenn `-DataDir`, `-Config` oder `-WorkDir` im Standard-Datenverzeichnis (`%LOCALAPPDATA%\postkauz`), im Standard-Konfigurationsordner (`%APPDATA%\postkauz`) oder in einem mit `-LivePath` genannten Pfad liegen (oder umgekehrt); ein Datei-Eintrag in `-LivePath` (die Live-`config.toml`) schützt auch ihren Ordner. Ist der Eintrag eine Datenbank-Datei (`*.db`, etwa `C:\PostKauz\data\postkauz.db` statt des Datenverzeichnisses), wird ihr Ordner wie ein Datenverzeichnis rekursiv nach Markern durchsucht (seit 0.3.0-alpha.7c). Dazu eine Markerdatei gegen Junction und `subst` (für `-DataDir`, `-WorkDir` und den Ordner von `-Config`), wie in D7a, und ein Abbruch, wenn neben `-Config` schon eine `policy.toml` liegt. Die Markersuche durchsucht die verbotenen Ordner rekursiv, den Ordner einer Live-Datei aber nur selbst (nicht seine Unterordner; sonst liefe sie mehrfach über das Live-Blob-Verzeichnis). Jedes aufgerufene Programm bekommt die Umgebung ohne **alle** `POSTKAUZ_*`-Variablen; ebenso `abnahme_D7_restore.sh`, das außerdem kanonisierte Pfade (Symlinks aufgelöst) gegen die Standardpfade vergleicht, auch unter `XDG_DATA_HOME`/`XDG_CONFIG_HOME`.
- **Protokoll** `D7_windows.txt` (Teil R: `D7_restore.txt`) in `-WorkDir`, exklusiv neu angelegt; Testdaten, Export, Sicherung und Kopie im neuen Laufordner `-WorkDir\lauf-<Datum-Uhrzeit>`. Schlüssel, Mailadressen und Pfade sind maskiert (`<DataDir>`, `<Lauf>`, `<Kopie>` …).
- Jede Zeile ist `OK`, `FAIL` oder `SKIP` mit Nummer; letzte Zeile `D7: <n> OK, <m> FAIL, <k> SKIP`. Exit 0 nur ohne `FAIL`, Exit 2 bei Abbruch vor dem ersten Schritt.
- Die Sicherung im Laufordner ist **unverschlüsselt** (synthetische Daten, unkritisch). Für echte Sicherungen gilt [betrieb.md](betrieb.md), Abschnitt 2.

## Voraussetzungen

| Datei | Herkunft |
|---|---|
| `postkauz.exe` | `dist/postkauz-windows-amd64.exe` dieses Zweigs, umbenannt; Prüfsumme gegen `dist/SHA256SUMS` |
| `abnahme_D7.ps1` | `docs/abnahme_D7.ps1` |
| `postkauz-linux-amd64`, `abnahme_D7_restore.sh` | für Schritt 5 im Linux-Container der Prüfsitzung |

Für die Thunderbird-Prüfung (4.13): Thunderbird mit dem Add-on ImportExportTools NG ([betrieb.md](betrieb.md), Abschnitt 6.3).

## Aufruf

Teil A (offline; darf ohne sichtbares Fenster laufen, dann ohne Frage nach Thunderbird):

```powershell
$d='C:\PK\d7'; Unblock-File "$d\postkauz.exe","$d\abnahme_D7.ps1"
powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File "$d\abnahme_D7.ps1" -ExePath "$d\postkauz.exe" -DataDir "$d\daten" -Config "$d\config.toml" -WorkDir $d -LivePath <Live-Datenverzeichnis>,<Live-config.toml>
```

Mit Thunderbird-Frage: Fenster sichtbar lassen und `-Interactive` anhängen.

Restore im Linux-Container (Paket aus Schritt 5.13 übertragen):

```bash
sh docs/abnahme_D7_restore.sh dist/postkauz-linux-amd64 D7_sicherung.tar /tmp/d7-linux
```

Teil R auf dem Heim-PC (zweite saubere Maschine; `D7_sicherung.tar` dort mit `tar -xf D7_sicherung.tar` entpacken):

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File C:\PK\d7\abnahme_D7.ps1 -ExePath C:\PK\d7\postkauz.exe -DataDir C:\PK\d7\kopie -Config C:\PK\d7\config-r.toml -WorkDir C:\PK\d7 -RestoreFrom C:\PK\d7\paket\sicherung -Reference C:\PK\d7\paket\D7_reference.json -LivePath <Live-Datenverzeichnis>,<Live-config.toml>
```

**Gegenprobe vor Teil A (seit der Nachbesserung T3, nichts wird verändert):** `-Config` in den Ordner der Live-Konfiguration legen, z. B. `-Config C:\PostKauz\config-d7.toml` bei `-LivePath C:\PostKauz\data,C:\PostKauz\config.toml`. Erwartet: `ABBRUCH: -Config liegt im Standard- oder Live-Pfad C:\PostKauz; nichts veraendert.`, Exit 2, kein Protokoll, keine Datei angelegt. Ebenso ohne `-LivePath`: PowerShell fragt nach dem Pflichtparameter (abbrechen mit Strg+C).

Weitere Schalter: `-ExpectVersion` (Standard `0.3.0-alpha.7b`). Ein Wiederholungslauf braucht ein neues `-DataDir`, ein neues `-Config` und ein neues `-WorkDir` (oder das alte Protokoll umbenennen).

## Teil A

| Nr. | Was passiert | Erwartet |
|---|---|---|
| 0.1 | `version` | nennt `-ExpectVersion`; SHA-256 der Programmdatei im Protokoll |
| 0.2–0.4 | Konto `demo` ohne Server, `import mbox` mit drei Mails (Rechnung mit PDF, Verein mit `CON.txt`, „Neues Werkzeug“ mit `werkzeug.pdf.exe`; Adressen nur `example.org`/`.net`), lokale IDs | `new: 3, existing: 0, skipped: 0, errors: 0` |
| 0.5–0.6 | Schlüssel `test-agent` (reader), `test-operator` (operator); `tag add rechnung <id>` | angelegt |
| 0.7 | `doctor` | `data dir:` ist `-DataDir`; `export: <DataDir>\export [paths.export_dir]` |
| 4.1 | `attach list` je Mail | je ein Anhang |
| 4.2 | `attach save <PDF> --subdir rechnungen` | `saved attachment … as rechnungen/Rechnung-2026-117.pdf below export_dir`; Datei Byte für Byte wie der Anhang |
| 4.3 | dasselbe noch einmal | `Rechnung-2026-117-2.pdf` (nie überschreiben) |
| 4.4 | `attach save <CON.txt>` | `_CON.txt` (Gerätename von Windows entschärft) |
| 4.5 | `attach save <werkzeug.pdf.exe>` | `werkzeug.pdf.exe.blocked`, `warning: an executable type …` |
| 4.6 | `attach save <PDF> --subdir ..\ausserhalb` | Exit 3, `denied (outside_export_dir)`, nichts angelegt |
| 4.7 | `attach save` mit `--key test-agent` bzw. `--key test-operator` | reader: Exit 3 `denied (missing_scope)`; operator: `dry run: would save attachment …` |
| 4.8 | `export eml --to <lauf>\export-eml -a demo` | `messages: 3, skipped: 0`; drei `.eml`, deren Name mit dem Anfang ihres SHA-256 endet (Bytes wie im Blob-Store); `index.csv` mit Kopfzeile `id,message_id,date,folder,size,sha256,file`, **ohne Betreff** |
| 4.9 | `export mbox --to <lauf>\export-mbox -a demo` | eine Datei `_imported.sbd\INBOX` mit drei Trennzeilen `From ` (nur importierte Mails: Ordner `_imported`) |
| 4.10 | `export eml` in denselben Ordner | Exit 3, `denied (target_not_fresh)` |
| 4.11 | `export mbox … --key test-operator` | Exit 3, `denied (human_only)`; kein Ordner |
| 4.12 | Rundlauf: Konto `zwei`, `import mbox <export-mbox> -a zwei`, zweimal | `new: 3, …`; zweiter Lauf `new: 0` |
| 4.13 | **[Du]** mbox in Thunderbird öffnen (nur mit `-Interactive`) | „ja“: Betreff, Text, Anhang lesbar (Akzeptanzkriterium 4); sonst `SKIP` |
| 5.1 | `audit export --to <lauf>\audit-export` | Exit 0 (damit die Kopie Exporttage des Originals kennt) |
| 5.2 | `backup create --to <lauf>\sicherung` | `backup written to …`, `blobs: N (copied N, already there 0 …)`, `manifest.json` und `ENCRYPT-ME.txt` im Ordner, auf stderr `warning: the backup is not encrypted; …` (Akzeptanzkriterium 6) |
| 5.3 | `backup create` noch einmal in denselben Ordner | `copied 0, already there N`, `replaced: 1 database snapshot of the earlier backup` |
| 5.4 | `backup create … --key test-operator` | Exit 3, `denied (human_only)` |
| 5.5 | `backup create --to <DataDir>\sicherung` | Exit 1, Ziel im Datenverzeichnis abgelehnt |
| 5.6 | Referenz `D7_reference.json`: Treffer von fünf Suchen (`Rechnung`, `Verein`, `Werkzeug`, `Heizungswartung`, `tag:rechnung`), IDs der drei Mails, Zeile `verify:` von `doctor --blobs=all` | `result: ok` |
| 5.7 | `restore --from <sicherung> --data-dir <lauf>\wiederhergestellt` | `restored: …`, `identity: new data directory id`, danach `doctor --blobs=all` mit `result: ok` |
| 5.8–5.9 | Suche, IDs und `doctor --blobs=all` in der Kopie | identisch mit dem Original (Akzeptanzkriterium 3) |
| 5.10 | `audit verify --json` in Original **und** Kopie | beide `status: ok`, keine Befunde (A10: kein „changed“) |
| 5.11 | `doctor` der Kopie | `account demo has no password` (der Schlüsselbund des Originals wird nicht benutzt) |
| 5.12 | Kopie der Sicherung mit einem veränderten Byte in einem Blob, `restore` | Exit 1, `nothing was restored`, kein Zielordner |
| 5.13 | `tar.exe -cf D7_sicherung.tar sicherung D7_reference.json` | Paket für den Container und den Heim-PC (Prüfsumme im Protokoll) |
| 7.1 | `purge --older-than 0s --dry-run --json` | alle Kandidaten sind importiert: `kept_imported` = `gone_from_server` (≥ 3), `eligible` 0 |
| 7.2 | `purge --older-than 0s` (echt) | `deleted: 0 messages, …`: das importierte Archiv bleibt |
| 7.3 | `blob gc --dry-run` | `without reference: 0`, `dry run: nothing changed` |
| 7.4 | `audit rotate --dry-run` | `nothing to rotate: no exported day ended before … (retain 365d)` |
| 7.5 | `purge`, `blob gc`, `audit rotate` mit `--key test-operator` | je Exit 3, `denied (human_only)` |
| 6.1 | `audit --since 30m --json` | Einträge für `attachments.save`, `messages.export`, `backup.create`, `retention.purge`, `blobs.gc`, `audit.rotate`; kein Betreff, kein Anhangsname |
| 6.2 | `audit verify --json` | `status: ok` |

**Abweichung von A03 D7.7:** Dort steht „`purge --dry-run --older-than 0s` zählt nur importierte Nachrichten“. Seit A13.2 löscht `purge` das importierte Archiv nie; es zählt die importierten Mails deshalb nur unter `kept (imported archive)` und würde 0 löschen. Echt läuft `purge` hier, weil es nichts löschen darf (7.2 belegt das).

## Linux-Container und Heim-PC (D7.5, saubere Maschinen)

`abnahme_D7_restore.sh` (Zeilen `OK`/`FAIL`, Protokoll `D7_linux.txt` im Arbeitsordner): entpacken, `restore --dry-run` (`is complete and unchanged`), `restore` (neue Identität, `result: ok`), dieselben fünf Suchen und drei IDs wie in der Referenz, `doctor --blobs=all` mit derselben Zeile `verify:`, `audit verify` ohne Befund. Teil R des PowerShell-Skripts prüft dasselbe auf dem Heim-PC (`R.1` bis `R.6`).

Probelauf in der Bau-Sitzung (28.10.2026, Linux-Programm des Stands): Python-Nachbau von Teil A mit denselben Kommandos und Mustern 35 OK, 0 FAIL; `abnahme_D7_restore.sh` auf dessen Paket 7 OK, 0 FAIL. Das PowerShell-Skript selbst lief im Container nicht (kein PowerShell); die Prüfsitzung führt es auf dem Büro-PC aus.
