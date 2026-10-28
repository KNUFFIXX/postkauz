-- PostKauz, Migration 0021 (Nachbesserung T2, V13): mailboxes.virtual_view.
--
-- 1 = virtuelle Sicht, die nur Nachrichten anderer Ordner zeigt (Gmail
-- „Markiert“/„Wichtig“, SPECIAL-USE \Flagged/\Important). Sie ist nie Quelle
-- von move/trash ohne ausdrücklich genannten Ordner. Gesetzt vom
-- Ordnerabgleich; der Bestand bekommt den Wert hier aus der
-- Gmail-Namenszuordnung (die Attribute kennt erst der nächste Abgleich).
--
-- Eigene Migration statt Nachtrag in 0020: Der Läufer vergleicht nur
-- Nummern; eine Datenbank, die 0.3.0-alpha.4 schon auf Schema 20 gebracht
-- hat, bekäme einen Nachtrag in 0020 nie (Nachprüfung der Prüfsitzung zu
-- fd70f19, Punkt 1). Keine Zeile wird verworfen oder inhaltlich geändert.

ALTER TABLE mailboxes ADD COLUMN virtual_view INTEGER NOT NULL DEFAULT 0 CHECK (virtual_view IN (0,1));
UPDATE mailboxes SET virtual_view = 1
 WHERE lower(path) IN ('[gmail]/starred', '[gmail]/markiert', '[gmail]/important', '[gmail]/wichtig',
                       '[google mail]/starred', '[google mail]/markiert', '[google mail]/important', '[google mail]/wichtig');
