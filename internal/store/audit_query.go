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
	// EndID ist in AuditCalls die ID des Abschluss-Eintrags (0 = keiner,
	// der Aufruf läuft noch oder brach ab); sonst immer 0.
	EndID int64
}

// AuditFilter wählt Audit-Einträge (alle Felder optional).
type AuditFilter struct {
	Since, Until time.Time // Until ausschließlich
	Actor        string
	Key          string
	Operation    string
	Result       string
	AfterID      int64 // nur Einträge mit größerer ID (Seiten)
	// UpToID: nur Einträge mit ID bis einschließlich (0 = ohne Grenze).
	UpToID int64
	// OpenSince/OpenAfterID: nur Einträge mit ts >= OpenSince ODER
	// id > OpenAfterID (beide gesetzt; Kandidaten der noch offenen
	// Exporttage, ops.buildAuditPlan).
	OpenSince   time.Time
	OpenAfterID int64
	Limit       int
}

const auditCols = `id, ts, actor_type, COALESCE(api_key_id,''), COALESCE(scope,''), operation, params_redacted, targets,
	result, COALESCE(reason,''), dry_run, COALESCE(duration_ms, 0), COALESCE(policy_version, 0),
	COALESCE(ref_id, 0)`

func scanAudit(sc interface{ Scan(...any) error }) (AuditRecord, error) {
	var r AuditRecord
	var ts, dur int64
	var actor, params, targets, result string
	var dry int
	if err := sc.Scan(&r.ID, &ts, &actor, &r.KeyID, &r.Scope, &r.Operation, &params, &targets, &result, &r.Reason, &dry, &dur, &r.PolicyVersion, &r.RefID); err != nil {
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
	if f.UpToID > 0 {
		add("id <= ?", f.UpToID)
	}
	if !f.OpenSince.IsZero() {
		where = append(where, "(ts >= ? OR id > ?)")
		args = append(args, f.OpenSince.UnixMilli(), f.OpenAfterID)
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

// AuditCalls liefert je Aufruf eine Zeile (A1 Variante A, seit Migration
// 0018): den Start-Eintrag mit Ergebnis, Grund und Dauer seines Abschlusses
// (EndID). Einträge ohne Verweis, die kein Start sind (Policy-Ablehnungen,
// Systemereignisse, Einträge vor der Migration), erscheinen unverändert;
// Abschluss-Einträge nur über ihren Start. Ein Start ohne Abschluss bleibt
// started. Since, Until, Actor, Key, Operation und AfterID gelten für den
// Start, Result für das Endergebnis; aufsteigend nach der ID des Starts.
func (s *SQLite) AuditCalls(ctx context.Context, f AuditFilter) ([]AuditRecord, error) {
	where := []string{"a.ref_id IS NULL"}
	var args []any
	add := func(cond string, v any) { where = append(where, cond); args = append(args, v) }
	if !f.Since.IsZero() {
		add("a.ts >= ?", f.Since.UnixMilli())
	}
	if !f.Until.IsZero() {
		add("a.ts < ?", f.Until.UnixMilli())
	}
	if f.Actor != "" {
		add("a.actor_type = ?", f.Actor)
	}
	if f.Key != "" {
		add("a.api_key_id = ?", f.Key)
	}
	if f.Operation != "" {
		add("a.operation = ?", f.Operation)
	}
	if f.Result != "" {
		add("COALESCE(c.result, a.result) = ?", f.Result)
	}
	if f.AfterID > 0 {
		add("a.id > ?", f.AfterID)
	}
	q := `SELECT a.id, a.ts, a.actor_type, COALESCE(a.api_key_id,''), COALESCE(a.scope,''), a.operation, a.params_redacted,
		a.targets, COALESCE(c.result, a.result), COALESCE(c.reason, a.reason, ''), a.dry_run,
		COALESCE(c.duration_ms, a.duration_ms, 0), COALESCE(a.policy_version, 0), 0, COALESCE(c.id, 0)
		FROM audit_events a LEFT JOIN audit_events c ON c.ref_id = a.id
		WHERE ` + strings.Join(where, " AND ") + ` ORDER BY a.id`
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
		var end int64
		r, err := scanAudit(scanExtra{rows, &end})
		if err != nil {
			return nil, err
		}
		r.EndID = end
		out = append(out, r)
	}
	return out, rows.Err()
}

// scanExtra hängt an die Spalten von scanAudit weitere Ziele an.
type scanExtra struct {
	sc    interface{ Scan(...any) error }
	extra *int64
}

func (x scanExtra) Scan(dest ...any) error { return x.sc.Scan(append(dest, x.extra)...) }

// AuditOpenStarts zählt Start-Einträge ohne Abschluss, die vor before
// begannen (doctor, A1: abgestürzte oder abgebrochene Aufrufe). Nicht
// gezählt werden Starts eines offenen Ablageversuchs (drafts.push_attempt:
// Ausgang unbekannt, der nächste Push klärt ihn; A7) und Starts der
// Operationen in skipOps (doctor: sync.run und import.run, solange ein Sync
// oder Import läuft: ein sync --watch ist ein einziger Aufruf über Stunden).
func (s *SQLite) AuditOpenStarts(ctx context.Context, before time.Time, skipOps ...string) (int, error) {
	q := `SELECT count(*) FROM audit_events a WHERE a.result = 'started' AND a.ts < ?
		AND NOT EXISTS (SELECT 1 FROM audit_events c WHERE c.ref_id = a.id)
		AND NOT EXISTS (SELECT 1 FROM drafts d WHERE d.push_attempt IS NOT NULL AND json_extract(d.push_attempt, '$.audit_id') = a.id)`
	args := []any{before.UnixMilli()}
	if len(skipOps) > 0 {
		q += ` AND a.operation NOT IN (` + strings.TrimSuffix(strings.Repeat("?,", len(skipOps)), ",") + `)`
		for _, op := range skipOps {
			args = append(args, op)
		}
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// AuditCountUpTo zählt die Einträge mit ID bis einschließlich id (audit
// verify, Zählabgleich der abgeschlossenen Tage; nur über den Primärschlüssel,
// ohne Zeilen zu lesen).
func (s *SQLite) AuditCountUpTo(ctx context.Context, id int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE id <= ?`, id).Scan(&n)
	return n, err
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
		params_redacted, targets, result, reason, dry_run, duration_ms, policy_version, ref_id) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) RETURNING id`,
		e.TS.UnixMilli(), string(e.ActorType), nullStr(e.KeyID), nullStr(e.Scope), e.Operation, string(params), string(targets),
		string(e.Result), nullStr(e.Reason), boolInt(e.DryRun), durationMilli(e.Duration), nullInt(e.PolicyVersion),
		nullInt(e.RefID)).Scan(&id)
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
