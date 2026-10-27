# T1c — Stand (A03, Teil T1c)

Für: die Prüfsitzung, die T1c gegenprüft und nach `main` überträgt (Rahmen 10 in `auftraege/A03_teile.md`: Parallel laufende Teile schreiben Stand und CHANGELOG-Zeilen nur hierher; `docs/roadmap.md` und `CHANGELOG.md` bleiben unberührt).

- **Zweig:** `claude/compassionate-rubin-tsaqjd`, Ausgangsstand `main` `1c9847a` (27.10.2026). Keine Migration (Rahmen 4), Schema bleibt 16.
- **Build:** `0.3.0-alpha.c1` in `dist/` (nur für die Abnahme A3, nicht für den Dauerbetrieb).
- **Haltepunkt:** Build. Abnahme A3 (Messmatrix) führt die Prüfsitzung auf dem Büro-PC aus: [`docs/abnahme_A3.md`](../abnahme_A3.md), Skript [`docs/abnahme_A3.ps1`](../abnahme_A3.ps1).

## Erledigt

| Punkt | Inhalt | Nachweis |
|---|---|---|
| A3 | Neues Paket `internal/prompt`: Unter Windows liest die verdeckte Abfrage über `ReadConsoleW` (Echo aus, Zeilen-Eingabe ein, Strg+C bleibt, VT-Eingabe aus, UTF-16 → UTF-8, Rückschritt und Strg+Z, höchstens 4096 Zeichen), sonst `x/term`; Abbruch nach 120 s ohne Eingabe (`prompt_timeout`, Exit 1, Hinweis auf `--password-stdin`), Modus wird immer wiederhergestellt. `account add` und `account set --password-stdin` (am Terminal) nutzen ihn; `POSTKAUZ_PASSWORD_READER=term` schaltet zurück. Verstecktes Kommando `postkauz internal prompt-probe` (nicht in der Hilfe, nicht im Register, öffnet kein Datenverzeichnis): Art von stdin, Konsolenmodus vorher/während/nachher, Codepages, Windows Terminal ja/nein, Zähler, Zeilenende, SHA-256, nie der Wert. | `internal/prompt/console_test.go` (Fake-Konsolenstrom: ASCII, Umlaute und Surrogatpaar über mehrere Lesevorgänge, zeichenweise mit Rückschritt und CR, einzelnes Surrogat, ESC, Zeitgrenze, ctx, Fehler, Kanarientest), `internal/cli/a3_test.go`; manuell unter Linux mit Pseudo-Terminal (`script`): Hash eines Umlaut-Werts stimmt, Zeitgrenze greift |
| A5.2 | `[Google Mail]/…` kannte die Rollenheuristik schon (seit S1.4); Test für Entwürfe, Gesendet, Alle Nachrichten, Spam, Groß-/Kleinschreibung; `architektur.md` | `internal/provider/imap/t1c_test.go` |
| A5.3 | `sync` meldet `N skipped (larger than X bytes, [imap] max_message_bytes); …` als eigene Zeile (`--json`: `skipped_too_large`, `max_message_bytes`); `account add` und README nennen die Einstellung mit Beispiel (10 MiB) | `TestRenderSyncSkippedTooLarge`, `TestSyncDoesNotFetchMessageOverLimit` |
| A5.4 | `doctor --blobs` (1 %-Stichprobe, `--blobs=all`): Hash und Größe über `blob.Store.Verify`, Zeilen ohne Datei, Dateien ohne Zeile, Entwurfsanhänge eigens (A13.3), unbekannte und Temporärdateien; nie Inhalt; Probleme → Exit 1 `blob check failed` | `internal/cli/a54_test.go`, `internal/blob/scan_test.go`; Messung 50.000 Blobs: Stichprobe 0,6 s, alle 2,1 s |
| A5.5 | `make guard` verbietet `go list` mit dem Muster aus drei Punkten in Makefile, Skripten und Workflows (Go 1.28); Notiz in `CONTRIBUTING.md` | Guard von Hand gegen ein Testskript geprüft (schlägt an, Exit 2) |
| A5.6 | Roadmap-Einträge: siehe unten „Für die Roadmap“ (die Prüfsitzung überträgt) | — |
| A12.1 | golangci-lint v2.15.0 (`make lint`, `.golangci.yml`, CI-Job `lint` mit `continue-on-error`), Erstlauf eingestuft | [`lint_erstlauf.md`](lint_erstlauf.md): 88 Befunde, 16 echt (10 behoben, 6 offen), 44 Fehlalarm, 28 später |
| A12.2 | CodeQL für Go (`.github/workflows/codeql.yml`), `github/codeql-action` v4.39.1 per Commit-SHA gepinnt, `build-mode: manual` | läuft erst nach dem Push auf GitHub (ungeprüft) |
| A12.3 | `.githooks/pre-commit` ruft `make check`, `make hooks` setzt `core.hooksPath`; `CONTRIBUTING.md` | Hook direkt ausgeführt (grün) |
| Kleines | Ordnernamen und Serverfehler gefiltert ins Terminal (`error:`/`hint:`/Stack, `account test`, `--explain`) · Secret-Scan mit JWT-Muster (`secretscan.JWTPattern`) · Message-ID bei Dubletten eindeutig (Eingabefehler `ambiguous message_id` mit bis zu 20 IDs) · `ParseSince` mit Überlaufprüfung (höchstens 100.000 Tage) · Guard ohne `vendor/` | je ein Test (`t1c_*_test.go`), Guard von Hand mit `vendor/`-Datei geprüft |

