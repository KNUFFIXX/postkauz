-- PostKauz, Migration 0011: Schalter des Nachrichtenbaus je Entwurf (S2.3,
-- Auftrag A02, Entscheide E3, E11).
--
-- drafts.create und drafts.update bauen die Nachricht mit internal/compose.
-- Eine Änderung einzelner Felder (draft edit --subject …) baut sie neu aus
-- den gespeicherten Eingaben; dafür muss bekannt sein, ob der Entwurf mit
-- Zitat und Signatur angelegt wurde (quote, signature; Standard jeweils
-- an). Die Schalter stehen als JSON-Objekt hier, nicht in headers_json
-- (das sind Kopfzeilen) und nicht im preview_hash (sie wirken nur über
-- body_text, der im Hash steht). Kein Mailinhalt.

ALTER TABLE drafts ADD COLUMN compose_options TEXT NOT NULL DEFAULT '{}'
    CHECK (json_valid(compose_options) AND json_type(compose_options) = 'object');
