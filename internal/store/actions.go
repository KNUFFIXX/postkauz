package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// Postfach-Aktionen (Migration 0020, A03 S3.1/S3.2, E1, E3): Markieren,
// Verschieben, Papierkorb und Keywords schreiben lokal eine Aktion mit
// ihren Zielen (Nachricht, Ordner, UID, UIDVALIDITY) und reihen den Job
// mailbox_action des Kontos ein. Ausgeführt wird er nur vom Prozess mit
// Zugangsdaten (internal/mailsync, internal/actions). Der Papierkorb wartet
// vorher auf die Freigabe eines Menschen (Zustand requested).

// JobMailboxAction führt die wartenden Aktionen eines Kontos aus.
const JobMailboxAction = "mailbox_action"

// Arten von Aktionen.
const (
	ActionMark     = "mark"
	ActionMove     = "move"
	ActionTrash    = "trash"
	ActionKeywords = "keywords"
)

// Zustände einer Aktion.
const (
	ActionRequested = "requested" // wartet auf die Freigabe (Papierkorb)
	ActionQueued    = "queued"
	ActionRunning   = "running"
	ActionDone      = "done"
	ActionPartial   = "partial"
	ActionFailed    = "failed"
	ActionRejected  = "rejected"
	ActionExpired   = "expired"
)

// Ergebnisse je Ziel.
const (
	TargetOK          = "ok"
	TargetNotFound    = "not_found"
	TargetStale       = "stale"
	TargetUnsupported = "unsupported"
	TargetFailed      = "failed"
	TargetSkipped     = "skipped"
)

// ErrActionNotFound: Die Aktion gibt es nicht.
var ErrActionNotFound = errors.New("store: action not found")

// ActionTarget ist ein Ziel einer Aktion: eine Serverkopie.
type ActionTarget struct {
	MessageID   int64
	MailboxID   int64
	UID         uint32
	UIDValidity uint32
	// Mailbox und Role: Pfad und Rolle des Ordners (beim Lesen; "" wenn der
	// Ordner inzwischen fehlt).
	Mailbox string
	Role    string
	// Result ist das Ergebnis als Code ("" = noch offen).
	Result string
	// NewUID ist die UID im Zielordner nach MOVE (0 = unbekannt).
	NewUID uint32
}

// MarkParams sind die Parameter von mark (nil = nicht ändern).
type MarkParams struct {
	Seen    *bool `json:"seen,omitempty"`
	Flagged *bool `json:"flagged,omitempty"`
}

// ActionParams sind die Parameter einer Aktion, nie Inhalte.
type ActionParams struct {
	Mark *MarkParams `json:"mark,omitempty"`
	// TargetMailboxID, TargetMailbox, TargetRole: Ziel von move und trash.
	TargetMailboxID int64  `json:"target_mailbox_id,omitempty"`
	TargetMailbox   string `json:"target_mailbox,omitempty"`
	TargetRole      string `json:"target_role,omitempty"`
	// AddKeywords und RemoveKeywords: keywords (Tags als IMAP-Keywords).
	AddKeywords    []string `json:"add_keywords,omitempty"`
	RemoveKeywords []string `json:"remove_keywords,omitempty"`
	// Source: Herkunft außerhalb der Operationen (cleanup_copies).
	Source string `json:"source,omitempty"`
}

// Action ist eine Zeile der Tabelle actions mit ihren Zielen.
type Action struct {
	ID           int64
	AccountID    int64
	Kind         string
	Params       ActionParams
	State        string
	TargetCount  int
	IDsHash      string
	ConfirmCount *int
	RequestedBy  string
	AuditID      int64
	ApprovalID   int64
	Error        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DoneAt       time.Time
	Targets      []ActionTarget
}

// NewAction beschreibt eine neue Aktion.
type NewAction struct {
	AccountID    int64
	Kind         string
	Params       ActionParams
	State        string // ActionQueued oder ActionRequested
	IDsHash      string
	MessageCount int
	ConfirmCount *int
	RequestedBy  string
	AuditID      int64
	Targets      []ActionTarget
	// Approval: Antrag auf Freigabe (nur mit ActionRequested); SubjectID
	// setzt CreateAction.
	Approval *Approval
	Now      time.Time
}

