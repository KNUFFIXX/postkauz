-- postkauz:foreign_keys=off
-- PostKauz, Migration 0010: Entwürfe, Freigaben, Identitäten, Richtlinien
-- (S2.1, Auftrag A02, Entscheide E1, E6, E7, E8, E9).
-- Begründungen je Tabelle: docs/datenmodell.md, "Entwürfe, Freigaben, Schlüssel".
--
-- Die Tabellen drafts, approvals, identities und policies stehen seit 0001,
-- wurden aber bis S2.1 nie beschrieben. drafts, approvals und identities
-- werden neu aufgebaut (CHECK-Listen und Spaltentypen ändern sich; SQLite
-- kann das nur per Neubau), vorhandene Zeilen werden übernommen. Deshalb
-- laufen die Fremdschlüssel für diese Migration nicht (Direktive oben);
-- danach prüft der Läufer PRAGMA foreign_key_check.
--
--   1. jobs: neuer Typ draft_push (E1: der Prozess mit Zugangsdaten legt
--      Entwürfe per APPEND ab; reiht drafts.create ab S2.3 ein). Neubau ohne
--      RENAME der Tabelle jobs: Die Trigger aus 0004 schreiben in jobs, und
--      ein RENAME schriebe ihre Verweise mit um bzw. fände die Tabelle
--      zwischendurch nicht. Deshalb über eine Zwischentabelle und vor allen
--      anderen Umbauten dieser Datei.
--   2. identities (E9): Signatur als Text und HTML, Stildatei, Präfixe für
--      Antwort und Weiterleitung; höchstens eine Standardidentität je Konto.
--   3. approvals (E7): decision mit pending und superseded, expires_at;
--      requested_by als Text wie im Audit ("cli" | "key:<name>"); höchstens
--      eine offene Freigabe je Gegenstand.
--   4. drafts (E3, E6, E7): Markdown, Text, HTML, Kopfzeilen, Absender als
--      Kopie der Identität, Stand der Serverkopie (remote_*), Ergebnis des
--      Prüf-Hooks, closed_at; created_by als Text wie im Audit.
--   5. policies (E8): SHA-256 der Policy-Datei; die Version zählt je neuer
--      Datei hoch.
--   6. audit_events (E8): policy_version je Entscheidung.

-- 1. jobs ---------------------------------------------------------------
CREATE TABLE jobs_0010 (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL,
    payload    TEXT NOT NULL,
    state      TEXT NOT NULL,
    run_at     INTEGER NOT NULL,
    attempts   INTEGER NOT NULL,
    last_error TEXT,
    locked_by  TEXT,
    locked_at  INTEGER
) STRICT;
INSERT INTO jobs_0010 SELECT id, type, payload, state, run_at, attempts, last_error, locked_by, locked_at FROM jobs;
DROP TABLE jobs;
CREATE TABLE jobs (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('sync','index','extract','import','export','draft_push')),
    payload    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    state      TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','done','failed')),
    run_at     INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    locked_by  TEXT,
    locked_at  INTEGER
) STRICT;
INSERT INTO jobs SELECT * FROM jobs_0010;
DROP TABLE jobs_0010;
CREATE INDEX jobs_due ON jobs(state, run_at);
CREATE UNIQUE INDEX jobs_open ON jobs(type, json_extract(payload, '$.account_id'))
    WHERE state IN ('queued','running');

-- 2. identities ---------------------------------------------------------
CREATE TABLE identities_new (
    id             INTEGER PRIMARY KEY,
    account_id     INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name           TEXT NOT NULL DEFAULT '',
    email          TEXT NOT NULL,
    reply_to       TEXT,
    signature_text TEXT NOT NULL DEFAULT '',
    signature_html TEXT NOT NULL DEFAULT '',
    style_file     TEXT,                        -- Pfad, Inhalt für mail://style/{identity} (S2.8)
    reply_prefix   TEXT NOT NULL DEFAULT 'Re: ',
    forward_prefix TEXT NOT NULL DEFAULT 'Fwd: ',
    is_default     INTEGER NOT NULL DEFAULT 0 CHECK (is_default IN (0,1)),
    created_at     INTEGER NOT NULL DEFAULT 0
) STRICT;
INSERT INTO identities_new(id, account_id, name, email, reply_to, signature_text, is_default)
    SELECT id, account_id, name, email, reply_to, signature, is_default FROM identities;
DROP TABLE identities;
ALTER TABLE identities_new RENAME TO identities;
-- Höchstens eine Standardidentität je Konto (bei Altbestand gilt die erste).
UPDATE identities SET is_default = 0
    WHERE is_default = 1 AND id NOT IN (SELECT MIN(id) FROM identities WHERE is_default = 1 GROUP BY account_id);
CREATE UNIQUE INDEX identities_default ON identities(account_id) WHERE is_default = 1;
CREATE INDEX identities_account ON identities(account_id);

