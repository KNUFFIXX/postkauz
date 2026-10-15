# Mitwirken

PostKauz (Arbeitstitel) ist pre-alpha. Beiträge sind willkommen, aber die Architektur ist in [`docs/entscheidungen.md`](docs/entscheidungen.md) festgelegt; Änderungen daran beginnen mit einem Vorschlag als Issue, nicht mit einem Pull Request.

## Vorab

- Lies `docs/entscheidungen.md` und `docs/roadmap.md`. Was in „Nicht-Ziele" steht (README), wird nicht angenommen.
- Entwickle **nie gegen ein echtes Postfach**. Nutze die Testserver (`make testmail-up`, siehe `deploy/testmail/README.md`).
- **Keine Geheimnisse und keine personenbezogenen Daten** im Repository: keine Passwörter, Schlüssel, echten Namen, Adressen, Kontonummern oder Mailadressen Dritter. Testadressen nur aus `example.com`/`.org`/`.net` und `*.invalid`.

## Arbeitsweise

- Go 1.27 (ADR-008), `CGO_ENABLED=0`, `GOTOOLCHAIN=local` (das Makefile setzt beides; die Toolchain 1.27.x muss installiert sein). Neue Abhängigkeiten nur mit Begründung und mit Apache-2.0-kompatibler Lizenz.
- Kleine Commits und kleine Pull Requests. Je Operation ein Test; `make check` (gofmt, vet, test, Guards, PII-Gate) muss grün sein. Zusätzlich `make cross`.
- Commit-Nachrichten **Englisch** mit Präfix: `feat:`, `fix:`, `docs:`, `test:`, `chore:`. Sign-off per `git commit -s` (Developer Certificate of Origin); keine CLA.
- Kommentare im Code und Dokumentation unter `docs/` **Deutsch**; `README.md` Englisch. Programmausgaben und Fehlermeldungen Englisch.
- Doku und `CHANGELOG.md` gehören zum Beitrag; ein Verhalten ohne Test und ohne Doku ist nicht fertig.

## Sicherheitsprobleme

Nicht als öffentliches Issue melden, sondern wie in [`SECURITY.md`](SECURITY.md) beschrieben.
