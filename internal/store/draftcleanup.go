package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Aufräumen eigener Entwurfskopien (A11 Teil 1; Nachtrag zu ADR-009 / E2
// und E3 in docs/entscheidungen.md). Ohne Schema-Upgrade: Die Herkunft
// einer Aktion und ihr Entwurf stehen im JSON von actions.params (source,
// draft_id), der Zähler der Versuche je Kopie in meta.

// ActionSourceDraftUpdate ist die Herkunft der Aktion, die frühere eigene,
// unveränderte Kopien eines geänderten Entwurfs in den Papierkorb legt
// (A11 Teil 1 (c), (d)).
const ActionSourceDraftUpdate = "draft_update"

// ActionSourceDraftDiscard ist die Herkunft der Aktion, die drafts.discard
// anlegt (A11 Teil 2).
const ActionSourceDraftDiscard = "draft_discard"

// IsDraftSource meldet, ob eine Herkunft die eines Entwurfs ist
// (draft_supersede, draft_update, draft_discard): Der Runner verlangt
// dafür den Scope mail:draft statt mail:trash und keinen Antrag, solange
// jedes Ziel im Entwürfe-Ordner liegt, und verschiebt nichts, wenn ein Ziel
// nicht mehr passt. draft_cleanup (drafts.cleanup_copies des Menschen)
// gehört nicht dazu.
func IsDraftSource(source string) bool {
	switch source {
	case ActionSourceDraftSupersede, ActionSourceDraftUpdate, ActionSourceDraftDiscard:
		return true
	}
	return false
}

// DraftSources sind die Herkünfte eines Entwurfs (IsDraftSource).
var DraftSources = []string{ActionSourceDraftSupersede, ActionSourceDraftUpdate, ActionSourceDraftDiscard}

// CopyBusy meldet, ob für die Kopie (Ordner, UID, UIDVALIDITY) schon eine
// Aktion wartet oder läuft: ein Ziel ohne Ergebnis, dessen Aktion requested,
// queued oder running ist (A11 Teil 1). Jeder Weg, der eine Aktion auf
// Entwurfskopien anlegt, überspringt solche Kopien (sonst wäre die zweite
// Aktion stale, sobald die erste die Kopie verschoben hat).
func (x *DraftTx) CopyBusy(mailboxID int64, uid int64, uidValidity uint32) (bool, error) {
	return copyBusy(x.ctx, x.tx, mailboxID, uid, uidValidity)
}

// CopyBusy wie DraftTx.CopyBusy, außerhalb einer Transaktion.
func (s *SQLite) CopyBusy(ctx context.Context, mailboxID int64, uid int64, uidValidity uint32) (bool, error) {
	return copyBusy(ctx, s.db, mailboxID, uid, uidValidity)
}

func copyBusy(ctx context.Context, q rowQuerier, mailboxID int64, uid int64, uidValidity uint32) (bool, error) {
	var busy bool
	err := q.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM actions a JOIN action_targets t ON t.action_id = a.id
		WHERE a.state IN ('requested', 'queued', 'running') AND t.result IS NULL
		AND t.mailbox_id = ? AND t.uid = ? AND t.uidvalidity = ?)`, mailboxID, uid, int64(uidValidity)).Scan(&busy)
	return busy, err
}

// MailboxCursor ist der Stand eines Ordners im Spiegel: UIDVALIDITY und
// UIDNEXT beim letzten Sync (uidnext ist der Cursor „alle UIDs darunter
// sind gespiegelt“, Migration 0002; 0 = Ordner noch nie gespiegelt).
type MailboxCursor struct {
	Found       bool
	UIDValidity uint32
	UIDNext     uint32
}

// MailboxCursor liefert den Stand des Ordners mailboxID (A11 Teil 1 (b):
// copy_not_mirrored).
func (x *DraftTx) MailboxCursor(mailboxID int64) (MailboxCursor, error) {
	return mailboxCursor(x.ctx, x.tx, mailboxID)
}

// MailboxCursor wie DraftTx.MailboxCursor, außerhalb einer Transaktion.
func (s *SQLite) MailboxCursor(ctx context.Context, mailboxID int64) (MailboxCursor, error) {
	return mailboxCursor(ctx, s.db, mailboxID)
}

func mailboxCursor(ctx context.Context, q rowQuerier, mailboxID int64) (MailboxCursor, error) {
	var c MailboxCursor
	var uv, un sql.NullInt64
	err := q.QueryRowContext(ctx, `SELECT uidvalidity, uidnext FROM mailboxes WHERE id = ?`, mailboxID).Scan(&uv, &un)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	c.Found, c.UIDValidity, c.UIDNext = true, uint32(uv.Int64), uint32(un.Int64)
	return c, nil
}

// Zähler der Aufräumversuche je Kopie (A11 Teil 1 (d)): in meta unter
// draft_copy_cleanup/<Ordner>/<UIDVALIDITY>/<UID>, Wert mit Zahl und erstem
// Zeitpunkt. Er hängt an der Kopie, nicht am Entwurf, und überlebt deshalb
// das Schließen des Entwurfs (UpdateDraft löscht nur scheduled/<id>);
// gelöscht wird er mit dem Erfolg oder nach 7 Tagen (Nachlauf).
const cleanupKeyPrefix = "draft_copy_cleanup/"

// CleanupCount ist der Zähler einer Kopie.
type CleanupCount struct {
	// Copy: Ordner, UID und UIDVALIDITY (ContentHash leer).
	Copy RemoteCopy `json:"-"`
	// N: Zahl der angelegten Aktionen (Versuche).
	N int `json:"n"`
	// First: Zeitpunkt des ersten Versuchs.
	First time.Time `json:"first"`
}

func cleanupKey(mailboxID int64, uidValidity uint32, uid int64) string {
	return cleanupKeyPrefix + strconv.FormatInt(mailboxID, 10) + "/" + strconv.FormatUint(uint64(uidValidity), 10) + "/" +
		strconv.FormatInt(uid, 10)
}

func parseCleanupKey(key string) (RemoteCopy, bool) {
	rest, ok := strings.CutPrefix(key, cleanupKeyPrefix)
	if !ok {
		return RemoteCopy{}, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) != 3 {
		return RemoteCopy{}, false
	}
	mb, err1 := strconv.ParseInt(parts[0], 10, 64)
	uv, err2 := strconv.ParseUint(parts[1], 10, 32)
	uid, err3 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return RemoteCopy{}, false
	}
	return RemoteCopy{MailboxID: mb, UIDValidity: uint32(uv), UID: uid}, true
}

// CleanupCountOf liefert den Zähler einer Kopie; ok false: keiner.
func (x *DraftTx) CleanupCountOf(c RemoteCopy) (CleanupCount, bool, error) {
	return cleanupCountOf(x.ctx, x.tx, c)
}

// CleanupCountOf liefert den Zähler einer Kopie außerhalb einer
// Transaktion (Anzeige, A12 Teil 2 (b)); ok false: keiner.
func (s *SQLite) CleanupCountOf(ctx context.Context, c RemoteCopy) (CleanupCount, bool, error) {
	return cleanupCountOf(ctx, s.db, c)
}

func cleanupCountOf(ctx context.Context, q querier, c RemoteCopy) (CleanupCount, bool, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, cleanupKey(c.MailboxID, c.UIDValidity, c.UID)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return CleanupCount{}, false, nil
	}
	if err != nil {
		return CleanupCount{}, false, err
	}
	var n CleanupCount
	if json.Unmarshal([]byte(v), &n) != nil || n.N <= 0 {
		return CleanupCount{}, false, nil // unlesbar: wie keiner
	}
	n.Copy = RemoteCopy{MailboxID: c.MailboxID, UIDValidity: c.UIDValidity, UID: c.UID}
	return n, true, nil
}

// BumpCleanupCount zählt einen Versuch für die Kopie (erster Versuch:
// Zeitpunkt now).
func (x *DraftTx) BumpCleanupCount(c RemoteCopy, now time.Time) error {
	n, ok, err := x.CleanupCountOf(c)
	if err != nil {
		return err
	}
	if !ok {
		n = CleanupCount{First: now.UTC()}
	}
	n.N++
	b, err := json.Marshal(CleanupCount{N: n.N, First: n.First.UTC()})
	if err != nil {
		return err
	}
	_, err = x.tx.ExecContext(x.ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		cleanupKey(c.MailboxID, c.UIDValidity, c.UID), string(b))
	return err
}

// DeleteCleanupCount löscht den Zähler einer Kopie.
func (x *DraftTx) DeleteCleanupCount(c RemoteCopy) error {
	_, err := x.tx.ExecContext(x.ctx, `DELETE FROM meta WHERE key = ?`, cleanupKey(c.MailboxID, c.UIDValidity, c.UID))
	return err
}

// CleanupCounts liefert alle Zähler (Nachlauf: aufräumen nach Erfolg oder
// nach 7 Tagen); Ordner aller Konten, mailboxID > 0 schränkt nicht ein.
func (s *SQLite) CleanupCounts(ctx context.Context) ([]CleanupCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM meta WHERE key LIKE '`+cleanupKeyPrefix+`%' ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CleanupCount
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		c, ok := parseCleanupKey(k)
		if !ok {
			continue
		}
		var n CleanupCount
		if json.Unmarshal([]byte(v), &n) != nil {
			n = CleanupCount{}
		}
		n.Copy = c
		out = append(out, n)
	}
	return out, rows.Err()
}

// DeleteCleanupCount löscht den Zähler einer Kopie außerhalb einer
// Transaktion.
func (s *SQLite) DeleteCleanupCount(ctx context.Context, c RemoteCopy) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, cleanupKey(c.MailboxID, c.UIDValidity, c.UID))
	return err
}

// LastCopyResult liefert das Ergebnis des Ziels der jüngsten Aktion mit
// einer Herkunft eines Entwurfs auf die Kopie (Ordner, UID, UIDVALIDITY):
// Zustand der Aktion und Ergebnis des Ziels ("" = offen). ok false: keine
// solche Aktion.
func (s *SQLite) LastCopyResult(ctx context.Context, c RemoteCopy) (actionID int64, state, result string, ok bool, err error) {
	var res sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT a.id, a.state, t.result FROM actions a JOIN action_targets t ON t.action_id = a.id
		WHERE t.mailbox_id = ? AND t.uid = ? AND t.uidvalidity = ? AND json_extract(a.params, '$.source') IN (?, ?, ?)
		ORDER BY a.id DESC LIMIT 1`, c.MailboxID, c.UID, int64(c.UIDValidity),
		ActionSourceDraftSupersede, ActionSourceDraftUpdate, ActionSourceDraftDiscard).Scan(&actionID, &state, &res)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", "", false, nil
	}
	if err != nil {
		return 0, "", "", false, err
	}
	return actionID, state, res.String, true, nil
}

