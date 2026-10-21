package store

import (
	"context"
	"fmt"
	"time"
)

// Übersprungene Nachrichten je Ordner (Migration 0009): zu groß für
// [imap] max_message_bytes. Der Sync merkt sie sich, damit er sie nicht in
// jedem Lauf neu anfordert; `sync --full` leert die Liste und prüft erneut.

// SkipTooLarge ist der Grund für eine Nachricht über max_message_bytes.
const SkipTooLarge = "too_large"

// SkippedUIDs liefert die übersprungenen UIDs eines Ordners unter
// uidValidity, aufsteigend.
func (s *SQLite) SkippedUIDs(ctx context.Context, mailboxID int64, uidValidity uint32) ([]uint32, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid FROM skipped_messages WHERE mailbox_id = ? AND uidvalidity = ? ORDER BY uid`,
		mailboxID, int64(uidValidity))
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

// MarkSkipped merkt sich eine übersprungene Nachricht (überschreibt einen
// früheren Eintrag derselben UID).
func (s *SQLite) MarkSkipped(ctx context.Context, mailboxID int64, uidValidity, uid uint32, size int64, reason string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO skipped_messages(mailbox_id, uid, uidvalidity, size, reason, skipped_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(mailbox_id, uid) DO UPDATE SET uidvalidity = excluded.uidvalidity, size = excluded.size,
			reason = excluded.reason, skipped_at = excluded.skipped_at`,
		mailboxID, int64(uid), int64(uidValidity), size, reason, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("store: mark skipped: %w", err)
	}
	return nil
}

// PruneSkipped entfernt Einträge eines Ordners, die nicht mehr gelten:
// anderes UIDVALIDITY oder eine UID, die der Server nicht mehr führt
// (present ist die vollständige UID-Liste des Servers).
func (s *SQLite) PruneSkipped(ctx context.Context, mailboxID int64, uidValidity uint32, present []uint32) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid, uidvalidity FROM skipped_messages WHERE mailbox_id = ?`, mailboxID)
	if err != nil {
		return 0, err
	}
	keep := make(map[uint32]bool, len(present))
	for _, u := range present {
		keep[u] = true
	}
	var drop []int64
	for rows.Next() {
		var u, uv int64
		if err := rows.Scan(&u, &uv); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if uint32(uv) != uidValidity || !keep[uint32(u)] {
			drop = append(drop, u)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	for _, u := range drop {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM skipped_messages WHERE mailbox_id = ? AND uid = ?`, mailboxID, u); err != nil {
			return 0, fmt.Errorf("store: prune skipped: %w", err)
		}
	}
	return len(drop), nil
}

// ClearSkipped leert die Liste eines Ordners (`sync --full`).
func (s *SQLite) ClearSkipped(ctx context.Context, mailboxID int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM skipped_messages WHERE mailbox_id = ?`, mailboxID); err != nil {
		return fmt.Errorf("store: clear skipped: %w", err)
	}
	return nil
}
