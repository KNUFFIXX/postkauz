package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"
)

// Spiegel (S1.4/S1.5): Ordner, Nachrichten und ihre Zuordnung zu Ordnern.
// Idempotenzschlüssel ist (mailbox_id, uid); eine Nachricht (messages)
// kann in mehreren Ordnern liegen. Der Server löscht, das Archiv behält
// (ADR-003): Verschwindet die letzte Zuordnung, setzt der Store
// deleted_remote_at, die Nachricht bleibt.

// Sync-Phasen eines Ordners (mailboxes.sync_phase).
const (
	PhaseNew     = "new"
	PhaseInitial = "initial"
	PhaseReady   = "ready"
)

// ErrMailboxNotFound: Ordner unbekannt.
var ErrMailboxNotFound = errors.New("store: mailbox not found")

// MailboxSpec ist ein Ordner, wie ihn der Server meldet.
type MailboxSpec struct {
	Path string
	Name string
	Role string
}

// Mailbox ist eine Zeile aus mailboxes samt lokalen Zählern.
type Mailbox struct {
	ID            int64
	AccountID     int64
	Path          string
	Name          string
	Role          string
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
	Phase         string
	Total         int // Nachrichten laut Server beim letzten Sync
	Mirrored      int // lokal gespiegelte Kopien (Zuordnungen)
	Unread        int // davon ungelesen
	LastSyncAt    time.Time
}

// SyncMailboxes gleicht die Ordnerliste eines Kontos ab: neue anlegen,
// Name und Rolle nachziehen, verschwundene entfernen. Nachrichten eines
// verschwundenen Ordners bleiben im Archiv (ohne Zuordnung:
// deleted_remote_at). Liefert die Pfade der angelegten und entfernten Ordner.
func (s *SQLite) SyncMailboxes(ctx context.Context, accountID int64, boxes []MailboxSpec) (added, removed []string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	existing := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT id, path FROM mailboxes WHERE account_id = ?`, accountID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		existing[p] = id
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, b := range boxes {
		seen[b.Path] = true
		role := b.Role
		if role == "" {
			role = "custom"
		}
		if _, ok := existing[b.Path]; ok {
			if _, err := tx.ExecContext(ctx, `UPDATE mailboxes SET name = ?, role = ? WHERE account_id = ? AND path = ?`,
				b.Name, role, accountID, b.Path); err != nil {
				return nil, nil, fmt.Errorf("store: update mailbox %q: %w", b.Path, err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO mailboxes(account_id, path, name, role) VALUES (?,?,?,?)`,
			accountID, b.Path, b.Name, role); err != nil {
			return nil, nil, fmt.Errorf("store: add mailbox %q: %w", b.Path, err)
		}
		added = append(added, b.Path)
	}
	now := time.Now().UnixMilli()
	for p, id := range existing {
		if seen[p] {
			continue
		}
		if _, err := unlinkAll(ctx, tx, id, now); err != nil {
			return nil, nil, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM mailboxes WHERE id = ?`, id); err != nil {
			return nil, nil, err
		}
		removed = append(removed, p)
	}
	slices.Sort(added)
	slices.Sort(removed)
	return added, removed, tx.Commit()
}

const mailboxCols = `m.id, m.account_id, m.path, m.name, m.role, COALESCE(m.uidvalidity,0), COALESCE(m.uidnext,0),
	COALESCE(m.highestmodseq,0), m.sync_phase, m.total, COALESCE(m.last_sync_at,0),
	(SELECT count(*) FROM message_mailboxes mm WHERE mm.mailbox_id = m.id),
	(SELECT count(*) FROM message_mailboxes mm WHERE mm.mailbox_id = m.id AND mm.flag_seen = 0)`

func scanMailbox(sc interface{ Scan(...any) error }) (Mailbox, error) {
	var mb Mailbox
	var uv, un, hm, last int64
	if err := sc.Scan(&mb.ID, &mb.AccountID, &mb.Path, &mb.Name, &mb.Role, &uv, &un, &hm, &mb.Phase, &mb.Total, &last,
		&mb.Mirrored, &mb.Unread); err != nil {
		return mb, err
	}
	mb.UIDValidity, mb.UIDNext, mb.HighestModSeq = uint32(uv), uint32(un), uint64(hm)
	if last > 0 {
		mb.LastSyncAt = time.UnixMilli(last).UTC()
	}
	return mb, nil
}

// Mailboxes liefert die Ordner eines Kontos: INBOX zuerst, dann nach Pfad.
func (s *SQLite) Mailboxes(ctx context.Context, accountID int64) ([]Mailbox, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mailboxCols+` FROM mailboxes m WHERE m.account_id = ?
		ORDER BY m.role = 'inbox' DESC, m.path`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Mailbox
	for rows.Next() {
		mb, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, mb)
	}
	return out, rows.Err()
}

// MailboxByID liefert einen Ordner.
func (s *SQLite) MailboxByID(ctx context.Context, id int64) (Mailbox, error) {
	mb, err := scanMailbox(s.db.QueryRowContext(ctx, `SELECT `+mailboxCols+` FROM mailboxes m WHERE m.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return mb, fmt.Errorf("%w: id %d", ErrMailboxNotFound, id)
	}
	return mb, err
}

