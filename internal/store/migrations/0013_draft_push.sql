-- PostKauz, Migration 0013: Ablage der Entwürfe im Entwürfe-Ordner (S2.5,
-- Auftrag A02, Entscheide E1, E6, E11, E12).
--
-- Der Job draft_push legt einen Entwurf per IMAP APPEND ab und merkt sich die
-- Kopie in remote_mailbox_id, remote_uid und pushed_content_hash (seit 0010).
-- Dazu kommen:
--
--   pushed_date         Die Date-Kopfzeile der abgelegten Kopie (RFC 5322,
--                       z. B. "Thu, 22 Oct 2026 14:03:05 +0200"). Date setzt
--                       jeder Push neu (E11) und steht nicht im message_hash;
--                       mit diesem Wert baut draft show --eml (S2.7) die
--                       abgelegte Kopie byte-gleich nach.
--   remote_uidvalidity  UIDVALIDITY des Ordners beim APPEND: Nur unter ihr
--                       gilt remote_uid (Übernahme-Erkennung, E6).
--   remote_previous     Frühere Kopien desselben Entwurfs (JSON-Liste
--                       {"mailbox_id", "uid", "uidvalidity"}): Nach einer
--                       Änderung legt der Push eine neue Kopie ab, die alte
--                       bleibt im Entwürfe-Ordner (kein Expunge im MVP);
--                       draft show nennt beide.
--   push_note           Warum der Push einen Entwurf nicht (oder nicht so)
--                       abgelegt hat, als Code ohne Mailinhalt:
--                       approval_stale (ready, aber die gebaute Nachricht
--                       passt nicht mehr zur Freigabe), no_drafts_mailbox,
--                       push_failed. NULL = nichts zu melden.

ALTER TABLE drafts ADD COLUMN pushed_date TEXT;
ALTER TABLE drafts ADD COLUMN remote_uidvalidity INTEGER;
ALTER TABLE drafts ADD COLUMN remote_previous TEXT NOT NULL DEFAULT '[]'
    CHECK (json_valid(remote_previous) AND json_type(remote_previous) = 'array');
ALTER TABLE drafts ADD COLUMN push_note TEXT
    CHECK (push_note IS NULL OR push_note IN ('approval_stale', 'no_drafts_mailbox', 'push_failed'));

-- Wartende Kopien je Konto (Job draft_push, CompleteJob).
CREATE INDEX drafts_push_pending ON drafts(account_id) WHERE remote_state = 'pending';
