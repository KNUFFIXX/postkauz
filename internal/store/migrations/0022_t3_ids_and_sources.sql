-- PostKauz, Migration 0022 (T3): IDs ohne Wiederverwendung, Quellpfad je Ziel.
--
-- 1. Höchststand der IDs von messages, mailboxes, attachments und threads in
--    meta (A14, A03_teile.md T3; attachments und threads seit der
--    Nachbesserung T3, Punkt 8): Die Tabellen haben INTEGER PRIMARY KEY ohne
--    AUTOINCREMENT; SQLite vergibt nach dem Löschen der größten Zeile deren
--    ID neu. Ab S3.4 löschen retention.purge (Nachrichten samt Anhängen und
--    leer gewordenen Threads), der Ordnerabgleich (Ordner), das Ersetzen der
--    Anhänge einer Nachricht und das Zusammenführen von Threads Zeilen; eine
--    neu vergebene ID zeigte dann in Aktionen, Audit-Zielen (message:<id>,
--    attachment:<id>, thread:<id>) und Entwurfsverweisen auf etwas anderes.
--    Statt die größten Tabellen neu zu bauen (Default des Auftrags), merkt
--    sich meta den Höchststand (id_floor_<tabelle>); ein Trigger hebt ihn
--    beim Löschen an, und der Store vergibt neue IDs oberhalb von
--    max(größte ID, Höchststand) (store/ids.go).
--
--    Startwert: die größte ID der Tabelle und, für messages und mailboxes,
--    jeder Verweis ohne Fremdschlüssel (oder mit ON DELETE SET NULL, falls
--    er verwaist ist), der auf eine vor 0022 gelöschte Zeile zeigen kann.
--    Sonst bekäme ein neuer Ordner die ID eines gelöschten, auf den ein
--    offenes Aktionsziel noch zeigt (Nachprüfung G2-4). Berücksichtigt
--    werden alle Aktionen und Entwürfe, nicht nur offene (sie bleiben
--    Beleg):
--    - messages: action_targets.message_id, drafts.in_reply_to_msg,
--      drafts.sent_message_id, sent_check_marks.last_message_id (Marke der
--      Versanderkennung: eine wiederverwendete ID darunter würde nie
--      geprüft);
--    - mailboxes: action_targets.mailbox_id, Zielordner in actions.params
--      ($.target_mailbox_id), drafts.remote_mailbox_id, die früheren Kopien
--      in drafts.remote_previous und der offene Ablageversuch in
--      drafts.push_attempt (je $.mailbox_id).
--    attachments und threads verweist keine Tabelle ohne Fremdschlüssel;
--    ihr Startwert ist max(id).
-- 2. action_targets.source_path (A03_teile.md T3, Nachprüfung zu fd70f19):
--    Pfad des Quellordners beim Anlegen des Ziels; vor dem Kommando vergleicht
--    der Runner ihn mit dem Pfad unter mailbox_id (wie beim Zielordner, V1).
--    Offene Ziele bekommen den heutigen Pfad; abgeschlossene bleiben NULL.
--
-- Keine Zeile wird verworfen oder inhaltlich geändert.

INSERT INTO meta(key, value) VALUES ('id_floor_messages', CAST((SELECT coalesce(max(v), 0) FROM (
        SELECT max(id) AS v FROM messages
        UNION ALL SELECT max(message_id) FROM action_targets
        UNION ALL SELECT max(in_reply_to_msg) FROM drafts
        UNION ALL SELECT max(sent_message_id) FROM drafts
        UNION ALL SELECT max(last_message_id) FROM sent_check_marks)) AS TEXT))
    ON CONFLICT(key) DO NOTHING;
