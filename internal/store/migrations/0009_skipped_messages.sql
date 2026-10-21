-- PostKauz, Migration 0009: übersprungene Nachrichten je Ordner (S2.0,
-- Nachbesserung nach der Gegenprüfung vom 21.10.2026, Befund zu A4).
--
-- Eine Nachricht, deren Größe laut Server (RFC822.SIZE) über
-- [imap] max_message_bytes liegt, lädt der Sync nicht: Sie zählt einmal als
-- Fehler je Nachricht und steht dann hier. Folgende Läufe fordern sie nicht
-- erneut an (sonst überträgt der Server sie bei jedem Lauf ganz, weil sie
-- lokal fehlt) und zählen sie beim Zahlenvergleich als bekannt.
-- `sync --full` leert die Liste des Ordners und prüft erneut (etwa nach
-- einer höheren Grenze). Die Zeilen gelten nur unter dem UIDVALIDITY, unter
-- dem sie entstanden; der Löschabgleich entfernt Zeilen, deren Nachricht
-- der Server nicht mehr führt. Kein Mailinhalt, nur UID, Größe und Grund.

CREATE TABLE skipped_messages (
    mailbox_id   INTEGER NOT NULL REFERENCES mailboxes(id) ON DELETE CASCADE,
    uid          INTEGER NOT NULL,
    uidvalidity  INTEGER NOT NULL,
    size         INTEGER NOT NULL,
    reason       TEXT NOT NULL CHECK (reason IN ('too_large')),
    skipped_at   INTEGER NOT NULL,
    PRIMARY KEY (mailbox_id, uid)
) STRICT;