// pendingFlags ist der Inhalt von message_mailboxes.pending_flags: die
// Aktion, die die Flags lokal schon geändert hat, und die Werte davor.
type pendingFlags struct {
	Action int64           `json:"action"`
	Prev   map[string]bool `json:"prev"`
}

// CreateAction legt die Aktion mit Zielen an. Ist sie queued, wirkt mark
// sofort im Spiegel (pending_flags) und der Job mailbox_action des Kontos
// wird fällig; ist sie requested, entsteht in derselben Transaktion der
// Antrag (approvals, subject_type action).
func (s *SQLite) CreateAction(ctx context.Context, n NewAction) (a Action, err error) {
	if n.State != ActionQueued && n.State != ActionRequested {
		return a, fmt.Errorf("store: create action: state %q", n.State)
	}
	if len(n.Targets) == 0 {
		return a, errors.New("store: create action: no targets")
	}
	if n.Now.IsZero() {
		n.Now = time.Now()
	}
	params, err := json.Marshal(n.Params)
	if err != nil {
		return a, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	count := n.MessageCount
	if count <= 0 {
		count = len(n.Targets)
	}
	now := n.Now.UnixMilli()
	var id int64
	if err = tx.QueryRowContext(ctx, `INSERT INTO actions(account_id, kind, params, state, target_count, ids_hash, confirm_count,
		requested_by, audit_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		n.AccountID, n.Kind, string(params), n.State, count, n.IDsHash, nullIntPtr(n.ConfirmCount), n.RequestedBy,
		nullID(n.AuditID), now, now).Scan(&id); err != nil {
		return a, fmt.Errorf("store: insert action: %w", err)
	}
	for _, t := range n.Targets {
		if _, err = tx.ExecContext(ctx, `INSERT INTO action_targets(action_id, message_id, mailbox_id, uid, uidvalidity)
			VALUES (?,?,?,?,?)`, id, t.MessageID, t.MailboxID, int64(t.UID), int64(t.UIDValidity)); err != nil {
			return a, fmt.Errorf("store: insert action target: %w", err)
		}
	}
	if n.Approval != nil {
		if n.State != ActionRequested {
			return a, errors.New("store: create action: approval only for requested actions")
		}
		ap := *n.Approval
		ap.SubjectType, ap.SubjectID = "action", id
		x := &DraftTx{ctx: ctx, tx: tx}
		apID, err2 := x.InsertApproval(ap)
		if err2 != nil {
			return a, err2
		}
		if _, err = tx.ExecContext(ctx, `UPDATE actions SET approval_id = ? WHERE id = ?`, apID, id); err != nil {
			return a, err
		}
	}
	if n.State == ActionQueued {
		if err = queueAction(ctx, tx, id, n.Kind, n.Params, n.AccountID, n.RequestedBy, n.AuditID, now); err != nil {
			return a, err
		}
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	return s.Action(ctx, id)
}

// queueAction macht eine Aktion ausführbar: mark ändert den Spiegel sofort
// (pending_flags hält die Werte davor), der Job wird eingereiht bzw. fällig.
func queueAction(ctx context.Context, tx *sql.Tx, id int64, kind string, p ActionParams, accountID int64, keyID string, auditID, now int64) error {
	if kind == ActionMark && p.Mark != nil {
		if err := applyLocalMark(ctx, tx, id, *p.Mark, now); err != nil {
			return err
		}
	}
	return enqueueActionJob(ctx, tx, accountID, keyID, auditID, now)
}

// enqueueActionJob reiht den Job mailbox_action ein; ein offener wird
// sofort fällig (seine Herkunft bleibt die des ersten Aufrufs).
func enqueueActionJob(ctx context.Context, tx execQuerier, accountID int64, keyID string, auditID, now int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO jobs(type, payload, run_at, key_id, audit_id)
		VALUES (?, json_object('account_id', ?), ?, ?, ?)`, JobMailboxAction, accountID, now, nullStr(keyID), nullID(auditID)); err != nil {
		return fmt.Errorf("store: queue mailbox_action: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET run_at = MIN(run_at, ?) WHERE type = ? AND state = 'queued'
		AND json_extract(payload, '$.account_id') = ?`, now, JobMailboxAction, accountID); err != nil {
		return fmt.Errorf("store: queue mailbox_action: %w", err)
	}
	return nil
}

// applyLocalMark setzt die Flags der Zielkopien im Spiegel und merkt die
// Werte davor in pending_flags (für Rücknahme, wenn der Server ablehnt).
func applyLocalMark(ctx context.Context, tx *sql.Tx, actionID int64, m MarkParams, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT mm.message_id, mm.mailbox_id, mm.flag_seen, mm.flag_flagged, mm.pending_flags
		FROM action_targets t JOIN message_mailboxes mm ON mm.message_id = t.message_id AND mm.mailbox_id = t.mailbox_id AND mm.uid = t.uid
		WHERE t.action_id = ?`, actionID)
	if err != nil {
		return err
	}
	type cp struct {
		mid, mbid      int64
		seen, flagged  bool
		pendingOldJSON sql.NullString
	}
	var cps []cp
	for rows.Next() {
		var c cp
		if err := rows.Scan(&c.mid, &c.mbid, &c.seen, &c.flagged, &c.pendingOldJSON); err != nil {
			_ = rows.Close()
			return err
		}
		cps = append(cps, c)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, c := range cps {
		pf := pendingFlags{Action: actionID, Prev: map[string]bool{}}
		// Liegt schon eine offene Änderung auf der Kopie, bleibt deren
		// Ausgangswert der „vorher“-Wert.
		var old pendingFlags
		if c.pendingOldJSON.Valid && json.Unmarshal([]byte(c.pendingOldJSON.String), &old) == nil {
			for k, v := range old.Prev {
				pf.Prev[k] = v
			}
		}
		seen, flagged := c.seen, c.flagged
		if m.Seen != nil {
			if _, ok := pf.Prev["seen"]; !ok {
				pf.Prev["seen"] = c.seen
			}
			seen = *m.Seen
		}
		if m.Flagged != nil {
			if _, ok := pf.Prev["flagged"]; !ok {
				pf.Prev["flagged"] = c.flagged
			}
			flagged = *m.Flagged
		}
		b, _ := json.Marshal(pf)
		if _, err := tx.ExecContext(ctx, `UPDATE message_mailboxes SET flag_seen = ?, flag_flagged = ?, pending_flags = ?
			WHERE message_id = ? AND mailbox_id = ?`, b2i(seen), b2i(flagged), string(b), c.mid, c.mbid); err != nil {
			return err
		}
		if err := refreshMessage(ctx, tx, c.mid, now); err != nil {
			return err
		}
		if err := refreshUnread(ctx, tx, c.mbid); err != nil {
			return err
		}
	}
	return nil
}

func refreshUnread(ctx context.Context, tx execQuerier, mailboxID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE mailboxes SET unread = (SELECT count(*) FROM message_mailboxes WHERE mailbox_id = ? AND flag_seen = 0)
		WHERE id = ?`, mailboxID, mailboxID)
	return err
}

func nullIntPtr(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

const actionCols = `id, account_id, kind, params, state, target_count, ids_hash, confirm_count, requested_by,
	COALESCE(audit_id, 0), COALESCE(approval_id, 0), COALESCE(error, ''), created_at, updated_at, done_at`

func scanAction(sc interface{ Scan(...any) error }) (Action, error) {
	var a Action
	var params string
	var confirm, done sql.NullInt64
	var created, updated int64
	if err := sc.Scan(&a.ID, &a.AccountID, &a.Kind, &params, &a.State, &a.TargetCount, &a.IDsHash, &confirm, &a.RequestedBy,
		&a.AuditID, &a.ApprovalID, &a.Error, &created, &updated, &done); err != nil {
		return a, err
	}
	if err := json.Unmarshal([]byte(params), &a.Params); err != nil {
		return a, fmt.Errorf("store: action %d: params are corrupt", a.ID)
	}
	if confirm.Valid {
		c := int(confirm.Int64)
		a.ConfirmCount = &c
	}
	a.CreatedAt, a.UpdatedAt = unixMilliUTC(created), unixMilliUTC(updated)
	if done.Valid {
		a.DoneAt = unixMilliUTC(done.Int64)
	}
	return a, nil
}

// Action liest eine Aktion mit ihren Zielen.
func (s *SQLite) Action(ctx context.Context, id int64) (Action, error) {
	a, err := scanAction(s.db.QueryRowContext(ctx, `SELECT `+actionCols+` FROM actions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("%w: %d", ErrActionNotFound, id)
	}
	if err != nil {
		return a, err
	}
	a.Targets, err = actionTargets(ctx, s.db, id)
	return a, err
}

func actionTargets(ctx context.Context, q querier, id int64) ([]ActionTarget, error) {
	rows, err := q.QueryContext(ctx, `SELECT t.message_id, t.mailbox_id, t.uid, t.uidvalidity, COALESCE(b.path, ''), COALESCE(b.role, ''),
		COALESCE(t.result, ''), COALESCE(t.new_uid, 0)
		FROM action_targets t LEFT JOIN mailboxes b ON b.id = t.mailbox_id WHERE t.action_id = ? ORDER BY t.message_id, t.mailbox_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ActionTarget
	for rows.Next() {
		var t ActionTarget
		var uid, uv, nu int64
		if err := rows.Scan(&t.MessageID, &t.MailboxID, &uid, &uv, &t.Mailbox, &t.Role, &t.Result, &nu); err != nil {
			return nil, err
		}
		t.UID, t.UIDValidity, t.NewUID = uint32(uid), uint32(uv), uint32(nu)
		out = append(out, t)
	}
	return out, rows.Err()
}

// ActionFilter wählt Aktionen für ListActions.
type ActionFilter struct {
	AccountID int64    // 0 = alle Konten
	States    []string // leer = alle
	Limit     int      // 0 = 50
}

// ListActions liefert Aktionen ohne Ziele, neueste zuerst.
func (s *SQLite) ListActions(ctx context.Context, f ActionFilter) ([]Action, error) {
	q := `SELECT ` + actionCols + ` FROM actions WHERE 1=1`
	var args []any
	if f.AccountID > 0 {
		q += ` AND account_id = ?`
		args = append(args, f.AccountID)
	}
	if len(f.States) > 0 {
		q += ` AND state IN (?` + strings.Repeat(",?", len(f.States)-1) + `)`
		for _, st := range f.States {
			args = append(args, st)
		}
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Action
	for rows.Next() {
		a, err := scanAction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ErrActionState: Die Aktion ist nicht (mehr) im erwarteten Zustand.
var ErrActionState = errors.New("store: action is not in the expected state")

// ActionDecision ist die Entscheidung eines Menschen über einen Antrag.
type ActionDecision struct {
	ActionID  int64
	Approve   bool
	DecidedBy string
	Reason    string
	Now       time.Time
	// KeyID und AuditID: Herkunft des Jobs (der Mensch, der freigibt).
	KeyID   string
	AuditID int64
}

// DecideAction gibt einen Antrag frei oder lehnt ihn ab. Freigegeben wird
// nur ein offener, nicht abgelaufener Antrag; Ziele, deren Serverkopie
// sich seit dem Antrag geändert hat (Ordner, UID, UIDVALIDITY), werden
// stale und nie ausgeführt; sind alle stale, scheitert die Aktion (failed,
// stale). Liefert die Aktion nach der Entscheidung.
func (s *SQLite) DecideAction(ctx context.Context, d ActionDecision) (a Action, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	a, err = scanAction(tx.QueryRowContext(ctx, `SELECT `+actionCols+` FROM actions WHERE id = ?`, d.ActionID))
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("%w: %d", ErrActionNotFound, d.ActionID)
	}
	if err != nil {
		return a, err
	}
	if a.State != ActionRequested || a.ApprovalID == 0 {
		return a, fmt.Errorf("%w: action %d is %s", ErrActionState, a.ID, a.State)
	}
	x := &DraftTx{ctx: ctx, tx: tx}
	ap, err := x.Approval(a.ApprovalID)
	if err != nil {
		return a, err
	}
	if ap.Decision != "pending" {
		return a, fmt.Errorf("%w: approval %d is %s", ErrActionState, ap.ID, ap.Decision)
	}
	now := d.Now.UnixMilli()
	ap.DecidedBy, ap.DecidedAt, ap.Reason = d.DecidedBy, d.Now, d.Reason
	state := ActionRejected
	if d.Approve {
		ap.Decision, state = "approved", ActionQueued
	} else {
		ap.Decision = "rejected"
	}
	if err = x.UpdateApproval(ap); err != nil {
		return a, err
	}
	errCode := ""
	if d.Approve {
		stale, live, err2 := markStaleTargets(ctx, tx, a.ID)
		if err2 != nil {
			return a, err2
		}
		if live == 0 {
			state, errCode = ActionFailed, TargetStale
		} else if stale > 0 {
			errCode = ""
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE actions SET state = ?, error = ?, updated_at = ?,
		done_at = CASE WHEN ? IN ('rejected','failed') THEN ? ELSE NULL END WHERE id = ?`,
		state, nullStr(errCode), now, state, now, a.ID); err != nil {
		return a, err
	}
	if state == ActionQueued {
		if err = queueAction(ctx, tx, a.ID, a.Kind, a.Params, a.AccountID, d.KeyID, d.AuditID, now); err != nil {
			return a, err
		}
	}
	if err = tx.Commit(); err != nil {
		return a, err
	}
	return s.Action(ctx, a.ID)
}

// markStaleTargets setzt Ziele ohne passende Serverkopie im Spiegel auf
// stale (Ordner, UID oder UIDVALIDITY anders als beim Antrag).
func markStaleTargets(ctx context.Context, tx execQuerier, actionID int64) (stale, live int, err error) {
	res, err := tx.ExecContext(ctx, `UPDATE action_targets SET result = 'stale' WHERE action_id = ? AND result IS NULL AND NOT EXISTS (
		SELECT 1 FROM message_mailboxes mm JOIN mailboxes b ON b.id = mm.mailbox_id
		WHERE mm.message_id = action_targets.message_id AND mm.mailbox_id = action_targets.mailbox_id
		  AND mm.uid = action_targets.uid AND b.uidvalidity = action_targets.uidvalidity)`, actionID)
	if err != nil {
		return 0, 0, err
	}
	n, _ := res.RowsAffected()
	var l int
	err = tx.(querier).QueryRowContext(ctx, `SELECT count(*) FROM action_targets WHERE action_id = ? AND result IS NULL`, actionID).Scan(&l)
	return int(n), l, err
}

// ExpireActions lässt offene Anträge verfallen, deren Freigabe abgelaufen
// ist (expires_at vor now): Freigabe expired, Aktion expired, je ein
// Systemereignis actions.expire im Audit (in derselben Transaktion).
// Liefert die Zahl.
func (s *SQLite) ExpireActions(ctx context.Context, now time.Time) (n int, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	rows, err := tx.QueryContext(ctx, `SELECT a.id, a.approval_id, ac.name FROM actions a
		JOIN approvals p ON p.id = a.approval_id JOIN accounts ac ON ac.id = a.account_id
		WHERE a.state = 'requested' AND p.decision = 'pending' AND p.expires_at IS NOT NULL AND p.expires_at <= ?`, now.UnixMilli())
	if err != nil {
		return 0, err
	}
	type exp struct {
		id, ap  int64
		account string
	}
	var es []exp
	for rows.Next() {
		var e exp
		if err = rows.Scan(&e.id, &e.ap, &e.account); err != nil {
			_ = rows.Close()
			return 0, err
		}
		es = append(es, e)
	}
	if err = rows.Close(); err != nil {
		return 0, err
	}
	x := &DraftTx{ctx: ctx, tx: tx}
	ms := now.UnixMilli()
	for _, e := range es {
		if _, err = tx.ExecContext(ctx, `UPDATE approvals SET decision = 'expired', decided_at = ? WHERE id = ?`, ms, e.ap); err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE actions SET state = 'expired', error = 'approval_expired', updated_at = ?, done_at = ?
			WHERE id = ?`, ms, ms, e.id); err != nil {
			return 0, err
		}
		if _, err = x.AppendAudit(audit.Event{TS: now.UTC(), ActorType: audit.ActorSystem, KeyID: "system",
			Operation: "actions.expire", Result: audit.ResultOK, Reason: "approval_expired",
			ParamsRedacted: json.RawMessage(fmt.Sprintf(`{"action_id":%d,"approval_id":%d}`, e.id, e.ap)),
			Targets:        []string{fmt.Sprintf("action:%d", e.id), fmt.Sprintf("approval:%d", e.ap), "account:" + e.account}}); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(es), nil
}

// QueuedActions liefert die ausführbaren Aktionen eines Kontos (queued
// sowie running eines abgebrochenen Laufs), älteste zuerst, mit Zielen.
func (s *SQLite) QueuedActions(ctx context.Context, accountID int64) ([]Action, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+actionCols+` FROM actions WHERE account_id = ? AND state IN ('queued','running')
		ORDER BY id`, accountID)
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Targets, err = actionTargets(ctx, s.db, out[i].ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// HasQueuedActions meldet, ob ein Konto ausführbare Aktionen hat.
func (s *SQLite) HasQueuedActions(ctx context.Context, accountID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM actions WHERE account_id = ? AND state IN ('queued','running')`, accountID).Scan(&n)
	return n > 0, err
}

// EnsureActionJob reiht den Job mailbox_action ein, wenn ausführbare
// Aktionen warten und keiner offen ist (etwa nach fünf Fehlern des alten).
func (s *SQLite) EnsureActionJob(ctx context.Context, accountID int64, now time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO jobs(type, payload, run_at, key_id)
		SELECT ?, json_object('account_id', ?), ?, 'system'
		WHERE EXISTS (SELECT 1 FROM actions WHERE account_id = ? AND state IN ('queued','running'))`,
		JobMailboxAction, accountID, now.UnixMilli(), accountID)
	if err != nil {
		return false, fmt.Errorf("store: ensure mailbox_action job: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// StartAction setzt eine Aktion auf running.
func (s *SQLite) StartAction(ctx context.Context, id int64, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE actions SET state = 'running', updated_at = ? WHERE id = ? AND state IN ('queued','running')`,
		now.UnixMilli(), id)
	return err
}

// TargetResult ist das Ergebnis eines Ziels nach der Ausführung.
type TargetResult struct {
	MessageID int64
	MailboxID int64
	Result    string
	// Bei move/trash: neuer Ordner und neue UID (0 = vom Server nicht
	// genannt; der nächste Sync verknüpft die Kopie per Message-ID).
	DestMailboxID   int64
	DestUIDValidity uint32
	NewUID          uint32
}

// FinishAction schreibt die Ergebnisse der Ziele und den Endzustand
// (done, partial, failed) in einer Transaktion und passt den Spiegel an:
//
//   - mark: Bei ok fällt pending_flags (die Flags gelten, der Server hat
//     sie angenommen); sonst gelten wieder die Werte davor (Server gewinnt).
//   - move/trash: Bei ok wandert die Kopie in den Zielordner (mit neuer UID,
//     wenn bekannt und die UIDVALIDITY des Zielordners zum Spiegel passt;
//     sonst nur aus dem Quellordner entfernt).
//
// errCode ist der Code eines Fehlers der ganzen Aktion ("" = keiner).
func (s *SQLite) FinishAction(ctx context.Context, id int64, results []TargetResult, errCode string, now time.Time) (state string, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var kind string
	var accountID int64
	if err = tx.QueryRowContext(ctx, `SELECT kind, account_id FROM actions WHERE id = ?`, id).Scan(&kind, &accountID); err != nil {
		return "", err
	}
	ms := now.UnixMilli()
	touchedBoxes := map[int64]bool{}
	for _, r := range results {
		if _, err = tx.ExecContext(ctx, `UPDATE action_targets SET result = ?, new_uid = ? WHERE action_id = ? AND message_id = ? AND mailbox_id = ?`,
			r.Result, nullID(int64(r.NewUID)), id, r.MessageID, r.MailboxID); err != nil {
			return "", err
		}
		switch kind {
		case ActionMark:
			if err = settlePending(ctx, tx, id, r.MessageID, r.MailboxID, r.Result == TargetOK, ms); err != nil {
				return "", err
			}
			touchedBoxes[r.MailboxID] = true
		case ActionMove, ActionTrash:
			if r.Result == TargetOK && r.DestMailboxID != r.MailboxID {
				if err = moveLink(ctx, tx, id, r, ms); err != nil {
					return "", err
				}
				touchedBoxes[r.MailboxID], touchedBoxes[r.DestMailboxID] = true, true
			}
		}
	}
	// Ziele ohne Ergebnis (Lauf vorher abgebrochen) zählen als gescheitert.
	if kind == ActionMark {
		rows, err2 := tx.QueryContext(ctx, `SELECT message_id, mailbox_id FROM action_targets WHERE action_id = ? AND result IS NULL`, id)
		if err2 != nil {
			return "", err2
		}
		var open [][2]int64
		for rows.Next() {
			var p [2]int64
			if err = rows.Scan(&p[0], &p[1]); err != nil {
				_ = rows.Close()
				return "", err
			}
			open = append(open, p)
		}
		_ = rows.Close()
		for _, p := range open {
			if err = settlePending(ctx, tx, id, p[0], p[1], false, ms); err != nil {
				return "", err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE action_targets SET result = 'failed' WHERE action_id = ? AND result IS NULL`, id); err != nil {
		return "", err
	}
	for mb := range touchedBoxes {
		if err = refreshUnread(ctx, tx, mb); err != nil {
			return "", err
		}
	}
	var ok, all int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE result IN ('ok','skipped')), count(*) FROM action_targets WHERE action_id = ?`,
		id).Scan(&ok, &all); err != nil {
		return "", err
	}
	switch {
	case errCode == "" && ok == all:
		state = ActionDone
	case ok > 0:
		state = ActionPartial
	default:
		state = ActionFailed
	}
	if _, err = tx.ExecContext(ctx, `UPDATE actions SET state = ?, error = ?, updated_at = ?, done_at = ? WHERE id = ?`,
		state, nullStr(errCode), ms, ms, id); err != nil {
		return "", err
	}
	return state, tx.Commit()
}

// settlePending löst die offene Änderung einer Kopie auf: angenommen
// (pending fällt) oder abgelehnt (Werte davor zurück). Nur, wenn die
// Kopie noch die Änderung dieser Aktion trägt.
func settlePending(ctx context.Context, tx *sql.Tx, actionID, messageID, mailboxID int64, accepted bool, now int64) error {
	var raw sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT pending_flags FROM message_mailboxes WHERE message_id = ? AND mailbox_id = ?`,
		messageID, mailboxID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !raw.Valid) {
		return nil
	}
	if err != nil {
		return err
	}
	var pf pendingFlags
	if json.Unmarshal([]byte(raw.String), &pf) != nil || pf.Action != actionID {
		return nil // eine spätere Aktion hat übernommen
	}
	if accepted {
		_, err = tx.ExecContext(ctx, `UPDATE message_mailboxes SET pending_flags = NULL WHERE message_id = ? AND mailbox_id = ?`,
			messageID, mailboxID)
		return err
	}
	set := `pending_flags = NULL`
	var args []any
	if v, ok := pf.Prev["seen"]; ok {
		set += `, flag_seen = ?`
		args = append(args, b2i(v))
	}
	if v, ok := pf.Prev["flagged"]; ok {
		set += `, flag_flagged = ?`
		args = append(args, b2i(v))
	}
	args = append(args, messageID, mailboxID)
	if _, err = tx.ExecContext(ctx, `UPDATE message_mailboxes SET `+set+` WHERE message_id = ? AND mailbox_id = ?`, args...); err != nil {
		return err
	}
	return refreshMessage(ctx, tx, messageID, now)
}

// moveLink trägt eine verschobene Kopie im Spiegel um.
func moveLink(ctx context.Context, tx *sql.Tx, actionID int64, r TargetResult, now int64) error {
	var uid int64
	var seen, flagged, answered, draft, loaded int
	var kw string
	err := tx.QueryRowContext(ctx, `SELECT mm.uid, mm.flag_seen, mm.flag_flagged, mm.flag_answered, mm.flag_draft, mm.keywords, mm.content_loaded
		FROM message_mailboxes mm JOIN action_targets t ON t.message_id = mm.message_id AND t.mailbox_id = mm.mailbox_id AND t.uid = mm.uid
		WHERE t.action_id = ? AND t.message_id = ? AND t.mailbox_id = ?`, actionID, r.MessageID, r.MailboxID).
		Scan(&uid, &seen, &flagged, &answered, &draft, &kw, &loaded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // der Sync hat die Kopie schon umgetragen
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM message_mailboxes WHERE message_id = ? AND mailbox_id = ? AND uid = ?`,
		r.MessageID, r.MailboxID, uid); err != nil {
		return err
	}
	if r.NewUID > 0 && r.DestMailboxID > 0 && r.DestUIDValidity > 0 {
		var uv sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT uidvalidity FROM mailboxes WHERE id = ?`, r.DestMailboxID).Scan(&uv); err != nil &&
			!errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if uv.Valid && uint32(uv.Int64) == r.DestUIDValidity {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO message_mailboxes(message_id, mailbox_id, uid, flag_seen, flag_flagged,
				flag_answered, flag_draft, keywords, content_loaded) VALUES (?,?,?,?,?,?,?,?,?)`,
				r.MessageID, r.DestMailboxID, int64(r.NewUID), seen, flagged, answered, draft, kw, loaded); err != nil {
				return err
			}
		}
	}
	return refreshMessage(ctx, tx, r.MessageID, now)
}

