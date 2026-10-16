-- PostKauz, Migration 0005: schmale Indizes für die Suche (S1.7).
-- Begründung und Messung: docs/datenmodell.md („Volltextsuche") und
-- docs/suche_messung.md.
--
--   messages_search: Kandidaten der Volltextsuche nachschlagen (Konto,
--     Datum, Filterspalten), ohne die breite Tabellenzeile zu lesen.
--   messages_sortdate: Anfragen nur mit Operatoren (neueste zuerst) laufen
--     den Index entlang und hören nach der Seitengröße auf.
--
-- Der Ausdruck COALESCE(date_header, internal_date, 0) muss mit dem in
-- internal/store/search.go übereinstimmen, sonst nutzt SQLite den Index
-- nicht.

CREATE INDEX messages_search ON messages(id, account_id, COALESCE(date_header, internal_date, 0), has_attachments,
    flag_seen, flag_flagged, flag_answered, flag_draft, thread_id);
CREATE INDEX messages_sortdate ON messages(COALESCE(date_header, internal_date, 0), id, account_id, has_attachments,
    flag_seen, flag_flagged, flag_answered, flag_draft, thread_id);
