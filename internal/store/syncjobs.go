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

// JobSync ist ein angestoßener Sync eines Kontos (sync.request, MCP
// mail_sync_now; T2, ADR-009 E1, Entscheid 23). Der Typ ist seit Migration
// 0001 erlaubt; ausgeführt wird er vom Prozess mit Zugangsdaten und
// Kontosperre (sync --watch je --jobs-interval), ein einmaliger sync des
// Kontos erledigt ihn mit.
const JobSync = "sync"

// syncPayload ist payload eines Jobs sync: Konto, Ordner (leer = alle) und
// Zeitpunkt der letzten Anforderung (ms). requested_at unterscheidet eine
// Anforderung, die während eines Laufs dazukam, von der, die der Lauf
// erledigt (CompleteSyncJob, SettleSyncJobs).
type syncPayload struct {
	AccountID   int64    `json:"account_id"`
	Mailboxes   []string `json:"mailboxes,omitempty"`
	RequestedAt int64    `json:"requested_at"`
}

// SyncJob ist ein offener Job sync mit Nutzlast und Herkunft.
type SyncJob struct {
	Job
	// Mailboxes: nur diese Ordner (leer = alle).
	Mailboxes []string
	// RequestedAt: letzte Anforderung (ms seit 1970).
	RequestedAt int64
	// Origin: wer ihn zuerst eingereiht hat (bleibt, solange er offen ist).
	Origin JobOrigin
}

// MaxSyncJobMailboxes: Mehr Ordner in einem offenen Job werden zu „alle".
const MaxSyncJobMailboxes = 20

// mergeMailboxes vereinigt die Ordner zweier Anforderungen; leer heißt
// alle und gewinnt.
func mergeMailboxes(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	out := slices.Clone(a)
	for _, m := range b {
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	if len(out) > MaxSyncJobMailboxes {
		return nil
	}
	slices.Sort(out)
	return out
}

// SyncRequestResult ist das Ergebnis von RequestSyncJob.
type SyncRequestResult struct {
	// ID: der offene Job (Created: neu) bzw. bei TooSoon der letzte Job
	// des Kontos.
	ID      int64
	Created bool
	// TooSoon: Die letzte Anforderung liegt weniger als den Mindestabstand
	// zurück, und es gibt keinen noch wartenden Job, dem sie sich
	// anschließen könnte; nichts wurde geändert (V18). RetryAt: ab dann
	// wirkt eine neue Anforderung.
	TooSoon bool
	RetryAt time.Time
	// State: Zustand des Jobs (queued, running, done, failed).
	State string
}

// RequestSyncJob ist EnqueueSyncJob mit Mindestabstand (V18 der
// Nachbesserung T2, mail_sync_now): Wartet schon ein Job des Kontos
// (queued), nimmt er die Anforderung auf wie bisher (kein zusätzlicher
// Lauf). Sonst gilt: Liegt die letzte Anforderung (requested_at des
// jüngsten Jobs sync, gleich in welchem Zustand) weniger als minGap
// zurück, ändert sich nichts (TooSoon); ein laufender Job bekäme sonst
// einen Nachlauf, ein erledigter einen neuen Lauf, und ein Agent könnte
// das Postfach im Takt von --jobs-interval abfragen lassen. minGap <= 0:
// ohne Mindestabstand.
func (s *SQLite) RequestSyncJob(ctx context.Context, accountID int64, mailboxes []string, now time.Time, origin JobOrigin,
	minGap time.Duration) (SyncRequestResult, error) {
	if minGap > 0 {
		var id int64
		var state, raw string
		err := s.db.QueryRowContext(ctx, `SELECT id, state, payload FROM jobs WHERE type = ? AND json_extract(payload, '$.account_id') = ?
			ORDER BY id DESC LIMIT 1`, JobSync, accountID).Scan(&id, &state, &raw)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return SyncRequestResult{}, err
		case state != "queued":
			var p syncPayload
			if err := json.Unmarshal([]byte(raw), &p); err != nil {
				return SyncRequestResult{}, fmt.Errorf("store: sync job %d: payload: %w", id, err)
			}
			if last := time.UnixMilli(p.RequestedAt); now.Sub(last) < minGap {
				return SyncRequestResult{ID: id, TooSoon: true, RetryAt: last.Add(minGap), State: state}, nil
			}
		}
	}
	id, created, err := s.EnqueueSyncJob(ctx, accountID, mailboxes, now, origin)
	return SyncRequestResult{ID: id, Created: created, State: "queued"}, err
}

