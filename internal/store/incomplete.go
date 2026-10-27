package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// IncompleteAnswers zählt die leeren oder unvollständigen Serverantworten
// eines Kontos (T1a Punkt 3, K2): LIST ohne INBOX, UID SEARCH ALL mit
// weniger oder mehr UIDs als EXISTS. Gespeichert in meta (Schlüssel
// incomplete_answers/<konto-id>, keine Migration), damit doctor sie zeigt.
type IncompleteAnswers struct {
	// Total: alle bisher gezählten; Last: im letzten Lauf mit solchen
	// Antworten; LastAt: Zeitpunkt dieses Laufs (UTC).
	Total  int       `json:"total"`
	Last   int       `json:"last"`
	LastAt time.Time `json:"last_at"`
}

func incompleteKey(accountID int64) string { return fmt.Sprintf("incomplete_answers/%d", accountID) }

// RecordIncompleteAnswers addiert n (> 0) unvollständige Antworten eines
// Laufs zum Zähler des Kontos.
func (s *SQLite) RecordIncompleteAnswers(ctx context.Context, accountID int64, n int, at time.Time) error {
	if n <= 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var ia IncompleteAnswers
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, incompleteKey(accountID)).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	default:
		_ = json.Unmarshal([]byte(raw), &ia) // ein unlesbarer Wert beginnt neu
	}
	ia.Total += n
	ia.Last = n
	ia.LastAt = at.UTC().Truncate(time.Second)
	b, err := json.Marshal(ia)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, incompleteKey(accountID), string(b)); err != nil {
		return err
	}
	return tx.Commit()
}

// IncompleteAnswersByAccount liefert die Zähler je Kontoname (nur Konten
// mit gezählten Antworten).
func (s *SQLite) IncompleteAnswersByAccount(ctx context.Context) (map[string]IncompleteAnswers, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.name, m.value FROM accounts a
		JOIN meta m ON m.key = 'incomplete_answers/' || a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]IncompleteAnswers{}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		var ia IncompleteAnswers
		if json.Unmarshal([]byte(raw), &ia) == nil {
			out[name] = ia
		}
	}
	return out, rows.Err()
}