## Festlegungen (Einspruch möglich)

1. **Leser unter Windows:** Der neue Leser (`ReadConsoleW`) ist ohne Messung Standard (A03 Risiko 7: „der neue Leser trotzdem drin“). Rückweg ohne neues Programm: Umgebungsvariable `POSTKAUZ_PASSWORD_READER=term`. Default, weil die Ursache offen ist und der neue Weg Zeilen-Eingabe und Einfügen der Konsole selbst überlässt.
2. **Zeitgrenze 120 s** auch unter Linux/macOS (x/term in einer Goroutine; nach Ablauf wird der Terminal-Zustand wiederhergestellt). Fehlercode `prompt_timeout`, Exit 1 (kein Eingabefehler).
3. **`prompt-probe`** liegt unter dem versteckten Kommando `internal` (cobra `Hidden`), schreibt kein Audit und öffnet kein Datenverzeichnis; der SHA-256 der Eingabe steht in der Ausgabe, damit das Skript bekannte Testwerte vergleichen kann (Testwerte sind keine Geheimnisse; mit einem echten Passwort nie benutzen, steht in `abnahme_A3.md`).
4. **Abnahme A3:** Skript `docs/abnahme_A3.ps1` misst je Fenster beide Leser (console = Abnahme, term = Messung des Befunds D5), fünf Schritte (tippen/Strg+V/Rechtsklick, ASCII/Umlaut); `-Launch` öffnet die Matrix (WT/conhost × PS 5.1/PS 7/über cmd.exe). Pflichtparameter `-DataDir`, `-Config`, `-Protocol`, `-HostLabel`; verweigert `%LOCALAPPDATA%\postkauz`, `%APPDATA%\postkauz` und `-LivePath`.
5. **Mehrdeutige Message-ID:** Fehler statt Auswahl (kein „bevorzugt die mit Rohmail“), weil ein Agent sonst still auf die falsche Fassung antwortet. Der CLI-Vertragstest zeigt dafür eine eindeutige Message-ID (`antwort@example.org`), weil sein eigener Import eine zweite Fassung von `html@example.org` anlegt.
6. **`ParseSince`:** Obergrenze 100.000 Tage (statt der rechnerischen Grenze von rund 106.751), runde Zahl mit Abstand.
7. **JWT-Muster:** zwei Base64url-Teile, die mit `eyJ` beginnen (je mindestens 11 Zeichen), Signatur darf fehlen. Fehlalarme sind möglich, aber selten (der Scan läuft nur über Log, Konfiguration, Tabellen ohne Mailinhalt und Audit-Exporte).
8. **Terminal-Filter für Fehler:** `termText` (Zeilenumbrüche bleiben) für Meldung und Hinweis, `termLine` für Stack-Zeilen und `--explain`.
9. **A5.3:** gezählt werden die in **diesem** Lauf übersprungenen Nachrichten (ein späterer Lauf fordert gemerkte nicht erneut an; `sync --full` prüft sie wieder und zählt sie dann erneut).
10. **golangci-lint ist GPL-3.0:** Es ist ein Werkzeug, das `go run` nur zum Prüfen baut; es steht nicht in `go.mod` und wird nicht mit PostKauz ausgeliefert. Die Lizenzregel (MIT/BSD/Apache) gilt für Abhängigkeiten des Programms; A12.1 verlangt das Werkzeug ausdrücklich.
11. **Lint-Pflicht-Job:** bleibt Berichtsmodus, bis die 6 offenen echten Befunde behoben sind (eigener Commit; Vorschlag: T1b oder T2 nimmt sie mit).
12. **`doctor --blobs`:** zusätzlich `unreadable` und `draft_attachments_without_file` als Probleme; `--blobs all` (mit Leerzeichen) ist ein Eingabefehler, nur `--blobs=all` gilt (cobra, optionaler Wert).

