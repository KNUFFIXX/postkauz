# PostKauz

> **Name.** "PostKauz" started as a working title; it was checked and stays (decision of 15.10.2026). Names live in one place, `internal/brand` (see `docs/architektur.md`, section 8).

An agent-first e-mail client written in Go. It mirrors an **existing** IMAP mailbox into a local, searchable archive (SQLite + raw `.eml` files) and exposes it through a CLI and an MCP server, with scopes, risk levels, dry runs, approvals and an append-only audit log. No GUI. The MVP is **draft-only**: it can write drafts into your Drafts folder, but it never sends mail; a human does that in their usual client.

**Status: 0.2.0-rc1 (release candidate, pre-alpha): slice 2, drafts, is built; its acceptance with real mail clients (D5) is pending. Last published release: 0.1.0-rc2 (slice 1).** Slice 2 adds [drafts](#drafts): replies and new mails written by you or an agent, put into your Drafts folder, an approval step that only a human can complete, a check hook, and detection of the mail you then sent yourself. Slice 1: accounts, the IMAP mirror (windowed first sync, incremental sync, `sync --watch` with IDLE), threads, cleaned message text (HTML to text, hidden text removed and reported, links made visible, quotes stripped), attachments (type check by magic bytes, text extraction in a subprocess), German-aware full-text search (word forms, umlaut and ß spellings, parts of compound words, operators, cursor paging), import from mbox and Maildir (Thunderbird profiles), API keys with roles and scopes, a stdio MCP server for agents such as Claude Code (read-only keys, or drafts with the role `assistant`), and an append-only audit log with a daily export (SHA-256 hash chain, manifest) that `audit verify` checks.

## Who is it for

- People and small organisations who let AI agents (Claude Code, Cowork, Cursor, Codex CLI) help with their mail and want that access to be **controlled and auditable**, instead of handing an agent a full mailbox token.
- Self-hosters and script users who want a provider-neutral, local mail mirror with a JSON CLI.
- Privacy-conscious users who keep mail on their own machine and only pass excerpts to a model.

## Non-goals

- No GUI before 1.0 (a minimal web UI may follow, on top of a REST API).
- No bulk mail or newsletter sending.
- No agent-owned mailboxes (for that, see OpenAgentEmail and similar projects).
- No archive for chat, calendars or meetings, and no web UI before 1.0 (for that, see msgvault).
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
9. Jobs (index, extract, draft push) run from a table, no broker; the only write to the server (a draft into the Drafts folder) happens in `sync`, the process that holds the mailbox password, never in the MCP server.
10. Details: `docs/architektur.md`, `docs/datenmodell.md` (documentation is in German).

## Install in under 15 minutes

You need one file: the program for your system. Nothing else has to be installed (no Go, no Docker, no database server). PDF text extraction is optional (see below).

### 1. Download and check the program

Take the file for your system from the release page (`https://github.com/KNUFFIXX/postkauz/releases`) or from `dist/` after `make release VERSION=…`, together with `SHA256SUMS`:

| System | File | Rename to |
|---|---|---|
| Windows 10/11 (64-bit) | `postkauz-windows-amd64.exe` | `postkauz.exe` |
| Linux (64-bit) | `postkauz-linux-amd64` | `postkauz` |
| macOS (Apple silicon) | `postkauz-darwin-arm64` | `postkauz` |

Compare the checksum with the line in `SHA256SUMS`:

```powershell
# Windows (PowerShell): put the file into C:\PostKauz first
cd C:\PostKauz
Get-FileHash .\postkauz-windows-amd64.exe -Algorithm SHA256
Rename-Item .\postkauz-windows-amd64.exe postkauz.exe
Unblock-File .\postkauz.exe          # removes the "downloaded from the internet" mark (SmartScreen)
```

```sh
# Linux
sha256sum postkauz-linux-amd64 && install -Dm755 postkauz-linux-amd64 ~/.local/bin/postkauz
# macOS
shasum -a 256 postkauz-darwin-arm64 && mkdir -p ~/bin && install -m755 postkauz-darwin-arm64 ~/bin/postkauz
xattr -d com.apple.quarantine ~/bin/postkauz   # unsigned program: allow it once (Gatekeeper)
```

Optional on Windows: add the folder to your user `PATH`, then `postkauz` works in every window:

```powershell
$p = [Environment]::GetEnvironmentVariable("Path", "User"); [Environment]::SetEnvironmentVariable("Path", "$p;C:\PostKauz", "User")
```

(Open a new PowerShell window afterwards. Without it, call `C:\PostKauz\postkauz.exe` or `.\postkauz.exe` in that folder; the examples below write `postkauz`.)

### 2. Check the installation

```sh
postkauz version
postkauz doctor
```

`doctor` shows the configuration file, the data directory (Windows `%LOCALAPPDATA%\postkauz`, Linux `~/.local/share/postkauz`, macOS `~/Library/Application Support/postkauz`), the database, the keyring and the optional `pdftotext`. Expected on a fresh install: `database: missing` (it is created by the first real command) and `keyring: available (Windows Credential Manager)`, `(macOS Keychain)` or `(Secret Service (D-Bus))`. Keep the data directory out of OneDrive, Dropbox or iCloud; `doctor` warns if it is inside one.

### 3. Add your mailbox

```sh
postkauz account add mail --imap imap.example.org:993 --user me@example.org
postkauz account test mail
```

`account add` asks for the password and stores it in the operating system's keyring (never in a file of PostKauz). Port 993 means TLS. Many providers require an **app password** for IMAP (for example Gmail, iCloud). **Microsoft mailboxes** (Outlook.com, Microsoft 365) only allow OAuth2: register your own app once (steps in German in [docs/betrieb_microsoft.md](docs/betrieb_microsoft.md); PostKauz ships no app of its own), then `postkauz account add outlook --imap outlook.office365.com:993 --user you@example.org --auth oauth2 --client-id <your client id> --tenant consumers` and `postkauz account login outlook` (opens the browser; `--device-code` signs in on another device). Only the refresh token is stored, in the OS keyring; OAuth2 accounts need a keyring (not on servers without one). `account test` logs in read-only and lists capabilities and folders with their roles (`inbox`, `sent`, `drafts`, …).

### 4. Mirror and search

```sh
postkauz sync --full -a mail      # first sync: resumable, Ctrl+C and run again continues
postkauz search "Rechnung" -a mail --limit 5
postkauz show <id>                # an id from the search result
```

The first sync of a mailbox with a few thousand messages takes minutes; very large mailboxes (tens of thousands of messages, many attachments) take longer, mostly limited by your connection. It can be interrupted at any time. Afterwards `postkauz sync -a mail` only fetches changes; `postkauz sync -a mail --watch` keeps running and picks up new mail in the inbox within seconds (IDLE). Older archives (Thunderbird, mbox, Maildir) can be imported without touching the server: `postkauz import mbox <file or folder> -a mail --mailbox Archive` (`docs/cli.md`, section 2i).

### 5. Connect Claude Code

Create a read-only key for the agent and register the MCP server, see [Set up MCP in Claude Code](#set-up-mcp-in-claude-code):

```sh
postkauz key create --name claude-code --role reader --expires 30d
claude mcp add --env POSTKAUZ_KEY=pk_key_... --scope user --transport stdio postkauz -- /full/path/to/postkauz mcp
```

Then set up the [daily audit export](#daily-audit-export) to a second location. That's it.

### Troubleshooting

| Symptom | Cause and fix |
|---|---|
| Windows: "Windows protected your PC" (SmartScreen) | The program is not signed yet. Run `Unblock-File .\postkauz.exe` once, or click "More info", then "Run anyway". Check the checksum first (step 1). |
| macOS: "cannot be opened because the developer cannot be verified" | Unsigned program: `xattr -d com.apple.quarantine ~/bin/postkauz`, or right-click the file in Finder, "Open". The first access to the Keychain asks for permission: choose "Always Allow". |
| `keyring: unavailable` (Linux servers, containers, SSH sessions without a desktop) | No Secret Service is running. Install and unlock one (GNOME Keyring, KWallet) or keep the password in the environment: `export POSTKAUZ_PASSWORD_MAIL='…'` and `account add … --password-from-env`. There is deliberately no silent fallback to a file. |
| `account test` fails with `auth` | Wrong user name or password, or the provider wants an app password / has IMAP switched off in its settings. PostKauz never retries a rejected login. |
| `oauth_refresh`, `no_token` or `login_failed` (Microsoft account) | Sign in again with `account login <name>`; without a browser on the computer use `--device-code`. `token_not_stored` means the keyring refused the renewed token: check `doctor`, then run the command again. |
| `account test` fails with `connection` (exit code 5) | Host, port or TLS mode wrong, or a firewall blocks port 993. |
| `pdftotext: not found` | Optional. Without it PDF attachments get no searchable text (file names still are). Install Poppler (below); the next `sync` extracts the skipped PDFs. |
| `sync_running` | Another `sync` or `import` of the same account is running (for example `sync --watch` in another window). |
| Anything else | `postkauz doctor` and `postkauz doctor --secrets`; error messages and the log (`postkauz.log` in the data directory) never contain passwords or mail text, so they are safe to share. |

## Command overview

From a source checkout (`make build`, needs Go 1.27) the program is `./bin/postkauz`; installed, it is just `postkauz`:

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

## Drafts

PostKauz is **draft-only**: it writes drafts into your Drafts folder; you send them from your usual mail client.

**What works** (slice 2):

- `draft new`, `draft reply` (`--all` for reply-all) and `draft forward` with a Markdown (`--body-file`) or HTML (`--html-file`) body. Replies get `In-Reply-To`, `References`, the `Re:` prefix and a quote; reply-all leaves out your own addresses (account address, aliases, identities). The Message-ID is fixed when the draft is created. Signature, reply prefix and a style file for agents come from an identity (`identity add`); attachments from the mirror (attachment id) or from files under `[paths] attach_dir`.
- The next `postkauz sync` puts the draft into the Drafts folder (IMAP APPEND, flags `\Draft \Seen`); a running `sync --watch` does it within about 10 seconds. `draft show <id> --eml` prints exactly the bytes of that copy.
- Optional approval: `draft request <id>` runs your check hook and asks for approval; `approve <approval-id>` (a human, no key) marks the draft `ready`. The approval is bound to the exact message: any change sends the draft back to `draft`.
- When you have sent the draft from your mail client, the next `sync` finds it in Sent (by its Message-ID, or by recipients, subject, time and text when the client gave it a new Message-ID) and marks it `sent_by_human`. Otherwise you close it with `draft mark-sent <id>` or `draft abandon <id>`.
- Agents with a key of the role `assistant` get six MCP tools: create, change, read and list drafts, submit one for approval, read the state of an approval.

**What never happens:** PostKauz never sends mail (there is no SMTP code), never deletes, expunges or moves a message, and never overwrites a draft you changed in your mail client (further changes are refused with `draft_taken_over`). After a change it puts a new copy into Drafts and leaves the older one there; `draft list` and `draft show` warn about it, and you delete it in your mail client. The MCP server never holds the mailbox password. No API key, whatever its role, can approve, reject, abandon or mark a draft as sent, or manage identities (`human_only`).

```sh
postkauz identity add -a mail --email me@example.org --name "Me" --signature-file signature.txt
postkauz draft reply 42 --body-file answer.md        # draft 1 created (status draft, remote pending)
postkauz sync -a mail                                # puts it into the Drafts folder, then mirrors
postkauz draft show 1                                # status, remote: pushed, copies, text
postkauz draft request 1                             # check hook, then approval 1 (optional)
postkauz approve 1                                   # only a human: status ready
# now open the draft in your mail client and send it
postkauz sync -a mail && postkauz draft list         # status sent_by_human
```

The check hook is a program you name as a list in `policy.toml` (`[draft] check_hook`, no shell); the examples `check-example.ps1` (Windows PowerShell 5.1) and `check-example.sh` come with each release and lie in `deploy/hooks/`. For a German quote line set `[draft] quote_line = 'Am {date_numeric} schrieb {from}:'` in `config.toml`. Details: `docs/cli.md` (sections 2j and 5), `docs/versanderkennung.md` (how sending is detected), `docs/sicherheit.md` (section 5).

## Set up MCP in Claude Code

`postkauz mcp` is an MCP server over stdio. An agent gets its own API key; the tools it sees follow the key's scopes, every call lands in the audit log under the key's name, and every piece of mail content comes wrapped as untrusted data. A reader key only reads; a key with the scope `mail:draft` (role `assistant`) can also create drafts and submit them for approval. Only a human approves and sends them from the mail client; nothing is ever sent, deleted or moved by PostKauz.

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

**3. Check it.** In Claude Code, `/mcp` lists the server and its tools: `mail_search`, `mail_get_message`, `mail_get_thread`, `mail_list_accounts`, `mail_list_mailboxes`, `mail_list_attachments`, `mail_get_attachment_text`, `mail_audit_log`, `mail_get_policy` (with a key of the role `assistant` also the six draft tools, see [Drafts](#drafts)). Ask something like "Search my mail for invoices and summarise them"; afterwards `postkauz audit --since 10m --key-id key:claude-code` shows each call with the key name and without any mail text.

The server only reads the local mirror and writes drafts into it; keep it current with `postkauz sync` (or `sync --watch`), which also puts the drafts into the Drafts folder. Revoke a key you no longer need: `postkauz key revoke claude-code`. Note that the client configuration stores the key in plain text in your user profile; use a short expiry and the narrowest role that works. Details: `docs/mcp.md`, `docs/sicherheit.md`.

## Daily audit export

The audit log is append-only; its real tamper protection is a daily export to a **second location** (another disk, a network share or a synced folder such as Google Drive). Each day becomes one JSONL file whose lines form a SHA-256 hash chain across all days, plus a manifest (`postkauz-audit-manifest.json`) with checksum and chain head per file. `postkauz audit verify` compares the export with the database and the chain and names the first changed line (`mismatch`, exit code 1). Set the target once in the configuration (`postkauz doctor` shows its path), for example:

```toml
[audit]
export_dir = 'G:\My Drive\PostKauz-Audit'
```

**Windows** (Task Scheduler, no administrator rights; the script `Register-AuditExport.ps1` comes with each release and lies in `deploy/windows/`; put it next to `postkauz.exe` or anywhere else):

```powershell
powershell -ExecutionPolicy Bypass -File .\Register-AuditExport.ps1 -ExePath C:\PostKauz\postkauz.exe -ExportDir "G:\My Drive\PostKauz-Audit"
```

It registers the task "PostKauz audit export" (daily at 12:30, runs as soon as possible after a missed run, only while you are logged on, without a console window), starts it once and checks that the export arrived. The task gets the data directory and configuration file that `postkauz doctor` shows at that moment (`--data-dir`, `--config`), so it works on the same data as your window even if `POSTKAUZ_DATA_DIR` is only set there. Options: `-DataDir C:\PostKauz\data`, `-At 08:15`, `-ProfileName <name>`, `-ShowWindow` (if the hidden console is blocked), `-Unregister`.

If you start PostKauz from a packaged Windows app (for example from a terminal inside the Claude desktop app), Windows may redirect new folders under `%LOCALAPPDATA%` and `%APPDATA%` into that app's package folder, where the Task Scheduler does not see them. `postkauz doctor` warns about this (`really: …`), and the script refuses to register the task; it also refuses a data directory without a database. Use a data directory outside these folders, for example `-DataDir C:\PostKauz\data` (and `--data-dir` or `POSTKAUZ_DATA_DIR` for your own commands); to keep existing data, copy the folder shown after `really:` there first. If Windows refuses to run a downloaded script, run `Unblock-File .\Register-AuditExport.ps1` first.

**Linux** (systemd user timer, daily at 12:30 with `Persistent=true`):

```sh
install -Dm644 deploy/systemd/postkauz-audit-export.service deploy/systemd/postkauz-audit-export.timer -t ~/.config/systemd/user/
systemctl --user daemon-reload && systemctl --user enable --now postkauz-audit-export.timer
systemctl --user start postkauz-audit-export.service && postkauz audit verify
```

The service expects the program at `~/.local/bin/postkauz`; change `ExecStart` with `systemctl --user edit --full postkauz-audit-export.service` otherwise. On a server without login sessions also run `loginctl enable-linger "$USER"`. On macOS use `launchd` or `cron` with `postkauz audit export --quiet`.

`postkauz doctor` shows the last export (line `audit:`) and warns when days older than two days were not exported.

Test servers (Dovecot, Stalwart, GreenMail) for development: `make testmail-up`. Never develop against a real mailbox.

## Security

PostKauz assumes that **every mail may contain instructions** aimed at your agent, and that no filter recognises them reliably. Protection therefore lives outside the model. Details and honest limits: `docs/sicherheit.md`.

- **Draft-only.** The MVP contains no SMTP code at all (`make guard` checks it). Agents can at most prepare drafts and submit them for approval; only a human approves, and a human sends them in the usual mail client. There is no expunge in the provider interface; agents reach the trash at most (slice 3).
- **Scopes and roles.** Every agent gets its own API key (`key create`), only its SHA-256 hash is stored. Roles bundle scopes (`reader`: `mail:read`, `mail:search`, `audit:read`); `--scopes` can only narrow a role (for example `mail:search` without message bodies). The MCP server lists only the tools the key may use; revoking a key takes effect within seconds, also in a running session. `policy show --key <name>` shows what a key may do.
- **Untrusted envelope.** Every output with mail content (CLI `show`, `thread`, `search`, `attach`; all MCP results) is marked `untrusted: true`, `source: "email"` with a notice to treat it as data. HTML is reduced to text, hidden text is removed and reported, links are listed with warnings, nothing is loaded remotely, control and invisible characters are removed.
- **Audit.** Every call (CLI and MCP, allowed or denied) is written to an append-only table **before** it runs (fail closed), with key name and parameters but never mail text or search terms. The daily export to a second location with a hash chain and manifest makes later changes visible (`audit verify`).
- **Secrets.** Passwords live in the OS keyring or the environment, never in files of PostKauz, logs, audit, error messages, `--json` output or crash reports; a canary test across all output channels runs with every `make check`, and `doctor --secrets` searches your own installation.
- **What leaves your computer:** PostKauz itself sends nothing anywhere. An agent you connect sees what it reads through its key; use the narrowest role and a short expiry.

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

Stand: Release-Kandidat 0.2.0-rc1 (Pre-Alpha), Schnitt 2 (Entwürfe) gebaut, Abnahme D5 mit echten Mailclients steht aus; zuletzt veröffentlicht: 0.1.0-rc2 (Schnitt 1). Schnitt 2: Entwürfe im Entwürfe-Ordner, Freigabe nur durch den Menschen, Prüf-Hook, Erkennung des Versands (Abschnitt „Drafts"). Schnitt 1: Konten, Spiegel mit Sync und IDLE, Threads, bereinigter Text, Anhänge mit Typprüfung und Textextraktion, Volltextsuche mit deutschen Wortformen, Umlaut-/ß-Schreibweisen und Teilwörtern, API-Schlüssel, Audit mit Export und Prüfung, MCP-Server über stdio (lesend, mit Rolle `assistant` auch Entwürfe), Import aus mbox/Maildir, täglicher Audit-Export mit Hash-Kette). Installation in unter 15 Minuten: Abschnitt „Install in under 15 minutes"; Einrichtung in Claude Code: „Set up MCP in Claude Code". Text aus PDF-Anhängen nur mit dem optionalen `pdftotext` (siehe oben). Die Dokumentation unter `docs/` ist deutsch; Einstieg: `docs/architektur.md` und `docs/entscheidungen.md`. Entwicklungsregeln für KI-Sitzungen: `CLAUDE.md`.

Nicht-Ziele: keine GUI vor 1.0, kein Massenversand, keine Agenten-Postfächer, keine eigene Google-OAuth-App, kein Spamfilter, kein PGP/S/MIME im MVP.
