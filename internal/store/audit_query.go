package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// AuditRecord ist ein gespeicherter Audit-Eintrag mit seiner ID.
type AuditRecord struct {
	ID int64
	audit.Event
}

// AuditFilter wählt Audit-Einträge (alle Felder optional).
type AuditFilter struct {
	Since, Until time.Time // Until ausschließlich
	Actor        string
	Key          string
	Operation    string
	Result       string
	AfterID      int64 // nur Einträge mit größerer ID (Seiten)
	Limit        int
}

const auditCols = `id, ts, actor_type, COALESCE(api_key_id,''), COALESCE(scope,''), operation, params_redacted, targets,
	result, COALESCE(reason,''), dry_run, COALESCE(duration_ms, 0), COALESCE(policy_version, 0)`

func scanAudit(sc interface{ Scan(...any) error }) (AuditRecord, error) {
	var r AuditRecord
	var ts, dur int64
	var actor, params, targets, result string
	var dry int
	if err := sc.Scan(&r.ID, &ts, &actor, &r.KeyID, &r.Scope, &r.Operation, &params, &targets, &result, &r.Reason, &dry, &dur, &r.PolicyVersion); err != nil {
		return r, err
	}
	r.TS = unixMilliUTC(ts)
	r.ActorType = audit.ActorType(actor)
	r.ParamsRedacted = []byte(params)
	_ = json.Unmarshal([]byte(targets), &r.Targets)
	r.Result = audit.Result(result)
	r.DryRun = dry == 1
	r.Duration = time.Duration(dur) * time.Millisecond
	return r, nil
}

// AuditQuery liefert Audit-Einträge aufsteigend nach ID.
func (s *SQLite) AuditQuery(ctx context.Context, f AuditFilter) ([]AuditRecord, error) {
	var where []string
	var args []any
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) }
	if !f.Since.IsZero() {
		add("ts >= ?", f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		add("ts < ?", f.Until.UnixMilli())
	}
	if f.Actor != "" {
		add("actor_type = ?", f.Actor)
	}
	if f.Key != "" {
		add("api_key_id = ?", f.Key)
	}
	if f.Operation != "" {
		add("operation = ?", f.Operation)
	}
	if f.Result != "" {
		add("result = ?", f.Result)
	}
	if f.AfterID > 0 {
		add("id > ?", f.AfterID)
	}
	q := `SELECT ` + auditCols + ` FROM audit_events`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY id`
	if f.Limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditRecord
	for rows.Next() {
		r, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AuditDays liefert die Tage (UTC, YYYY-MM-DD) mit Audit-Einträgen, aufsteigend.
func (s *SQLite) AuditDays(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT strftime('%Y-%m-%d', ts / 1000, 'unixepoch') FROM audit_events ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AuditExport ist eine Zeile der Tabelle audit_exports: Nachweis, dass die
// Einträge eines Tages in eine Datei an einem zweiten Ort geschrieben wurden.
type AuditExport struct {
	Day          string // YYYY-MM-DD (UTC)
	FirstEventID int64
	LastEventID  int64
	SHA256       string // Prüfsumme der Datei
	Location     string // Verzeichnis, ohne Zugangsdaten
	ExportedAt   time.Time
}

// SaveAuditExport trägt einen Export ein oder ersetzt den Eintrag desselben
// Tages (ein Tag wird erneut exportiert, solange er noch läuft).
func (s *SQLite) SaveAuditExport(ctx context.Context, e AuditExport) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_exports(day, first_event_id, last_event_id, sha256, location, exported_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(day) DO UPDATE SET first_event_id = excluded.first_event_id,
		last_event_id = excluded.last_event_id, sha256 = excluded.sha256, location = excluded.location,
		exported_at = excluded.exported_at`, e.Day, e.FirstEventID, e.LastEventID, e.SHA256, e.Location, e.ExportedAt.UnixMilli())
	return err
}

// AuditExports liefert alle Export-Nachweise nach Tag.
func (s *SQLite) AuditExports(ctx context.Context) ([]AuditExport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT day, first_event_id, last_event_id, sha256, location, exported_at FROM audit_exports ORDER BY day`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditExport
	for rows.Next() {
		var e AuditExport
		var at int64
		if err := rows.Scan(&e.Day, &e.FirstEventID, &e.LastEventID, &e.SHA256, &e.Location, &at); err != nil {
			return nil, err
		}
		e.ExportedAt = unixMilliUTC(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// AppendID schreibt einen Eintrag und liefert seine ID (audit.IDWriter).
func (w sqliteAudit) AppendID(ctx context.Context, e audit.Event) (int64, error) {
	params := e.ParamsRedacted
	if len(params) == 0 {
		params = []byte(`{}`)
	}
	if !json.Valid(params) {
		return 0, fmt.Errorf("store: audit: params_redacted is not valid JSON")
	}
	targets := e.Targets
	if targets == nil {
		targets = []string{}
	}
	tj, err := json.Marshal(targets)
	if err != nil {
		return 0, err
	}
	return appendAudit(ctx, w.s.db, e, params, tj)
}

// appendAudit schreibt einen geprüften Eintrag; q ist die Datenbank oder
// eine Transaktion (Systemereignisse der Entwürfe stehen in derselben
// Transaktion wie ihr Übergang, DraftTx.AppendAudit).
func appendAudit(ctx context.Context, q querier, e audit.Event, params []byte, targets []byte) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO audit_events(ts, actor_type, api_key_id, scope, operation,
		params_redacted, targets, result, reason, dry_run, duration_ms, policy_version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		e.TS.UnixMilli(), string(e.ActorType), nullStr(e.KeyID), nullStr(e.Scope), e.Operation, string(params), string(targets),
		string(e.Result), nullStr(e.Reason), boolInt(e.DryRun), durationMilli(e.Duration), nullInt(e.PolicyVersion)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: audit append: %w", err)
	}
	return id, nil
}

func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func durationMilli(d time.Duration) any {
	if d <= 0 {
		return nil
	}
	return d.Milliseconds()
}
