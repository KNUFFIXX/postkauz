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
--    - attachments und threads: max(id) und die Audit-Ziele attachment:<id>
--      bzw. thread:<id> in audit_events (zweite Nachbesserung T3, Punkt 12:
--      Das Zusammenführen von Threads löscht oft den mit der größten ID, das
--      Ersetzen der Anhänge einer Nachricht deren Zeilen; eine Tabelle ohne
--      Fremdschlüssel verweist sonst nicht auf sie). Archive der Audit-
--      Rotation kann es vor 0022 nicht geben (audit_archives entsteht erst
--      unten).
--      Dritte Nachbesserung T3, Punkt 3: Die Ziele stammen aus der Eingabe
--      und stehen auch bei abgelehnten oder gescheiterten Aufrufen im Audit
--      (threads.get mit erfundener ID 9223372036854775807 ergab sonst den
--      Startwert INT64_MAX, nextIDExpr lief über, jedes neue INSERT
--      scheiterte mit datatype mismatch). Gezählt werden nur Ziele mit
--      result = 'ok' und einer ID zwischen 1 und 2^53-1.
-- 2. action_targets.source_path (A03_teile.md T3, Nachprüfung zu fd70f19):
--    Pfad des Quellordners beim Anlegen des Ziels; vor dem Kommando vergleicht
--    der Runner ihn mit dem Pfad unter mailbox_id (wie beim Zielordner, V1).
--    Offene Ziele bekommen den heutigen Pfad; abgeschlossene bleiben NULL.
--
-- 3. audit_archives (unten).
-- 4. attachments.executable für Altbestand (unten, dritte Nachbesserung T3).
--
-- Keine Zeile wird verworfen; inhaltlich geändert wird nur
-- attachments.executable von 0 auf 1 (Abschnitt 4).

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
INSERT INTO meta(key, value) VALUES ('id_floor_attachments', CAST((SELECT coalesce(max(v), 0) FROM (
        SELECT max(id) AS v FROM attachments
        UNION ALL SELECT max(CAST(substr(j.value, 12) AS INTEGER))
                    FROM audit_events, json_each(CASE WHEN json_valid(targets) THEN targets ELSE '[]' END) AS j
                    WHERE j.value LIKE 'attachment:%' AND audit_events.result = 'ok'
                      AND CAST(substr(j.value, 12) AS INTEGER) BETWEEN 1 AND 9007199254740991)) AS TEXT))
    ON CONFLICT(key) DO NOTHING;
INSERT INTO meta(key, value) VALUES ('id_floor_threads', CAST((SELECT coalesce(max(v), 0) FROM (
        SELECT max(id) AS v FROM threads
        UNION ALL SELECT max(CAST(substr(j.value, 8) AS INTEGER))
                    FROM audit_events, json_each(CASE WHEN json_valid(targets) THEN targets ELSE '[]' END) AS j
                    WHERE j.value LIKE 'thread:%' AND audit_events.result = 'ok'
                      AND CAST(substr(j.value, 8) AS INTEGER) BETWEEN 1 AND 9007199254740991)) AS TEXT))
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

-- 4. attachments.executable für Altbestand (dritte Nachbesserung T3, Punkt
--    9c): Seit S3.3 kennt magic.ExecutableName mehr Endungen und bewertet
--    den Namen so, wie Windows ihn ausführt (magic.EffectiveExt: letzter
--    Pfadteil, NTFS-Datenstrom ab ":" weg, Punkte, Leerzeichen und
--    Tabulatoren am Ende weg). Anhänge, die eine ältere Version gespiegelt
--    hat, zeigten sonst executable = false. Die Liste ist die ganze
--    riskyExt aus internal/magic/check.go (der Test
--    TestT3N3Migration0022MarksExecutables vergleicht sie). Steuer- und
--    Formatzeichen im Namen entfernt SQL nicht (Restfall). Nur 0 → 1.
UPDATE attachments SET executable = 1
 WHERE executable = 0 AND filename IS NOT NULL
   AND (SELECT CASE WHEN instr(t, '.') > 0 THEN lower('.' || replace(t, rtrim(t, replace(t, '.', '')), '')) END
          FROM (SELECT rtrim(CASE WHEN instr(b, ':') > 0 THEN substr(b, 1, instr(b, ':') - 1) ELSE b END, '. ' || char(9)) AS t
                  FROM (SELECT replace(n, rtrim(n, replace(n, '/', '')), '') AS b
                          FROM (SELECT replace(attachments.filename, '\', '/') AS n)))) IN (
    '.accdb', '.accde', '.ade', '.adp', '.apk', '.app', '.appinstaller', '.application',
    '.appref-ms', '.appx', '.appxbundle', '.bas', '.bat', '.cer', '.chm', '.cmd', '.com',
    '.command', '.cpl', '.crt', '.csh', '.der', '.diagcab', '.dll', '.docm', '.dotm', '.exe',
    '.gadget', '.hlp', '.hta', '.img', '.inf', '.ins', '.iso', '.isp', '.jar', '.jnlp', '.js',
    '.jse', '.ksh', '.library-ms', '.lnk', '.mda', '.mdb', '.mde', '.msc', '.msh', '.msh1',
    '.msh1xml', '.msh2', '.msh2xml', '.mshxml', '.msi', '.msix', '.msixbundle', '.msp', '.mst',
    '.one', '.pif', '.potm', '.ppam', '.ppsm', '.pptm', '.ps1', '.ps1xml', '.ps2xml', '.psc1',
    '.psd1', '.psm1', '.rdp', '.reg', '.scf', '.scr', '.sct', '.search-ms', '.settingcontent-ms',
    '.sh', '.shb', '.sys', '.url', '.vb', '.vbe', '.vbs', '.vhd', '.vhdx', '.vsto', '.website',
    '.ws', '.wsb', '.wsc', '.wsf', '.wsh', '.xbap', '.xlam', '.xll', '.xlsm', '.xltm', '.xnk');
