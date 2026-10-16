-- PostKauz, Migration 0004: Volltextindex und Warteschlange (S1.7).
-- Begründungen: docs/datenmodell.md, „Volltextsuche" und „Jobs".
--
--   1. fts_messages und fts_trigram werden neu angelegt (bis S1.6 nie
--      beschrieben, also leer): dieselben fünf Spalten in beiden Tabellen
--      (subject, sender, recipients, body, attachments), damit Feldfilter
--      wie from: in beiden gleich wirken. fts_messages enthält die
--      app-seitig gefalteten und gestemmten Wörter (internal/search),
--      fts_trigram den gefalteten Text für Teilwörter. prefix='1 2'
--      beschleunigt kurze Anfragen (unter 3 Zeichen nur Wortindex mit
--      Präfix).
--   2. index_queue: Nachrichten, deren Indexzeilen fehlen oder veraltet
--      sind. Trigger reihen ein, wenn eine Nachricht, ihr Text oder ein
--      Anhang (Name, extrahierter Text) entsteht oder sich ändert; der
--      index-Job arbeitet die Warteschlange in Transaktionen ab
--      (wiederaufnehmbar). reindex = 0: Die Nachricht war nie im Index
--      (kein Löschen nötig). Gelöschte Nachrichten verlieren ihre
--      Indexzeilen per Trigger.
--   3. jobs: höchstens ein offener Job je Art und Konto (partieller
--      Unique-Index); Trigger legen den index-Job an, sobald eine Nachricht
--      eingereiht wird, und den extract-Job, sobald ein Anhang auf
--      Extraktion wartet.
--   4. Bestand: alle Nachrichten werden eingereiht, wartende Anhänge
--      bekommen ihren extract-Job; meta.fts_version = 1.

DROP TABLE fts_messages;
DROP TABLE fts_trigram;

CREATE VIRTUAL TABLE fts_messages USING fts5(
    subject, sender, recipients, body, attachments,
    content = '', contentless_delete = 1,
    tokenize = 'unicode61 remove_diacritics 2',
    prefix = '1 2'
);

CREATE VIRTUAL TABLE fts_trigram USING fts5(
    subject, sender, recipients, body, attachments,
    content = '', contentless_delete = 1,
    tokenize = 'trigram'
);

CREATE TABLE index_queue (
    message_id INTEGER PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
    reindex    INTEGER NOT NULL DEFAULT 1 CHECK (reindex IN (0,1)),
    queued_at  INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX jobs_open ON jobs(type, json_extract(payload, '$.account_id'))
    WHERE state IN ('queued','running');

-- Einreihen (ohne OR-Klauseln: NOT EXISTS statt INSERT OR IGNORE, damit die
-- Konfliktbehandlung der auslösenden Anweisung nicht hineinwirkt).
CREATE TRIGGER messages_index_insert AFTER INSERT ON messages BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.id, 0, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.id);
END;

CREATE TRIGGER messages_index_update AFTER UPDATE OF subject, from_addr, to_addrs, cc_addrs ON messages BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.id, 1, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.id);
END;

CREATE TRIGGER messages_index_delete AFTER DELETE ON messages BEGIN
    DELETE FROM fts_messages WHERE rowid = OLD.id;
    DELETE FROM fts_trigram WHERE rowid = OLD.id;
END;

CREATE TRIGGER bodies_index_insert AFTER INSERT ON bodies BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.message_id, 1, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.message_id);
END;

CREATE TRIGGER bodies_index_update AFTER UPDATE OF text_plain, text_from_html ON bodies
WHEN NEW.text_plain IS NOT OLD.text_plain OR NEW.text_from_html IS NOT OLD.text_from_html BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.message_id, 1, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.message_id);
END;

CREATE TRIGGER attachments_index_insert AFTER INSERT ON attachments BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.message_id, 1, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.message_id);
END;

CREATE TRIGGER attachments_index_update AFTER UPDATE OF filename, extracted_text ON attachments
WHEN NEW.filename IS NOT OLD.filename OR NEW.extracted_text IS NOT OLD.extracted_text BEGIN
    INSERT INTO index_queue(message_id, reindex, queued_at)
    SELECT NEW.message_id, 1, CAST(unixepoch('subsec') * 1000 AS INTEGER)
    WHERE NOT EXISTS (SELECT 1 FROM index_queue WHERE message_id = NEW.message_id);
END;

-- Jobs anlegen (höchstens einer offen je Art und Konto).
CREATE TRIGGER index_queue_job AFTER INSERT ON index_queue BEGIN
    INSERT INTO jobs(type, payload, state, run_at)
    SELECT 'index', json_object('account_id', m.account_id), 'queued', CAST(unixepoch('subsec') * 1000 AS INTEGER)
    FROM messages m WHERE m.id = NEW.message_id
      AND NOT EXISTS (SELECT 1 FROM jobs WHERE type = 'index' AND state IN ('queued','running')
                      AND json_extract(payload, '$.account_id') = m.account_id);
END;

CREATE TRIGGER attachments_extract_job_insert AFTER INSERT ON attachments WHEN NEW.extract_status = 'pending' BEGIN
    INSERT INTO jobs(type, payload, state, run_at)
    SELECT 'extract', json_object('account_id', m.account_id), 'queued', CAST(unixepoch('subsec') * 1000 AS INTEGER)
    FROM messages m WHERE m.id = NEW.message_id
      AND NOT EXISTS (SELECT 1 FROM jobs WHERE type = 'extract' AND state IN ('queued','running')
                      AND json_extract(payload, '$.account_id') = m.account_id);
END;

CREATE TRIGGER attachments_extract_job_update AFTER UPDATE OF extract_status ON attachments
WHEN NEW.extract_status = 'pending' AND OLD.extract_status IS NOT 'pending' BEGIN
    INSERT INTO jobs(type, payload, state, run_at)
    SELECT 'extract', json_object('account_id', m.account_id), 'queued', CAST(unixepoch('subsec') * 1000 AS INTEGER)
    FROM messages m WHERE m.id = NEW.message_id
      AND NOT EXISTS (SELECT 1 FROM jobs WHERE type = 'extract' AND state IN ('queued','running')
                      AND json_extract(payload, '$.account_id') = m.account_id);
END;

-- Bestand: alles einreihen (der Trigger legt die index-Jobs an), wartende
-- Anhänge bekommen ihren extract-Job.
INSERT INTO index_queue(message_id, reindex, queued_at)
SELECT id, 0, CAST(unixepoch('subsec') * 1000 AS INTEGER) FROM messages;

INSERT INTO jobs(type, payload, state, run_at)
SELECT 'extract', json_object('account_id', account_id), 'queued', CAST(unixepoch('subsec') * 1000 AS INTEGER)
FROM (SELECT DISTINCT m.account_id AS account_id FROM attachments a JOIN messages m ON m.id = a.message_id
      WHERE a.extract_status = 'pending') p
WHERE NOT EXISTS (SELECT 1 FROM jobs WHERE type = 'extract' AND state IN ('queued','running')
                  AND json_extract(payload, '$.account_id') = p.account_id);

INSERT INTO meta(key, value) VALUES ('fts_version', '1')
    ON CONFLICT(key) DO UPDATE SET value = excluded.value;
