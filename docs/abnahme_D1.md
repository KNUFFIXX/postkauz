# Abnahme D1: Sync (nach S1.5)

Stand: 15.10.2026 (nach Gegenprüfung korrigiert). Gehört zu `auftraege/A01_phase0.md`, Teil D, Abschnitt D1. Die Kommandos und erwarteten Ausgaben sind gegen den Dovecot-Testserver durchgespielt (Linux-Entsprechungen; PowerShell-Fassung unten).

**Was du prüfst:** PostKauz spiegelt ein Testpostfach mit 1.000 Nachrichten, setzt nach einem Abbruch ohne Doppelte fort, zeigt die Ordner mit Zählern und erkennt eine Gelesen-Markierung, die außerhalb von PostKauz gesetzt wurde. Dauer: rund 15 Minuten. Du musst nichts programmieren: Kommando eingeben, Ausgabe mit „Erwartet" vergleichen, abhaken. Bei einer Abweichung: Kommando und Ausgabe an Claude geben, nichts reparieren.

**Es wird nur der lokale Testserver benutzt** (Docker auf deinem Rechner), nie ein echtes Postfach.

## Voraussetzungen

- Docker Desktop läuft.
- Go 1.27 ist installiert (wie für D0).
- PowerShell ist im Repository-Ordner geöffnet, der Stand ist aktuell (`git pull`).

Alle Kommandos sind für **PowerShell** geschrieben. `make` brauchst du nicht (unter Windows erzeugt `make build` die Datei ohne `.exe`; deshalb hier `go build`).

## 0. Vorbereitung

```powershell
$env:GOTOOLCHAIN = "local"
go version
docker compose -f deploy/testmail/docker-compose.yml up -d
docker compose -f deploy/testmail/docker-compose.yml up -d --force-recreate --renew-anon-volumes dovecot
go build -o bin\postkauz.exe .\cmd\postkauz
$env:POSTKAUZ_DATA_DIR = "$env:TEMP\postkauz-abnahme-d1"
.\bin\postkauz.exe version
```

Erwartet:

- `go version` zeigt `go version go1.27.` und eine Ziffer, dann `windows/amd64`.
- Die zweite `docker compose`-Zeile setzt nur Dovecot frisch auf (leeres Testpostfach); Stalwart und GreenMail bleiben unberührt. Wichtig ist `--renew-anon-volumes`: Das Dovecot-Image legt die Postfächer in einem eigenen, unbenannten Docker-Speicher ab; `--force-recreate` allein würde den alten Speicher samt Nachrichten wieder einhängen.
- `version` zeigt eine Zeile mit `postkauz`, Version und `go1.27`.

Erklärungen:

- `GOTOOLCHAIN = "local"` verhindert, dass Go still eine andere Version nachlädt (ADR-008).
- `POSTKAUZ_DATA_DIR` legt für die Abnahme eine **eigene Datenbank** an. Dein normales Datenverzeichnis bleibt unberührt. Die Einstellung gilt nur in diesem PowerShell-Fenster; öffnest du ein neues Fenster, die Zeilen `$env:…` wiederholen.
- **Passwortspeicher:** Unter Windows landet das Testpasswort in der Windows-Anmeldeinformationsverwaltung; dafür ist nichts zu tun. Gibt es **keinen Schlüsselbund des Betriebssystems** (z. B. in einem Linux-Container oder auf einem Server ohne Desktop), endet D1.1 mit `secret store unavailable`. Dann **nur für den Testserver** vorher `$env:POSTKAUZ_SECRETS_BACKEND = "insecure-file"` setzen (unter Linux: `export POSTKAUZ_SECRETS_BACKEND=insecure-file`). Das Passwort liegt dann unverschlüsselt im Abnahme-Datenverzeichnis; für echte Konten nie verwenden.
- **Passwort ohne Abfrage:** Statt es einzutippen, kann man es auch übergeben: `"pass" | .\bin\postkauz.exe account add demo --imap 127.0.0.1:11143 --user tester --insecure-test-server --password-stdin`. Das ist praktisch, wenn die verdeckte Abfrage im Terminal nicht funktioniert.