## Offen

- **Messmatrix A3** auf dem Büro-PC (Prüfsitzung, `abnahme_A3.md`); danach Protokoll `docs/protokolle/A3_prompt_windows.txt`, Eintrag in `docs/abnahme_D5.md` mit dem Ergebnis und ggf. Umstellung des Standards.
- 6 echte Lint-Befunde (`lint_erstlauf.md`, Abschnitt „echt, offen“), danach `lint` als Pflicht-Job.
- Erster CodeQL-Lauf nach dem Merge ansehen (Security → Code scanning).
- `actions/checkout` und `actions/setup-go` stehen weiter auf Tags, nicht auf SHAs (A12.2 verlangte das Pinnen nur für CodeQL).
- `CLAUDE.md` nennt `make lint` und `make hooks` noch nicht (bewusst nicht geändert; Vorschlag: eine Zeile wie bei `make vuln`).
- Vorbefund `TestIntegrationImportOverlapDovecot` (auf `main` rot, siehe „Prüfläufe“).
- `make vuln` im CI nach dem Push ansehen (im Container gesperrt).

## Prüfläufe (27.10.2026, Cloud-Container, go1.27.3)

| Lauf | Ergebnis |
|---|---|
| `make check` | grün auf dem Endstand; zusätzlich **jeder der 17 Code-Commits einzeln** mit `go vet ./...` und `go test -short ./...` in einem eigenen Worktree: alle ok |
| `make race` | grün (Exit 0) |
| `make cross` | grün (linux/amd64, windows/amd64, darwin/arm64) |
| `make integration` (Dovecot, Stalwart, GreenMail per Docker) | **1 FAIL, kein T1c-Befund:** `TestIntegrationImportOverlapDovecot` erwartet 305 neu / 173 vorhanden, bekommt 307 / 171. Auf `main` `1c9847a` (ohne T1c) zweimal gleich reproduziert. Vermutung: Erwartungswerte von vor rc6 (seit rc6 fasst der Import zwei Fassungen mit gleicher Message-ID nicht mehr zusammen, wenn der Inhalt abweicht); nicht weiter untersucht, weil T1c den Import nicht berührt. Alle übrigen 32 Integrationstests grün. **Für die Prüfsitzung:** vor dem Merge klären (Testerwartung anpassen oder Import-Befund). |
| `make vuln` | **nicht gelaufen:** Die Netzregel dieses Containers sperrt `vuln.go.dev` (Proxy 403). Läuft nach dem Push im CI-Job `vuln`; die Prüfsitzung prüft dort das Ergebnis. |
| `make lint` | 80 Befunde, eingestuft in `lint_erstlauf.md` (Berichtsmodus, kein Abbruchkriterium) |
| Linux-Pseudo-Terminal (`script`) | `internal prompt-probe` mit Leser `term`: Hash eines Umlaut-Werts stimmt mit `sha256sum` überein; `--timeout 1s` bricht ab, Terminal danach wieder normal |

**Build `0.3.0-alpha.c1`** (`make release VERSION=0.3.0-alpha.c1`, Code-Stand `e6f8be8`; danach nur dieser Doku-Nachtrag; `dist/` ist nicht versioniert, die Prüfsitzung baut nach und vergleicht):

