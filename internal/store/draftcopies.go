package store

import (
	"context"
	"database/sql"
)

// Die Abfragen nach Kopien mit einer Message-ID im Entwürfe-Ordner filtern
// zusätzlich nach dem Konto des Ordners (m.account_id): Nur so nutzt SQLite
// den Index messages_message_id(account_id, message_id_header) und liest
// nicht alle Nachrichten des Ordners (Gegenprüfung S2.6–S2.8, K6; geprüft
// per EXPLAIN QUERY PLAN in review_r1_test.go). CROSS JOIN legt die
// Reihenfolge fest (messages außen): Sonst wählt SQLite wegen ORDER BY uid
// den Index (mailbox_id, uid) von message_mailboxes. mailboxAccount ist
// das Konto des Ordners mit der ID im ersten Parameter.
const mailboxAccount = `(SELECT account_id FROM mailboxes WHERE id = ?)`

// qMirrorDraftCopies: Parameter Ordner, Ordner, Message-ID.
const qMirrorDraftCopies = `SELECT mm.uid, b.uidvalidity FROM messages m
	CROSS JOIN message_mailboxes mm ON mm.message_id = m.id JOIN mailboxes b ON b.id = mm.mailbox_id
	WHERE mm.mailbox_id = ? AND m.account_id = ` + mailboxAccount + ` AND m.message_id_header = ? ORDER BY mm.uid`

// MirrorDraftCopy ist eine gespiegelte Kopie eines Entwurfs: Ordner, UID
// und die UIDVALIDITY des Ordners beim letzten Sync (S2.7, Entscheid 43).
type MirrorDraftCopy struct {
	MailboxID   int64
	UID         int64
	UIDValidity uint32
}

// MirrorDraftCopies liefert alle Kopien mit der Message-ID messageID (ohne
// <>) in den Ordnern mailboxIDs laut Spiegel, nach Ordner und UID
// aufsteigend. Nach einer Änderung legt der Push eine neue Kopie ab, die
// alte bleibt (kein Expunge im MVP); draft show und draft list zählen damit
// die älteren Kopien. Kopien in anderen Ordnern (z. B. Gmail „All Mail")
// zählen nicht.
func (s *SQLite) MirrorDraftCopies(ctx context.Context, mailboxIDs []int64, messageID string) ([]MirrorDraftCopy, error) {
	var out []MirrorDraftCopy
	for _, mb := range mailboxIDs {
		if mb <= 0 || messageID == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, qMirrorDraftCopies, mb, mb, messageID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c MirrorDraftCopy
			var uv sql.NullInt64
			if err := rows.Scan(&c.UID, &uv); err != nil {
				_ = rows.Close()
				return nil, err
			}
			c.MailboxID, c.UIDValidity = mb, uint32(uv.Int64)
			out = append(out, c)
		}
		err = rows.Err()
		if cerr := rows.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
