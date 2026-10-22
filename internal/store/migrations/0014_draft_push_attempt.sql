-- postkauz:foreign_keys=off
-- PostKauz, Migration 0014: Ablage der Entwürfe nach der Gegenprüfung von
-- S2.4/S2.5 (Befunde W1, W2, W3, K5; Auftrag A02, Entscheide E1, E6, E7).
--
-- Neu in drafts:
--
--   push_attempt         Ein Ablageversuch, dessen Ausgang offen ist (W1):
--                        JSON-Objekt {"mailbox_id", "uidvalidity",
--                        "content_hash", "message_hash", "date",
--                        "fingerprint"}, geschrieben VOR dem APPEND, gelöscht,
--                        sobald die Kopie vermerkt ist oder der Server das
--                        Ablegen abgelehnt hat. Bricht der Lauf zwischen
--                        APPEND und Vermerk ab (Verbindung, Strg+C,
--                        Prozessende), sucht der nächste Lauf im Spiegel des
--                        Entwürfe-Ordners eine Kopie mit dieser Message-ID und
--                        diesem Inhalts-Hash und übernimmt sie, statt eine
--                        zweite abzulegen. Kein Mailinhalt, nur Hashes,
--                        Datum und IDs.
--   pushed_message_hash  compose.MessageHash der zuletzt abgelegten Kopie
--                        (SHA-256 ohne Date, wie approvals.message_hash; W2):
--                        drafts.request_ready und drafts.approve erkennen
--                        damit, dass die Kopie im Ordner nicht mehr die
--                        Nachricht ist, die vorgelegt bzw. freigegeben wird,
--                        und reihen eine neue Kopie ein. NULL bei Kopien aus
--                        Schema 13 (gilt dann als abweichend).
--
-- push_note bekommt zwei Codes:
--
--   push_rejected        Der Server hat das Ablegen dieses Entwurfs
--                        abgelehnt (NO/BAD auf APPEND, z. B. zu groß oder
--                        Kontingent voll; W3). Die übrigen Entwürfe werden
--                        weiter abgelegt.
--   pushed_after_close   Die Kopie kam in den Ordner, nachdem der Entwurf
--                        abgeschlossen wurde (Wettlauf zwischen Prüfung und
--                        APPEND; K5). Sie bleibt dort (kein Löschen).
--
-- Die CHECK-Liste von push_note lässt sich in SQLite nur per Neubau ändern;
-- deshalb wird drafts neu gebaut (wie in 0010, Fremdschlüssel während der
-- Migration aus, danach PRAGMA foreign_key_check durch den Läufer). Auf
-- drafts verweist kein Fremdschlüssel.

CREATE TABLE drafts_new (
    id                  INTEGER PRIMARY KEY,
    account_id          INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    identity_id         INTEGER REFERENCES identities(id) ON DELETE SET NULL,
    kind                TEXT NOT NULL CHECK (kind IN ('new','reply','reply_all','forward')),
    in_reply_to_msg     INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    message_id_header   TEXT NOT NULL,
    from_name           TEXT NOT NULL DEFAULT '',
    from_email          TEXT NOT NULL DEFAULT '',
    to_addrs            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(to_addrs)),
    cc_addrs            TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(cc_addrs)),
    bcc_addrs           TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(bcc_addrs)),
    subject             TEXT NOT NULL DEFAULT '',
    body_markdown       TEXT,
    body_text           TEXT NOT NULL DEFAULT '',
    body_html           TEXT,
    headers_json        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(headers_json) AND json_type(headers_json) = 'object'),
    attachment_blobs    TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(attachment_blobs)),
    status              TEXT NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft','pending_approval','ready','sent_by_human','abandoned')),
    preview_hash        TEXT,
    hook_result         TEXT CHECK (hook_result IN ('pass','fail','timeout','error')),
    remote_mailbox_id   INTEGER REFERENCES mailboxes(id) ON DELETE SET NULL,
    remote_uid          INTEGER,
    remote_state        TEXT NOT NULL DEFAULT 'pending'
                        CHECK (remote_state IN ('pending','pushed','taken_over','missing')),
    pushed_content_hash TEXT,
    created_by          TEXT,
    approval_id         INTEGER REFERENCES approvals(id) ON DELETE SET NULL,
    sent_message_id     INTEGER REFERENCES messages(id) ON DELETE SET NULL,
    created_at          INTEGER NOT NULL,
    updated_at          INTEGER NOT NULL,
    closed_at           INTEGER,
    compose_options     TEXT NOT NULL DEFAULT '{}'
                        CHECK (json_valid(compose_options) AND json_type(compose_options) = 'object'),
    pushed_date         TEXT,
    remote_uidvalidity  INTEGER,
    remote_previous     TEXT NOT NULL DEFAULT '[]'
                        CHECK (json_valid(remote_previous) AND json_type(remote_previous) = 'array'),
    push_note           TEXT
                        CHECK (push_note IS NULL OR push_note IN ('approval_stale', 'no_drafts_mailbox', 'push_failed',
                                                                  'push_rejected', 'pushed_after_close')),
    pushed_message_hash TEXT,
    push_attempt        TEXT CHECK (push_attempt IS NULL OR (json_valid(push_attempt) AND json_type(push_attempt) = 'object')),
    CHECK ((status IN ('sent_by_human','abandoned')) = (closed_at IS NOT NULL))
) STRICT;
INSERT INTO drafts_new(id, account_id, identity_id, kind, in_reply_to_msg, message_id_header, from_name, from_email,
                       to_addrs, cc_addrs, bcc_addrs, subject, body_markdown, body_text, body_html, headers_json,
                       attachment_blobs, status, preview_hash, hook_result, remote_mailbox_id, remote_uid, remote_state,
                       pushed_content_hash, created_by, approval_id, sent_message_id, created_at, updated_at, closed_at,
                       compose_options, pushed_date, remote_uidvalidity, remote_previous, push_note)
    SELECT id, account_id, identity_id, kind, in_reply_to_msg, message_id_header, from_name, from_email,
           to_addrs, cc_addrs, bcc_addrs, subject, body_markdown, body_text, body_html, headers_json,
           attachment_blobs, status, preview_hash, hook_result, remote_mailbox_id, remote_uid, remote_state,
           pushed_content_hash, created_by, approval_id, sent_message_id, created_at, updated_at, closed_at,
           compose_options, pushed_date, remote_uidvalidity, remote_previous, push_note
    FROM drafts;
DROP TABLE drafts;
ALTER TABLE drafts_new RENAME TO drafts;
CREATE UNIQUE INDEX drafts_message_id ON drafts(message_id_header);
CREATE INDEX drafts_account_status ON drafts(account_id, status);
CREATE INDEX drafts_push_pending ON drafts(account_id) WHERE remote_state = 'pending';