INSERT INTO meta(key, value) VALUES ('id_floor_mailboxes', CAST((SELECT coalesce(max(v), 0) FROM (
        SELECT max(id) AS v FROM mailboxes
        UNION ALL SELECT max(mailbox_id) FROM action_targets
        UNION ALL SELECT max(CAST(json_extract(params, '$.target_mailbox_id') AS INTEGER)) FROM actions
        UNION ALL SELECT max(remote_mailbox_id) FROM drafts
        UNION ALL SELECT max(CAST(json_extract(p.value, '$.mailbox_id') AS INTEGER))
                    FROM drafts, json_each(drafts.remote_previous) AS p WHERE p.type = 'object'
        UNION ALL SELECT max(CAST(json_extract(push_attempt, '$.mailbox_id') AS INTEGER)) FROM drafts
                    WHERE push_attempt IS NOT NULL)) AS TEXT))
    ON CONFLICT(key) DO NOTHING;
INSERT INTO meta(key, value) VALUES ('id_floor_attachments', CAST((SELECT coalesce(max(id), 0) FROM attachments) AS TEXT))
    ON CONFLICT(key) DO NOTHING;
INSERT INTO meta(key, value) VALUES ('id_floor_threads', CAST((SELECT coalesce(max(id), 0) FROM threads) AS TEXT))
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

CREATE TRIGGER attachments_id_floor AFTER DELETE ON attachments
WHEN old.id > coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_attachments'), 0)
BEGIN
    INSERT INTO meta(key, value) VALUES ('id_floor_attachments', CAST(old.id AS TEXT))
        ON CONFLICT(key) DO UPDATE SET value = excluded.value;
END;

CREATE TRIGGER threads_id_floor AFTER DELETE ON threads
WHEN old.id > coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = 'id_floor_threads'), 0)
BEGIN
    INSERT INTO meta(key, value) VALUES ('id_floor_threads', CAST(old.id AS TEXT))
        ON CONFLICT(key) DO UPDATE SET value = excluded.value;
END;

ALTER TABLE action_targets ADD COLUMN source_path TEXT;
UPDATE action_targets SET source_path = (SELECT path FROM mailboxes WHERE mailboxes.id = action_targets.mailbox_id)
 WHERE result IS NULL;

-- 3. Paket C (Löschkonzept, T3, A03 E8, A14): Archive der Audit-Rotation.
--    audit rotate kopiert die Einträge exportierter und per audit verify
--    bestätigter Tage, die älter als [audit] retain sind, in eine
--    Archivdatenbank audit-archive-<von>-<bis>.db neben der Datenbank und
--    löscht sie danach aus audit_events (eine Transaktion: Trigger lösen,
--    löschen, Trigger neu anlegen). Hier stehen je Archiv Dateiname,
--    Tagesbereich, ID-Bereich, Zahl der Einträge, die Prüfsumme der Datei
--    und die Hash-Kette davor (chain_prev) und danach (chain_head, der
--    Anker für den ersten verbleibenden Tag). Append-only wie audit_events.
--    Neue Tabelle, kein Umbau: keine Zeile wird verworfen oder geändert.

CREATE TABLE audit_archives (
    id             INTEGER PRIMARY KEY,
    file           TEXT NOT NULL UNIQUE CHECK (file GLOB 'audit-archive-*.db' AND file NOT GLOB '*[/\]*'),
    from_day       TEXT NOT NULL,                 -- YYYY-MM-DD (UTC), erster Exporttag
    to_day         TEXT NOT NULL CHECK (to_day >= from_day),
    first_event_id INTEGER NOT NULL,
    last_event_id  INTEGER NOT NULL CHECK (last_event_id >= first_event_id),
    events         INTEGER NOT NULL CHECK (events > 0),
    sha256         TEXT NOT NULL CHECK (length(sha256) = 64),  -- Prüfsumme der Archivdatei
    chain_prev     TEXT NOT NULL,                 -- Kettenwert vor from_day (Format 2)
    chain_head     TEXT NOT NULL,                 -- Anker: Kettenwert nach to_day
    created_at     INTEGER NOT NULL
) STRICT;

CREATE TRIGGER audit_archives_no_update BEFORE UPDATE ON audit_archives
BEGIN SELECT RAISE(ABORT, 'audit_archives ist append-only'); END;
CREATE TRIGGER audit_archives_no_delete BEFORE DELETE ON audit_archives
BEGIN SELECT RAISE(ABORT, 'audit_archives ist append-only'); END;