// EnqueueSyncJob reiht den Job sync für ein Konto ein (höchstens einer
// offen je Konto, Index jobs_open). Ist schon einer offen, nimmt er diese
// Anforderung auf: Ordner vereinigt (ohne Ordner = alle), requested_at
// neu, ein später fälliger wird sofort fällig; seine Herkunft bleibt die
// des ersten Aufrufs (wie draft_push, A8). Liefert die ID des offenen Jobs
// und ob er neu ist.
func (s *SQLite) EnqueueSyncJob(ctx context.Context, accountID int64, mailboxes []string, now time.Time, origin JobOrigin) (id int64, created bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT id, payload FROM jobs WHERE type = ? AND state IN ('queued','running')
		AND json_extract(payload, '$.account_id') = ?`, JobSync, accountID).Scan(&id, &raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		var b []byte
		b, err = json.Marshal(syncPayload{AccountID: accountID, Mailboxes: mergeMailboxes(mailboxes, mailboxes), RequestedAt: now.UnixMilli()})
		if err != nil {
			return 0, false, err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO jobs(type, payload, run_at, key_id, audit_id) VALUES (?, ?, ?, ?, ?) RETURNING id`,
			JobSync, string(b), now.UnixMilli(), nullStr(origin.KeyID), nullInt(origin.AuditID)).Scan(&id)
		if err != nil {
			return 0, false, fmt.Errorf("store: enqueue sync: %w", err)
		}
		created = true
	case err != nil:
		return 0, false, err
	default:
		var p syncPayload
		if err = json.Unmarshal([]byte(raw), &p); err != nil {
			return 0, false, fmt.Errorf("store: enqueue sync: payload: %w", err)
		}
		p.Mailboxes = mergeMailboxes(p.Mailboxes, mailboxes)
		p.RequestedAt = max(p.RequestedAt+1, now.UnixMilli())
		var b []byte
		if b, err = json.Marshal(p); err != nil {
			return 0, false, err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE jobs SET payload = ?,
			run_at = CASE WHEN state = 'queued' AND run_at > ? THEN ? ELSE run_at END WHERE id = ?`,
			string(b), now.UnixMilli(), now.UnixMilli(), id); err != nil {
			return 0, false, fmt.Errorf("store: enqueue sync: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, false, err
	}
	return id, created, nil
}

// ClaimSyncJob übernimmt den fälligen Job sync des Kontos (ClaimJob) und
// liest Nutzlast und Herkunft. ok = false: nichts zu tun.
func (s *SQLite) ClaimSyncJob(ctx context.Context, accountID int64, owner string, lease time.Duration, takeOver bool) (SyncJob, bool, error) {
	j, ok, err := s.ClaimJob(ctx, JobSync, accountID, owner, lease, takeOver)
	if err != nil || !ok {
		return SyncJob{}, ok, err
	}
	sj := SyncJob{Job: j}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT payload, COALESCE(key_id, ''), COALESCE(audit_id, 0) FROM jobs WHERE id = ?`, j.ID).
		Scan(&raw, &sj.Origin.KeyID, &sj.Origin.AuditID); err != nil {
		return sj, true, err
	}
	var p syncPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return sj, true, fmt.Errorf("store: sync job %d: payload: %w", j.ID, err)
	}
	sj.Mailboxes, sj.RequestedAt = p.Mailboxes, p.RequestedAt
	return sj, true, nil
}

// CompleteSyncJob beendet einen ausgeführten Job sync. Kam während des
// Laufs eine neue Anforderung dazu (requested_at größer als beim
// Übernehmen), bleibt er fällig (queued). done meldet, ob er erledigt ist.
func (s *SQLite) CompleteSyncJob(ctx context.Context, j SyncJob, owner string) (done bool, err error) {
	now := time.Now().UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET
		state = CASE WHEN COALESCE(json_extract(payload, '$.requested_at'), 0) > ? THEN 'queued' ELSE 'done' END,
		locked_by = NULL, locked_at = NULL, last_error = NULL, attempts = 0, run_at = ?
		WHERE id = ? AND state = 'running' AND locked_by = ?`, j.RequestedAt, now, j.ID, owner)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, ErrJobLost
	}
	var state string
	if err := s.db.QueryRowContext(ctx, `SELECT state FROM jobs WHERE id = ?`, j.ID).Scan(&state); err != nil {
		return false, err
	}
	return state == "done", nil
}

// SettleSyncJobs erledigt die offenen Jobs sync des Kontos, die ein
// fehlerfreier Lauf mit beginn since abdeckt (T2): angefordert bis since
// und mit Ordnern, die der Lauf alle gespiegelt hat (mailboxes leer = der
// Lauf ging über alle Ordner). Aufrufen nur unter der Kontosperre: Ein
// Job im Zustand running stammt dann von einem abgebrochenen Prozess und
// wird mit erledigt. Liefert die Zahl.
func (s *SQLite) SettleSyncJobs(ctx context.Context, accountID int64, mailboxes []string, since time.Time) (int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, payload FROM jobs WHERE type = ? AND state IN ('queued','running')
		AND json_extract(payload, '$.account_id') = ?`, JobSync, accountID)
	if err != nil {
		return 0, err
	}
	type cand struct {
		id int64
		p  syncPayload
	}
	var cs []cand
	for rows.Next() {
		var c cand
		var raw string
		if err := rows.Scan(&c.id, &raw); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if json.Unmarshal([]byte(raw), &c.p) == nil {
			cs = append(cs, c)
		}
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range cs {
		if c.p.RequestedAt > since.UnixMilli() {
			continue
		}
		if len(mailboxes) > 0 && (len(c.p.Mailboxes) == 0 || slices.ContainsFunc(c.p.Mailboxes, func(m string) bool {
			return !slices.Contains(mailboxes, m)
		})) {
			continue
		}
		res, err := s.db.ExecContext(ctx, `UPDATE jobs SET state = 'done', locked_by = NULL, locked_at = NULL, last_error = NULL,
			attempts = 0, run_at = ? WHERE id = ? AND state IN ('queued','running') AND COALESCE(json_extract(payload, '$.requested_at'), 0) = ?`,
			time.Now().UnixMilli(), c.id, c.p.RequestedAt)
		if err != nil {
			return n, err
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

// SyncJobState liefert Zustand und letzten Fehler eines Jobs (Tests,
// Anzeigen); ok = false, wenn es ihn nicht gibt.
func (s *SQLite) SyncJobState(ctx context.Context, id int64) (state, lastError string, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT state, COALESCE(last_error, '') FROM jobs WHERE id = ?`, id).Scan(&state, &lastError)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return state, lastError, err == nil, err
}
