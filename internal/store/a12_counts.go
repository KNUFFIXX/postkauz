package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Zähler für doctor (A12 Teil 3 (d)) und Auswahl für attach retry (A12
// Teil 3 (c)). Nur Spalten und Vermerke, die es seit Schema 22 gibt.

// ExtractCounts sind die Zähler des Textauszugs eines Kontos.
type ExtractCounts struct {
	// Failed: Anhänge mit extract_status failed; FailedExhausted davon mit
	// vorübergehendem Grund nach mehreren Versuchen („(n attempts)“).
	Failed          int
	FailedExhausted int
	// Waiting: pending in der Wartezeit nach einem vorübergehenden Fehler.
	Waiting int
	// SkippedTool: skipped mit einem Grund des PDF-Programms (reasons).
	SkippedTool int
}

// ExtractCountsOf zählt für das Konto accountID; toolReasons sind die
// Gründe „PDF-Programm fehlt oder ist falsch“ (extract.ToolReasons).
func (s *SQLite) ExtractCountsOf(ctx context.Context, accountID int64, toolReasons []string, now time.Time) (ExtractCounts, error) {
	var c ExtractCounts
	args := []any{now.UnixMilli()}
	for _, r := range toolReasons {
		args = append(args, r)
	}
	args = append(args, accountID)
	in := "NULL"
	if len(toolReasons) > 0 {
		in = placeholders(len(toolReasons))
	}
	err := s.db.QueryRowContext(ctx, `SELECT
		COALESCE(sum(a.extract_status = 'failed'), 0),
		COALESCE(sum(a.extract_status = 'failed' AND a.extract_error LIKE '% attempts)'), 0),
		COALESCE(sum(a.extract_status = 'pending' AND EXISTS (SELECT 1 FROM meta mt WHERE mt.key = '`+extractRetryPrefix+`' || a.id
			AND json_valid(mt.value) AND json_extract(mt.value, '$.next_ms') > ?)), 0),
		COALESCE(sum(a.extract_status = 'skipped' AND a.extract_error IN (`+in+`)), 0)
		FROM attachments a JOIN messages m ON m.id = a.message_id WHERE m.account_id = ?`, args...).
		Scan(&c.Failed, &c.FailedExhausted, &c.Waiting, &c.SkippedTool)
	if err != nil {
		return c, fmt.Errorf("store: extract counts: %w", err)
	}
	return c, nil
}

// GivenUpCopies liefert die IDs der offenen Entwürfe des Kontos, von denen
// eine frühere eigene Kopie nach maxAttempts Versuchen des Nachlaufs noch
// im Spiegel liegt (A12 Teil 2 (b), Teil 3 (d)).
func (s *SQLite) GivenUpCopies(ctx context.Context, accountID int64, maxAttempts int) ([]int64, error) {
	counts, err := s.CleanupCounts(ctx)
	if err != nil {
		return nil, err
	}
	exhausted := map[RemoteCopy]bool{}
	for _, n := range counts {
		if n.N >= maxAttempts {
			exhausted[RemoteCopy{MailboxID: n.Copy.MailboxID, UIDValidity: n.Copy.UIDValidity, UID: n.Copy.UID}] = true
		}
	}
	if len(exhausted) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, remote_previous FROM drafts WHERE account_id = ? AND status NOT IN `+closedStatuses+`
		AND json_array_length(remote_previous) > 0 ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	type cand struct {
		id   int64
		prev []RemoteCopy
	}
	var list []cand
	for rows.Next() {
		var c cand
		var prev string
		if err := rows.Scan(&c.id, &prev); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if json.Unmarshal([]byte(prev), &c.prev) == nil {
			list = append(list, c)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var out []int64
	for _, c := range list {
		for _, p := range c.prev {
			if !exhausted[RemoteCopy{MailboxID: p.MailboxID, UIDValidity: p.UIDValidity, UID: p.UID}] {
				continue
			}
			sc, _, ok, err := s.CopyAt(ctx, p.MailboxID, uint32(p.UID))
			if err != nil {
				return nil, err
			}
			if ok && sc.UIDValidity == p.UIDValidity {
				out = append(out, c.id)
				break
			}
		}
	}
	return out, nil
}

// RetrySelect wählt Anhänge mit extract_status failed für attach retry
// (A12 Teil 3 (c)): im Konto accountID, dazu optional eine Nachricht
// (messageID > 0) oder Anhänge (ids); all: alle des Kontos.
type RetrySelect struct {
	AccountID   int64
	MessageID   int64
	Attachments []int64
	All         bool
}

func (r RetrySelect) where() (string, []any) {
	q := `a.extract_status = 'failed' AND a.message_id IN (SELECT id FROM messages WHERE account_id = ?)`
	args := []any{r.AccountID}
	if r.MessageID > 0 {
		q += ` AND a.message_id = ?`
		args = append(args, r.MessageID)
	}
	if len(r.Attachments) > 0 {
		q += ` AND a.id IN (` + placeholders(len(r.Attachments)) + `)`
		for _, id := range r.Attachments {
			args = append(args, id)
		}
	}
	return q, args
}

// CountFailedExtractions zählt die Auswahl (Trockenlauf).
func (s *SQLite) CountFailedExtractions(ctx context.Context, r RetrySelect) (int, error) {
	w, args := r.where()
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments a WHERE `+w, args...).Scan(&n)
	return n, err
}

// RetryFailedExtractions setzt die Auswahl auf pending und löscht ihre
// Vermerke extract_retry/<id> (eine Transaktion); liefert die Zahl.
func (s *SQLite) RetryFailedExtractions(ctx context.Context, r RetrySelect) (int, error) {
	w, args := r.where()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT a.id FROM attachments a WHERE `+w, args...)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		a := make([]any, len(chunk))
		keys := make([]any, len(chunk))
		for i, id := range chunk {
			a[i], keys[i] = id, extractRetryKey(id)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE attachments SET extract_status = 'pending', extract_error = NULL
			WHERE extract_status = 'failed' AND id IN (`+placeholders(len(chunk))+`)`, a...); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM meta WHERE key IN (`+placeholders(len(chunk))+`)`, keys...); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(ids), nil
}
