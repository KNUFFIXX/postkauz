-- Mailwerk, Migration 0001: Ausgangsschema (Phase 0, noch nicht produktiv).
-- Begründungen je Tabelle: docs/datenmodell.md.
--
-- Konventionen
--   * STRICT-Tabellen, Fremdschlüssel an (PRAGMA foreign_keys=ON setzt der Store).
--   * Zeitstempel: INTEGER, Unix-Millisekunden UTC, Spaltennamen enden auf _at oder ts.
--   * Listen und Objekte: TEXT mit JSON, per CHECK(json_valid(...)) abgesichert.
--   * Rohmails und Anhänge liegen NIE in SQLite, nur als Blob-Verweis (sha256).
--   * Keine Geheimnisse in dieser Datenbank: credential_ref verweist auf den
--     Schlüsselbund, api_keys speichert nur Hashes.

-- Schlüssel/Wert-Ablage für Stand-Informationen (z. B. fts_version, nächster
-- Export-Zeitpunkt). Keine fachlichen Daten.
CREATE TABLE meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
) STRICT;

CREATE TABLE accounts (
    id             INTEGER PRIMARY KEY,
    name           TEXT NOT NULL UNIQUE,
    email          TEXT NOT NULL,
    provider_type  TEXT NOT NULL DEFAULT 'imap' CHECK (provider_type IN ('imap','jmap')),
    host           TEXT NOT NULL,
    port           INTEGER NOT NULL,
    tls            TEXT NOT NULL DEFAULT 'tls' CHECK (tls IN ('tls','starttls','none')),
    username       TEXT NOT NULL,
    auth_type      TEXT NOT NULL DEFAULT 'password' CHECK (auth_type IN ('password','app_password','oauth2')),
    credential_ref TEXT NOT NULL,              -- Name im Schlüsselbund, nie der Wert
    sync_policy    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(sync_policy)),
    capabilities   TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(capabilities)),
    status         TEXT NOT NULL DEFAULT 'new' CHECK (status IN ('new','ok','error','disabled')),
    last_error     TEXT,
    created_at     INTEGER NOT NULL
) STRICT;

CREATE TABLE identities (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name        TEXT NOT NULL DEFAULT '',
    email       TEXT NOT NULL,
    signature   TEXT NOT NULL DEFAULT '',
    reply_to    TEXT,
    is_default  INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1))
) STRICT;

CREATE TABLE mailboxes (
    id            INTEGER PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    path          TEXT NOT NULL,                -- Server-Pfad, UTF-8 (IMAP-UTF-7 ist dekodiert)
    name          TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'custom'
                  CHECK (role IN ('inbox','sent','drafts','trash','junk','archive','custom')),
    parent_id     INTEGER REFERENCES mailboxes(id) ON DELETE SET NULL,
    uidvalidity   INTEGER,
    uidnext       INTEGER,
    highestmodseq INTEGER,
    total         INTEGER NOT NULL DEFAULT 0,
    unread        INTEGER NOT NULL DEFAULT 0,
    last_sync_at  INTEGER,
    UNIQUE (account_id, path)
) STRICT;

CREATE TABLE threads (
    id           INTEGER PRIMARY KEY,
    account_id   INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    subject_norm TEXT NOT NULL,
    first_date   INTEGER,
    last_date    INTEGER,
    message_count INTEGER NOT NULL DEFAULT 0,
    participants TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(participants))
) STRICT;
CREATE INDEX threads_account_last ON threads(account_id, last_date DESC);

