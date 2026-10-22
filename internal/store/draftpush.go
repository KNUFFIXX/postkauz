package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Ablage der Entwürfe im Entwürfe-Ordner (S2.5, A02 E1, E6, E12): Der Job
// draft_push arbeitet alle Entwürfe eines Kontos ab, deren Kopie aussteht;
// die Übernahme-Erkennung liest den Spiegel des Entwürfe-Ordners. Die Regeln
// stehen in internal/drafts und internal/ops, hier nur die Abfragen.

// draftPushPending ist die Bedingung für einen Entwurf, dessen Kopie
// aussteht: remote_state pending und nicht abgeschlossen (sent_by_human und
// abandoned werden nie abgelegt).
const draftPushPending = `remote_state = 'pending' AND status IN ('draft','pending_approval','ready')`

// DraftPushCandidates liefert die Entwürfe eines Kontos, deren Kopie
// aussteht, älteste zuerst, und wie viele davon schon früher abgelegt
// wurden (deren alte Kopie die Übernahme-Erkennung vor dem Push prüft).
func (s *SQLite) DraftPushCandidates(ctx context.Context, accountID int64) (ids []int64, withCopy int, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, remote_uid IS NOT NULL FROM drafts WHERE account_id = ? AND `+draftPushPending+`
		ORDER BY id`, accountID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var had bool
		if err := rows.Scan(&id, &had); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
		if had {
			withCopy++
		}
	}
	return ids, withCopy, rows.Err()
}

// EnsureDraftPushJob reiht den Job draft_push für ein Konto ein, wenn
// Entwürfe auf ihre Kopie warten und kein Job offen ist (etwa nach fünf
// Fehlern des alten: failed). Ein offener Job, auch einer, der nach einem
// Fehler erst später fällig ist, bleibt, wie er ist. Liefert, ob einer
// angelegt wurde.
func (s *SQLite) EnsureDraftPushJob(ctx context.Context, accountID int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO jobs(type, payload, run_at)
		SELECT ?, json_object('account_id', ?), ? WHERE EXISTS (SELECT 1 FROM drafts WHERE account_id = ? AND `+draftPushPending+`)`,
		JobDraftPush, accountID, now.UnixMilli(), accountID)
	if err != nil {
		return false, fmt.Errorf("store: ensure draft_push job: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetPushNotePending setzt den Hinweis des Pushs (push_note) an allen
// Entwürfen eines Kontos, deren Kopie aussteht (z. B. no_drafts_mailbox).
func (x *DraftTx) SetPushNotePending(accountID int64, note string) (int, error) {
	res, err := x.tx.ExecContext(x.ctx, `UPDATE drafts SET push_note = ? WHERE account_id = ? AND `+draftPushPending+`
		AND (push_note IS NULL OR push_note != 'approval_stale')`, nullStr(note), accountID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MirrorCopy ist, was der Spiegel über die abgelegte Kopie eines Entwurfs
// weiß (E6). Der Spiegel kennt nur den Stand des letzten Syncs des
// Entwürfe-Ordners.
type MirrorCopy struct {
	// MailboxFound: Der Ordner ist im Spiegel bekannt.
	MailboxFound bool
	// Synced: Der Ordner wurde mindestens einmal gespiegelt.
	Synced bool
	// UIDValidity und UIDNext des Ordners beim letzten Sync: Der Spiegel
	// kennt alle UIDs unter UIDNext (bei gleicher UIDVALIDITY).
	UIDValidity uint32
	UIDNext     uint32
	// AtUID: Unter der UID der Kopie liegt im Spiegel eine Nachricht, mit
	// dieser Message-ID und diesem Inhalts-Hash.
	AtUID            bool
	AtUIDMessageID   string
	AtUIDContentHash string
	// NewerSameID: Im Ordner liegt eine Nachricht mit derselben Message-ID
	// und höherer UID (ein Mailclient hat eine bearbeitete Fassung
	// gespeichert).
	NewerSameID bool
	// SameIDAndHash: Irgendwo im Ordner liegt eine Nachricht mit derselben
	// Message-ID und demselben Inhalts-Hash (nach einem UIDVALIDITY-Wechsel
	// gelten die UIDs nicht mehr).
	SameIDAndHash bool
}

// MirrorCopy liest den Spiegel zur Kopie eines Entwurfs: Ordner mailboxID,
// UID uid (0 = unbekannt), Message-ID ohne <> und Inhalts-Hash der Kopie.
func (x *DraftTx) MirrorCopy(mailboxID, uid int64, messageID, contentHash string) (MirrorCopy, error) {
	var mc MirrorCopy
	var uv, un sql.NullInt64
	var phase string
	err := x.tx.QueryRowContext(x.ctx, `SELECT uidvalidity, uidnext, sync_phase FROM mailboxes WHERE id = ?`, mailboxID).
		Scan(&uv, &un, &phase)
	if errors.Is(err, sql.ErrNoRows) {
		return mc, nil
	}
	if err != nil {
		return mc, err
	}
	mc.MailboxFound, mc.Synced = true, phase != PhaseNew
	mc.UIDValidity, mc.UIDNext = uint32(uv.Int64), uint32(un.Int64)
	if uid > 0 {
		err = x.tx.QueryRowContext(x.ctx, `SELECT COALESCE(m.message_id_header, ''), COALESCE(m.content_hash, '')
			FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id WHERE mm.mailbox_id = ? AND mm.uid = ?`,
			mailboxID, uid).Scan(&mc.AtUIDMessageID, &mc.AtUIDContentHash)
		switch {
		case err == nil:
			mc.AtUID = true
		case !errors.Is(err, sql.ErrNoRows):
			return mc, err
		}
	}
	err = x.tx.QueryRowContext(x.ctx, `SELECT
		EXISTS (SELECT 1 FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
			WHERE mm.mailbox_id = ? AND m.message_id_header = ? AND mm.uid > ?),
		EXISTS (SELECT 1 FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
			WHERE mm.mailbox_id = ? AND m.message_id_header = ? AND m.content_hash = ?)`,
		mailboxID, messageID, uid, mailboxID, messageID, contentHash).Scan(&mc.NewerSameID, &mc.SameIDAndHash)
	return mc, err
}
