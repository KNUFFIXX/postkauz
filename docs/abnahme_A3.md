# Abnahme A3: verdeckte Passwortabfrage unter Windows (T1c)

Für: die Prüfsitzung auf dem Büro-PC. Grundlage: `auftraege/A03_s3.md`, Teil A, A3, und `auftraege/A03_teile.md`, T1c. Der Betreiber tippt nur auf Aufforderung; Claude startet das Skript und wertet das Protokoll aus.

## Worum es geht

Beim D5 (23.10.2026, Heim-PC) nahm die verdeckte Passwortabfrage von `account add` im Windows Terminal mit Windows PowerShell 5.1 weder Tippen noch Einfügen an. Ab `0.3.0-alpha.c1` liest PostKauz unter Windows die Konsole selbst (`ReadConsoleW`: Echo aus, Zeilen-Eingabe ein, UTF-16 nach UTF-8) und bricht nach 120 s ohne Eingabe ab (`prompt_timeout`). Diese Abnahme misst beide Leser in allen Fenstern der Matrix:

- **console** (neu): muss überall gehen. Das ist die Abnahme.
- **term** (alt, `golang.org/x/term`): nur Messung. Zeigt, ob der Befund D5 auf dem Büro-PC überhaupt auftritt.

Geprüft wird nie der Wert: Das versteckte Kommando `postkauz internal prompt-probe --json --expect-sha256 <hex>` liest eine Zeile wie `account add` und meldet Zähler, Konsolenmodus und nur, ob der SHA-256 der Eingabe dem vorgegebenen Hash entspricht (`hash_match`); den Hash selbst gibt es nie aus. Die Testwerte stehen im Skript (`PostKauz-Test-123` und `Prüfung-äöü-ß7`); sie sind keine Geheimnisse. **Nie ein echtes Passwort eingeben**, auch nicht zum Ausprobieren.

## Vorbereitung

1. Build `0.3.0-alpha.c2` aus `dist/` (`postkauz-windows-amd64.exe`) in einen eigenen Ordner kopieren, z. B. `D:\pk\a3\postkauz.exe`. Prüfsumme gegen `dist/SHA256SUMS`.
2. Einen **neuen, leeren** Ordner für `-DataDir` und einen Pfad für `-Config` wählen. Nie das Live-Datenverzeichnis des Dauerbetriebs: Das Skript verweigert `%LOCALAPPDATA%\postkauz`, `%APPDATA%\postkauz` und jeden Pfad aus `-LivePath` (dort das Live-Datenverzeichnis und die Live-Konfiguration angeben, mehrere Pfade durch Komma getrennt; das Skript trennt sie selbst, weil `powershell.exe -File` `a,b` als einen Wert übergibt). `prompt-probe` öffnet weder Datenbank noch Schlüsselbund; das Skript prüft am Ende, dass im Datenverzeichnis nichts entstanden ist.
3. Der Dauerläufer (rc8) darf weiterlaufen; der Test berührt ihn nicht.

## Ablauf

Im Windows Terminal ein Fenster mit Windows PowerShell 5.1 öffnen (Standardprofil „Windows PowerShell“) und starten:

```powershell
powershell.exe -NoProfile -ExecutionPolicy Bypass -File <Repo>\docs\abnahme_A3.ps1 -Exe D:\pk\a3\postkauz.exe -DataDir D:\pk\a3\data -Config D:\pk\a3\config.toml -Protocol D:\pk\a3\A3_prompt_windows.txt -HostLabel WT+PS51 -LivePath <Live-Datenverzeichnis>,<Live-Konfiguration> -Launch
```

Das Skript misst zuerst das Startfenster. Mit `-Launch` öffnet es danach die übrigen Fenster der Matrix **nacheinander**, jedes mit eigenem Namen: Vor jedem Fenster fragt das Startfenster „Enter öffnet das Fenster“, dann wartet es, bis das Fenster fertig ist (bei `conhost.exe` über `Start-Process -Wait`, bei Windows Terminal über eine Fertig-Datei im Temp-Ordner, weil `wt.exe` sofort zurückkehrt; `Q` im Startfenster bricht das Warten ab und zählt das Fenster als `FAIL … no result`). Es läuft also immer nur ein Fenster; Zwischenablage und Protokoll teilt sich keines mit einem anderen. Jede Protokollzeile wird bei einer kurz gesperrten Datei (IOException) bis zu 20-mal wiederholt.

