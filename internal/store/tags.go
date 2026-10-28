package store

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Tags (E5, Migration 0020): lokale Etiketten je Konto. Sie überleben
// „Server gewinnt“ und das Löschen auf dem Server; Keywords vom Server
// bleiben Keywords (messages.keywords), keine Tags.

// TagChange ändert die Tags von Nachrichten eines Kontos.
type TagChange struct {
	AccountID  int64
	MessageIDs []int64
	Add        []string
	Remove     []string
	// SetBy: "cli" oder "key:<name>" wie im Audit.
	SetBy string
	Now   time.Time
}

// TagResult nennt je Nachricht die Tags nach der Änderung und was sich
// geändert hat.
type TagResult struct {
	Tags    map[int64][]string
	Added   int
	Removed int
}

// ChangeTags setzt und entfernt Tags in einer Transaktion. Unbekannte Tags
// werden beim Setzen angelegt (CHECK der Namen greift, kein OR IGNORE); ein Tag ohne Nachricht bleibt bestehen
// (tag list zeigt ihn mit 0).
func (s *SQLite) ChangeTags(ctx context.Context, c TagChange) (r TagResult, err error) {
	if c.Now.IsZero() {
		c.Now = time.Now()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := c.Now.UnixMilli()
	for _, name := range c.Add {
		if _, err = tx.ExecContext(ctx, `INSERT INTO tags(account_id, name, created_at) VALUES (?,?,?) ON CONFLICT(account_id, name) DO NOTHING`, c.AccountID, name, now); err != nil {
			return r, fmt.Errorf("store: tag %q: %w", name, err)
		}
		for _, mid := range c.MessageIDs {
			res, err2 := tx.ExecContext(ctx, `INSERT INTO message_tags(message_id, tag_id, set_by, set_at)
				SELECT ?, id, ?, ? FROM tags WHERE account_id = ? AND name = ? ON CONFLICT(message_id, tag_id) DO NOTHING`, mid, c.SetBy, now, c.AccountID, name)
			if err2 != nil {
				err = err2
				return r, err
			}
			n, _ := res.RowsAffected()
			r.Added += int(n)
		}
	}
	for _, name := range c.Remove {
		for _, mid := range c.MessageIDs {
			res, err2 := tx.ExecContext(ctx, `DELETE FROM message_tags WHERE message_id = ?
				AND tag_id = (SELECT id FROM tags WHERE account_id = ? AND name = ?)`, mid, c.AccountID, name)
			if err2 != nil {
				err = err2
				return r, err
			}
			n, _ := res.RowsAffected()
			r.Removed += int(n)
		}
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	r.Tags, err = s.MessageTags(ctx, c.MessageIDs)
	return r, err
}

// MessageTags liefert die Tags je Nachricht, alphabetisch (Nachrichten
// ohne Tag mit leerer Liste).
func (s *SQLite) MessageTags(ctx context.Context, ids []int64) (map[int64][]string, error) {
	out := make(map[int64][]string, len(ids))
	for _, id := range ids {
		out[id] = []string{}
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT mt.message_id, t.name FROM message_tags mt JOIN tags t ON t.id = mt.tag_id
			WHERE mt.message_id IN (?`+strings.Repeat(",?", len(chunk)-1)+`) ORDER BY t.name`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			var name string
			if err := rows.Scan(&id, &name); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = append(out[id], name)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	for id := range out {
		slices.Sort(out[id])
	}
	return out, nil
}

// TagCount ist ein Tag mit der Zahl seiner Nachrichten.
type TagCount struct {
	Name     string
	Messages int
}

// ListTags liefert die Tags eines Kontos mit Zahl der Nachrichten.
func (s *SQLite) ListTags(ctx context.Context, accountID int64) ([]TagCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.name, count(mt.message_id) FROM tags t
		LEFT JOIN message_tags mt ON mt.tag_id = t.id WHERE t.account_id = ? GROUP BY t.id ORDER BY t.name`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TagCount
	for rows.Next() {
		var c TagCount
		if err := rows.Scan(&c.Name, &c.Messages); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SetAccountDraftsMailbox setzt den Entwürfe-Ordner eines Kontos (A5.1;
// "" = keiner, dann gilt [draft] mailbox bzw. die Rolle drafts).
func (s *SQLite) SetAccountDraftsMailbox(ctx context.Context, id int64, path string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET drafts_mailbox = ? WHERE id = ?`, nullStr(path), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