-- 3. approvals ----------------------------------------------------------
CREATE TABLE approvals_new (
    id           INTEGER PRIMARY KEY,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('draft','action')),
    subject_id   INTEGER,
    description  TEXT NOT NULL,                 -- ohne Mailinhalt
    requested_by TEXT,                          -- "cli" | "key:<name>" wie audit_events.api_key_id
    requested_at INTEGER NOT NULL,
    preview_hash TEXT NOT NULL,                 -- Hash des exakt vorgelegten Inhalts
    expires_at   INTEGER,                       -- danach verfällt die Freigabe (approval_ttl)
    decision     TEXT NOT NULL DEFAULT 'pending'
                 CHECK (decision IN ('pending','approved','rejected','superseded','expired')),
    decided_by   TEXT,                          -- [operator] name, sonst Benutzername des Systems
    decided_at   INTEGER,
    reason       TEXT
) STRICT;
INSERT INTO approvals_new(id, subject_type, subject_id, description, requested_by, requested_at, preview_hash,
                          decision, decided_by, decided_at, reason)
    SELECT a.id, a.subject_type, a.subject_id, a.description,
           (SELECT 'key:' || k.name FROM api_keys k WHERE k.id = a.requested_by),
           a.requested_at, a.preview_hash, COALESCE(a.decision, 'pending'), a.decided_by, a.decided_at, a.reason
    FROM approvals a;
DROP TABLE approvals;
ALTER TABLE approvals_new RENAME TO approvals;
CREATE INDEX approvals_subject ON approvals(subject_type, subject_id);
CREATE UNIQUE INDEX approvals_open ON approvals(subject_type, subject_id) WHERE decision = 'pending';

-- 4. drafts -------------------------------------------------------------
CREATE TABLE drafts_new (
    id                  INTEGER PRIMARY KEY,
    account_id          INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    identity_id         INTEGER REFERENCES identities(id) ON DELETE SET NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('new','reply','reply_all','forward')),
    in_reply_to_msg     INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    message_id_header   TEXT NOT NULL,          -- ohne <>; beim Anlegen vergeben, unveränderlich (E11)
    from_name           TEXT NOT NULL DEFAULT '',  -- Kopie der Identität (oder leer + Kontoadresse)
    from_email          TEXT NOT NULL DEFAULT '',
    to_addrs            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(to_addrs)),
    cc_addrs            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(cc_addrs)),
    bcc_addrs           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(bcc_addrs)),
    subject             TEXT NOT NULL DEFAULT '',
    body_markdown       TEXT,                   -- Eingabe (E3), oder body_html
    body_text           TEXT NOT NULL DEFAULT '',
    body_html           TEXT,
    headers_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(headers_json) AND json_type(headers_json) = 'object'),
    attachment_blobs    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachment_blobs)),
    status              TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft','pending_approval','ready','sent_by_human','abandoned')),
    preview_hash        TEXT,
    hook_result         TEXT CHECK (hook_result IN ('pass','fail','timeout','error')),
    remote_mailbox_id   INTEGER REFERENCES mailboxes(id) ON DELETE SET NULL,
    remote_uid          INTEGER,
    remote_state        TEXT NOT NULL DEFAULT 'pending'
                        CHECK (remote_state IN ('pending','pushed','taken_over','missing')),
    pushed_content_hash TEXT,
    created_by          TEXT,                   -- "cli" | "key:<name>" wie audit_events.api_key_id
    approval_id         INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
    sent_message_id     INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    closed_at           INTEGER,                -- gesetzt genau in sent_by_human und abandoned
    CHECK ((status IN ('sent_by_human','abandoned')) = (closed_at IS NOT NULL))
) STRICT;
INSERT INTO drafts_new(id, account_id, identity_id, kind, in_reply_to_msg, message_id_header, to_addrs, cc_addrs,
                       bcc_addrs, subject, body_text, body_html, attachment_blobs, status, preview_hash,
                       remote_uid, remote_state, created_by, approval_id, sent_message_id, created_at,
                       updated_at, closed_at)
    SELECT d.id, d.account_id, d.identity_id, d.kind, d.in_reply_to_msg, d.message_id_header, d.to_addrs, d.cc_addrs,
           d.bcc_addrs, d.subject, d.body_text, d.body_html, d.attachment_blobs, d.status, d.preview_hash,
           d.remote_uid, CASE WHEN d.remote_uid IS NULL THEN 'pending' ELSE 'pushed' END,
           (SELECT 'key:' || k.name FROM api_keys k WHERE k.id = d.created_by),
           d.approval_id, d.sent_message_id, d.created_at, d.updated_at,
           CASE WHEN d.status IN ('sent_by_human','abandoned') THEN d.updated_at END
    FROM drafts d;
DROP TABLE drafts;
ALTER TABLE drafts_new RENAME TO drafts;
CREATE UNIQUE INDEX drafts_message_id ON drafts(message_id_header);
CREATE INDEX drafts_account_status ON drafts(account_id, status);

-- 5. policies -----------------------------------------------------------
-- sha256: Prüfsumme der Policy-Datei (NULL = keine Datei, eingebaute
-- Standards). version zählt je neuem Inhalt hoch (policy.Record).
ALTER TABLE policies ADD COLUMN sha256 TEXT CHECK (sha256 IS NULL OR length(sha256) = 64);

-- 6. audit_events -------------------------------------------------------
-- Version der Richtlinie, unter der die Entscheidung fiel (NULL bei
-- Einträgen vor S2.1 und bei Werkzeugen ohne Richtlinie).
ALTER TABLE audit_events ADD COLUMN policy_version INTEGER;
