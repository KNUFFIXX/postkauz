# PostKauz

> **Arbeitstitel.** "PostKauz" is a working title; the name is being checked and will probably change. Names live in `internal/brand` (see `docs/architektur.md`, section 8).

An agent-first e-mail client written in Go. It mirrors an **existing** IMAP mailbox into a local, searchable archive (SQLite + raw `.eml` files) and exposes it through a CLI and an MCP server, with scopes, risk levels, dry runs, approvals and an append-only audit log. No GUI. The MVP is **draft-only**: it can write drafts into your Drafts folder, but it never sends mail; a human does that in their usual client.

**Status: pre-alpha, slice S1.6.** Accounts, the IMAP mirror (windowed first sync, incremental sync, `sync --watch` with IDLE), threads, cleaned message text (HTML to text, hidden text removed and reported, links made visible, quotes stripped) and attachments (type check by magic bytes, text extraction in a subprocess) work. There is no search and no MCP server yet. The name "PostKauz" is a working title and will still change.

## Who is it for

- People and small organisations who let AI agents (Claude Code, Cowork, Cursor, Codex CLI) help with their mail and want that access to be **controlled and auditable**, instead of handing an agent a full mailbox token.
- Self-hosters and script users who want a provider-neutral, local mail mirror with a JSON CLI.
- Privacy-conscious users who keep mail on their own machine and only pass excerpts to a model.

## Non-goals

- No GUI before 1.0 (a minimal web UI may follow, on top of a REST API).
- No bulk mail or newsletter sending.
- No agent-owned mailboxes (for that, see OpenAgentEmail and similar projects).
- No own verified Google OAuth app ("bring your own client" instead).
- No spam filter.
- No PGP/S/MIME in the MVP.

## Architecture in 10 lines

1. CLI, MCP (and later REST) are thin, hand-written adapters over one **operation registry** (`internal/ops`).
2. Every call runs the same chain: scope, policy, approval, audit, handler. Interfaces cannot bypass it.
3. Operations are data: name, JSON schemas, scope, risk level R0-R3, idempotency, handler.
4. **Two truths:** the server is the truth for the current mailbox state; the local database is the truth for archive, index, approvals and audit.
5. A provider interface with JMAP semantics sits in front of IMAP (CONDSTORE/QRESYNC where available, IDLE on INBOX).
6. Raw mails live in a content-addressed blob store, metadata and full-text index (FTS5) in SQLite.
7. SQLite via `modernc.org/sqlite`: pure Go, no CGO, cross-compiles to Windows, macOS and Linux.
8. German-aware search: unicode61 + trigram tokenizers plus an application-side German stemmer.
9. Jobs (sync, index, extract, import) run from a table, no broker.
10. Details: `docs/architektur.md`, `docs/datenmodell.md` (documentation is in German).

## Quick start

Works today:

```sh
make build                       # builds bin/postkauz (and the alias bin/pk)
./bin/postkauz version --json
./bin/postkauz doctor            # paths, keyring, database, schema version
./bin/postkauz account add demo --imap imap.example.org:993 --user demo@example.org   # password prompt; stored in the OS keyring
./bin/postkauz account test demo # login, capabilities, mailboxes with roles (read-only)
./bin/postkauz sync -a demo      # mirror the mailbox (resumable); --watch keeps running with IDLE
./bin/postkauz mailbox list -a demo
./bin/postkauz show 42           # cleaned text, links with warnings, "hidden text removed" when the HTML hid text
./bin/postkauz thread --message 42 --strip-quotes
./bin/postkauz attach list 42    # detected type, type mismatch, executable, text extraction status
./bin/postkauz attach text 7
```

Without an OS keyring (servers, containers) set `POSTKAUZ_PASSWORD_<NAME>` and use `account add --password-from-env`. Configuration and paths: `docs/cli.md`, section 5. Text from PDF attachments needs `pdftotext` (Poppler) on the `PATH`; without it PDFs are skipped with a reason. Everything else is built in.

Planned (not implemented yet):

```sh
postkauz search "from:anna has:attachment" --json
postkauz mcp        # stdio MCP server for Claude Code and other clients
```

Test servers (Dovecot, Stalwart, GreenMail) for development: `make testmail-up`. Never develop against a real mailbox.

## Security model in 8 lines

1. Every agent gets its own API key with **scopes** (`mail:read`, `mail:search`, `mail:draft`, ...). Tools the key may not use are not even listed.
2. Operations have **risk levels** R0 (read) to R3 (external or hard to undo); `dry_run` is available on every write.
3. The MVP has **no send capability at all**; agents can only prepare drafts, and drafts need human approval before they count as "ready".
4. Agents can move mail to the trash at most; there is no expunge in the provider interface.
5. Every mail content that reaches an agent is wrapped as **untrusted data**; HTML is reduced to text, links are listed separately, nothing is loaded remotely.
6. Credentials live in the OS keyring or the environment, never in the repository, logs or audit.
7. The audit log is append-only and exported daily with a checksum to a second location.
8. Residual risk (prompt injection, exfiltration via other tools) is documented honestly in `docs/sicherheit.md`.

## Roadmap

- **Phase 0:** scaffold, ADRs, test servers, spike with `go-imap/v2`, go/no-go gate.
- **S1:** accounts, sync, mirror, threads, full-text search, read-only CLI and stdio MCP, audit, mbox/Maildir import.
- **S2:** drafts (draft-only), policy file, approval states, pre-ready check hook.
- **S3:** tags and mailbox actions, export/backup/restore, HTTP MCP with OAuth 2.1.
- Later: SMTP sending with approval, rules, REST, JMAP. Details and acceptance criteria: `docs/roadmap.md`.

## Contributing

See `CONTRIBUTING.md`. Short version: Go 1.27 with `GOTOOLCHAIN=local` (ADR-008), small commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`), `make check` before every commit, no secrets and no personal data in the repository, never test against a real mailbox. Security issues: `SECURITY.md`.

## License

Apache-2.0, see `LICENSE` and `NOTICE`.

---

## Kurzfassung (Deutsch)

PostKauz (Arbeitstitel) ist ein E-Mail-Client für KI-Agenten, ohne Oberfläche: Er spiegelt ein vorhandenes IMAP-Postfach lokal (SQLite plus `.eml`-Dateien), macht es durchsuchbar und bietet es per Kommandozeile und MCP an. Jeder Agent bekommt einen Schlüssel mit begrenzten Rechten; Aktionen sind nach Risiko gestuft, lassen sich im Trockenlauf prüfen und stehen in einem Audit-Log.

Der MVP ist **draft-only**: Er legt Entwürfe in den Entwürfe-Ordner, sendet aber nie selbst. Gesendet wird vom Menschen im gewohnten Mailclient.

Stand: Pre-Alpha, Phase 0 (Gerüst, Architektur, Entscheidungen; Spike bestanden mit Auflagen, siehe `spike/s0/REPORT.md`). Die Dokumentation unter `docs/` ist deutsch; Einstieg: `docs/architektur.md` und `docs/entscheidungen.md`. Entwicklungsregeln für KI-Sitzungen: `CLAUDE.md`.

Nicht-Ziele: keine GUI vor 1.0, kein Massenversand, keine Agenten-Postfächer, keine eigene Google-OAuth-App, kein Spamfilter, kein PGP/S/MIME im MVP.
