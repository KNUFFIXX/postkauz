-- PostKauz, Migration 0015: Zeit der letzten Inhaltsänderung eines Entwurfs
-- (Gegenprüfung S2.6–S2.8, Befund W1; Auftrag A02, Entscheide E5, E7).
--
-- Das Zeitfenster des Rückfall-Abgleichs der Versanderkennung (E5: Versand
-- zwischen −1 h und +14 Tage) hing an updated_at. updated_at ändert aber
-- auch, was den Inhalt nicht ändert: Vorlage, Freigabe, Ablehnung und vor
-- allem der Ablauf der Freigabe (beim Sync und beim Lesen). Ein Entwurf,
-- den der Mensch am Tag 6 aus dem Webmail gesendet hat und den der Sync
-- erst am Tag 8 sieht, fiel am Tag 7 auf draft zurück, sein Fenster
-- begann dann erst am Tag 7, und der Rückfall griff nie mehr.
--
--   content_changed_at  Zeit (Unix-Millisekunden) der letzten Änderung des
--                       Inhalts: Anlegen und jede Änderung durch
--                       drafts.update (Empfänger, Betreff, Text, Anhänge,
--                       Kopfzeilen, Schalter des Nachrichtenbaus). Ablauf,
--                       Lesen, Vorlage, Freigabe und Push ändern sie nicht.
--                       Bestand: aus updated_at übernommen.

ALTER TABLE drafts ADD COLUMN content_changed_at INTEGER NOT NULL DEFAULT 0;
UPDATE drafts SET content_changed_at = updated_at;
