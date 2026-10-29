package store

// IDs ohne Wiederverwendung (A14, Migration 0022): messages, mailboxes,
// attachments und threads haben INTEGER PRIMARY KEY ohne AUTOINCREMENT.
// SQLite vergäbe nach dem Löschen der größten Zeile deren ID neu; Aktionen,
// Audit-Ziele (message:<id>, attachment:<id>, thread:<id>) und
// Entwurfsverweise zeigten dann auf etwas anderes. Neue Zeilen bekommen
// deshalb ausdrücklich eine ID oberhalb von max(größte ID, Höchststand in
// meta); den Höchststand hebt ein Trigger beim Löschen an. attachments und
// threads kamen mit der Nachbesserung T3 (Punkt 8) dazu: Anhänge fallen mit
// purge und beim Ersetzen der Teile einer Nachricht weg, Threads mit purge
// und beim Zusammenführen.

// nextIDExpr liefert den SQL-Ausdruck für die nächste ID einer Tabelle mit
// Höchststand in meta (nur die festen Namen unten, nie Eingaben).
func nextIDExpr(table, floorKey string) string {
	return `(SELECT max(coalesce((SELECT max(id) FROM ` + table + `), 0),
		coalesce((SELECT CAST(value AS INTEGER) FROM meta WHERE key = '` + floorKey + `'), 0)) + 1)`
}

var (
	nextMessageID    = nextIDExpr("messages", "id_floor_messages")
	nextMailboxID    = nextIDExpr("mailboxes", "id_floor_mailboxes")
	nextAttachmentID = nextIDExpr("attachments", "id_floor_attachments")
	nextThreadID     = nextIDExpr("threads", "id_floor_threads")
)