## D1.1 Konto anlegen

```powershell
.\bin\postkauz.exe account add demo --imap 127.0.0.1:11143 --user tester --insecure-test-server
```

Die Abfrage `Password for tester at 127.0.0.1:` erscheint; `pass` eingeben (es ist nichts zu sehen) und Enter.

Erwartet:

```text
account demo added (127.0.0.1:11143, tls none, user tester); password stored in the OS keyring as account/demo
```

Steht dort `account already exists`: Die Abnahme-Datenbank ist von einem früheren Versuch übrig. `Remove-Item -Recurse -Force $env:POSTKAUZ_DATA_DIR` und D1.1 wiederholen.

## D1.2 Verbindung prüfen

```powershell
.\bin\postkauz.exe account test demo
```

Erwartet (die Liste der Fähigkeiten ist lang; wichtig sind die markierten Stellen):

```text
account:       demo
login:         ok                          <- Anmeldung klappt
sync strategy: condstore                   <- Dovecot meldet Änderungen per CONDSTORE
capabilities:  BINARY CATENATE … CONDSTORE … IDLE IMAP4rev1 IMAP4rev2 … UTF8=ACCEPT WITHIN
mailboxes:
  Drafts  drafts
  INBOX   inbox
  Junk    junk
  Sent    sent
  Trash   trash
```

## D1.3 Testkorpus laden und erster Sync

Zuerst die 1.000 synthetischen Testnachrichten in den Testserver legen (das Werkzeug erzeugt sie selbst, `make corpus` ist nicht nötig; es arbeitet nur mit `127.0.0.1`/`localhost`):

```powershell
go run .\internal\corpus\cmd\corpusload -imap 127.0.0.1:11143 -user tester -password pass
```

Erwartet (Fortschrittszeilen wie `  800/1000` dürfen dazwischen stehen, die Sekunden variieren):

```text
Archiv/2024                386
Archiv/2025                373
Drafts                      20
INBOX                      221
1000 messages loaded in 3.9s
```

Steht dort `target mailbox is not empty: INBOX has … messages (already loaded?)`: Das Testpostfach ist nicht frisch. Dovecot mit `docker compose -f deploy/testmail/docker-compose.yml up -d --force-recreate --renew-anon-volumes dovecot` neu aufsetzen (ohne `--renew-anon-volumes` bleiben die alten Nachrichten erhalten), dann dieses Kommando noch einmal. Hast du in diesem Datenverzeichnis schon gesynct, vorher auch die Abnahme-Datenbank löschen und D1.1 wiederholen (siehe dort).

Jetzt der erste vollständige Sync:

```powershell
.\bin\postkauz.exe sync --full -a demo
```

Erwartet (Zeilen wie `  Archiv/2024: 200/386` erscheinen währenddessen; die Sekunden variieren):

```text
account demo (strategy condstore, full sync, 2.0s)
MAILBOX      ROLE     NEW  UPDATED  REMOVED  ERRORS  MIRRORED  TOTAL
INBOX        inbox    221  0        0        0       221       221
Archiv       archive  0    0        0        0       0         0
Archiv/2024  custom   386  0        0        0       386       386
Archiv/2025  custom   373  0        0        0       373       373
Drafts       drafts   20   0        0        0       20        20
Junk         junk     0    0        0        0       0         0
Sent         sent     0    0        0        0       0         0
Trash        trash    0    0        0        0       0         0
1000 messages mirrored
0 errors
```

Wichtig sind die letzten zwei Zeilen: **`1000 messages mirrored`** und **`0 errors`**. (`Archiv` ist der übergeordnete Ordner von `Archiv/2024` und `Archiv/2025` und selbst leer.)

## D1.4 Abbruch und Neustart

Damit der Abbruch einen **laufenden** ersten Sync trifft, brauchst du eine zweite, leere Datenbank (das Testpostfach bleibt dasselbe):

