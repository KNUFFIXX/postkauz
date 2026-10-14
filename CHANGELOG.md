# Changelog

Format nach [Keep a Changelog](https://keepachangelog.com/de/1.1.0/); Versionierung nach [SemVer](https://semver.org/lang/de/) (bis 1.0 ohne Kompatibilitätszusage).

## [Unreleased]

### Added

- Repository-Gerüst: Apache-2.0-Lizenz, `NOTICE`, `.gitignore`, `.editorconfig`, `Makefile`, CI (vet, test, Cross-Compile), PII-Gate (`scripts/pii-check.sh`).
- Dokumentation (Deutsch) unter `docs/`: Architektur, Datenmodell, MCP, CLI, Sicherheit, Roadmap, Entscheidungen (ADR-001 bis ADR-006), Testkorpus; `README.md` (Englisch, mit deutscher Kurzfassung), `CLAUDE.md`, `AGENTS.md`, `CONTRIBUTING.md`, `SECURITY.md`.
- Go-Gerüst (kompiliert, getestet): Operationsregister und Executor mit Beispiel `accounts.list`, Policy-Stub (Scopes, Risiko R0–R3), Audit-Typ, Migration `0001_init.sql` (inkl. FTS5) auf `modernc.org/sqlite`, Provider-Interface (`MailProvider`, `Submission`), CLI (`version`, `doctor`, `accounts list`, `--json`, Exit-Codes).
- Testserver-Compose (`deploy/testmail`): Dovecot, Stalwart, GreenMail.
- Auftrag A01 (Go-Anhebung, Spike-Auflagen, Schnitt 1).
- Spike S0 unter `spike/s0/` als eigenständiges Go-Modul (Code, Testserver-Skripte, Korpus-Generator, Rohdaten, `REPORT.md`): **Gate Go bestanden mit Auflagen** (14.10.2026).
- ADR-007 (go-imap v2 beta.8 mit Auflagen: `internal/imapx`, Watchdog, WordDecoder, UID-SEARCH statt QRESYNC, `go-message`) und ADR-008 (Zielversion Go 1.27).

### Changed

- ADR-001 und ADR-005 an die Spike-Befunde angepasst (`modernc.org/sqlite`: v1.46.1 ist das Maximum für Go 1.24).
- CI: `GOTOOLCHAIN=local`, Go 1.27 ausdrücklich installiert.
- README, `docs/roadmap.md` (S0 erledigt, entschiedene Fragen), `docs/architektur.md`, `docs/datenmodell.md` (ß/ss-Normalisierer), `docs/sicherheit.md` (Rückfall-Abgleich), A01 (Schritt 0, Vorgaben, Prüfliste).
