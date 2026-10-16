-- PostKauz, Migration 0006: Abweichung zwischen Klartext und HTML.
--
-- Bei multipart/alternative liest der Agent den Klartext, der Mensch sieht
-- das HTML. Weichen beide wesentlich ab (sanitize.AlternativeMismatch),
-- steht hier 1; messages.get, threads.get, `show` und MCP melden es wie
-- hidden_text. Gesetzt ab mailsync.CleanVersion 3; der nächste Sync bereitet
-- älteren Bestand aus der Rohmail neu auf.

ALTER TABLE bodies ADD COLUMN alternative_mismatch INTEGER NOT NULL DEFAULT 0 CHECK (alternative_mismatch IN (0,1));
