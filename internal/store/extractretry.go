package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"
)

// Vorübergehend gescheiterter Textauszug (A11 Teil 3, Befund B5). Ohne
// Schema-Upgrade: Der Anhang bleibt pending, Zahl der Versuche, letzter
// Grund und Zeit stehen in meta unter extract_retry/<Anhang-ID>; der
// Vermerk endet mit dem Abschluss (SetExtraction). Anhänge in der
// Wartezeit schließt schon die Abfrage PendingExtractionsAt aus (sonst
// holte runExtract sie in einer Schleife immer wieder); ExtractPending
// bleibt die volle Zählung (Fortschritt, doctor, Import), und der Job
// bleibt fällig, solange Anhänge warten (CompleteJob zählt jedes
// pending).

const extractRetryPrefix = "extract_retry/"

// extractRequeuePrefix: einmaliges Zurücksetzen vorübergehend
// gescheiterter Auszüge des Bestands je Konto (A11 Teil 3 (b)).
const extractRequeuePrefix = "extract_requeue/"

func extractRetryKey(id int64) string { return extractRetryPrefix + strconv.FormatInt(id, 10) }

// ExtractRetry ist der Vermerk eines vorübergehend gescheiterten Auszugs.
type ExtractRetry struct {
	// N: Zahl der gescheiterten Versuche.
	N int `json:"n"`
	// Reason: letzter Grund (wie extract_error, ohne Inhalt); Kind: Art
	// des Fehlers (start, signal, ntstatus).
	Reason string `json:"reason"`
	Kind   string `json:"kind,omitempty"`
	// At: Zeit des letzten Versuchs; Next: frühester nächster Versuch.
	At   time.Time `json:"at"`
	Next time.Time `json:"next"`
}

// PendingExtractionsAt liefert bis zu limit Anhänge des Kontos mit Status
// pending, älteste zuerst, ohne die, deren nächster Versuch nach now
// liegt (A11 Teil 3: Wartezeit nach einem vorübergehenden Fehler).
func (s *SQLite) PendingExtractionsAt(ctx context.Context, accountID int64, limit int, now time.Time) ([]ExtractItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.message_id, COALESCE(a.sha256, ''), a.content_type, a.size FROM attachments a
		JOIN messages m ON m.id = a.message_id
		WHERE m.account_id = ? AND a.extract_status = 'pending'
		AND NOT EXISTS (SELECT 1 FROM meta mt WHERE mt.key = '`+extractRetryPrefix+`' || a.id
			AND json_valid(mt.value) AND json_extract(mt.value, '$.next_ms') > ?)
		ORDER BY a.id LIMIT ?`, accountID, now.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtractItem
	for rows.Next() {
		var it ExtractItem
		if err := rows.Scan(&it.ID, &it.MessageID, &it.SHA256, &it.ContentType, &it.Size); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// extractRetryRow ist die Form in meta (Zeiten zusätzlich in ms für die
// Abfrage).
type extractRetryRow struct {
	ExtractRetry
	NextMS int64 `json:"next_ms"`
}

// ExtractRetryOf liefert den Vermerk eines Anhangs; ok false: keiner.
func (s *SQLite) ExtractRetryOf(ctx context.Context, id int64) (ExtractRetry, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, extractRetryKey(id)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return ExtractRetry{}, false, nil
	}
	if err != nil {
		return ExtractRetry{}, false, err
	}
	var r extractRetryRow
	if json.Unmarshal([]byte(v), &r) != nil || r.N <= 0 {
		return ExtractRetry{}, false, nil
	}
	return r.ExtractRetry, true, nil
}

// DeferExtraction vermerkt einen vorübergehend gescheiterten Auszug: Der
// Anhang bleibt pending, der Vermerk nennt Zahl, Grund und Zeit.
func (s *SQLite) DeferExtraction(ctx context.Context, id int64, r ExtractRetry) error {
	b, err := json.Marshal(extractRetryRow{ExtractRetry: ExtractRetry{N: r.N, Reason: r.Reason, Kind: r.Kind, At: r.At.UTC(),
		Next: r.Next.UTC()}, NextMS: r.Next.UnixMilli()})
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `UPDATE attachments SET extract_status = 'pending', extract_error = NULL WHERE id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		extractRetryKey(id), string(b)); err != nil {
		return err
	}
	return tx.Commit()
}

// TransientExtractPatterns erkennen in extract_error einen vorübergehenden
// Grund (das Hilfsprogramm lief nicht; A11 Teil 3 (b)): Präfix bzw.
// enthaltene Zeichenfolgen. extract.IsTransientReason nutzt dieselben.
var (
	TransientExtractPrefix   = "extractor failed: exit status 0xc"
	TransientExtractContains = []string{"fork/exec", "exec:", "signal:"}
)

// RequeueTransientFailed setzt einmal je Konto (Vermerk in meta) Anhänge
// mit failed wieder auf pending, deren extract_error einen vorübergehenden
// Grund nennt (A11 Teil 3 (b), Bestand vor dem Programmwechsel). Liefert
// die Zahl; ran meldet, ob das Zurücksetzen jetzt lief (false: schon
// erledigt).
func (s *SQLite) RequeueTransientFailed(ctx context.Context, accountID int64) (n int, ran bool, err error) {
	key := extractRequeuePrefix + strconv.FormatInt(accountID, 10)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM meta WHERE key = ?)`, key).Scan(&done); err != nil {
		return 0, false, err
	}
	if done {
		err = tx.Rollback()
		return 0, false, err
	}
	cond := `extract_error LIKE ? ESCAPE '\'`
	args := []any{likePrefix(TransientExtractPrefix)}
	for _, c := range TransientExtractContains {
		cond += ` OR instr(extract_error, ?) > 0`
		args = append(args, c)
	}
	args = append(args, accountID)
	res, err := tx.ExecContext(ctx, `UPDATE attachments SET extract_status = 'pending', extract_error = NULL
		WHERE extract_status = 'failed' AND extract_error IS NOT NULL AND (`+cond+`)
		AND message_id IN (SELECT id FROM messages WHERE account_id = ?)`, args...)
	if err != nil {
		return 0, false, err
	}
	m, _ := res.RowsAffected()
	if _, err = tx.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?)`, key, strconv.FormatInt(time.Now().UnixMilli(), 10)); err != nil {
		return 0, false, err
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return int(m), true, nil
}

// likePrefix maskiert % _ \ für LIKE … ESCAPE '\' und hängt % an.
func likePrefix(s string) string {
	out := make([]rune, 0, len(s)+1)
	for _, r := range s {
		if r == '%' || r == '_' || r == '\\' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	return string(out) + "%"
}
