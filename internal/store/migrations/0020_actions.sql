-- postkauz:foreign_keys=off
-- PostKauz, Migration 0020 (Schnitt 3, Teil T2; A03 S3.1, S3.2, E1, E3, E5,
-- A5.1, T2 Punkte 3 und 7): Postfach-Aktionen, Tags, Entwürfe-Ordner je
-- Konto. Begründungen je Tabelle: docs/datenmodell.md.
--
--   1. jobs: neuer Typ mailbox_action (E1: Markieren, Verschieben,
--      Papierkorb und Keywords schreibt nur der Prozess mit Zugangsdaten).
--      Neubau wie in 0010 über eine Zwischentabelle ohne RENAME der Tabelle
--      jobs (die Trigger aus 0004 schreiben in jobs); key_id und audit_id
--      (0019) bleiben.
--   2. actions und action_targets: je Aufruf eine Aktion (Art, Parameter
--      ohne Inhalt, Zustand, Herkunft), je Ziel eine Zeile, gebunden an
--      Nachricht, Ordner, UID und UIDVALIDITY (CHECK: beide größer 0, UID
--      nie 4294967295); Ergebnis je Ziel als Code.
--   3. message_mailboxes: pending_flags (lokal schon geänderte Flags, deren
--      Job aussteht; JSON mit Aktion und vorherigem Wert) und
--      content_loaded (1 = PostKauz hat den Inhalt dieser Serverkopie selbst
--      geladen; 0 = nur per Message-ID und Größe verknüpft oder unbekannt,
--      dann prüft der Job vor einer Aktion den Kopf; Bestand: 0).
--   4. accounts.drafts_mailbox (A5.1): Entwürfe-Ordner je Konto (NULL =
--      [draft] mailbox der Konfiguration, sonst Rolle drafts).
--   5. tags und message_tags nach E5: Namen aus Kleinbuchstaben, Ziffern,
--      '.', '_', '-', höchstens 64 Zeichen; set_by 'cli' oder 'key:<name>'.
--      Die Spalte source entfällt (Keywords vom Server bleiben Keywords,
--      keine Tags). Neubau, weil SQLite keine CHECKs nachträgt; vorher prüft
--      der Läufer (store.migrationChecks), dass vorhandene Zeilen passen,
--      sonst bricht er ohne Änderung ab.
--   6. drafts.updated_by (Nachbesserung T1b, T2 Punkt 7): Für Entwürfe aus
--      der Zeit vor 0019 (updated_by NULL) trägt die Migration den Schlüssel
--      des letzten drafts.update aus dem Audit nach (auch gescheiterte
--      Versuche zählen, das ist strenger). Ohne solchen Eintrag bleibt NULL
--      (dann gilt created_by).
--   7. mailboxes.virtual_view (Nachbesserung T2, V13): 1 = virtuelle Sicht,
--      die nur Nachrichten anderer Ordner zeigt (Gmail „Markiert“/„Wichtig“,
--      SPECIAL-USE \Flagged/\Important). Sie ist nie Quelle von move/trash
--      ohne ausdrücklich genannten Ordner. Gesetzt vom Ordnerabgleich; der
--      Bestand bekommt den Wert hier aus der Gmail-Namenszuordnung (die
--      Attribute kennt erst der nächste Abgleich).
--
-- Keine Zeile wird verworfen oder inhaltlich geändert.

-- 1. jobs ---------------------------------------------------------------
CREATE TABLE jobs_0020 (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL,
    payload    TEXT NOT NULL,
    state      TEXT NOT NULL,
    run_at     INTEGER NOT NULL,
    attempts   INTEGER NOT NULL,
    last_error TEXT,
    locked_by  TEXT,
    locked_at  INTEGER,
    key_id     TEXT,
    audit_id   INTEGER
) STRICT;
INSERT INTO jobs_0020 SELECT id, type, payload, state, run_at, attempts, last_error, locked_by, locked_at, key_id, audit_id FROM jobs;
DROP TABLE jobs;
CREATE TABLE jobs (
    id         INTEGER PRIMARY KEY,
    type       TEXT NOT NULL CHECK (type IN ('sync','index','extract','import','export','draft_push','mailbox_action')),
    payload    TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(payload)),
    state      TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued','running','done','failed')),
    run_at     INTEGER NOT NULL,
    attempts   INTEGER NOT NULL DEFAULT 0,
    last_error TEXT,
    locked_by  TEXT,
    locked_at  INTEGER,
    key_id     TEXT,
    audit_id   INTEGER
) STRICT;
INSERT INTO jobs SELECT * FROM jobs_0020;
DROP TABLE jobs_0020;
CREATE INDEX jobs_due ON jobs(state, run_at);
CREATE UNIQUE INDEX jobs_open ON jobs(type, json_extract(payload, '$.account_id'))
    WHERE state IN ('queued','running');

