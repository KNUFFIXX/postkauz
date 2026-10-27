package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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

// draftPushDue ist die Bedingung für einen Entwurf, den der nächste Push
// ablegt: wartend und nicht mit push_rejected oder push_failed vermerkt
// (Gegenprüfung S2.4/S2.5, W3). Solche Entwürfe versucht erst der nächste
// postkauz sync erneut (im Dauerbetrieb der stündliche volle Abgleich),
// nicht jede Prüfung alle 10 s, und sie halten den Job nicht fällig; eine
// Änderung des Entwurfs löscht den Vermerk.
//
// Geänderte Entwürfe in ihrer Ruhezeit (Entscheid 43, S2.6; die Wartezeit
// prüft internal/ops mit der Uhr des Pushs) zählen hier mit: Sie halten den
// Job fällig, damit sync --watch sie nach Ablauf ablegt.
const draftPushDue = draftPushPending + ` AND (push_note IS NULL OR push_note NOT IN ('push_rejected','push_failed'))`

// DraftPushCandidates liefert die Entwürfe eines Kontos, deren Kopie
// aussteht, älteste zuerst, und wie viele davon schon früher abgelegt
// wurden oder einen Ablageversuch mit offenem Ausgang haben (deren Kopien
// die Übernahme-Erkennung bzw. die Suche nach einer verwaisten Kopie vor
// dem Push im Spiegel prüft). retry: auch die mit push_rejected oder
// push_failed vermerkten (mailsync.Options.RetryDrafts), sonst nur die
// fälligen.
func (s *SQLite) DraftPushCandidates(ctx context.Context, accountID int64, retry bool) (ids []int64, withCopy int, err error) {
	items, err := s.DraftPushQueue(ctx, accountID, retry)
	for _, it := range items {
		ids = append(ids, it.ID)
		if it.HadCopy {
			withCopy++
		}
	}
	return ids, withCopy, err
}

// DraftPushItem ist ein Entwurf, dessen Kopie aussteht (DraftPushQueue).
type DraftPushItem struct {
	ID int64
	// HadCopy: frühere Kopie oder Ablageversuch mit offenem Ausgang (die
	// Prüfung vor dem Push liest dafür den Spiegel des Entwürfe-Ordners).
	HadCopy bool
	// Pushed: Der Entwurf wurde schon einmal abgelegt (pushed_content_hash);
	// die nächste Kopie ist die eines geänderten Entwurfs (Entscheid 43).
	Pushed bool
	// UpdatedAt: letzte Änderung (drafts.updated_at).
	UpdatedAt time.Time
}