// MailboxUIDs liefert die lokal bekannten UIDs eines Ordners, aufsteigend.
func (s *SQLite) MailboxUIDs(ctx context.Context, mailboxID int64) ([]uint32, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid FROM message_mailboxes WHERE mailbox_id = ? ORDER BY uid`, mailboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var u int64
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, uint32(u))
	}
	return out, rows.Err()
}

// LinkCount zählt die Zuordnungen (gespiegelte Kopien) eines Ordners.
func (s *SQLite) LinkCount(ctx context.Context, mailboxID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ?`, mailboxID).Scan(&n)
	return n, err
}

// CountPresentMessages zählt die Nachrichten eines Kontos, die der Server
// noch führt (mindestens eine Zuordnung, deleted_remote_at leer).
func (s *SQLite) CountPresentMessages(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE account_id = ? AND deleted_remote_at IS NULL`, accountID).Scan(&n)
	return n, err
}

// FindLinkCandidate sucht eine schon gespiegelte Nachricht des Kontos mit
// gleicher Message-ID und gleicher Größe, die in diesem Ordner noch keine
// Zuordnung hat (Kopie, Gmail-Label, verschobene Nachricht, Ordner nach
// UIDVALIDITY-Wechsel). So wird der Inhalt nicht erneut geladen und es
// entsteht keine Dublette.
func (s *SQLite) FindLinkCandidate(ctx context.Context, accountID, mailboxID int64, messageID string, size int64) (int64, bool, error) {
	if messageID == "" {
		return 0, false, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT m.id FROM messages m
		WHERE m.account_id = ? AND m.message_id_header = ? AND m.size = ? AND m.blob_sha256 IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id AND mm.mailbox_id = ?)
		ORDER BY m.id LIMIT 1`, accountID, messageID, size, mailboxID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// FindByContentHash sucht wie FindLinkCandidate, aber über content_hash
// (Nachrichten ohne oder mit abweichend gelesener Message-ID).
func (s *SQLite) FindByContentHash(ctx context.Context, accountID, mailboxID int64, hash string) (int64, bool, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT m.id FROM messages m
		WHERE m.account_id = ? AND m.content_hash = ?
		  AND NOT EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id AND mm.mailbox_id = ?)
		ORDER BY m.id LIMIT 1`, accountID, hash, mailboxID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// LinkFlags sind die Flags einer Ordnerkopie.
type LinkFlags struct {
	UID                            uint32
	ModSeq                         uint64
	Seen, Flagged, Answered, Draft bool
	Keywords                       []string
}

// NewMessage ist eine neu gespiegelte Nachricht mit ihrer ersten Zuordnung.
type NewMessage struct {
	Link LinkFlags

	BlobSHA256     string
	BlobSize       int64
	BlobCompressed bool

	MessageID      string
	Subject        string
	From           string
	To, Cc         json.RawMessage // JSON-Arrays von {name,address}
	ReplyTo        string
	DateHeader     time.Time // Nullzeit = keine lesbare Date-Kopfzeile
	InternalDate   time.Time
	Size           int64
	HasAttachments bool
	Snippet        string
	InReplyTo      string
	References     []string
	ListID         string
	AuthResults    string
	ContentHash    string
	TextPlain      string
	TextCharset    string
}

// Link ordnet eine vorhandene Nachricht einer UID dieses Ordners zu.
type Link struct {
	MessageID int64
	Flags     LinkFlags
}

// MailboxState ist der neue Sync-Zustand eines Ordners.
type MailboxState struct {
	UIDValidity   uint32
	UIDNext       uint32
	HighestModSeq uint64
	Phase         string
	Total         int
}

// MirrorBatch ist ein Fenster des Syncs; ApplyBatch schreibt es in einer
// Transaktion. Der Zustand (State) wird nur zusammen mit den Daten
// gespeichert: Bricht der Prozess vorher ab, wiederholt der nächste Lauf
// das Fenster; (mailbox_id, uid) verhindert Doppelte.
type MirrorBatch struct {
	AccountID int64
	MailboxID int64
	// Reset löst vorher alle Zuordnungen des Ordners (UIDVALIDITY-Wechsel).
	Reset   bool
	New     []NewMessage
	Links   []Link
	Flags   []LinkFlags
	Removed []uint32
	State   *MailboxState
}

// BatchResult zählt, was ApplyBatch tatsächlich geändert hat.
type BatchResult struct {
	New      int // neue Nachrichten (Zeilen in messages)
	Linked   int // vorhandene Nachrichten neu zugeordnet
	Updated  int // Kopien mit geänderten Flags
	Removed  int // gelöste Zuordnungen
	Unlinked int // bei Reset gelöste Zuordnungen
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func keywordsJSON(k []string) string {
	if len(k) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(k)
	return string(b)
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func jsonOr(raw json.RawMessage, def string) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return def
	}
	return string(raw)
}

// ApplyBatch schreibt ein Fenster atomar.
func (s *SQLite) ApplyBatch(ctx context.Context, b MirrorBatch) (res BatchResult, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UnixMilli()
	touched := map[int64]bool{}

	if b.Reset {
		if res.Unlinked, err = unlinkAll(ctx, tx, b.MailboxID, now); err != nil {
			return res, err
		}
	}
	for _, uid := range b.Removed {
		var mid int64
		err := tx.QueryRowContext(ctx, `DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ? RETURNING message_id`,
			b.MailboxID, uid).Scan(&mid)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return res, fmt.Errorf("store: remove uid %d: %w", uid, err)
		}
		res.Removed++
		touched[mid] = true
	}
	linkExists := func(uid uint32) (bool, error) {
		var n int
		err := tx.QueryRowContext(ctx, `SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, b.MailboxID, uid).Scan(&n)
		return n > 0, err
	}
	insertLink := func(mid int64, f LinkFlags) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO message_mailboxes(message_id, mailbox_id, uid, modseq, flag_seen, flag_flagged,
			flag_answered, flag_draft, keywords) VALUES (?,?,?,?,?,?,?,?,?)`,
			mid, b.MailboxID, f.UID, nullModSeq(f.ModSeq), b2i(f.Seen), b2i(f.Flagged), b2i(f.Answered), b2i(f.Draft), keywordsJSON(f.Keywords))
		return err
	}
	for _, n := range b.New {
		if ok, err := linkExists(n.Link.UID); err != nil {
			return res, err
		} else if ok {
			continue // schon gespiegelt (wiederholtes Fenster)
		}
		if n.BlobSHA256 != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO blobs(sha256, size, compressed, created_at) VALUES (?,?,?,?)
				ON CONFLICT(sha256) DO NOTHING`, n.BlobSHA256, n.BlobSize, b2i(n.BlobCompressed), now); err != nil {
				return res, fmt.Errorf("store: blob row: %w", err)
			}
		}
		refs, _ := json.Marshal(n.References)
		if n.References == nil {
			refs = []byte("[]")
		}
		var mid int64
		err := tx.QueryRowContext(ctx, `INSERT INTO messages(account_id, message_id_header, subject, from_addr, to_addrs, cc_addrs,
			reply_to, date_header, internal_date, size, flag_seen, flag_flagged, flag_answered, flag_draft, keywords,
			has_attachments, snippet, blob_sha256, in_reply_to, refs, list_id, auth_results, content_hash, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
			b.AccountID, nullStr(n.MessageID), n.Subject, n.From, jsonOr(n.To, "[]"), jsonOr(n.Cc, "[]"),
			nullStr(n.ReplyTo), nullTime(n.DateHeader), nullTime(n.InternalDate), n.Size,
			b2i(n.Link.Seen), b2i(n.Link.Flagged), b2i(n.Link.Answered), b2i(n.Link.Draft), keywordsJSON(n.Link.Keywords),
			b2i(n.HasAttachments), n.Snippet, nullStr(n.BlobSHA256), nullStr(n.InReplyTo), string(refs),
			nullStr(n.ListID), nullStr(n.AuthResults), nullStr(n.ContentHash), now).Scan(&mid)
		if err != nil {
			return res, fmt.Errorf("store: insert message: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO bodies(message_id, text_plain, charset) VALUES (?,?,?)`,
			mid, n.TextPlain, nullStr(n.TextCharset)); err != nil {
			return res, fmt.Errorf("store: insert body: %w", err)
		}
		if err := insertLink(mid, n.Link); err != nil {
			return res, fmt.Errorf("store: insert link: %w", err)
		}
		res.New++
	}
	for _, l := range b.Links {
		if ok, err := linkExists(l.Flags.UID); err != nil {
			return res, err
		} else if ok {
			continue
		}
		if err := insertLink(l.MessageID, l.Flags); err != nil {
			return res, fmt.Errorf("store: link message %d: %w", l.MessageID, err)
		}
		touched[l.MessageID] = true // refreshMessage löscht deleted_remote_at
		res.Linked++
	}
	for _, f := range b.Flags {
		var mid int64
		kw := keywordsJSON(f.Keywords)
		err := tx.QueryRowContext(ctx, `UPDATE message_mailboxes SET flag_seen = ?, flag_flagged = ?, flag_answered = ?,
			flag_draft = ?, keywords = ?, modseq = COALESCE(?, modseq)
			WHERE mailbox_id = ? AND uid = ?
			  AND (flag_seen != ? OR flag_flagged != ? OR flag_answered != ? OR flag_draft != ? OR keywords != ?)
			RETURNING message_id`,
			b2i(f.Seen), b2i(f.Flagged), b2i(f.Answered), b2i(f.Draft), kw, nullModSeq(f.ModSeq), b.MailboxID, f.UID,
			b2i(f.Seen), b2i(f.Flagged), b2i(f.Answered), b2i(f.Draft), kw).Scan(&mid)
		if errors.Is(err, sql.ErrNoRows) {
			continue // unbekannte UID oder unverändert
		}
		if err != nil {
			return res, fmt.Errorf("store: flags uid %d: %w", f.UID, err)
		}
		touched[mid] = true
		res.Updated++
	}
	for mid := range touched {
		if err := refreshMessage(ctx, tx, mid, now); err != nil {
			return res, err
		}
	}
	if st := b.State; st != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE mailboxes SET uidvalidity = ?, uidnext = ?, highestmodseq = ?, sync_phase = ?,
			total = ?, last_sync_at = ?,
			unread = (SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ? AND flag_seen = 0)
			WHERE id = ?`, st.UIDValidity, st.UIDNext, nullModSeq(st.HighestModSeq), st.Phase, st.Total, now,
			b.MailboxID, b.MailboxID); err != nil {
			return res, fmt.Errorf("store: mailbox state: %w", err)
		}
	}
	return res, tx.Commit()
}

func nullModSeq(v uint64) any {
	if v == 0 {
		return nil
	}
	return int64(v)
}

type execQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// unlinkAll löst alle Zuordnungen eines Ordners; Nachrichten ohne weitere
// Zuordnung erhalten deleted_remote_at (bleiben aber im Archiv).
func unlinkAll(ctx context.Context, tx execQuerier, mailboxID int64, now int64) (int, error) {
	rows, err := tx.QueryContext(ctx, `DELETE FROM message_mailboxes WHERE mailbox_id = ? RETURNING message_id`, mailboxID)
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
	for _, id := range ids {
		if err := refreshMessage(ctx, tx, id, now); err != nil {
			return 0, err
		}
	}
	return len(ids), nil
}

// refreshMessage leitet Flags und Keywords der Nachricht aus ihren Kopien
// ab (gesetzt, wenn eine Kopie es trägt) und setzt bzw. löscht
// deleted_remote_at je nachdem, ob noch eine Kopie existiert. Ohne Kopie
// bleiben die zuletzt bekannten Flags stehen.
func refreshMessage(ctx context.Context, tx execQuerier, id int64, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE messages SET
		flag_seen     = COALESCE((SELECT MAX(flag_seen)     FROM message_mailboxes WHERE message_id = messages.id), flag_seen),
		flag_flagged  = COALESCE((SELECT MAX(flag_flagged)  FROM message_mailboxes WHERE message_id = messages.id), flag_flagged),
		flag_answered = COALESCE((SELECT MAX(flag_answered) FROM message_mailboxes WHERE message_id = messages.id), flag_answered),
		flag_draft    = COALESCE((SELECT MAX(flag_draft)    FROM message_mailboxes WHERE message_id = messages.id), flag_draft),
		keywords = CASE WHEN EXISTS (SELECT 1 FROM message_mailboxes WHERE message_id = messages.id)
			THEN (SELECT json_group_array(value) FROM (SELECT DISTINCT j.value AS value FROM message_mailboxes mm, json_each(mm.keywords) j
			      WHERE mm.message_id = messages.id ORDER BY 1))
			ELSE keywords END,
		deleted_remote_at = CASE WHEN EXISTS (SELECT 1 FROM message_mailboxes WHERE message_id = messages.id)
			THEN NULL ELSE COALESCE(deleted_remote_at, ?) END
		WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("store: refresh message %d: %w", id, err)
	}
	return nil
}
