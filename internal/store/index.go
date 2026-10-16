package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
)

// Volltextindex (docs/datenmodell.md, „Volltextsuche"). Die Spalten
// berechnet internal/search (Falten, Stemmen); hier werden sie gelesen und
// geschrieben. Die Warteschlange index_queue füllen Trigger (Migration
// 0004); IndexBatch arbeitet sie in Transaktionen ab.

// IndexBatch indiziert bis zu limit eingereihte Nachrichten des Kontos
// (accountID 0 = alle Konten) in einer Transaktion: Text lesen, mit build
// in Spalten umrechnen, Zeilen beider FTS-Tabellen ersetzen, aus der
// Warteschlange nehmen. Lesen und Schreiben in derselben Transaktion: Eine
// Änderung dazwischen kann nicht verloren gehen. Liefert die Zahl der
// indizierten Nachrichten (0 = Warteschlange leer).
func (s *SQLite) IndexBatch(ctx context.Context, accountID int64, limit int, build func(search.Source) search.Doc) (n int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	q := `SELECT q.message_id, q.reindex, m.subject, m.from_addr, m.to_addrs, m.cc_addrs,
		COALESCE(b.text_plain, ''), COALESCE(b.text_from_html, '')
		FROM index_queue q JOIN messages m ON m.id = q.message_id LEFT JOIN bodies b ON b.message_id = m.id`
	args := []any{}
	if accountID > 0 {
		q += ` WHERE m.account_id = ?`
		args = append(args, accountID)
	}
	q += ` ORDER BY q.message_id LIMIT ?`
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	type item struct {
		id      int64
		reindex bool
		src     search.Source
	}
	var items []item
	for rows.Next() {
		var it item
		var reindex int
		var to, cc, plain, html string
		if err = rows.Scan(&it.id, &reindex, &it.src.Subject, &it.src.From, &to, &cc, &plain, &html); err != nil {
			_ = rows.Close()
			return 0, err
		}
		it.reindex = reindex == 1
		it.src.To, it.src.Cc = json.RawMessage(to), json.RawMessage(cc)
		it.src.Body = plain
		if strings.TrimSpace(plain) == "" {
			it.src.Body = html
		}
		items = append(items, it)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, tx.Commit()
	}
	// Anhänge: Dateinamen und extrahierter Text (nur erfolgreich extrahiert).
	idx := make(map[int64]int, len(items))
	ids := make([]string, len(items))
	for i, it := range items {
		idx[it.id] = i
		ids[i] = strconv.FormatInt(it.id, 10)
	}
	arows, err := tx.QueryContext(ctx, `SELECT message_id, COALESCE(filename, ''),
		CASE WHEN extract_status = 'ok' THEN COALESCE(extracted_text, '') ELSE '' END
		FROM attachments WHERE message_id IN (`+strings.Join(ids, ",")+`) ORDER BY message_id, id`)
	if err != nil {
		return 0, err
	}
	for arows.Next() {
		var mid int64
		var name, text string
		if err = arows.Scan(&mid, &name, &text); err != nil {
			_ = arows.Close()
			return 0, err
		}
		it := &items[idx[mid]]
		for _, s := range []string{name, text} {
			if strings.TrimSpace(s) != "" {
				it.src.Attachments = append(it.src.Attachments, s)
			}
		}
	}
	if err = arows.Close(); err != nil {
		return 0, err
	}

	cols := strings.Join(search.ColNames[:], ", ")
	insWord, err := tx.PrepareContext(ctx, `INSERT INTO fts_messages(rowid, `+cols+`) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer insWord.Close()
	insTri, err := tx.PrepareContext(ctx, `INSERT INTO fts_trigram(rowid, `+cols+`) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return 0, err
	}
	defer insTri.Close()
	for _, it := range items {
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		d := build(it.src)
		if it.reindex {
			for _, del := range []string{`DELETE FROM fts_messages WHERE rowid = ?`, `DELETE FROM fts_trigram WHERE rowid = ?`} {
				if _, err = tx.ExecContext(ctx, del, it.id); err != nil {
					return 0, err
				}
			}
		}
		if _, err = insWord.ExecContext(ctx, it.id, d.Word[0], d.Word[1], d.Word[2], d.Word[3], d.Word[4]); err != nil {
			return 0, err
		}
		if _, err = insTri.ExecContext(ctx, it.id, d.Tri[0], d.Tri[1], d.Tri[2], d.Tri[3], d.Tri[4]); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM index_queue WHERE message_id = ?`, it.id); err != nil {
			return 0, err
		}
	}
	return len(items), tx.Commit()
}

// IndexPending zählt eingereihte Nachrichten (accountID 0 = alle Konten).
func (s *SQLite) IndexPending(ctx context.Context, accountID int64) (int, error) {
	var n int
	var err error
	if accountID > 0 {
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM index_queue q JOIN messages m ON m.id = q.message_id
			WHERE m.account_id = ?`, accountID).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM index_queue`).Scan(&n)
	}
	return n, err
}

// ExtractPending zählt Anhänge mit ausstehender Textextraktion
// (accountID 0 = alle Konten).
func (s *SQLite) ExtractPending(ctx context.Context, accountID int64) (int, error) {
	var n int
	var err error
	if accountID > 0 {
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments a JOIN messages m ON m.id = a.message_id
			WHERE a.extract_status = 'pending' AND m.account_id = ?`, accountID).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM attachments WHERE extract_status = 'pending'`).Scan(&n)
	}
	return n, err
}

// FTSVersion liefert meta.fts_version (0 = nicht gesetzt).
func (s *SQLite) FTSVersion(ctx context.Context) (int, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'fts_version'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(v)
	return n, nil
}

// EnsureFTSVersion baut den Index neu auf, wenn meta.fts_version nicht
// version ist (Normalisierer, Stemmer oder Spalten geändert): beide
// Tabellen leeren, alle Nachrichten einreihen (die Trigger legen die
// index-Jobs an), Version setzen; alles in einer Transaktion. rebuilt
// meldet, ob das nötig war.
func (s *SQLite) EnsureFTSVersion(ctx context.Context, version int) (rebuilt bool, err error) {
	have, err := s.FTSVersion(ctx)
	if err != nil || have == version {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UnixMilli()
	for _, q := range []string{
		`INSERT INTO fts_messages(fts_messages) VALUES ('delete-all')`,
		`INSERT INTO fts_trigram(fts_trigram) VALUES ('delete-all')`,
		`UPDATE index_queue SET reindex = 0`,
	} {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return false, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO index_queue(message_id, reindex, queued_at)
		SELECT id, 0, ? FROM messages WHERE id NOT IN (SELECT message_id FROM index_queue)`, now); err != nil {
		return false, err
	}
	// Konten mit eingereihten Nachrichten ohne offenen index-Job (z. B. nach
	// einem endgültig gescheiterten Job) bekommen einen.
	if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(type, payload, state, run_at)
		SELECT 'index', json_object('account_id', account_id), 'queued', ?
		FROM (SELECT DISTINCT m.account_id AS account_id FROM index_queue q JOIN messages m ON m.id = q.message_id) p
		WHERE NOT EXISTS (SELECT 1 FROM jobs WHERE type = 'index' AND state IN ('queued','running')
		                  AND json_extract(payload, '$.account_id') = p.account_id)`, now); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES ('fts_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, strconv.Itoa(version)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
