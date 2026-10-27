# T1a — Prüfläufe (27.10.2026)

Umgebung: Cloud-Container Linux, 4 Kerne, `go version go1.27.3 linux/amd64` (nach der Ausnahme in `CLAUDE.md` über `GOTOOLCHAIN=go1.27.3` geholt, danach `GOTOOLCHAIN=local`). Zweig `claude/intelligent-fermat-4gmewu`, Ausgangsstand `main` `1c9847a`. Go-Code für alle Läufe außer `make check` vor den einzelnen Commits: Stand `bc7bb93` (danach nur Doku, Protokoll und der standardmäßig übersprungene Messtest).

| Befehl | Ergebnis | Laufzeit |
|---|---|---|
| `make check` | vor jedem Commit exit 0 (gofmt, vet, alle Pakete, guard, pii-check); Basis vor Beginn ebenfalls exit 0 | je rund 3–4 min |
| `make race` | exit 0: 29 Pakete ok, kein Datenrennen (längste: importer 843 s, ops 785 s, mailsync 691 s, cli 649 s) | 991 s |
| `make integration` | 8 Integrationstests mit 24 Teilfällen PASS gegen Dovecot 2.4.5, Stalwart 0.11.8, GreenMail 2.1.14 (Sync, IDLE, DraftPush, DraftPushRefusals, DraftPushWatchAndOffline, SentDetection, CapabilitiesAndRoles, WrongPassword); **1 FAIL vorbestehend:** `TestIntegrationImportOverlapDovecot` (siehe unten) | rund 4 min |
| `make vuln` | **nicht gelaufen:** Die Netzwerk-Richtlinie dieser Cloud-Umgebung weist `vuln.go.dev` ab (Proxy 403). Nachholen in der Prüfsitzung bzw. im CI-Job `vuln` (läuft bei Pull Requests und auf `main`). | – |
| `make cross` | exit 0: linux/amd64 26,4 MB, windows/amd64 26,3 MB, darwin/arm64 24,9 MB | < 1 min |

**Docker:** Der Daemon wurde von Hand gestartet (`dockerd`). Docker Hub lieferte beim Pull `429 Too Many Requests`; die drei Images kamen unverändert (gleiche Tags) über den öffentlichen Spiegel `mirror.gcr.io` und wurden lokal auf die Namen aus `deploy/testmail/docker-compose.yml` getaggt.

**Vorbestehender Fehlschlag, nicht T1a:** `TestIntegrationImportOverlapDovecot` meldet `new 307, existing 171, skipped 3` statt `305/173/3`. Derselbe Test am Ausgangsstand `1c9847a` (eigener Worktree, gleiche Testserver) liefert dasselbe Ergebnis. Bei S2.9 (23.10.2026) war er grün (`docs/protokolle/S29_pruefungen.txt`); die Abweichung stammt also aus den Import-Änderungen rc6 bis rc8 (zwei Fassungen mit gleicher Message-ID werden getrennt). Zwei Nachrichten des Thunderbird-Exports gelten jetzt als neue Fassung statt als vorhanden. Ob die Erwartung des Tests oder der Import angepasst werden muss, entscheidet die Prüfsitzung (Vorschlag: Ursache an den zwei Nachrichten prüfen; liegt sie in der Umkodierung durch Dovecot, die Erwartung des Tests anpassen).

## Laufzeit Migration 0017 (Rahmen 4)

`POSTKAUZ_MEASURE_MIGRATION=1 go test ./internal/store -run TestMeasureMigration0017 -v`: synthetische Datenbank mit Schema 16, 50.000 Nachrichten, 80.000 Ordnerkopien (alle in „Alle Nachrichten“, 30.000 zusätzlich in INBOX), 20.000 Audit-Zeilen, Datei 28 MB.

| Lauf | Dauer (Snapshot `VACUUM INTO` + Vorprüfung + Tabellenumbau) |
|---|---|
| 1 | 426 ms |
| 2 | 403 ms |
| 3 | 454 ms |

Zeilen vorher und nachher gleich (Test prüft das). Die echte Datenbank des Betreibers (rund 1,4 GB, mehr als 50.000 Nachrichten) ist vor allem wegen Volltextindex und Texten größer; der Tabellenumbau wächst mit der Zahl der Ordnerkopien, der Snapshot mit der Dateigröße. Die Dauer an der Kopie der echten Datenbank misst die Prüfsitzung in Abnahme T1, Schritt 2.
