-- postkauz:foreign_keys=off
-- PostKauz, Migration 0017 (Schnitt 3, Teil T1a; A03 A6/A13.4): Eine
-- Ordnerkopie hat immer eine UID größer 0.
--
-- go-imap schreibt UID 0 als „*“, also „die neueste Nachricht des
-- Ordners“. Eine UID 0 aus einem Fehlerpfad träfe bei einer Aktion (S3)
-- eine fremde Mail. Importierte Ordner haben keine UID (import_folders,
-- message_import_folders, seit 0007) und stehen nicht in dieser Tabelle.
--
-- SQLite kann einen CHECK nicht nachträglich anfügen; deshalb wird
-- message_mailboxes neu gebaut (Fremdschlüssel während der Migration aus,
-- danach PRAGMA foreign_key_check durch den Läufer). Spalten, Schlüssel und
-- Daten bleiben unverändert. Vorher prüft der Läufer (store.migrationChecks),
-- dass keine Zeile uid <= 0 hat, und bricht sonst mit einer Meldung ab: Die
-- Migration verwirft und ändert keine Daten. Auf message_mailboxes verweist
-- kein Fremdschlüssel; Trigger und Sichten gibt es nicht.

CREATE TABLE message_mailboxes_new (
    message_id    INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    mailbox_id    INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    uid           INTEGER NOT NULL CHECK (uid > 0),
    modseq        INTEGER,
    flag_seen     INTEGER NOT NULL DEFAULT 0 CHECK (flag_seen IN (0,1)),
    flag_flagged  INTEGER NOT NULL DEFAULT 0 CHECK (flag_flagged IN (0,1)),
    flag_answered INTEGER NOT NULL DEFAULT 0 CHECK (flag_answered IN (0,1)),
    flag_draft    INTEGER NOT NULL DEFAULT 0 CHECK (flag_draft IN (0,1)),
    keywords      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(keywords)),
    PRIMARY KEY (message_id, mailbox_id),
    UNIQUE (mailbox_id, uid)
) STRICT;
INSERT INTO message_mailboxes_new(message_id, mailbox_id, uid, modseq, flag_seen, flag_flagged, flag_answered, flag_draft, keywords)
    SELECT message_id, mailbox_id, uid, modseq, flag_seen, flag_flagged, flag_answered, flag_draft, keywords
    FROM message_mailboxes;
DROP TABLE message_mailboxes;
ALTER TABLE message_mailboxes_new RENAME TO message_mailboxes;
