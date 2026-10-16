package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Bereinigte Texte und Anhänge (S1.6).

// upsertBody schreibt den Text einer Nachricht (neu oder überschreibend).
func upsertBody(ctx context.Context, q querier, mid int64, n NewMessage) error {
	_, err := q.ExecContext(ctx, `INSERT INTO bodies(message_id, text_plain, text_from_html, hidden_text, charset, text_unquoted,
		links, clean_version) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(message_id) DO UPDATE SET text_plain = excluded.text_plain, text_from_html = excluded.text_from_html,
		hidden_text = excluded.hidden_text, charset = excluded.charset, text_unquoted = excluded.text_unquoted,
		links = excluded.links, clean_version = excluded.clean_version`,
		mid, n.TextPlain, n.TextHTML, b2i(n.HiddenText), nullStr(n.TextCharset), n.TextUnquoted, jsonOr(n.Links, "[]"), n.CleanVersion)
	if err != nil {
		return fmt.Errorf("store: body %d: %w", mid, err)
	}
	return nil
}

// upsertAttachments schreibt die Anhänge einer Nachricht. replace: Teile,
// die es nicht mehr gibt, werden gelöscht; ein Teil mit unverändertem
// Inhalt behält sein Extraktionsergebnis.
func upsertAttachments(ctx context.Context, q querier, mid int64, atts []NewAttachment, now int64, replace bool) error {
	keep := make([]any, 0, len(atts)+1)
	keep = append(keep, mid)
	for _, a := range atts {
		if a.SHA256 != "" {
			if _, err := q.ExecContext(ctx, `INSERT INTO blobs(sha256, size, compressed, created_at) VALUES (?,?,?,?)
				ON CONFLICT(sha256) DO NOTHING`, a.SHA256, a.BlobSize, b2i(a.BlobCompressed), now); err != nil {
				return fmt.Errorf("store: attachment blob: %w", err)
			}
		}
		status := a.ExtractStatus
		if status == "" {
			status = "pending"
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO attachments(message_id, part_id, filename, content_type, declared_type, size,
			sha256, inline, content_id, truncated, type_mismatch, mismatch_reason, executable, extract_status, extract_error)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(message_id, part_id) DO UPDATE SET filename = excluded.filename, content_type = excluded.content_type,
			declared_type = excluded.declared_type, size = excluded.size, inline = excluded.inline, content_id = excluded.content_id,
			truncated = excluded.truncated, type_mismatch = excluded.type_mismatch, mismatch_reason = excluded.mismatch_reason,
			executable = excluded.executable,
			extract_status = CASE WHEN attachments.sha256 IS excluded.sha256 AND attachments.extract_status IN ('ok','failed')
				THEN attachments.extract_status ELSE excluded.extract_status END,
			extracted_text = CASE WHEN attachments.sha256 IS excluded.sha256 THEN attachments.extracted_text ELSE NULL END,
			extract_error = CASE WHEN attachments.sha256 IS excluded.sha256 AND attachments.extract_status IN ('ok','failed')
				THEN attachments.extract_error ELSE excluded.extract_error END,
			hidden_text = CASE WHEN attachments.sha256 IS excluded.sha256 THEN attachments.hidden_text ELSE 0 END,
			sha256 = excluded.sha256`,
			mid, a.PartID, nullStr(a.Filename), a.ContentType, nullStr(a.DeclaredType), a.Size, nullStr(a.SHA256),
			b2i(a.Inline), nullStr(a.ContentID), b2i(a.Truncated), b2i(a.TypeMismatch), nullStr(a.MismatchReason),
			b2i(a.Executable), status, nullStr(a.ExtractError)); err != nil {
			return fmt.Errorf("store: attachment %s: %w", a.PartID, err)
		}
		keep = append(keep, a.PartID)
	}
	if replace {
		cond := ""
		if len(keep) > 1 {
			cond = ` AND part_id NOT IN (` + placeholders(len(keep)-1) + `)`
		}
		if _, err := q.ExecContext(ctx, `DELETE FROM attachments WHERE message_id = ?`+cond, keep...); err != nil {
			return err
		}
	}
	return nil
}

// ReprocessItem ist eine Nachricht, deren Aufbereitung älter ist als die
// des Programms (oder die noch keine hat).
type ReprocessItem struct {
	ID           int64
	BlobSHA256   string
	InternalDate time.Time
}

// ReprocessCandidates liefert bis zu limit Nachrichten des Kontos mit
// Rohmail, deren clean_version kleiner als version ist.
func (s *SQLite) ReprocessCandidates(ctx context.Context, accountID int64, version, limit int) ([]ReprocessItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.blob_sha256, m.internal_date FROM messages m
		LEFT JOIN bodies b ON b.message_id = m.id
		WHERE m.account_id = ? AND m.blob_sha256 IS NOT NULL AND COALESCE(b.clean_version, -1) < ?
		ORDER BY m.id LIMIT ?`, accountID, version, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReprocessItem
	for rows.Next() {
		var it ReprocessItem
		var idate sql.NullInt64
		if err := rows.Scan(&it.ID, &it.BlobSHA256, &idate); err != nil {
			return nil, err
		}
		if idate.Valid {
			it.InternalDate = time.UnixMilli(idate.Int64).UTC()
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// Reprocessed ist eine neu aufbereitete Nachricht. Body enthält die
// Textfelder und Anhänge (TextPlain … Attachments, CleanVersion); ist
// KeepBody gesetzt (Rohmail unlesbar), wird nur clean_version gesetzt.
type Reprocessed struct {
	MessageID int64
	Body      NewMessage
	KeepBody  bool
}

// SaveReprocessed schreibt neu aufbereitete Nachrichten in einer
// Transaktion.
func (s *SQLite) SaveReprocessed(ctx context.Context, items []Reprocessed) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UnixMilli()
	for _, it := range items {
		if it.KeepBody {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bodies(message_id, clean_version) VALUES (?, ?)
				ON CONFLICT(message_id) DO UPDATE SET clean_version = excluded.clean_version`, it.MessageID, it.Body.CleanVersion); err != nil {
				return err
			}
			continue
		}
		if err := upsertBody(ctx, tx, it.MessageID, it.Body); err != nil {
			return err
		}
		if err := upsertAttachments(ctx, tx, it.MessageID, it.Body.Attachments, now, true); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE messages SET has_attachments = ?, snippet = ? WHERE id = ?`,
			b2i(hasRealAttachments(it.Body.Attachments)), it.Body.Snippet, it.MessageID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func hasRealAttachments(atts []NewAttachment) bool {
	for _, a := range atts {
		if !a.Inline {
			return true
		}
	}
	return false
}

// ExtractItem ist ein Anhang, dessen Text noch aussteht.
type ExtractItem struct {
	ID          int64
	SHA256      string
	ContentType string
	Size        int64
}

// PendingExtractions liefert bis zu limit Anhänge des Kontos mit Status
// pending (älteste zuerst).
func (s *SQLite) PendingExtractions(ctx context.Context, accountID int64, limit int) ([]ExtractItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, COALESCE(a.sha256, ''), a.content_type, a.size FROM attachments a
		JOIN messages m ON m.id = a.message_id
		WHERE m.account_id = ? AND a.extract_status = 'pending' ORDER BY a.id LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtractItem
	for rows.Next() {
		var it ExtractItem
		if err := rows.Scan(&it.ID, &it.SHA256, &it.ContentType, &it.Size); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// RequeueSkipped setzt übersprungene Anhänge des Kontos mit genau diesem
// Grund wieder auf pending (z. B. „pdftotext is not installed", nachdem
// das Werkzeug installiert wurde); der Trigger legt den extract-Job an.
func (s *SQLite) RequeueSkipped(ctx context.Context, accountID int64, reason string) (int, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE attachments SET extract_status = 'pending', extract_error = NULL
		WHERE extract_status = 'skipped' AND extract_error = ?
		  AND message_id IN (SELECT id FROM messages WHERE account_id = ?)`, reason, accountID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// SetExtraction speichert das Ergebnis einer Extraktion.
func (s *SQLite) SetExtraction(ctx context.Context, id int64, status, text, errText string, hidden bool) error {
	var t any
	if status == "ok" {
		t = text
	}
	_, err := s.db.ExecContext(ctx, `UPDATE attachments SET extract_status = ?, extracted_text = ?, extract_error = ?,
		hidden_text = ? WHERE id = ?`, status, t, nullStr(errText), b2i(hidden), id)
	return err
}

// ErrAttachmentNotFound: Anhang unbekannt.
var ErrAttachmentNotFound = errors.New("store: attachment not found")

// Attachment ist ein gespeicherter Anhang (ohne Text).
type Attachment struct {
	ID             int64
	MessageID      int64
	AccountID      int64
	PartID         string
	Filename       string
	ContentType    string
	DeclaredType   string
	Size           int64
	SHA256         string
	Inline         bool
	Truncated      bool
	TypeMismatch   bool
	MismatchReason string
	Executable     bool
	ExtractStatus  string
	ExtractError   string
	HiddenText     bool
}

const attachmentCols = `a.id, a.message_id, m.account_id, a.part_id, COALESCE(a.filename, ''), a.content_type,
	COALESCE(a.declared_type, ''), a.size, COALESCE(a.sha256, ''), a.inline, a.truncated, a.type_mismatch,
	COALESCE(a.mismatch_reason, ''), a.executable, a.extract_status, COALESCE(a.extract_error, ''), a.hidden_text`

func scanAttachment(sc interface{ Scan(...any) error }) (Attachment, error) {
	var a Attachment
	var inline, trunc, mism, exe, hidden int
	err := sc.Scan(&a.ID, &a.MessageID, &a.AccountID, &a.PartID, &a.Filename, &a.ContentType, &a.DeclaredType, &a.Size,
		&a.SHA256, &inline, &trunc, &mism, &a.MismatchReason, &exe, &a.ExtractStatus, &a.ExtractError, &hidden)
	a.Inline, a.Truncated, a.TypeMismatch, a.Executable, a.HiddenText = inline == 1, trunc == 1, mism == 1, exe == 1, hidden == 1
	return a, err
}

// AttachmentsByMessage liefert die Anhänge einer Nachricht nach Teilnummer.
func (s *SQLite) AttachmentsByMessage(ctx context.Context, messageID int64) ([]Attachment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+attachmentCols+` FROM attachments a JOIN messages m ON m.id = a.message_id
		WHERE a.message_id = ? ORDER BY a.id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		a, err := scanAttachment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// AttachmentByID liefert einen Anhang und, falls extrahiert, seinen Text.
func (s *SQLite) AttachmentByID(ctx context.Context, id int64) (Attachment, string, error) {
	var text sql.NullString
	row := s.db.QueryRowContext(ctx, `SELECT `+attachmentCols+`, a.extracted_text FROM attachments a
		JOIN messages m ON m.id = a.message_id WHERE a.id = ?`, id)
	var a Attachment
	var inline, trunc, mism, exe, hidden int
	err := row.Scan(&a.ID, &a.MessageID, &a.AccountID, &a.PartID, &a.Filename, &a.ContentType, &a.DeclaredType, &a.Size,
		&a.SHA256, &inline, &trunc, &mism, &a.MismatchReason, &exe, &a.ExtractStatus, &a.ExtractError, &hidden, &text)
	if errors.Is(err, sql.ErrNoRows) {
		return a, "", fmt.Errorf("%w: id %d", ErrAttachmentNotFound, id)
	}
	a.Inline, a.Truncated, a.TypeMismatch, a.Executable, a.HiddenText = inline == 1, trunc == 1, mism == 1, exe == 1, hidden == 1
	return a, text.String, err
}

// Body ist der gespeicherte Text einer Nachricht.
type Body struct {
	TextPlain    string
	TextHTML     string
	TextUnquoted string
	Links        json.RawMessage
	HiddenText   bool
	CleanVersion int
}

// MessageBody liefert den gespeicherten Text (leer, wenn es keinen gibt).
func (s *SQLite) MessageBody(ctx context.Context, id int64) (Body, error) {
	var b Body
	var links string
	var hidden int
	err := s.db.QueryRowContext(ctx, `SELECT text_plain, text_from_html, text_unquoted, links, hidden_text, clean_version
		FROM bodies WHERE message_id = ?`, id).Scan(&b.TextPlain, &b.TextHTML, &b.TextUnquoted, &links, &hidden, &b.CleanVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Body{Links: json.RawMessage("[]")}, nil
	}
	b.Links, b.HiddenText = json.RawMessage(links), hidden == 1
	if strings.TrimSpace(links) == "" {
		b.Links = json.RawMessage("[]")
	}
	return b, err
}
