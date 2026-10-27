# Mitwirken

PostKauz (Arbeitstitel) ist pre-alpha. Beiträge sind willkommen, aber die Architektur ist in [`docs/entscheidungen.md`](docs/entscheidungen.md) festgelegt; Änderungen daran beginnen mit einem Vorschlag als Issue, nicht mit einem Pull Request.

## Vorab

- Lies `docs/entscheidungen.md` und `docs/roadmap.md`. Was in „Nicht-Ziele" steht (README), wird nicht angenommen.
- Entwickle **nie gegen ein echtes Postfach**. Nutze die Testserver (`make testmail-up`, siehe `deploy/testmail/README.md`).
- **Keine Geheimnisse und keine personenbezogenen Daten** im Repository: keine Passwörter, Schlüssel, echten Namen, Adressen, Kontonummern oder Mailadressen Dritter. Testadressen nur aus `example.com`/`.org`/`.net` und `*.invalid`.

## Arbeitsweise

- Go 1.27 (ADR-008), `CGO_ENABLED=0`, `GOTOOLCHAIN=local` (das Makefile setzt beides; die Toolchain 1.27.x muss installiert sein). Neue Abhängigkeiten nur mit Begründung und mit Apache-2.0-kompatibler Lizenz.
- Go 1.28 (geplant Februar 2027) verbietet bei `go list` das Muster mit drei Punkten im Module-Modus. Skripte, Makefile und Workflows nutzen es deshalb nicht (`make guard` prüft das, A03 A5.5); für Pakete `go list -m` oder `go vet ./...`/`go test ./...` nehmen.
- Kleine Commits und kleine Pull Requests. Je Operation ein Test; `make check` (gofmt, vet, test, Guards, PII-Gate) muss grün sein. Zusätzlich `make cross`.
- Pre-Commit-Hook: `make hooks` schaltet für den eigenen Klon `.githooks/pre-commit` ein, der vor jedem Commit `make check` ausführt (braucht `sh` und `make`, unter Windows etwa Git Bash). Der Hook ist Komfort, die CI bleibt die Sperre. `make lint` (golangci-lint, Auswahl in `.golangci.yml`) und `make vuln` brauchen Netz und laufen deshalb nicht im Hook, sondern als eigene CI-Jobs; lint vorerst im Berichtsmodus, die Einstufung des Erstlaufs steht in [`docs/protokolle/lint_erstlauf.md`](docs/protokolle/lint_erstlauf.md).
- Commit-Nachrichten **Englisch** mit Präfix: `feat:`, `fix:`, `docs:`, `test:`, `chore:`. Sign-off per `git commit -s` (Developer Certificate of Origin); keine CLA.
- Kommentare im Code und Dokumentation unter `docs/` **Deutsch**; `README.md` Englisch. Programmausgaben und Fehlermeldungen Englisch.
- Doku und `CHANGELOG.md` gehören zum Beitrag; ein Verhalten ohne Test und ohne Doku ist nicht fertig.

## Sicherheitsprobleme

Nicht als öffentliches Issue melden, sondern wie in [`SECURITY.md`](SECURITY.md) beschrieben.