CREATE TABLE blobs (
    sha256     TEXT PRIMARY KEY,                -- 64 Hex-Zeichen, Pfad: blobs/ab/cd/<sha256>
    size       INTEGER NOT NULL,                -- unkomprimierte Größe
    compressed INTEGER NOT NULL DEFAULT 0 CHECK (compressed IN (0,1)),
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE messages (
    id                INTEGER PRIMARY KEY,      -- lokale ID, die die CLI zeigt
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    message_id_header TEXT,                     -- Message-ID ohne <>; NULL bei kaputten Mails
    thread_id         INTEGER REFERENCES threads(id) ON DELETE SET NULL,
    subject           TEXT NOT NULL DEFAULT '',
    from_addr         TEXT NOT NULL DEFAULT '', -- "Name <adresse>" wie im Header, dekodiert
    to_addrs          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(to_addrs)),
    cc_addrs          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(cc_addrs)),
    reply_to          TEXT,
    date_header       INTEGER,
    internal_date     INTEGER,
    size              INTEGER NOT NULL DEFAULT 0,
    flag_seen         INTEGER NOT NULL DEFAULT 0 CHECK (flag_seen IN (0,1)),
    flag_flagged      INTEGER NOT NULL DEFAULT 0 CHECK (flag_flagged IN (0,1)),
    flag_answered     INTEGER NOT NULL DEFAULT 0 CHECK (flag_answered IN (0,1)),
    flag_draft        INTEGER NOT NULL DEFAULT 0 CHECK (flag_draft IN (0,1)),
    keywords          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(keywords)),
    has_attachments   INTEGER NOT NULL DEFAULT 0 CHECK (has_attachments IN (0,1)),
    snippet           TEXT NOT NULL DEFAULT '',
    blob_sha256       TEXT REFERENCES blobs(sha256),   -- Rohmail (.eml); NULL = nur Kopf gespiegelt
    in_reply_to       TEXT,
    refs              TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(refs)),   -- References-Header
    list_id           TEXT,
    auth_results      TEXT,                     -- Authentication-Results (SPF/DKIM/DMARC), roh
    content_hash      TEXT,                     -- Hash über normalisierte Header+Body (Importabgleich)
    deleted_remote_at INTEGER,                  -- Server hat die Nachricht nicht mehr; Archiv behält sie
    created_at        INTEGER NOT NULL
) STRICT;
CREATE INDEX messages_account_date   ON messages(account_id, date_header DESC);
CREATE INDEX messages_message_id     ON messages(account_id, message_id_header);
CREATE INDEX messages_thread         ON messages(thread_id);
CREATE INDEX messages_content_hash   ON messages(content_hash);

CREATE TABLE message_mailboxes (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    uid        INTEGER NOT NULL,
    modseq     INTEGER,
    PRIMARY KEY (message_id, mailbox_id),
    UNIQUE (mailbox_id, uid)
) STRICT;

CREATE TABLE bodies (
    message_id     INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    text_plain     TEXT NOT NULL DEFAULT '',    -- bereinigt, ohne Quote-Block-Markierung
    text_from_html TEXT NOT NULL DEFAULT '',    -- HTML nie ungefiltert gespeichert
    hidden_text    INTEGER NOT NULL DEFAULT 0 CHECK (hidden_text IN (0,1)),
    charset        TEXT
) STRICT;

CREATE TABLE attachments (
    id             INTEGER PRIMARY KEY,
    message_id     INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    part_id        TEXT NOT NULL,               -- MIME-Teilnummer, z. B. "2.1"
    filename       TEXT,
    content_type   TEXT NOT NULL,               -- aus Magic Bytes, nicht aus der Endung
    declared_type  TEXT,
    size           INTEGER NOT NULL,
    sha256         TEXT NOT NULL REFERENCES blobs(sha256),
    extracted_text TEXT,
    extract_status TEXT NOT NULL DEFAULT 'pending'
                   CHECK (extract_status IN ('pending','ok','failed','skipped')),
    UNIQUE (message_id, part_id)
) STRICT;

CREATE TABLE tags (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    source     TEXT NOT NULL DEFAULT 'local' CHECK (source IN ('local','server')),
    UNIQUE (account_id, name)
) STRICT;

CREATE TABLE message_tags (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    set_by     TEXT,                            -- api_keys.id oder 'human'
    set_at     INTEGER NOT NULL,
    PRIMARY KEY (message_id, tag_id)
) STRICT;

CREATE TABLE api_keys (
    id               INTEGER PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    key_hash         TEXT NOT NULL,             -- nie der Klartext; Klartext wird einmal angezeigt
    role             TEXT NOT NULL,
    scopes           TEXT NOT NULL CHECK (json_valid(scopes)),
    dry_run_default  INTEGER NOT NULL DEFAULT 1 CHECK (dry_run_default IN (0,1)),
    expires_at       INTEGER,
    last_used_at     INTEGER,
    revoked_at       INTEGER,
    created_at       INTEGER NOT NULL
) STRICT;

