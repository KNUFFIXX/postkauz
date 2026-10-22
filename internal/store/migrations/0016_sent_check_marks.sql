-- PostKauz, Migration 0016: Hochwassermarke der Versanderkennung
-- (Gegenprüfung S2.6–S2.8, Befund W2; Auftrag A02, Entscheid E5).
--
-- Der Rückfall-Abgleich (E5) lud bei jedem Sync-Lauf alle gesendeten
-- Nachrichten vom ältesten offenen Entwurf bis zum neuesten + 14 Tage;
-- offene draft-Entwürfe verfallen nie, bei Gmail waren das bald alle
-- Nachrichten aus „Gesendet"/„Alle Nachrichten". E5 verlangt nur, jede
-- NEUE Nachricht zu prüfen. Deshalb:
--
--   sent_check_marks     je Konto die höchste messages.id, die eine
--                        vollständige Prüfung schon gesehen hat. Die
--                        nächste Prüfung lädt nur Nachrichten darüber
--                        (messages wird nie gelöscht, die IDs steigen).
--                        Fehlt die Zeile (neues Konto, Bestand vor 0016),
--                        prüft der nächste Lauf jeden offenen Entwurf über
--                        sein eigenes Zeitfenster und setzt die Marke.
--   drafts.fallback_checked_at
--                        content_changed_at des Entwurfs bei der letzten
--                        Prüfung seines ganzen Zeitfensters (NULL = noch
--                        nie). Weicht es ab, hat sich der Inhalt seitdem
--                        geändert (oder der Entwurf ist neu), und die
--                        nächste Prüfung vergleicht ihn mit allen
--                        gesendeten Nachrichten in seinem Fenster, nicht nur
--                        mit den neuen.
--
-- Kein Mailinhalt, nur IDs und Zeiten.

CREATE TABLE sent_check_marks (
    account_id      INTEGER PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    last_message_id INTEGER NOT NULL
) STRICT;

ALTER TABLE drafts ADD COLUMN fallback_checked_at INTEGER;