-- 2. actions ------------------------------------------------------------
CREATE TABLE actions (
    id            INTEGER PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    kind          TEXT NOT NULL CHECK (kind IN ('mark','move','trash','keywords')),
    params        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params)),  -- nie Inhalt: Flags, Zielordner, Keywords
    state         TEXT NOT NULL CHECK (state IN ('requested','queued','running','done','partial','failed','rejected','expired')),
    target_count  INTEGER NOT NULL CHECK (target_count > 0),
    ids_hash      TEXT NOT NULL,                -- SHA-256 der sortierten Nachrichten-IDs
    confirm_count INTEGER,
    requested_by  TEXT NOT NULL,                -- "cli" | "key:<name>" | "system" wie audit_events.api_key_id
    audit_id      INTEGER,                      -- Start-Eintrag des Aufrufs, der sie anlegte
    approval_id   INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
    error         TEXT,                         -- Code, nie Servertext
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    done_at       INTEGER
) STRICT;
CREATE INDEX actions_open ON actions(account_id, state);

CREATE TABLE action_targets (
    action_id   INTEGER NOT NULL REFERENCES actions(id) ON DELETE CASCADE,
    message_id  INTEGER NOT NULL,               -- lokale Nachrichten-ID (ohne Fremdschlüssel: die Aktion bleibt Beleg)
    mailbox_id  INTEGER NOT NULL,               -- Ordner der Serverkopie
    uid         INTEGER NOT NULL CHECK (uid > 0 AND uid < 4294967295),
    uidvalidity INTEGER NOT NULL CHECK (uidvalidity > 0 AND uidvalidity <= 4294967295),
    result      TEXT CHECK (result IS NULL OR result IN ('ok','not_found','stale','unsupported','failed','skipped')),
    new_uid     INTEGER CHECK (new_uid IS NULL OR new_uid > 0),
    PRIMARY KEY (action_id, message_id, mailbox_id)
) STRICT;

-- 3. message_mailboxes --------------------------------------------------
ALTER TABLE message_mailboxes ADD COLUMN pending_flags TEXT CHECK (pending_flags IS NULL OR json_valid(pending_flags));
ALTER TABLE message_mailboxes ADD COLUMN content_loaded INTEGER NOT NULL DEFAULT 0 CHECK (content_loaded IN (0,1));
CREATE INDEX message_mailboxes_pending ON message_mailboxes(mailbox_id) WHERE pending_flags IS NOT NULL;

-- 4. accounts -----------------------------------------------------------
ALTER TABLE accounts ADD COLUMN drafts_mailbox TEXT;

-- 5. tags ---------------------------------------------------------------
CREATE TABLE tags_new (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 64 AND name NOT GLOB '*[^a-z0-9._-]*'),
    created_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE (account_id, name)
) STRICT;
INSERT INTO tags_new(id, account_id, name) SELECT id, account_id, name FROM tags;
CREATE TABLE message_tags_new (
    message_id INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    set_by     TEXT NOT NULL CHECK (set_by = 'cli' OR set_by GLOB 'key:?*'),
    set_at     INTEGER NOT NULL,
    PRIMARY KEY (message_id, tag_id)
) STRICT;
INSERT INTO message_tags_new SELECT message_id, tag_id, set_by, set_at FROM message_tags;
DROP TABLE message_tags;
DROP TABLE tags;
ALTER TABLE tags_new RENAME TO tags;
ALTER TABLE message_tags_new RENAME TO message_tags;
CREATE INDEX message_tags_tag ON message_tags(tag_id);

-- 6. drafts.updated_by --------------------------------------------------
UPDATE drafts SET updated_by = (
    SELECT a.api_key_id FROM audit_events a
    WHERE a.operation = 'drafts.update' AND a.api_key_id IS NOT NULL AND a.result NOT IN ('denied','needs_approval')
      AND EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END) t
                  WHERE t.value = 'draft:' || drafts.id)
    ORDER BY a.id DESC LIMIT 1)
WHERE updated_by IS NULL;

-- 7. mailboxes -----------------------------------------------------------
ALTER TABLE mailboxes ADD COLUMN virtual_view INTEGER NOT NULL DEFAULT 0 CHECK (virtual_view IN (0,1));
UPDATE mailboxes SET virtual_view = 1
 WHERE lower(path) IN ('[gmail]/starred', '[gmail]/markiert', '[gmail]/important', '[gmail]/wichtig',
                       '[google mail]/starred', '[google mail]/markiert', '[google mail]/important', '[google mail]/wichtig');
