-- PostKauz, Migration 0007: Import aus mbox und Maildir (S1.10).
--
-- Importierte Ordner und Labels sind lokale Zuordnungen, getrennt von den
-- Ordnern des Servers (mailboxes/message_mailboxes): Der Server ist die
-- Wahrheit für seine Ordner (ADR-003), der Sync löst dort Zuordnungen und
-- entfernt Ordner, die der Server nicht mehr führt. Ein Import darf dem nie
-- in die Quere kommen, und der Sync nie einen Import löschen.
--
-- Abgleich (docs/architektur.md, Abschnitt 3, Regel 4): Eine importierte
-- Nachricht, die es im Konto schon gibt (Message-ID, sonst content_hash),
-- bekommt nur eine weitere Zeile in message_import_folders; Inhalt und
-- Flags der vorhandenen Nachricht bleiben (Server gewinnt).

CREATE TABLE import_folders (
    id         INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    path       TEXT NOT NULL,                    -- Ordnerpfad mit "/" (z. B. "Archiv/2024", "[Gmail]/Wichtig")
    created_at INTEGER NOT NULL,
    UNIQUE (account_id, path)
) STRICT;

CREATE TABLE message_import_folders (
    message_id    INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    folder_id     INTEGER NOT NULL REFERENCES import_folders(id) ON DELETE CASCADE,
    flag_seen     INTEGER NOT NULL DEFAULT 0 CHECK (flag_seen IN (0,1)),
    flag_flagged  INTEGER NOT NULL DEFAULT 0 CHECK (flag_flagged IN (0,1)),
    flag_answered INTEGER NOT NULL DEFAULT 0 CHECK (flag_answered IN (0,1)),
    flag_draft    INTEGER NOT NULL DEFAULT 0 CHECK (flag_draft IN (0,1)),
    keywords      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(keywords)),
    imported_at   INTEGER NOT NULL,
    PRIMARY KEY (message_id, folder_id)
) STRICT;
CREATE INDEX message_import_folders_folder ON message_import_folders(folder_id);

-- Stand je Quelle (eine mbox-Datei bzw. ein Maildir-Ordner) für die
-- Wiederaufnahme: position ist bei mbox die Byte-Position der nächsten noch
-- nicht gespeicherten Nachricht, cursor bei Maildir der Name der letzten
-- gespeicherten Datei. Beides wird in derselben Transaktion wie die
-- Nachrichten geschrieben; ein Abbruch (auch kill -9) wiederholt höchstens
-- den laufenden Stapel. size/mtime erkennen eine unveränderte Quelle.
CREATE TABLE import_sources (
    id          INTEGER PRIMARY KEY,
    account_id  INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    source      TEXT NOT NULL,                   -- absoluter Pfad der Datei bzw. des Maildir-Ordners
    format      TEXT NOT NULL CHECK (format IN ('mbox','maildir')),
    folder      TEXT NOT NULL,                   -- Zielordner (import_folders.path)
    mode        TEXT NOT NULL CHECK (mode IN ('content','labels')),
    state       TEXT NOT NULL CHECK (state IN ('running','done')),
    position    INTEGER NOT NULL DEFAULT 0,
    cursor      TEXT,
    size        INTEGER NOT NULL DEFAULT 0,
    mtime       INTEGER NOT NULL DEFAULT 0,
    started_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL,
    finished_at INTEGER,
    UNIQUE (account_id, source, folder, mode)
) STRICT;
