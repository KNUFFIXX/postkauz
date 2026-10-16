package store

import (
	"context"
	"encoding/json"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// AuditWriter liefert den SQLite-Audit-Writer: Er hängt Ereignisse an
// audit_events an (append-only, per Trigger gesichert) und ersetzt im CLI
// den Platzhalter audit.Nop.
func (s *SQLite) AuditWriter() audit.Writer { return sqliteAudit{s} }

type sqliteAudit struct{ s *SQLite }

// Append schreibt einen Eintrag (audit.Writer).
func (w sqliteAudit) Append(ctx context.Context, e audit.Event) error {
	_, err := w.AppendID(ctx, e)
	return err
}

// AuditEvents liest die Ereignisse ab einer ID (aufsteigend), höchstens limit.
// Für Tests und später `audit` (S1.11).
func (s *SQLite) AuditEvents(ctx context.Context, afterID int64, limit int) ([]audit.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ts, actor_type, COALESCE(api_key_id,''), COALESCE(scope,''), operation,
		params_redacted, targets, result, COALESCE(reason,''), dry_run FROM audit_events WHERE id > ? ORDER BY id LIMIT ?`,
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []audit.Event
	for rows.Next() {
		var e audit.Event
		var ts int64
		var actor, params, targets, result string
		var dry int
		if err := rows.Scan(&ts, &actor, &e.KeyID, &e.Scope, &e.Operation, &params, &targets, &result, &e.Reason, &dry); err != nil {
			return nil, err
		}
		e.TS = unixMilliUTC(ts)
		e.ActorType = audit.ActorType(actor)
		e.ParamsRedacted = []byte(params)
		_ = json.Unmarshal([]byte(targets), &e.Targets)
		e.Result = audit.Result(result)
		e.DryRun = dry == 1
		out = append(out, e)
	}
	return out, rows.Err()
}