// DraftCleanupActions liefert die Aktionen mit einer Herkunft eines
// Entwurfs, die zu d gehören, mit ihren Zielen, neueste zuerst (A11 Teil 1
// (c): cleanup_action_ids). Zu d gehört eine Aktion, die draft_id = d.ID
// in actions.params trägt; ältere Aktionen ohne draft_id (vor A11) über
// den Abgleich ihrer Ziele mit der aktuellen Kopie und remote_previous.
func (s *SQLite) DraftCleanupActions(ctx context.Context, d Draft) ([]Action, error) {
	copies := []RemoteCopy{}
	if d.RemoteMailboxID > 0 && d.RemoteUID > 0 && d.RemoteUIDValidity > 0 {
		copies = append(copies, RemoteCopy{MailboxID: d.RemoteMailboxID, UID: d.RemoteUID, UIDValidity: d.RemoteUIDValidity})
	}
	for _, c := range d.RemotePrevious {
		if c.MailboxID > 0 && c.UID > 0 && c.UIDValidity > 0 {
			copies = append(copies, c)
		}
	}
	q := `SELECT ` + actionCols + ` FROM actions WHERE account_id = ? AND json_extract(params, '$.source') IN (?, ?, ?)
		AND (json_extract(params, '$.draft_id') = ?`
	args := []any{d.AccountID, ActionSourceDraftSupersede, ActionSourceDraftUpdate, ActionSourceDraftDiscard, d.ID}
	if len(copies) > 0 {
		var cond []string
		for _, c := range copies {
			cond = append(cond, `(t.mailbox_id = ? AND t.uid = ? AND t.uidvalidity = ?)`)
			args = append(args, c.MailboxID, c.UID, int64(c.UIDValidity))
		}
		q += ` OR (json_extract(params, '$.draft_id') IS NULL AND EXISTS (SELECT 1 FROM action_targets t WHERE t.action_id = actions.id
			AND (` + strings.Join(cond, " OR ") + `)))`
	}
	q += `) ORDER BY id DESC`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	err = rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Targets, err = actionTargets(ctx, s.db, out[i].ID); err != nil {
			return nil, fmt.Errorf("store: targets of action %d: %w", out[i].ID, err)
		}
	}
	return out, nil
}

