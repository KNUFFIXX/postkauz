package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Warteschlange in SQLite (Tabelle jobs; docs/architektur.md, Abschnitt 7).
// Ein Job ist ein Arbeitsauftrag je Art und Konto („arbeite die wartenden
// Anhänge bzw. die Index-Warteschlange des Kontos ab"); der eigentliche
// Stand liegt in attachments.extract_status bzw. index_queue. Höchstens
// ein Job je Art und Konto ist offen (queued/running; Index jobs_open);
// Trigger legen ihn an, sobald Arbeit entsteht (Migration 0004).

// Job-Arten.
const (
	JobIndex   = "index"
	JobExtract = "extract"
	// JobDraftPush legt Entwürfe per APPEND im Entwürfe-Ordner ab (A02 E1;
	// Typ seit Migration 0010, eingereiht ab S2.3, ausgeführt seit S2.5 von
	// sync und sync --watch, internal/mailsync).
	JobDraftPush = "draft_push"
)

// Job ist ein offener Eintrag der Warteschlange.
type Job struct {
	ID        int64
	Type      string
	AccountID int64
	State     string
	Attempts  int
	LastError string
}

// ErrJobLost: Der Job gehört nicht mehr diesem Arbeiter (Sperre abgelaufen
// und von einem anderen übernommen).
var ErrJobLost = errors.New("store: job is no longer locked by this worker")

// ClaimJob übernimmt den offenen Job einer Art für ein Konto, wenn er
// fällig ist: queued mit run_at erreicht, oder running mit abgelaufener
// Sperre (lease; ein abgestürzter Arbeiter). takeOver übernimmt auch einen
// laufenden Job mit gültiger Sperre: Der Aufrufer hält die Kontosperre des
// Syncs, ein anderer Arbeiter für dieses Konto kann also nicht laufen
// (z. B. nach kill -9 ohne Wartezeit fortsetzen). ok = false: nichts zu tun.
func (s *SQLite) ClaimJob(ctx context.Context, typ string, accountID int64, owner string, lease time.Duration, takeOver bool) (j Job, ok bool, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return j, false, err
	}
	defer func() {
		if err != nil || !ok {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UnixMilli()
	var runAt int64
	var lockedAt sql.NullInt64
	err = tx.QueryRowContext(ctx, `SELECT id, state, attempts, COALESCE(last_error, ''), run_at, locked_at FROM jobs
		WHERE type = ? AND state IN ('queued','running') AND json_extract(payload, '$.account_id') = ?`, typ, accountID).
		Scan(&j.ID, &j.State, &j.Attempts, &j.LastError, &runAt, &lockedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return j, false, nil
	}
	if err != nil {
		return j, false, err
	}
	switch {
	case j.State == "queued" && runAt > now:
		return j, false, nil // noch nicht fällig (Wiederholung nach Fehler)
	case j.State == "running" && !takeOver && lockedAt.Valid && lockedAt.Int64 > now-lease.Milliseconds():
		return j, false, nil // ein anderer Arbeiter ist dran
	}
	if _, err = tx.ExecContext(ctx, `UPDATE jobs SET state = 'running', locked_by = ?, locked_at = ? WHERE id = ?`,
		owner, now, j.ID); err != nil {
		return j, false, err
	}
	j.Type, j.AccountID, j.State = typ, accountID, "running"
	if err = tx.Commit(); err != nil {
		return j, false, err
	}
	return j, true, nil
}

// TouchJob verlängert die Sperre eines laufenden Jobs (nach jedem Schritt).
func (s *SQLite) TouchJob(ctx context.Context, id int64, owner string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs SET locked_at = ? WHERE id = ? AND state = 'running' AND locked_by = ?`,
		time.Now().UnixMilli(), id, owner)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrJobLost
	}
	return nil
}

// CompleteJob beendet einen Job. Ist inzwischen neue Arbeit entstanden
// (ein anderer Prozess hat eingereiht, während der Job lief; die Trigger
// legen dann keinen zweiten Job an), bleibt er stattdessen fällig
// (queued). done meldet, ob er wirklich erledigt ist.
func (s *SQLite) CompleteJob(ctx context.Context, j Job, owner string) (done bool, err error) {
	var pending string
	switch j.Type {
	case JobIndex:
		pending = `SELECT EXISTS (SELECT 1 FROM index_queue q JOIN messages m ON m.id = q.message_id WHERE m.account_id = ?)`
	case JobExtract:
		pending = `SELECT EXISTS (SELECT 1 FROM attachments a JOIN messages m ON m.id = a.message_id
			WHERE m.account_id = ? AND a.extract_status = 'pending')`
	case JobDraftPush:
		pending = `SELECT EXISTS (SELECT 1 FROM drafts WHERE account_id = ? AND ` + draftPushPending + `)`
	default:
		return false, fmt.Errorf("store: unknown job type %q", j.Type)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var more bool
	if err = tx.QueryRowContext(ctx, pending, j.AccountID).Scan(&more); err != nil {
		return false, err
	}
	state := "done"
	if more {
		state = "queued"
	}
	res, err := tx.ExecContext(ctx, `UPDATE jobs SET state = ?, locked_by = NULL, locked_at = NULL, last_error = NULL,
		attempts = 0, run_at = ? WHERE id = ? AND state = 'running' AND locked_by = ?`, state, time.Now().UnixMilli(), j.ID, owner)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		err = ErrJobLost
		return false, err
	}
	return !more, tx.Commit()
}

// ReleaseJob gibt einen Job unerledigt zurück (Zeitbudget erschöpft,
// Abbruch); er ist ab runAt wieder fällig.
func (s *SQLite) ReleaseJob(ctx context.Context, id int64, owner string, runAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET state = 'queued', locked_by = NULL, locked_at = NULL, run_at = ?
		WHERE id = ? AND state = 'running' AND locked_by = ?`, runAt.UnixMilli(), id, owner)
	return err
}

// FailJob verbucht einen Fehler des Jobs (nicht eines einzelnen Anhangs):
// nach maxAttempts Fehlern endgültig failed, sonst wieder fällig ab retryAt.
// Der Fehlertext enthält keine Mailinhalte.
func (s *SQLite) FailJob(ctx context.Context, id int64, owner, errText string, maxAttempts int, retryAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET attempts = attempts + 1, last_error = ?, locked_by = NULL, locked_at = NULL,
		state = CASE WHEN attempts + 1 >= ? THEN 'failed' ELSE 'queued' END, run_at = ?
		WHERE id = ? AND locked_by = ?`, errText, maxAttempts, retryAt.UnixMilli(), id, owner)
	return err
}

// PruneJobs löscht erledigte und endgültig gescheiterte Jobs, die älter
// als before sind (die Tabelle bleibt klein).
func (s *SQLite) PruneJobs(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM jobs WHERE state IN ('done','failed') AND run_at < ?`, before.UnixMilli())
	return err
}

// JobInfo ist ein Job für Anzeigen (doctor, Tests).
type JobInfo struct {
	Type      string
	AccountID int64
	State     string
	Attempts  int
	LastError string
}

// OpenJobs liefert alle offenen und gescheiterten Jobs.
func (s *SQLite) OpenJobs(ctx context.Context) ([]JobInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT type, COALESCE(json_extract(payload, '$.account_id'), 0), state, attempts,
		COALESCE(last_error, '') FROM jobs WHERE state != 'done' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JobInfo
	for rows.Next() {
		var j JobInfo
		if err := rows.Scan(&j.Type, &j.AccountID, &j.State, &j.Attempts, &j.LastError); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
