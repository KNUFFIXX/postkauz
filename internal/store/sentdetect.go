package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Versanderkennung (S2.6, A02 E5): Abfragen über den Spiegel. Die Regeln
// (Message-ID, Rückfall-Abgleich) stehen in internal/drafts.
//
// Als gesendet zählt eine Nachricht des Kontos, die
//   - in einem Ordner mit Rolle sent oder all liegt,
//   - in keinem Ordner das Flag \Draft trägt und in keinem Entwürfe-Ordner
//     liegt (Rolle drafts oder ein Ordner, in den PostKauz Entwürfe abgelegt
//     hat; Befund K8: Gmail führt jede Entwurfskopie auch in „All Mail"),
//   - und noch keinem Entwurf als gesendete Nachricht zugeordnet ist
//     (drafts.sent_message_id).

// sentMessageCond ist die Bedingung oben für die Nachricht m.
const sentMessageCond = `EXISTS (SELECT 1 FROM message_mailboxes sl JOIN mailboxes sb ON sb.id = sl.mailbox_id
		WHERE sl.message_id = m.id AND sb.role IN ('sent','all'))
	AND NOT EXISTS (SELECT 1 FROM message_mailboxes dl JOIN mailboxes db ON db.id = dl.mailbox_id
		WHERE dl.message_id = m.id AND (dl.flag_draft = 1 OR db.role = 'drafts'
			OR db.id IN (SELECT remote_mailbox_id FROM drafts WHERE account_id = m.account_id AND remote_mailbox_id IS NOT NULL)))
	AND NOT EXISTS (SELECT 1 FROM drafts cd WHERE cd.sent_message_id = m.id)`

