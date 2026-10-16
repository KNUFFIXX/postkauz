# PostKauz

> **Arbeitstitel.** "PostKauz" is a working title; the name is being checked and will probably change. Names live in `internal/brand` (see `docs/architektur.md`, section 8).

An agent-first e-mail client written in Go. It mirrors an **existing** IMAP mailbox into a local, searchable archive (SQLite + raw `.eml` files) and exposes it through a CLI and an MCP server, with scopes, risk levels, dry runs, approvals and an append-only audit log. No GUI. The MVP is **draft-only**: it can write drafts into your Drafts folder, but it never sends mail; a human does that in their usual client.

**Status: pre-alpha, slice S1.9.** Accounts, the IMAP mirror (windowed first sync, incremental sync, `sync --watch` with IDLE), threads, cleaned message text (HTML to text, hidden text removed and reported, links made visible, quotes stripped), attachments (type check by magic bytes, text extraction in a subprocess), German-aware full-text search (word forms, umlaut and ß spellings, parts of compound words, operators, cursor paging), API keys with roles and scopes, and an audit log you can query, export with checksums and verify work. A read-only stdio MCP server (`postkauz mcp`) serves agents such as Claude Code (S1.9). The name "PostKauz" is a working title and will still change.

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
./bin/postkauz search "Rechnung" -a demo --limit 5          # finds "Rechnungen", "Abrechnung"; "prüfen" = "pruefen"
./bin/postkauz search 'from:anna has:attachment after:2026-01-01' --json   # NDJSON, one line per hit
./bin/postkauz search '"Erdgeschoss links" -Keller' --explain               # shows how the query was translated
./bin/postkauz key create --name assistant-1 --role reader  # API key for an agent; shown once, only its hash is stored
./bin/postkauz policy show --key assistant-1                # what that key may do
./bin/postkauz audit --since 1h                             # who called what, never message content
./bin/postkauz audit export --to /mnt/backup/audit && ./bin/postkauz audit verify
```

Without an OS keyring (servers, containers) set `POSTKAUZ_PASSWORD_<NAME>` and use `account add --password-from-env`. Configuration and paths: `docs/cli.md`, section 5; query language: `docs/cli.md`, section 2d. `sync` also extracts attachment text and builds the search index; `search` reads the local mirror only.

### Optional: text from PDF attachments

PostKauz has no PDF parser of its own (attack surface). If `pdftotext` from Poppler is on the `PATH`, it is used in a subprocess with time and size limits; without it PDF attachments are skipped with a reason and their text is not searchable (file names still are). `postkauz doctor` shows whether it was found (line `pdftotext`). Everything else is built in.

| System | Install |
|---|---|
| Debian, Ubuntu | `sudo apt install poppler-utils` |
| Fedora | `sudo dnf install poppler-utils` |
| macOS (Homebrew) | `brew install poppler` |
| Windows | install Poppler for Windows (for example `scoop install poppler`) and make sure its `bin` folder with `pdftotext.exe` is on the `PATH` |

After installing, the next `sync` also extracts the PDFs that were skipped because the tool was missing, and adds their text to the search index.

## Set up MCP in Claude Code

`postkauz mcp` is a read-only MCP server over stdio. An agent gets its own API key; the tools it sees follow the key's scopes, every call lands in the audit log under the key's name, and every piece of mail content comes wrapped as untrusted data. Nothing can be sent, deleted, moved or drafted.

**1. Create a key** (shown exactly once; only its hash is stored):

```sh
postkauz key create --name claude-code --role reader --expires 30d
```

`reader` may read and search mail and the audit log. For a triage agent without message bodies: `--role reader --scopes mail:search`. Check what a key may do with `postkauz policy show --key claude-code`.

**2. Register the server.** The key goes into the environment variable `POSTKAUZ_KEY`, never on the command line of the server. With the Claude Code CLI (all options before the server name `postkauz`, the program after `--`):

```sh
claude mcp add --env POSTKAUZ_KEY=pk_key_... --scope user --transport stdio postkauz -- /usr/local/bin/postkauz mcp
```

On Windows (PowerShell), with the full path to the program:

```powershell
claude mcp add --env POSTKAUZ_KEY=pk_key_... --scope user --transport stdio postkauz -- C:\PostKauz\postkauz.exe mcp
```

Or as JSON, for example in a project's `.mcp.json` (keep that file out of version control, it contains the key):

```json
{
  "mcpServers": {
    "postkauz": {
      "command": "C:\\PostKauz\\postkauz.exe",
      "args": ["mcp"],
      "env": { "POSTKAUZ_KEY": "pk_key_..." }
    }
  }
}
```

The Claude desktop app uses the same `mcpServers` block in `claude_desktop_config.json` (Windows: `%APPDATA%\Claude\claude_desktop_config.json`, macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`; open it via Settings, Developer, Edit Config) and needs a restart afterwards.

Optional entries in `env`: `POSTKAUZ_DATA_DIR` to use another data directory (for example a test database), `POSTKAUZ_KEY_FILE` instead of `POSTKAUZ_KEY` (path to a file holding the key; on Linux and macOS it must be readable only by you).

**3. Check it.** In Claude Code, `/mcp` lists the server and its tools: `mail_search`, `mail_get_message`, `mail_get_thread`, `mail_list_accounts`, `mail_list_mailboxes`, `mail_list_attachments`, `mail_get_attachment_text`, `mail_audit_log`, `mail_get_policy`. Ask something like "Search my mail for invoices and summarise them"; afterwards `postkauz audit --since 10m --key-id key:claude-code` shows each call with the key name and without any mail text.

The server only reads the local mirror; keep it current with `postkauz sync` (or `sync --watch`). Revoke a key you no longer need: `postkauz key revoke claude-code`. Note that the client configuration stores the key in plain text in your user profile; use a short expiry and the narrowest role that works. Details: `docs/mcp.md`, `docs/sicherheit.md`.

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

Stand: Pre-Alpha, Schnitt S1.9 (Konten, Spiegel mit Sync und IDLE, Threads, bereinigter Text, Anhänge mit Typprüfung und Textextraktion, Volltextsuche mit deutschen Wortformen, Umlaut-/ß-Schreibweisen und Teilwörtern, API-Schlüssel, Audit mit Export und Prüfung, lesender MCP-Server über stdio; Einrichtung in Claude Code siehe „Set up MCP in Claude Code"). Text aus PDF-Anhängen nur mit dem optionalen `pdftotext` (siehe oben). Die Dokumentation unter `docs/` ist deutsch; Einstieg: `docs/architektur.md` und `docs/entscheidungen.md`. Entwicklungsregeln für KI-Sitzungen: `CLAUDE.md`.

Nicht-Ziele: keine GUI vor 1.0, kein Massenversand, keine Agenten-Postfächer, keine eigene Google-OAuth-App, kein Spamfilter, kein PGP/S/MIME im MVP.
