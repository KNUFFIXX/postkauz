-- PostKauz, Migration 0022 (T3): IDs ohne Wiederverwendung, Quellpfad je Ziel.
--
-- 1. Höchststand der IDs von messages und mailboxes in meta (A14, A03_teile.md
--    T3): Beide Tabellen haben INTEGER PRIMARY KEY ohne AUTOINCREMENT; SQLite
--    vergibt nach dem Löschen der größten Zeile deren ID neu. Ab S3.4 löschen
--    retention.purge (Nachrichten) und der Ordnerabgleich (Ordner) Zeilen; eine
--    neu vergebene ID zeigte dann in Aktionen, Audit-Zielen und Entwurfs-
--    verweisen auf eine andere Nachricht bzw. einen anderen Ordner. Statt die
--    größten Tabellen neu zu bauen (Default des Auftrags), merkt sich meta den
--    Höchststand (id_floor_messages, id_floor_mailboxes); ein Trigger hebt ihn
--    beim Löschen an, und der Store vergibt neue IDs oberhalb von
--    max(größte ID, Höchststand) (store/ids.go).
-- 2. action_targets.source_path (A03_teile.md T3, Nachprüfung zu fd70f19):
--    Pfad des Quellordners beim Anlegen des Ziels; vor dem Kommando vergleicht
--    der Runner ihn mit dem Pfad unter mailbox_id (wie beim Zielordner, V1).
--    Offene Ziele bekommen den heutigen Pfad; abgeschlossene bleiben NULL.
--
-- Keine Zeile wird verworfen oder inhaltlich geändert.

INSERT INTO meta(key, value) VALUES ('id_floor_messages', CAST((SELECT coalesce(max(id), 0) FROM messages) AS TEXT))
    ON CONFLICT(key) DO NOTHING;
INSERT INTO meta(key, value) VALUES ('id_floor_mailboxes', CAST((SELECT coalesce(max(id), 0) FROM mailboxes) AS TEXT))
    ON CONFLICT(key) DO NOTHING;

CREATE TRIGGER messages_id_floor AFTER DELETE ON messages
WHEN old.id > coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_messages'), 0)
BEGIN
    INSERT INTO meta(key, value) VALUES ('id_floor_messages', CAST(old.id AS TEXT))
        ON CONFLICT(key) DO UPDATE SET value = excluded.value;
END;

CREATE TRIGGER mailboxes_id_floor AFTER DELETE ON mailboxes
WHEN old.id > coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_mailboxes'), 0)
BEGIN
    INSERT INTO meta(key, value) VALUES ('id_floor_mailboxes', CAST(old.id AS TEXT))
        ON CONFLICT(key) DO UPDATE SET value = excluded.value;
END;

ALTER TABLE action_targets ADD COLUMN source_path TEXT;
UPDATE action_targets SET source_path = (SELECT path FROM mailboxes WHERE mailboxes.id = action_targets.mailbox_id)
 WHERE result IS NULL;
