-- postkauz:foreign_keys=off
-- PostKauz, Migration 0002: Sync-Zustand (S1.4/S1.5).
--
--   1. mailboxes: Rolle 'all' (Gmail „Alle Nachrichten", SPECIAL-USE \All)
--      und Sync-Phase je Ordner. SQLite kann CHECK-Listen nicht ändern,
--      deshalb wird die Tabelle neu aufgebaut (Verfahren aus der
--      SQLite-Doku „Making Other Kinds Of Table Schema Changes"). Die
--      Kopfzeile oben lässt den Migrationsläufer die Fremdschlüssel für
--      diese Migration abschalten; sonst würde DROP TABLE die Zeilen in
--      message_mailboxes per ON DELETE CASCADE mitlöschen. Danach prüft er
--      PRAGMA foreign_key_check.
--   2. message_mailboxes: Flags je Ordnerkopie. Der Server führt Flags je
--      Kopie; messages.flag_* ist die Zusammenfassung (gesetzt, wenn eine
--      vorhandene Kopie das Flag trägt; docs/datenmodell.md).
--
-- Sync-Zustand je Ordner (docs/architektur.md, „Änderungserkennung"):
--   uidvalidity   UIDVALIDITY, unter der die gespeicherten UIDs gelten
--   uidnext       Cursor: alle UIDs darunter sind gespiegelt (im Erstsync
--                 das Ende des letzten fertigen Fensters)
--   highestmodseq CONDSTORE-Stand, ab dem Flag-Änderungen abgefragt werden
--   sync_phase    new (nie), initial (Erstsync in Fenstern läuft, wieder-
--                 aufnehmbar), ready (Erstsync fertig, inkrementell)
--   total         Nachrichtenzahl des Servers beim letzten SELECT

CREATE TABLE mailboxes_new (
    id            INTEGER PRIMARY KEY,
    account_id    INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    path          TEXT NOT NULL,
    name          TEXT NOT NULL,
    role          TEXT NOT NULL DEFAULT 'custom'
                  CHECK (role IN ('inbox','sent','drafts','trash','junk','archive','all','custom')),
    parent_id     INTEGER REFERENCES mailboxes(id) ON DELETE SET NULL,
    uidvalidity   INTEGER,
    uidnext       INTEGER,
    highestmodseq INTEGER,
    total         INTEGER NOT NULL DEFAULT 0,
    unread        INTEGER NOT NULL DEFAULT 0,
    last_sync_at  INTEGER,
    sync_phase    TEXT NOT NULL DEFAULT 'new' CHECK (sync_phase IN ('new','initial','ready')),
    UNIQUE (account_id, path)
) STRICT;

INSERT INTO mailboxes_new (id, account_id, path, name, role, parent_id, uidvalidity, uidnext,
                           highestmodseq, total, unread, last_sync_at, sync_phase)
SELECT id, account_id, path, name, role, parent_id, uidvalidity, uidnext,
       highestmodseq, total, unread, last_sync_at,
       CASE WHEN uidvalidity IS NULL THEN 'new' ELSE 'ready' END
FROM mailboxes;

DROP TABLE mailboxes;
ALTER TABLE mailboxes_new RENAME TO mailboxes;

ALTER TABLE message_mailboxes ADD COLUMN flag_seen     INTEGER NOT NULL DEFAULT 0 CHECK (flag_seen IN (0,1));
ALTER TABLE message_mailboxes ADD COLUMN flag_flagged  INTEGER NOT NULL DEFAULT 0 CHECK (flag_flagged IN (0,1));
ALTER TABLE message_mailboxes ADD COLUMN flag_answered INTEGER NOT NULL DEFAULT 0 CHECK (flag_answered IN (0,1));
ALTER TABLE message_mailboxes ADD COLUMN flag_draft    INTEGER NOT NULL DEFAULT 0 CHECK (flag_draft IN (0,1));
ALTER TABLE message_mailboxes ADD COLUMN keywords      TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(keywords));