// MessageAccounts liefert zu lokalen Nachrichten-IDs das Konto (fehlende
// IDs fehlen in der Antwort).
func (s *SQLite) MessageAccounts(ctx context.Context, ids []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		args := make([]any, len(chunk))
		for i, id := range chunk {
			args[i] = id
		}
		rows, err := s.db.QueryContext(ctx, `SELECT id, account_id FROM messages WHERE id IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, acc int64
			if err := rows.Scan(&id, &acc); err != nil {
				_ = rows.Close()
				return nil, err
			}
			out[id] = acc
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// PendingFlagCopies zählt Kopien einer Nachricht mit offener Änderung
// (Anzeige „pending“ in show).
func (s *SQLite) PendingFlagCopies(ctx context.Context, messageID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM message_mailboxes WHERE message_id = ? AND pending_flags IS NOT NULL`, messageID).Scan(&n)
	return n, err
}

// MessageHeaderBlob liefert den Blob einer Nachricht (für den Kopfvergleich
// vor Aktionen auf nie geladenen Kopien; "" = keiner).
func (s *SQLite) MessageHeaderBlob(ctx context.Context, messageID int64) (string, error) {
	var sha sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT blob_sha256 FROM messages WHERE id = ?`, messageID).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return sha.String, err
}

// MarkContentLoaded hält fest, dass der Kopf einer Serverkopie geprüft ist
// und zur lokalen Nachricht passt (Kopfvergleich vor Aktionen).
func (s *SQLite) MarkContentLoaded(ctx context.Context, messageID, mailboxID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE message_mailboxes SET content_loaded = 1 WHERE message_id = ? AND mailbox_id = ?`, messageID, mailboxID)
	return err
}

// CopyAt liefert die Serverkopie unter (Ordner, UID) und den Inhalts-Hash
// ihrer Nachricht (Aufräumen alter Entwurfskopien, drafts.cleanup_copies).
func (s *SQLite) CopyAt(ctx context.Context, mailboxID int64, uid uint32) (c ServerCopy, contentHash string, ok bool, err error) {
	var u, uv int64
	var h sql.NullString
	err = s.db.QueryRowContext(ctx, `SELECT mm.message_id, b.id, b.path, b.role, mm.uid, COALESCE(b.uidvalidity, 0), mm.content_loaded, m.content_hash
		FROM message_mailboxes mm JOIN mailboxes b ON b.id = mm.mailbox_id JOIN messages m ON m.id = mm.message_id
		WHERE mm.mailbox_id = ? AND mm.uid = ?`, mailboxID, int64(uid)).Scan(&c.MessageID, &c.MailboxID, &c.Mailbox, &c.Role, &u, &uv,
		&c.ContentLoaded, &h)
	if errors.Is(err, sql.ErrNoRows) {
		return c, "", false, nil
	}
	if err != nil {
		return c, "", false, err
	}
	c.UID, c.UIDValidity = uint32(u), uint32(uv)
	return c, h.String, true, nil
}
