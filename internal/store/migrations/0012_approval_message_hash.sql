-- PostKauz, Migration 0012: Die Freigabe bindet die fertige Nachricht
-- (Gegenprüfung S2.3, Befund W1; Auftrag A02, Entscheid E7).
--
-- preview_hash deckt nur die gespeicherten Felder eines Entwurfs ab. Was
-- beim Nachrichtenbau hinzukommt (Signatur und Reply-To der Identität, das
-- Zitat aus der Ausgangsnachricht, das aus Markdown erzeugte HTML, der
-- Inhalt der Anhänge, die Schalter quote/signature), sah der Mensch zwar,
-- band die Freigabe aber nicht. message_hash ist SHA-256 über die mit
-- internal/compose gebaute Nachricht ohne die Date-Kopfzeile
-- (compose.MessageHash): drafts.request_ready baut neu und speichert ihn,
-- drafts.approve baut neu und vergleicht (Abweichung: approval_stale), ab
-- S2.5 auch der Push. Ältere Freigaben haben '' und sind damit nicht mehr
-- freizugeben (neu vorlegen). Kein Mailinhalt, nur ein Hash.

ALTER TABLE approvals ADD COLUMN message_hash TEXT NOT NULL DEFAULT '';
