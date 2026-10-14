# Changelog

Format nach [Keep a Changelog](https://keepachangelog.com/de/1.1.0/); Versionierung nach [SemVer](https://semver.org/lang/de/) (bis 1.0 ohne Kompatibilitätszusage).

## [Unreleased]

### Added

- Repository-Gerüst: Apache-2.0-Lizenz, `NOTICE`, `.gitignore`, `.editorconfig`, `Makefile`, CI (vet, test, Cross-Compile), PII-Gate (`scripts/pii-check.sh`).
- Dokumentation (Deutsch) unter `docs/`: Architektur, Datenmodell, MCP, CLI, Sicherheit, Roadmap, Entscheidungen (ADR-001 bis ADR-006), Testkorpus; `README.md` (Englisch, mit deutscher Kurzfassung), `CLAUDE.md`, `AGENTS.md`, `CONTRIBUTING.md`, `SECURITY.md`.
- Go-Gerüst (kompiliert, getestet): Operationsregister und Executor mit Beispiel `accounts.list`, Policy-Stub (Scopes, Risiko R0–R3), Audit-Typ, Migration `0001_init.sql` (inkl. FTS5) auf `modernc.org/sqlite`, Provider-Interface (`MailProvider`, `Submission`), CLI (`version`, `doctor`, `accounts list`, `--json`, Exit-Codes).
- Testserver-Compose (`deploy/testmail`): Dovecot, Stalwart, GreenMail.
- Auftrag A01 (Phase 0: Spike, Gate, Schnitt 1).