```
0455c194443ac262b93e81220413e7ae6c1c118ccc545e49f4dcdea8a25d2d68  postkauz-linux-amd64
848139507b79b62420b5a5740fd77a64b15d2374e26f07929c02a0820d267b52  postkauz-windows-amd64.exe
4cdc62f09f7f44b4b54b3933d4fa18b65516b16fabb0c511adf97e2da04fb400  postkauz-darwin-arm64
```

## Für die Roadmap (A5.6; die Prüfsitzung überträgt nach `docs/roadmap.md`)

- **Stand T1c (27.10.2026):** A3 (Windows-Leser `ReadConsoleW`, Zeitgrenze, `internal prompt-probe`, Abnahme-Skript A3), A5.2 bis A5.5, A12.1 bis A12.3 und die kleinen Punkte aus dem Nachtrag 26.10.2026 (Terminal-Filter für Ordnernamen/Serverfehler, JWT-Muster, eindeutige Message-ID, `ParseSince`, Guard ohne `vendor/`) gebaut; Build `0.3.0-alpha.c1`; Messmatrix A3 offen (Prüfsitzung).
- **D5-Befunde erledigt:** `[Google Mail]`-Ordner (A5.2), großer Gesendet-Ordner (A5.3); Passwortabfrage unter Windows gebaut, Messung offen (A3).
- **Entschiedene Fragen:** Die Entscheide E1–E13 dieses Auftrags stehen schon unter 47 und 48; T1c fügt keinen neuen Entscheid hinzu, nur die Festlegungen oben.

## CHANGELOG (Unreleased; die Prüfsitzung überträgt)

### Added
- Hidden diagnostic command `postkauz internal prompt-probe` (`--reader auto|console|term`, `--timeout`, `--json`): reads one hidden line like `account add` and reports how it arrived (stdin type, console mode before/during/after, code pages, counts, line end, SHA-256), never the value.
- `doctor --blobs` verifies a 1% sample of the blob store (`--blobs=all`: every file) against the checksums and sizes in the database. It counts rows without file, files without row, unknown and temp files, and never shows content. Problems exit with code 1 (`blob check failed`). Draft file attachments, which have a blob file but no row in `blobs`, are counted separately (`draft_attachments`), not as orphans.
- `make lint`: golangci-lint v2.15.0 (gosec, staticcheck, errcheck, bodyclose, gocritic plus the standard set), configured in `.golangci.yml`; CI job `lint` in report mode (`continue-on-error`). The classified first run is in `docs/protokolle/lint_erstlauf.md` (88 findings: 16 real, 44 false alarms, 28 later).
- CodeQL for Go (`.github/workflows/codeql.yml`), pinned by commit SHA (v4.39.1), on push to main, pull requests and weekly.
- `.githooks/pre-commit` running `make check`, enabled with `make hooks` (see `CONTRIBUTING.md`).
- `doctor --secrets` also finds JSON Web Tokens (bearer/OAuth tokens) in log, configuration, tables and audit exports.
- `sync` reports messages skipped for `[imap] max_message_bytes` on a line of their own (`--json`: `skipped_too_large`, `max_message_bytes`); `account add` and the README name the setting with an example.
- Acceptance script `docs/abnahme_A3.ps1` (`.md`) for the hidden password prompt on Windows.

### Changed
- Windows: the hidden password prompt of `account add` and `account set --password-stdin` reads the console itself (`ReadConsoleW`, line input, echo off, UTF-16 to UTF-8); `POSTKAUZ_PASSWORD_READER=term` switches back to `golang.org/x/term`. All platforms give up after 120 s without input (`prompt_timeout`, exit 1).
- A Message-ID shared by several messages (`show <message-id>`, `mail_get_message`, replies and forwards by Message-ID) is an input error (`ambiguous message_id`) that lists up to 20 local ids, instead of silently taking the oldest.
- `make guard` skips `vendor/` and fails on `go list` with the three-dot pattern in scripts, Makefile and workflows (Go 1.28).

### Fixed
- Error messages, hints and stack lines on stderr, `account test` (folder names, capabilities) and the `--explain` error text are filtered for terminal control characters like mail content.
- `audit --since` with a day count that overflowed (for example `9223372036854775807d`) silently became a time in the future; such values are an input error now.
- Four unchecked errors in tests; raw invisible characters in test literals replaced with escapes (first lint run).