// SentByMessageID liefert die kleinste ID einer gesendeten Nachricht (siehe
// oben) des Kontos mit dieser Message-ID (ohne <>); ok false = keine.
func (s *SQLite) SentByMessageID(ctx context.Context, accountID int64, messageID string) (id int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT m.id FROM messages m WHERE m.account_id = ? AND m.message_id_header = ?
		AND `+sentMessageCond+` ORDER BY m.id LIMIT 1`, accountID, messageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// SentMessage ist eine gesendete Nachricht als Kandidat des
// Rückfall-Abgleichs: Kopffelder ohne Text (den liest MessageBody nur für
// Kandidaten, deren Empfänger und Betreff passen).
type SentMessage struct {
	ID      int64
	Subject string
	// From wie messages.from_addr („Name <adresse>" oder nur die Adresse).
	From   string
	To, Cc []Addr
	// Date: Date-Kopfzeile, sonst INTERNALDATE.
	Date time.Time
	// InSent: Die Nachricht liegt in einem Ordner mit Rolle sent (sonst nur
	// in einem mit Rolle all).
	InSent bool
	// ContentHash wie messages.content_hash ("" = unbekannt).
	ContentHash string
}

// sentCandidateCols sind die Spalten eines Kandidaten (scanSentMessages).
const sentCandidateCols = `m.id, m.subject, m.from_addr, m.to_addrs, m.cc_addrs,
	COALESCE(m.date_header, m.internal_date, 0),
	EXISTS (SELECT 1 FROM message_mailboxes il JOIN mailboxes ib ON ib.id = il.mailbox_id
		WHERE il.message_id = m.id AND ib.role = 'sent'),
	COALESCE(m.content_hash, '')`

// SentCandidates liefert die gesendeten Nachrichten (siehe oben) des
// Kontos mit Datum (Date-Kopfzeile, sonst INTERNALDATE) zwischen from und
// to einschließlich, älteste zuerst. Die Auswahl nach Datum läuft über den
// Index messages_account_date (zwei Teilabfragen statt OR, sonst liest
// SQLite alle Nachrichten des Kontos). Seit der Gegenprüfung S2.6–S2.8
// (W2) nur noch für das Zeitfenster eines neuen oder geänderten Entwurfs;
// sonst prüft der Sync nur neue Nachrichten (NewSentCandidates).
func (s *SQLite) SentCandidates(ctx context.Context, accountID int64, from, to time.Time) ([]SentMessage, error) {
	f, t := from.UnixMilli(), to.UnixMilli()
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+`
		FROM messages m WHERE m.id IN (
			SELECT id FROM messages WHERE account_id = ? AND date_header BETWEEN ? AND ?
			UNION ALL
			SELECT id FROM messages WHERE account_id = ? AND date_header IS NULL AND internal_date BETWEEN ? AND ?)
		AND `+sentMessageCond+` ORDER BY COALESCE(m.date_header, m.internal_date, 0), m.id`, accountID, f, t, accountID, f, t)
	if err != nil {
		return nil, err
	}
	return scanSentMessages(rows)
}

// NewSentCandidates liefert die gesendeten Nachrichten (siehe oben) des
// Kontos mit after < messages.id <= upto, älteste zuerst (W2: nur, was seit
// der letzten Prüfung neu gespiegelt wurde; Auswahl über den Primärschlüssel).
func (s *SQLite) NewSentCandidates(ctx context.Context, accountID, after, upto int64) ([]SentMessage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sentCandidateCols+`
		FROM messages m WHERE m.id > ? AND m.id <= ? AND m.account_id = ?
		AND `+sentMessageCond+` ORDER BY COALESCE(m.date_header, m.internal_date, 0), m.id`, after, upto, accountID)
	if err != nil {
		return nil, err
	}
	return scanSentMessages(rows)
}

func scanSentMessages(rows *sql.Rows) ([]SentMessage, error) {
	defer rows.Close()
	var out []SentMessage
	for rows.Next() {
		var m SentMessage
		var to, cc string
		var date int64
		if err := rows.Scan(&m.ID, &m.Subject, &m.From, &to, &cc, &date, &m.InSent, &m.ContentHash); err != nil {
			return nil, err
		}
		// Unlesbare Adresslisten zählen als leer (der Abgleich passt dann nicht).
		_ = json.Unmarshal([]byte(to), &m.To)
		_ = json.Unmarshal([]byte(cc), &m.Cc)
		m.Date = time.UnixMilli(date).UTC()
		out = append(out, m)
	}
	return out, rows.Err()
}

// MaxMessageID liefert die höchste messages.id (aller Konten; 0 = keine).
// Nachrichten werden nie gelöscht, die IDs steigen: Alles bis zu dieser ID
// ist gespiegelt (W2, Hochwassermarke).
func (s *SQLite) MaxMessageID(ctx context.Context) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(max(id), 0) FROM messages`).Scan(&id)
	return id, err
}

// SentCheckMark liefert die Hochwassermarke der Versanderkennung eines
// Kontos (sent_check_marks; Migration 0016, W2): die höchste messages.id,
// die eine vollständige Prüfung gesehen hat. ok false = noch keine.
func (s *SQLite) SentCheckMark(ctx context.Context, accountID int64) (last int64, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT last_message_id FROM sent_check_marks WHERE account_id = ?`, accountID).Scan(&last)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return last, err == nil, err
}

// SetSentCheckMark setzt die Hochwassermarke eines Kontos; sie sinkt nie.
func (s *SQLite) SetSentCheckMark(ctx context.Context, accountID, last int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sent_check_marks(account_id, last_message_id) VALUES (?, ?)
		ON CONFLICT(account_id) DO UPDATE SET last_message_id = max(last_message_id, excluded.last_message_id)`, accountID, last)
	return err
}

// SetFallbackChecked vermerkt, dass das ganze Zeitfenster eines Entwurfs
// im Stand contentChangedAt geprüft ist (drafts.fallback_checked_at; W2).
// Hat sich der Inhalt inzwischen geändert (content_changed_at weicht ab),
// bleibt der Vermerk aus: Die nächste Prüfung prüft das neue Fenster.
func (s *SQLite) SetFallbackChecked(ctx context.Context, draftID int64, contentChangedAt time.Time) error {
	ms := contentChangedAt.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE drafts SET fallback_checked_at = ? WHERE id = ? AND content_changed_at = ?`, ms, draftID, ms)
	return err
}