| Fenster | Konsole | Shell | Name |
|---|---|---|---|
| Startfenster | Windows Terminal | Windows PowerShell 5.1 | `WT+PS51` |
| neu | Windows Terminal | PowerShell 5.1, Aufruf über `cmd.exe /c` | `WT+cmd` |
| neu (nur wenn `pwsh.exe` da ist) | Windows Terminal | PowerShell 7 | `WT+PS7` |
| neu | Legacy-Konsole `conhost.exe` | Windows PowerShell 5.1 | `conhost+PS51` |
| neu | Legacy-Konsole `conhost.exe` | PowerShell 5.1, Aufruf über `cmd.exe /c` | `conhost+cmd` |
| neu (nur wenn `pwsh.exe` da ist) | Legacy-Konsole `conhost.exe` | PowerShell 7 | `conhost+PS7` |

In jedem Fenster fragt das Skript je Leser die Schritte unten ab. Die gelbe Zeile sagt, was zu tun ist; **erst Enter drücken** (Startsignal, „Bereit? Enter startet die Abfrage“), dann erscheint `Type or paste the test value (never a real password)`, und erst dann tippen oder einfügen. Die 60 s Zeitgrenze läuft ab diesem Enter. Am Ende eines weiteren Fensters schließt Enter es; danach geht es im Startfenster weiter.

1. `type-ascii`: `PostKauz-Test-123` tippen, Enter.
2. `paste-ctrlv-ascii`: mit Strg+V einfügen, Enter (der Wert liegt schon in der Zwischenablage).
3. `paste-rightclick-ascii`: mit Rechtsklick ins Fenster einfügen, Enter.
4. `type-umlaut`: `Prüfung-äöü-ß7` tippen, Enter.
5. `paste-ctrlv-umlaut`: mit Strg+V einfügen, Enter.
6. `paste-rightclick-umlaut`: mit Rechtsklick ins Fenster einfügen, Enter.

Das sind Tippen, Strg+V und Rechtsklick je mit ASCII und mit Umlaut (A3 Punkt 2).

Beim Tippen erscheint nichts; das ist richtig. Geht gar nichts, 60 s warten: Der Schritt endet dann mit `FAIL … why: error: no input` (beim Leser `console` ist das ein Befund, beim Leser `term` die Bestätigung von D5). Nach jedem Einfügen leert das Skript die Zwischenablage.

## Erwartet

Je Fenster im Protokoll:

```
== A3 WT+PS51  2026-…  PowerShell 5.1.…  via direct  Windows Terminal: True  D:\pk\a3\postkauz.exe
   version: postkauz 0.3.0-alpha.c2 (go1.27.3 windows/amd64)
OK WT+PS51 console type-ascii  (Erwartet: OK)
     stdin char, terminal True, code pages in/out/ansi 850/850/1252, windows terminal True
     mode before 0x… PROCESSED_INPUT|LINE_INPUT|ECHO_INPUT|…; during 0x… PROCESSED_INPUT|LINE_INPUT|…; after (wie before)
     reads 1, utf16 19, bytes 17, chars 17, non-ascii 0, invalid 0, escape 0, control 0, line end crlf, …
     expected 17 bytes, hash match True
…
OK WT+PS51 data-dir untouched
A3 WT+PS51: 7 OK, 0 FAIL (console), term: … OK, … FAIL
…
A3 matrix: 6 windows, 0 with FAIL (console), started from WT+PS51
```

- **Abnahme bestanden**, wenn in jedem Fenster alle Zeilen des Lesers `console` `OK` sind (Hash gleich, Modus nachher wie vorher, Exit 0) und `data-dir untouched` `OK` ist, und die letzte Zeile `A3 matrix: … 0 with FAIL (console)` lautet. Das Skript endet dann mit Exit 0.
- Die Zeilen des Lesers `term` sind Messung: Sie zeigen, welche Hypothese aus A3 zutrifft (stdin kein Konsolen-Handle → a; Zeilenende `cr` oder 0 Zeichen ohne Zeilen-Eingabe → b; falscher Hash nur bei Umlauten, `invalid` > 0 → c; Einfügen leer → d; `escape` > 0 → e).

## Danach

1. Protokoll ins Repository: `docs/protokolle/A3_prompt_windows.txt` (es enthält keine Geheimnisse; Pfade des Betreibers vorher durch `<…>` ersetzen).
2. Ergebnis in `docs/roadmap.md` und in `docs/protokolle/T1c_stand.md`: welche Fenster, welcher Leser, welche Hypothese.
3. Geht `console` irgendwo nicht: Befund mit den Messwerten an den Betreiber; bis zur Klärung gilt der Umweg aus `docs/abnahme_D5.md` (Zwischenablage → `--password-stdin`), und `POSTKAUZ_PASSWORD_READER=term` schaltet auf den alten Leser zurück.
4. Den Testordner (`-DataDir`, `-Config`) löschen.
