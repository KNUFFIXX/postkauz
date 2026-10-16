-- PostKauz, Migration 0003: Threads, bereinigte Texte, Anhänge (S1.6).
-- Begründungen: docs/datenmodell.md.
--
--   1. threads: root_orphan (die früheste Nachricht ist eine Antwort ohne
--      Verweise; Kandidat für den Betreff-Rückfall) und ein Index über den
--      normalisierten Betreff. thread_refs ordnet jede bekannte Message-ID
--      (eigene und verwiesene, auch von fehlenden Nachrichten) einem Thread
--      zu; darüber werden neue Nachrichten zugeordnet und Threads vereinigt.
--   2. bodies: Text ohne Zitate, Links (JSON) und die Version der
--      Bereinigung (clean_version); ältere Stände bereitet der Sync aus
--      der Rohmail neu auf.
--   3. attachments wird neu angelegt (bis S1.5 nie beschrieben, also leer):
--      sha256 darf NULL sein (Teil über der Grenze, nicht gespeichert),
--      dazu Inline/Content-ID, Typprüfung (Magic Bytes) und Fehlertext der
--      Extraktion.

ALTER TABLE threads ADD COLUMN root_orphan INTEGER NOT NULL DEFAULT 0 CHECK (root_orphan IN (0,1));
CREATE INDEX threads_subject ON threads(account_id, subject_norm);

CREATE TABLE thread_refs (
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    ref_id     TEXT NOT NULL,                  -- Message-ID ohne <>
    thread_id  INTEGER NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
    PRIMARY KEY (account_id, ref_id)
) STRICT;
CREATE INDEX thread_refs_thread ON thread_refs(thread_id);

ALTER TABLE bodies ADD COLUMN text_unquoted TEXT NOT NULL DEFAULT '';   -- neuer Text ohne Zitate
ALTER TABLE bodies ADD COLUMN links TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(links));
ALTER TABLE bodies ADD COLUMN clean_version INTEGER NOT NULL DEFAULT 0; -- 0 = vor S1.6 gespiegelt

DROP TABLE attachments;
CREATE TABLE attachments (
    id              INTEGER PRIMARY KEY,
    message_id      INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    part_id         TEXT NOT NULL,              -- MIME-Teilnummer, z. B. "2.1"
    filename        TEXT,
    content_type    TEXT NOT NULL,              -- aus Magic Bytes; unbekannt: application/octet-stream
    declared_type   TEXT,                       -- Angabe der Mail (Content-Type ohne Parameter)
    size            INTEGER NOT NULL,           -- dekodierte Bytes (bei truncated: bis zur Grenze)
    sha256          TEXT REFERENCES blobs(sha256),  -- dekodierter Inhalt im Blob-Store; NULL bei truncated
    inline          INTEGER NOT NULL DEFAULT 0 CHECK (inline IN (0,1)),
    content_id      TEXT,
    truncated       INTEGER NOT NULL DEFAULT 0 CHECK (truncated IN (0,1)),
    type_mismatch   INTEGER NOT NULL DEFAULT 0 CHECK (type_mismatch IN (0,1)),
    mismatch_reason TEXT,
    executable      INTEGER NOT NULL DEFAULT 0 CHECK (executable IN (0,1)),
    extracted_text  TEXT,
    extract_status  TEXT NOT NULL DEFAULT 'pending'
                    CHECK (extract_status IN ('pending','ok','failed','skipped')),
    extract_error   TEXT,                       -- Grund bei failed/skipped (ohne Inhalt)
    hidden_text     INTEGER NOT NULL DEFAULT 0 CHECK (hidden_text IN (0,1)),  -- HTML-Anhang mit verstecktem Text
    UNIQUE (message_id, part_id)
) STRICT;
CREATE INDEX attachments_pending ON attachments(extract_status) WHERE extract_status = 'pending';
CREATE INDEX attachments_sha256 ON attachments(sha256);