```powershell
$env:POSTKAUZ_DATA_DIR = "$env:TEMP\postkauz-abnahme-d1-abbruch"
.\bin\postkauz.exe account add demo --imap 127.0.0.1:11143 --user tester --insecure-test-server
```

(Passwort wieder `pass`; Ausgabe wie in D1.1.)

**a) Abbruch mit Strg+C.** `--window 1` holt die Nachrichten einzeln, damit du Zeit zum Abbrechen hast:

```powershell
.\bin\postkauz.exe sync --full -a demo --window 1
```

Sobald eine Zeile wie `  INBOX: 40/221` erscheint, **Strg+C** drücken. Erwartet:

```text
error: interrupted
hint: progress up to the last completed window is saved; run the same command again to resume
```

Endet die Ausgabe stattdessen schon mit `1000 messages mirrored`, war Strg+C zu spät. Das ist kein Fehler von PostKauz; nimm dann Variante b.

**b) Hart beenden (wie `kill -9`, ohne Aufräumen):**

```powershell
$p = Start-Process -FilePath .\bin\postkauz.exe -ArgumentList 'sync','--full','-a','demo','--window','1' -NoNewWindow -PassThru
Start-Sleep -Seconds 2
Stop-Process -Id $p.Id -Force
```

Erwartet: Die Fortschrittszeilen hören auf, es kommt keine Abschlussmeldung.

**c) Zwischenstand ansehen:**

```powershell
.\bin\postkauz.exe mailbox list -a demo
```

Erwartet: Mindestens ein Ordner ist noch nicht fertig, zum Beispiel `SYNC initial` mit `MIRRORED` kleiner als `TOTAL`, oder `SYNC new` mit `LAST SYNC never`.

**d) Neu starten:**

```powershell
.\bin\postkauz.exe sync --full -a demo
```

Erwartet: Die Tabelle zeigt nur noch die fehlenden Nachrichten als `NEW`, am Ende **dieselbe Zahl wie in D1.3, nicht mehr**:

```text
1000 messages mirrored
0 errors
```

**e) Gegenprobe:** Dasselbe Kommando noch einmal. Erwartet: In der Spalte `NEW` überall `0`, am Ende wieder `1000 messages mirrored` und `0 errors`.

## D1.5 Ordnerliste als JSON

```powershell
.\bin\postkauz.exe mailbox list -a demo --json
```

Erwartet: **eine** lange Zeile JSON, beginnend mit `{"account":"demo","mailboxes":[{"id":`…, darin je Ordner `"total"` (Zahl laut Server) und `"mirrored"` (lokal gespiegelt).

Lesbarer als Tabelle:

```powershell
.\bin\postkauz.exe mailbox list -a demo --json | ConvertFrom-Json | Select-Object -ExpandProperty mailboxes | Format-Table path, role, total, mirrored, unread, sync_phase
```

Erwartet (Spaltenbreiten können etwas abweichen):

```text
path        role    total mirrored unread sync_phase
----        ----    ----- -------- ------ ----------
INBOX       inbox     221      221    … ready
Archiv      archive     0        0    … ready
Archiv/2024 custom    386      386    … ready
Archiv/2025 custom    373      373    … ready
Drafts      drafts     20       20    … ready
Junk        junk        0        0    … ready
Sent        sent        0        0    … ready
Trash       trash       0        0    … ready
```

Prüfen: `INBOX` hat `total` 221 und `mirrored` 221; die Summe von `mirrored` ist 1000, die Zahl aus D1.3; überall `ready`. Die Spalte `unread` (ungelesen) hängt vom Zustand des Testpostfachs ab und wird nicht verglichen.

## D1.6 Gelesen-Markierung von außen

Statt eines Mailprogramms markiert das Korpus-Werkzeug die erste ungelesene Nachricht der INBOX auf dem Testserver als gelesen. Die erste Zeile führt es aus und merkt sich die Ausgabe, die zweite zeigt sie an, die dritte holt daraus die Message-ID (die Kennung der Nachricht) in `$mid`:

