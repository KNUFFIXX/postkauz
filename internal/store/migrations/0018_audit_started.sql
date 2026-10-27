-- PostKauz, Migration 0018 (Schnitt 3, Teil T1b; A03 A1 Variante A, E12):
-- Ein erlaubter Aufruf schreibt zwei Audit-Einträge.
--
-- Vor dem Handler steht ein Eintrag mit result = 'started' (fail closed
-- bleibt: ohne ihn läuft kein Handler), danach immer ein Abschluss
-- ('ok' | 'denied' | 'error') mit eigenem Zeitstempel (Ende des Aufrufs),
-- Dauer und ref_id = ID des Start-Eintrags. Policy-Ablehnungen bleiben ein
-- einzelner Eintrag ohne ref_id.
--
-- SQLite kann den CHECK der Spalte result nicht ändern; deshalb wird
-- audit_events neu gebaut. IDs werden mitkopiert, der Zähler der
-- AUTOINCREMENT-Folge (sqlite_sequence) bleibt mindestens auf dem alten
-- Stand, Trigger und Index entstehen neu. Alte Zeilen bleiben Wert für
-- Wert gleich (ref_id NULL); ein Export vor und nach der Migration ist
-- Byte für Byte derselbe (store.TestMigration0018AuditVerify). Auf
-- audit_events verweist kein Fremdschlüssel.
--
-- ref_id ist eindeutig (höchstens ein Abschluss je Start) und nie an einem
-- Start-Eintrag gesetzt.

CREATE TABLE audit_events_new (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ts              INTEGER NOT NULL,
    actor_type      TEXT NOT NULL CHECK (actor_type IN ('human','agent','system')),
    api_key_id      TEXT,
    scope           TEXT,
    operation       TEXT NOT NULL,
    params_redacted TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(params_redacted)),
    targets         TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(targets)),
    result          TEXT NOT NULL CHECK (result IN ('started','ok','denied','needs_approval','error')),
    reason          TEXT,
    dry_run         INTEGER NOT NULL DEFAULT 0 CHECK (dry_run IN (0,1)),
    duration_ms     INTEGER,
    policy_version  INTEGER,
    ref_id          INTEGER CHECK (ref_id IS NULL OR (ref_id > 0 AND result <> 'started'))
) STRICT;
INSERT INTO audit_events_new(id, ts, actor_type, api_key_id, scope, operation, params_redacted, targets, result, reason,
        dry_run, duration_ms, policy_version)
    SELECT id, ts, actor_type, api_key_id, scope, operation, params_redacted, targets, result, reason,
        dry_run, duration_ms, policy_version
    FROM audit_events ORDER BY id;
-- Folge mindestens auf dem alten Stand halten (nie eine ID zweimal).
UPDATE sqlite_sequence SET seq = (SELECT seq FROM sqlite_sequence WHERE name = 'audit_events')
    WHERE name = 'audit_events_new' AND seq < (SELECT seq FROM sqlite_sequence WHERE name = 'audit_events');
INSERT INTO sqlite_sequence(name, seq)
    SELECT 'audit_events_new', seq FROM sqlite_sequence WHERE name = 'audit_events'
    AND NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'audit_events_new');
DROP TABLE audit_events;
ALTER TABLE audit_events_new RENAME TO audit_events;

CREATE INDEX audit_events_ts ON audit_events(ts);
CREATE UNIQUE INDEX audit_events_ref ON audit_events(ref_id) WHERE ref_id IS NOT NULL;
-- doctor zählt Starts ohne Abschluss (store.AuditOpenStarts).
CREATE INDEX audit_events_started ON audit_events(ts) WHERE result = 'started';

CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit_events ist append-only'); END;
CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events
BEGIN SELECT RAISE(ABORT, 'audit_events ist append-only'); END;
