package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ErrMessageNotFound: Nachricht unbekannt.
var ErrMessageNotFound = errors.New("store: message not found")

// MessageLocation ist eine Ordnerkopie einer Nachricht.
type MessageLocation struct {
	Mailbox string
	Role    string
	UID     uint32
}

// Message ist eine gespiegelte Nachricht (Metadaten, ohne Inhalt).
type Message struct {
	ID              int64
	AccountID       int64
	Account         string
	MessageID       string
	ThreadID        int64 // 0 = noch keinem Thread zugeordnet
	Subject         string
	From            string
	To, Cc          json.RawMessage
	ReplyTo         string
	DateHeader      time.Time
	InternalDate    time.Time
	Size            int64
	Seen            bool
	Flagged         bool
	Answered        bool
	Draft           bool
	Keywords        []string
	HasAttachments  bool
	Snippet         string
	BlobSHA256      string
	InReplyTo       string
	References      []string
	ListID          string
	AuthResults     string // Authentication-Results, oberste zuerst, je Zeile
	DeletedRemoteAt time.Time
	Locations       []MessageLocation
}

// MessageByID liefert eine Nachricht samt Ordnerkopien.
func (s *SQLite) MessageByID(ctx context.Context, id int64) (Message, error) {
	var m Message
	var mid, reply, blob, irt, list, auth sql.NullString
	var dh, idate, del, tid sql.NullInt64
	var to, cc, kw, refs string
	var seen, flagged, answered, draft, att int
	err := s.db.QueryRowContext(ctx, `SELECT m.id, m.account_id, a.name, m.message_id_header, m.subject, m.from_addr,
		m.to_addrs, m.cc_addrs, m.reply_to, m.date_header, m.internal_date, m.size, m.flag_seen, m.flag_flagged,
		m.flag_answered, m.flag_draft, m.keywords, m.has_attachments, m.snippet, m.blob_sha256, m.in_reply_to, m.refs,
		m.list_id, m.deleted_remote_at, m.thread_id, m.auth_results
		FROM messages m JOIN accounts a ON a.id = m.account_id WHERE m.id = ?`, id).Scan(
		&m.ID, &m.AccountID, &m.Account, &mid, &m.Subject, &m.From, &to, &cc, &reply, &dh, &idate, &m.Size,
		&seen, &flagged, &answered, &draft, &kw, &att, &m.Snippet, &blob, &irt, &refs, &list, &del, &tid, &auth)
	if errors.Is(err, sql.ErrNoRows) {
		return m, fmt.Errorf("%w: id %d", ErrMessageNotFound, id)
	}
	if err != nil {
		return m, err
	}
	m.MessageID, m.ReplyTo, m.BlobSHA256, m.InReplyTo, m.ListID = mid.String, reply.String, blob.String, irt.String, list.String
	m.ThreadID, m.AuthResults = tid.Int64, auth.String
	m.To, m.Cc = json.RawMessage(to), json.RawMessage(cc)
	m.Seen, m.Flagged, m.Answered, m.Draft, m.HasAttachments = seen == 1, flagged == 1, answered == 1, draft == 1, att == 1
	_ = json.Unmarshal([]byte(kw), &m.Keywords)
	_ = json.Unmarshal([]byte(refs), &m.References)
	if dh.Valid {
		m.DateHeader = time.UnixMilli(dh.Int64).UTC()
	}
	if idate.Valid {
		m.InternalDate = time.UnixMilli(idate.Int64).UTC()
	}
	if del.Valid {
		m.DeletedRemoteAt = time.UnixMilli(del.Int64).UTC()
	}
	rows, err := s.db.QueryContext(ctx, `SELECT b.path, b.role, mm.uid FROM message_mailboxes mm
		JOIN mailboxes b ON b.id = mm.mailbox_id WHERE mm.message_id = ? ORDER BY b.path`, id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var l MessageLocation
		var uid int64
		if err := rows.Scan(&l.Mailbox, &l.Role, &uid); err != nil {
			return m, err
		}
		l.UID = uint32(uid)
		m.Locations = append(m.Locations, l)
	}
	return m, rows.Err()
}

// MessageIDsByHeader liefert die lokalen IDs der Nachrichten eines Kontos
// mit dieser Message-ID (ohne spitze Klammern), aufsteigend.
func (s *SQLite) MessageIDsByHeader(ctx context.Context, accountID int64, messageID string) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM messages WHERE account_id = ? AND message_id_header = ? ORDER BY id`,
		accountID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// MessageText liefert den gespeicherten Text (text/plain) einer Nachricht.
func (s *SQLite) MessageText(ctx context.Context, id int64) (string, error) {
	var t string
	err := s.db.QueryRowContext(ctx, `SELECT text_plain FROM bodies WHERE message_id = ?`, id).Scan(&t)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return t, err
}
