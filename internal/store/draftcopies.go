package store

import (
	"context"
	"database/sql"
)

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
		rows, err := s.db.QueryContext(ctx, `SELECT mm.uid, b.uidvalidity FROM message_mailboxes mm
			JOIN messages m ON m.id = mm.message_id JOIN mailboxes b ON b.id = mm.mailbox_id
			WHERE mm.mailbox_id = ? AND m.message_id_header = ? ORDER BY mm.uid`, mb, messageID)
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