```powershell
$ms = go run .\internal\corpus\cmd\corpusload -imap 127.0.0.1:11143 -user tester -password pass -mark-seen
$ms
$mid = ($ms | Select-String '^message-id: (.+)$').Matches[0].Groups[1].Value
```

Erwartet (Ordner, Nummer und Message-ID können abweichen, je nachdem, was schon gelesen ist):

```text
marked as read: INBOX uid …
message-id: pk.….@corpus.example.org
```

Weiter unten ist mit `$mid` immer **die Message-ID gemeint, die `corpusload -mark-seen` gerade ausgegeben hat**. (Ohne die dritte Zeile: die Message-ID aus der Ausgabe kopieren und statt `$mid` einsetzen.)

PostKauz weiß davon noch nichts:

```powershell
.\bin\postkauz.exe show $mid --headers
```

Erwartet (Auszug; `show` nimmt die lokale Nummer oder, wie hier, die Message-ID; Nummer und Betreff hängen von der Nachricht ab):

```text
mailboxes:         INBOX (uid …)
message-id:        pk.….@corpus.example.org
subject:           …
flags:
…
--- headers ---
From: …
…
```

**Erwartung vor dem Sync:** Die Zeile `flags:` ist **leer** (hinter `flags:` steht nichts, auch nicht `seen`). (Sonderzeichen oder Symbole im Betreff zeigt die Konsole eventuell als `??`; das ist eine Frage der Schriftart, nicht der Daten.)

Jetzt ein normaler (inkrementeller) Sync:

```powershell
.\bin\postkauz.exe sync -a demo
```

Erwartet: In der Zeile `INBOX` steht unter `UPDATED` eine **1**, am Ende `1000 messages mirrored` und `0 errors`:

```text
account demo (strategy condstore, incremental sync, 0.0s)
MAILBOX      ROLE     NEW  UPDATED  REMOVED  ERRORS  MIRRORED  TOTAL
INBOX        inbox    0    1        0        0       221       221
…
1000 messages mirrored
0 errors
```

Und noch einmal anzeigen:

```powershell
.\bin\postkauz.exe show $mid --headers
```

**Erwartung nach dem Sync:** Die Zeile lautet jetzt **`flags:             seen`**.

## Aufräumen

```powershell
Remove-Item -Recurse -Force "$env:TEMP\postkauz-abnahme-d1", "$env:TEMP\postkauz-abnahme-d1-abbruch"
cmdkey /delete:postkauz:account/demo
Remove-Item Env:\POSTKAUZ_DATA_DIR
Remove-Item Env:\POSTKAUZ_SECRETS_BACKEND -ErrorAction SilentlyContinue
```

Die erste Zeile löscht die beiden Abnahme-Datenbanken (mit `insecure-file` auch das dort abgelegte Testpasswort), die zweite das Testpasswort `pass` aus der Windows-Anmeldeinformationsverwaltung (meldet sie „nicht gefunden", ist das bei `insecure-file` richtig). `account remove demo` würde hier mit Absicht verweigern: Das Konto hat archivierte Nachrichten, und das Archiv löscht PostKauz nie nebenbei (ADR-003).

## Rückmeldung an Claude

Je Punkt „ok" oder „Abweichung":

| Punkt | Inhalt | Ergebnis |
|---|---|---|
| D1.1 | Konto angelegt, Passwort im Schlüsselbund | |
| D1.2 | `login: ok`, Ordner mit Rollen | |
| D1.3 | `1000 messages mirrored`, `0 errors` | |
| D1.4 | nach Abbruch (Strg+C oder hart) wieder genau `1000 messages mirrored` | |
| D1.5 | JSON mit `total` je Ordner, INBOX 221 | |
| D1.6 | `show $mid --headers`: vor dem Sync leere `flags:`-Zeile, danach `flags: seen` | |

Bei einer Abweichung: das Kommando und die vollständige Ausgabe schicken. Danach entscheidest du: S1.5 angenommen, Nachbesserung (Liste) oder weiter mit S1.6.