// DraftPushQueue liefert die Entwürfe eines Kontos, deren Kopie aussteht,
// älteste zuerst, mit dem, was der Push für die Wartezeit nach einer
// Änderung (Entscheid 43) und die Prüfung des Spiegels braucht. retry wie
// bei DraftPushCandidates.
func (s *SQLite) DraftPushQueue(ctx context.Context, accountID int64, retry bool) ([]DraftPushItem, error) {
	cond := draftPushDue
	if retry {
		cond = draftPushPending
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, remote_uid IS NOT NULL OR push_attempt IS NOT NULL,
		pushed_content_hash IS NOT NULL AND pushed_content_hash != '', updated_at FROM drafts
		WHERE account_id = ? AND `+cond+` ORDER BY id`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DraftPushItem
	for rows.Next() {
		var it DraftPushItem
		var up int64
		if err := rows.Scan(&it.ID, &it.HadCopy, &it.Pushed, &up); err != nil {
			return nil, err
		}
		it.UpdatedAt = time.UnixMilli(up).UTC()
		out = append(out, it)
	}
	return out, rows.Err()
}

// EnsureDraftPushJob reiht den Job draft_push für ein Konto ein, wenn
// Entwürfe auf ihre Kopie warten und kein Job offen ist (etwa nach fünf
// Fehlern des alten: failed). Ein offener Job, auch einer, der nach einem
// Fehler erst später fällig ist, bleibt, wie er ist. Liefert, ob einer
// angelegt wurde.
func (s *SQLite) EnsureDraftPushJob(ctx context.Context, accountID int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO jobs(type, payload, run_at, key_id)
		SELECT ?, json_object('account_id', ?), ?, 'system' WHERE EXISTS (SELECT 1 FROM drafts WHERE account_id = ? AND `+draftPushPending+`)`,
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
	// Skipped: Die UID der Kopie steht in skipped_messages (größer als
	// [imap] max_message_bytes, der Spiegel lädt sie nicht); ihr Inhalt
	// ist nicht prüfbar (Gegenprüfung S2.4/S2.5, K4).
	Skipped bool
}

// qMirrorCopySameID: neuere Kopie gleicher Message-ID, Kopie gleicher
// Message-ID und gleichen Inhalts; Parameter je Teil Ordner, Ordner,
// Message-ID, UID bzw. Inhalts-Hash (K6: mit Konto, siehe mailboxAccount).
const qMirrorCopySameID = `SELECT
	EXISTS (SELECT 1 FROM messages m CROSS JOIN message_mailboxes mm ON mm.message_id = m.id
		WHERE mm.mailbox_id = ? AND m.account_id = ` + mailboxAccount + ` AND m.message_id_header = ? AND mm.uid > ?),
	EXISTS (SELECT 1 FROM messages m CROSS JOIN message_mailboxes mm ON mm.message_id = m.id
		WHERE mm.mailbox_id = ? AND m.account_id = ` + mailboxAccount + ` AND m.message_id_header = ? AND m.content_hash = ?)`

// qMirrorOrphan: Parameter Ordner, Ordner, Message-ID, Inhalts-Hash.
const qMirrorOrphan = `SELECT mm.uid FROM messages m CROSS JOIN message_mailboxes mm ON mm.message_id = m.id
	WHERE mm.mailbox_id = ? AND m.account_id = ` + mailboxAccount + ` AND m.message_id_header = ? AND m.content_hash = ?
	ORDER BY mm.uid DESC`

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
		default:
			err = x.tx.QueryRowContext(x.ctx, `SELECT EXISTS (SELECT 1 FROM skipped_messages
				WHERE mailbox_id = ? AND uid = ? AND uidvalidity = ?)`, mailboxID, uid, mc.UIDValidity).Scan(&mc.Skipped)
			if err != nil {
				return mc, err
			}
		}
	}
	err = x.tx.QueryRowContext(x.ctx, qMirrorCopySameID,
		mailboxID, mailboxID, messageID, uid, mailboxID, mailboxID, messageID, contentHash).Scan(&mc.NewerSameID, &mc.SameIDAndHash)
	return mc, err
}

// MirrorOrphan sucht im Spiegel des Ordners mailboxID eine Kopie mit
// Message-ID messageID und Inhalts-Hash contentHash, deren UID nicht in
// known steht (die bekannten Kopien des Entwurfs): die höchste solche UID
// und die UIDVALIDITY des Ordners beim letzten Sync. uid 0 = keine
// (Gegenprüfung S2.4/S2.5, W1: Kopie eines Ablageversuchs, dessen Ausgang
// der abgebrochene Lauf nicht mehr vermerken konnte).
func (x *DraftTx) MirrorOrphan(mailboxID int64, messageID, contentHash string, known []int64) (uid int64, uidValidity uint32, err error) {
	var uv sql.NullInt64
	var phase string
	err = x.tx.QueryRowContext(x.ctx, `SELECT uidvalidity, sync_phase FROM mailboxes WHERE id = ?`, mailboxID).Scan(&uv, &phase)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && phase == PhaseNew) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	rows, err := x.tx.QueryContext(x.ctx, qMirrorOrphan, mailboxID, mailboxID, messageID, contentHash)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var u int64
		if err := rows.Scan(&u); err != nil {
			return 0, 0, err
		}
		if !slices.Contains(known, u) {
			return u, uint32(uv.Int64), rows.Close()
		}
	}
	return 0, 0, rows.Err()
}