CREATE TABLE policies (
    id         INTEGER PRIMARY KEY,
    api_key_id INTEGER REFERENCES api_keys(id) ON DELETE CASCADE,  -- NULL = Standardrichtlinie
    document   TEXT NOT NULL CHECK (json_valid(document)),
    version    INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE approvals (
    id           INTEGER PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('draft','action')),
    subject_id   INTEGER,
    description  TEXT NOT NULL,                 -- ohne Mailinhalt
    requested_by INTEGER REFERENCES api_keys(id),
    requested_at INTEGER NOT NULL,
    preview_hash TEXT NOT NULL,                 -- Hash des exakt freigegebenen Inhalts
    decision     TEXT CHECK (decision IN ('approved','rejected','expired')),
    decided_by   TEXT,
    decided_at   INTEGER,
    reason       TEXT
) STRICT;

CREATE TABLE drafts (
    id                INTEGER PRIMARY KEY,
    account_id        INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    identity_id       INTEGER REFERENCES identities(id),
    kind              TEXT NOT NULL CHECK (kind IN ('new','reply','reply_all','forward')),
    in_reply_to_msg   INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    message_id_header TEXT NOT NULL,            -- vorab vergeben; Erkennung von "gesendet" per Message-ID
    to_addrs          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(to_addrs)),
    cc_addrs          TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(cc_addrs)),
    bcc_addrs         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(bcc_addrs)),
    subject           TEXT NOT NULL DEFAULT '',
    body_text         TEXT NOT NULL DEFAULT '',
    body_html         TEXT,
    attachment_blobs  TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachment_blobs)),
    remote_uid        INTEGER,                  -- Kopie im IMAP-Entwürfe-Ordner (nach APPEND)
    status            TEXT NOT NULL DEFAULT 'draft'
                      CHECK (status IN ('draft','pending_approval','ready','sent_by_human','abandoned')),
    preview_hash      TEXT,
    created_by        INTEGER REFERENCES api_keys(id),
    approval_id       INTEGER REFERENCES approvals(id),
    sent_message_id   INTEGER REFERENCES messages(id),
    created_at        INTEGER NOT NULL,
    updated_at        INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX drafts_message_id ON drafts(message_id_header);

CREATE TABLE jobs (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('sync','index','extract','import','export')),
    payload    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    state      TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','done','failed')),
    run_at     INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    locked_by  TEXT,
    locked_at  INTEGER
) STRICT;
CREATE INDEX jobs_due ON jobs(state, run_at);

-- Audit: append-only, Sperre per Trigger. Mailinhalte und Geheimnisse gehören
-- nie hinein (siehe audit.Event). Kein Hash pro Zeile: Manipulationsschutz
-- kommt aus dem täglichen Export mit Prüfsumme (audit_exports) an einen
-- zweiten Ort (ADR-003 / docs/sicherheit.md).
CREATE TABLE audit_events (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('human','agent','system')),
    api_key_id      TEXT,
    scope           TEXT,
    operation       TEXT NOT NULL,
    params_redacted TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params_redacted)),
    targets         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(targets)),
    result          TEXT NOT NULL CHECK (result IN ('ok','denied','needs_approval','error')),
    reason          TEXT,
    dry_run         INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0,1)),
    duration_ms     INTEGER
) STRICT;
CREATE INDEX audit_events_ts ON audit_events(ts);

CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit_events ist append-only'); END;
CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit_events ist append-only'); END;

CREATE TABLE audit_exports (
    id            INTEGER PRIMARY KEY,
    day           TEXT NOT NULL UNIQUE,         -- YYYY-MM-DD (UTC)
    first_event_id INTEGER NOT NULL,
    last_event_id  INTEGER NOT NULL,
    sha256        TEXT NOT NULL,                -- Prüfsumme der exportierten JSONL-Datei
    location      TEXT NOT NULL,                -- zweiter Ort (Pfad oder URL), ohne Zugangsdaten
    exported_at   INTEGER NOT NULL
) STRICT;

-- Volltext (docs/datenmodell.md, "FTS5-Strategie"). Beide Tabellen sind
-- contentless: Sie speichern nur den Index, keinen Text (der liegt in
-- messages/bodies/attachments). rowid = messages.id. Die Pflege übernimmt
-- die Anwendung (Index-Job), nicht ein Trigger, weil der deutsche Wortstamm
-- app-seitig berechnet wird.
CREATE VIRTUAL TABLE fts_messages USING fts5(
    subject, sender, recipients, body, attachments_text, body_stem,
    content = '', contentless_delete = 1,
    tokenize = 'unicode61 remove_diacritics 2'
);

CREATE VIRTUAL TABLE fts_trigram USING fts5(
    subject, sender, body,
    content = '', contentless_delete = 1,
    tokenize = 'trigram'
);