// CleanupCandidates liefert die Entwürfe eines Kontos, die der Nachlauf
// prüft (A11 Teil 1 (d)): (i) offene mit früheren Kopien, (ii) seit since
// geschlossene (abandoned) mit einer Kopie. Der Grund des Schließens steht
// im Audit (DraftClosedBy); der Aufrufer prüft ihn.
func (s *SQLite) CleanupCandidates(ctx context.Context, accountID int64, since time.Time) (open, closed []Draft, err error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, status FROM drafts WHERE account_id = ? AND (
		(status IN ('draft', 'pending_approval') AND remote_state = 'pushed' AND json_array_length(remote_previous) > 0)
		OR (status = 'abandoned' AND closed_at >= ? AND (remote_uid > 0 OR json_array_length(remote_previous) > 0)))
		ORDER BY id`, accountID, since.UnixMilli())
	if err != nil {
		return nil, nil, err
	}
	type idStatus struct {
		id     int64
		status string
	}
	var ids []idStatus
	for rows.Next() {
		var v idStatus
		if err := rows.Scan(&v.id, &v.status); err != nil {
			_ = rows.Close()
			return nil, nil, err
		}
		ids = append(ids, v)
	}
	err = rows.Err()
	if cerr := rows.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, nil, err
	}
	for _, v := range ids {
		d, err := s.Draft(ctx, v.id)
		if err != nil {
			return nil, nil, err
		}
		if v.status == "abandoned" {
			closed = append(closed, d)
		} else {
			open = append(open, d)
		}
	}
	return open, closed, nil
}
