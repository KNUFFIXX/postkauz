# Messung: Import-Durchsatz (S1.10)

Stand: 16.10.2026. Gemessen wird `import mbox` mit einer synthetischen Thunderbird-mbox, die dem Befund des Betreibers zum Gmail-Konto nachgebildet ist (15.10.2026: 20,7 GB, 22.065 Mails, im Mittel 0,94 MB je Mail, 59 % mit Anhang). Ziel ist kein fester Wert, sondern eine belastbare Hochrechnung und der Nachweis, dass große Dateien gestreamt werden (Speicher flach).

## Ergebnis

| Lauf (1.055 MB, 1.051 Nachrichten, 1,00 MB je Mail) | Dauer | Durchsatz | Heap (Spitze, Zuwachs) |
|---|---|---|---|
| Trockenlauf (`--dry-run`: lesen, Message-ID und `content_hash` abgleichen) | 8,1 s | 130 MB/s | 3,8 MB (+3,1) |
| Import in ein leeres Konto (ohne Jobs) | 45,8 s | **23 MB/s** (23 Nachrichten/s) | 6,0 MB (+5,3) |
| zweiter Lauf mit `--rescan` (alles vorhanden, nur Message-ID) | 1,0 s | 1.054 MB/s | 3,7 MB (+2,8) |
| Jobs danach (Suchindex 1.051 Nachrichten; Anhänge hier ohne Text) | 2,8 s | – | – |
| 210 MB mit einem 30-MB-Anhang (`TestImportLargeFileStreams`, in `make check`) | 8,9 s | 24 MB/s | 5,1 MB (+4,4) |
| über die CLI (`postkauz import mbox`, 1.055 MB): Trockenlauf | 9,8 s | 108 MB/s | – |
| über die CLI: Import samt Suchindex (Ende-zu-Ende, neuer Prozess) | 48,2 s | 21,9 MB/s | – |
| über die CLI: Standardkorpus als Thunderbird-Profil (2,3 MB, 1.108 Kopien in 6 Dateien: `new: 1005, existing: 100, skipped: 3`) samt Textextraktion (PDF, DOCX) und Suchindex | 8,6 s | 117 Nachrichten/s | – |
| über die CLI: zweiter Lauf (alle Quellen `unchanged`) | 0,09 s | – | – |

**Hochrechnung Gmail-Archiv (20,7 GB):** bei 23 MB/s rund 15 Minuten für den ersten Import, danach Suchindex (rund 1.000 Nachrichten/s, [suche_messung.md](suche_messung.md)) und Textextraktion (60 s je Lauf, Rest in den folgenden Läufen). Ein zweiter Lauf über unveränderte Dateien liest nichts (`unchanged`); mit `--rescan` rund 20 s je 20 GB, solange die Message-IDs passen. Der Trockenlauf liest die ganze Datei: rund 3 Minuten.

**Speicher:** Der Heap wächst beim Import um rund 5 MB, unabhängig von der Dateigröße (200 MB und 1 GB gleich): Im Speicher liegen höchstens ein Kopf (bis 1 MiB), eine Zeile (bis 256 KiB je Stück) und die Metadaten eines Stapels (200 Nachrichten bzw. 32 MiB Inhalt in Dateien). Inhalt und Anhänge gehen direkt in den Blob-Store.

**Woher die Zeit kommt** (CPU-Profil, 210-MB-Lauf): SHA-256 rund 27 % (Rohmail im Blob-Store, `content_hash`, jeder Anhang einzeln), base64-Dekodierung und MIME-Parser rund 25 %, Schreiben der Dateien rund 12 %, Bereinigung und Zitate rund 7 %. Der Import arbeitet eine Nachricht nach der anderen auf einem Kern; der Trockenlauf zeigt, dass Lesen und Abgleichen nicht der Engpass sind. Möglich, wenn es im Alltag stört: Aufbereitung mehrerer Nachrichten nebeneinander (offene Frage 8 in [roadmap.md](roadmap.md)).

## Umgebung

- Linux-Container, 2 Kerne (Intel Xeon 2,8 GHz, **ohne** SHA-Befehlssatz), 8 GB RAM, SSD; Go 1.27.2, `modernc.org/sqlite` v1.60.1, `CGO_ENABLED=0`.
- Gleichzeitig keine andere Last. Die Werte schwanken zwischen Läufen um rund ±15 %.
- Ein Rechner mit SHA-Befehlssatz (alle aktuellen AMD- und Intel-Desktop-Prozessoren) hasht etwa dreimal so schnell; auf dem Windows-PC des Betreibers ist deshalb mehr zu erwarten.

## Testdaten

`internal/importer/large_test.go` schreibt eine mbox im Thunderbird-Format (Trennzeile `From - …`, `X-Mozilla-Status`), Zeilenenden CRLF: 41 % Textmails um 12 KB, 59 % mit einem Anhang aus Zufallsbytes (nicht komprimierbar, base64) von 80 KB, 500 KB, 2 MB oder 5 MB (35/30/25/10 %), im Mittel rund 1,0 MB je Mail.

## Nachmessen

```sh
# 1 GB synthetisch (Testdaten und Datenbank in einem Ordner mit genug Platz, etwa dreifache Größe)
POSTKAUZ_IMPORT_MEASURE=1000 POSTKAUZ_IMPORT_DIR=/pfad/mit/platz \
  go test ./internal/importer -run TestImportThroughput -v -count=1 -timeout 60m
```

Auf Windows (PowerShell, im Repository):

```powershell
$env:POSTKAUZ_IMPORT_MEASURE = "1000"; $env:POSTKAUZ_IMPORT_DIR = "D:\pk-messung"
go test ./internal/importer -run TestImportThroughput -v -count=1 -timeout 60m
```

Mit dem echten Thunderbird-Profil misst der Trockenlauf ohne zu schreiben (zeigt Dauer und MB/s in der ersten Zeile der Ausgabe). Das Konto muss im Profil angelegt sein; ein Passwort braucht der Import nicht (`account add gmail --imap imap.gmail.com --user <adresse> --password-from-env`):

```powershell
postkauz --profile messung import mbox "$env:APPDATA\Thunderbird\Profiles\<profil>\ImapMail\imap.gmail.com\[Gmail].sbd\Alle Nachrichten" -a gmail --dry-run
```
