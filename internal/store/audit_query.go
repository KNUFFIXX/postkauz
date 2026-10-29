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
	return auditQuery(ctx, s.db, f)
}

// auditQuery ist AuditQuery auf einer Datenbank mit der Tabelle
// audit_events (auch einem Archiv, AuditArchiveDB).
func auditQuery(ctx context.Context, db interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}, f AuditFilter) ([]AuditRecord, error) {
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
	rows, err := db.QueryContext(ctx, q, args...)
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

// LongRunOps sind die Operationen, die eine Kontosperre halten (Sync und
// Import; ein sync --watch ist ein einziger Aufruf über Stunden). Ihre
// offenen Starts zählt doctor nicht, solange die Sperre ihres Kontos
// gehalten wird, und wer die Sperre holt, schließt die toten ab (A-F9).
var LongRunOps = []string{"sync.run", "import.run"}

// openStart ist die Bedingung für einen Start-Eintrag a ohne Abschluss,
// der nicht zu einem offenen Ablageversuch gehört (drafts.push_attempt:
// Ausgang unbekannt, der nächste Push klärt ihn; A7).
const openStart = `a.result = 'started'
		AND NOT EXISTS (SELECT 1 FROM audit_events c WHERE c.ref_id = a.id)
		AND NOT EXISTS (SELECT 1 FROM drafts d WHERE d.push_attempt IS NOT NULL AND json_extract(d.push_attempt, '$.audit_id') = a.id)`

// startOfAccount ist die Bedingung "Start gehört zum Konto ?": Ziel
// account:<name>, oder ohne Ziel (sync ohne --account), solange es genau
// ein Konto gibt (nur dann löst ResolveAccount ein leeres Konto auf).
const startOfAccount = `(EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END) t WHERE t.value = 'account:' || ?)
		OR (COALESCE(json_array_length(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END), 0) = 0
			AND (SELECT count(*) FROM accounts) = 1))`

func longRunIn() string {
	return `a.operation IN ('` + strings.Join(LongRunOps, `','`) + `')`
}

// AuditOpenStarts zählt Start-Einträge ohne Abschluss, die vor before
// begannen (doctor, A1: abgestürzte oder abgebrochene Aufrufe). Nicht
// gezählt werden Starts eines offenen Ablageversuchs (A7) und Starts von
// Sync und Import (LongRunOps) der Konten in busy, deren Kontosperre
// gehalten wird (ein Lauf ist noch im Gang; A-F9: je Konto, nicht alle).
// Ein Start ohne Ziel zählt nicht, solange irgendeine Sperre gehalten wird.
// Starts von Sync und Import im Trockenlauf zählen nie: Sie halten keine
// Sperre, laufen also womöglich noch, und niemand schließt sie ab
// (CloseDeadStarts); geändert haben sie nichts.
func (s *SQLite) AuditOpenStarts(ctx context.Context, before time.Time, busy ...string) (int, error) {
	q := `SELECT count(*) FROM audit_events a WHERE ` + openStart + ` AND a.ts < ? AND NOT (` + longRunIn() + ` AND a.dry_run = 1)`
	args := []any{before.UnixMilli()}
	if len(busy) > 0 {
		q += ` AND NOT (` + longRunIn() + ` AND (COALESCE(json_array_length(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END), 0) = 0
			OR EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END) t
				WHERE t.value IN (` + strings.TrimSuffix(strings.Repeat("?,", len(busy)), ",") + `))))`
		for _, b := range busy {
			args = append(args, "account:"+b)
		}
	}
	var n int
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// DeadStartAge: So alt muss ein offener Start sein, bevor ein anderer
// Lauf desselben Kontos ihn als tot abschließt (CloseDeadStarts), und ab
// diesem Alter zählt doctor einen offenen Start als abgebrochenen Aufruf.
// Bis T1b galt für das Abschließen eine Minute: Ein sync.run oder
// import.run, der vor dem Holen der Kontosperre länger brauchte (langsamer
// Schlüsselbund, Discover über eine große Quelle), wurde von einem später
// gestarteten Lauf als process_ended abgeschlossen, und sein echter
// Abschluss scheiterte am UNIQUE-Index audit_events_ref (T2 Punkt 7,
// Prüfsitzung DRACO-PK09). Beide Werte sind seit T2 dieselbe Konstante.
const DeadStartAge = 10 * time.Minute

// CloseDeadStarts schließt die offenen Starts von Sync und Import
// (LongRunOps) des Kontos account ab (A-F9). Aufrufen nur, während man die
// Kontosperre hält: Dann läuft kein anderer Lauf des Kontos, ein offener
// Start stammt von einem hart beendeten Prozess (z. B. sync --watch, von
// der Aufgabenplanung beendet). Je Start ein Abschluss error/process_ended
// mit ref_id, Zeitstempel now, ohne Dauer; Akteur, Schlüssel, Scope,
// Operation, Parameter und Ziele wie der Start (Festlegung 1). Nur
// anhängen. exceptID ist der eigene Start (0 = keiner); geschlossen werden
// nur ältere Starts, die mindestens DeadStartAge alt sind. Liefert die
// Zahl der Abschlüsse.
//
// Ein Start mit mehreren Konten (sync --watch --all, T2) gehört einem
// Prozess, der mehrere Kontosperren hält. Er wird nur abgeschlossen, wenn
// busy für keines seiner anderen Konten true meldet (busy prüft die Sperre
// des Kontos; nil = keines gilt als belegt). So schließt ein Lauf für
// Konto a nicht den Start eines --all-Prozesses, der für Konto b noch
// läuft.
//
// Trockenläufe (dry_run = 1) schließt CloseDeadStarts nie ab: sync und
// import im Trockenlauf holen keine Kontosperre, ein solcher Start kann
// also zu einem Lauf gehören, der parallel zum Sperrhalter noch läuft; sein
// echter Abschluss scheiterte sonst am UNIQUE-Index audit_events_ref
// (Codex-Runde auf 589bb8d, Prüfsitzung DRACO-PK09). Ob sein Prozess noch
// lebt, lässt sich nicht nachweisen; ein Trockenlauf ändert nichts außer
// dem Audit, deshalb bleibt ein toter Trockenlauf-Start offen (doctor zählt
// ihn nicht, AuditOpenStarts).
func (s *SQLite) CloseDeadStarts(ctx context.Context, account string, exceptID int64, now time.Time, busy func(account string) bool) (int, error) {
	q := `SELECT a.id, a.targets FROM audit_events a
		WHERE ` + openStart + ` AND ` + longRunIn() + ` AND a.dry_run = 0 AND ` + startOfAccount + ` AND a.ts <= ?`
	args := []any{account, now.Add(-DeadStartAge).UnixMilli()}
	if exceptID > 0 {
		q += ` AND a.id < ?`
		args = append(args, exceptID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY a.id`, args...)
	if err != nil {
		return 0, fmt.Errorf("store: close dead starts: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		var targets string
		if err := rows.Scan(&id, &targets); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("store: close dead starts: %w", err)
		}
		if busy != nil && otherAccountBusy(targets, account, busy) {
			continue
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("store: close dead starts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("store: close dead starts: %w", err)
	}
	n := 0
	for _, id := range ids {
		// Je Start eine Anweisung; NOT EXISTS hält sie wiederholbar, wenn ein
		// anderer Prozess denselben Start (mehrere Konten) gleichzeitig
		// abschließt.
		res, err := s.db.ExecContext(ctx, `INSERT INTO audit_events(ts, actor_type, api_key_id, scope, operation, params_redacted,
			targets, result, reason, dry_run, duration_ms, policy_version, ref_id)
			SELECT ?, a.actor_type, a.api_key_id, a.scope, a.operation, a.params_redacted, a.targets, 'error', 'process_ended',
			a.dry_run, NULL, a.policy_version, a.id FROM audit_events a
			WHERE a.id = ? AND NOT EXISTS (SELECT 1 FROM audit_events c WHERE c.ref_id = a.id)`, now.UnixMilli(), id)
		if err != nil {
			return n, fmt.Errorf("store: close dead starts: %w", err)
		}
		k, _ := res.RowsAffected()
		n += int(k)
	}
	return n, nil
}

// otherAccountBusy meldet, ob ein Ziel account:<x> mit x != account belegt
// ist (busy).
func otherAccountBusy(targets, account string, busy func(string) bool) bool {
	var ts []string
	if json.Unmarshal([]byte(targets), &ts) != nil {
		return false
	}
	for _, t := range ts {
		if name, ok := strings.CutPrefix(t, "account:"); ok && name != account && busy(name) {
			return true
		}
	}
	return false
}

// LastAuditEvent liefert den letzten Eintrag (größte ID) der Operation op
// mit dem Ziel target (z. B. oauth.refresh, account:<name>; doctor, B-M4).
// ok = false, wenn es keinen gibt.
func (s *SQLite) LastAuditEvent(ctx context.Context, op, target string) (r AuditRecord, ok bool, err error) {
	r, err = scanAudit(s.db.QueryRowContext(ctx, `SELECT `+auditCols+` FROM audit_events a WHERE a.operation = ?
		AND EXISTS (SELECT 1 FROM json_each(CASE WHEN json_valid(a.targets) THEN a.targets ELSE '[]' END) t WHERE t.value = ?)
		ORDER BY a.id DESC LIMIT 1`, op, target))
	if errors.Is(err, sql.ErrNoRows) {
		return AuditRecord{}, false, nil
	}
	return r, err == nil, err
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
// Tages (ein Tag wird erneut exportiert, solange er noch läuft). In
// derselben Transaktion fallen alle Merker dieses Tages weg
// (AuditExportPending): Ab jetzt belegt der Nachweis die Datei. Seit der
// zweiten Nachbesserung T3 (Punkt 7b) auch die an anderen Orten: Ein
// Merker in einem aufgegebenen Ordner bliebe sonst für immer stehen.
func (s *SQLite) SaveAuditExport(ctx context.Context, e AuditExport) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_exports(day, first_event_id, last_event_id, sha256, location, exported_at)
		VALUES (?,?,?,?,?,?) ON CONFLICT(day) DO UPDATE SET first_event_id = excluded.first_event_id,
		last_event_id = excluded.last_event_id, sha256 = excluded.sha256, location = excluded.location,
		exported_at = excluded.exported_at`, e.Day, e.FirstEventID, e.LastEventID, e.SHA256, e.Location, e.ExportedAt.UnixMilli()); err != nil {
		return err
	}
	list, err := auditExportPending(ctx, tx)
	if err != nil {
		return err
	}
	keep := list[:0]
	for _, p := range list {
		if p.Day != e.Day {
			keep = append(keep, p)
		}
	}
	if err := setAuditExportPending(ctx, tx, keep); err != nil {
		return err
	}
	return tx.Commit()
}

// metaAuditExportPending ist der Schlüssel der Export-Merker in meta.
const metaAuditExportPending = "audit_export_pending"

// AuditExportPending ist der Merker einer Exportdatei, die geschrieben
// wird, bevor ihr Nachweis in audit_exports steht (Nachbesserung T3,
// Punkt 1): Bricht der Export zwischen Datei und Nachweis ab (Strg+C,
// Sperre der Datenbank), weist der Merker die Datei weiter als eigene aus.
// Kein Schema: eine JSON-Liste in meta.
type AuditExportPending struct {
	Day      string `json:"day"`
	Location string `json:"location"`
	SHA256   string `json:"sha256"`
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func auditExportPending(ctx context.Context, q queryer) ([]AuditExportPending, error) {
	var v string
	err := q.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaAuditExportPending).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []AuditExportPending
	if err := json.Unmarshal([]byte(v), &list); err != nil {
		return nil, fmt.Errorf("store: meta %s: %w", metaAuditExportPending, err)
	}
	return list, nil
}

func setAuditExportPending(ctx context.Context, q queryer, list []AuditExportPending) error {
	if len(list) == 0 {
		_, err := q.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, metaAuditExportPending)
		return err
	}
	b, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaAuditExportPending, string(b))
	return err
}

// AuditExportPending liefert die offenen Merker (leer = keiner).
func (s *SQLite) AuditExportPending(ctx context.Context) ([]AuditExportPending, error) {
	return auditExportPending(ctx, s.db)
}

// AddAuditExportPending setzt einen Merker, bevor die Datei geschrieben
// wird. Frühere Merker desselben Tages und Ortes bleiben: Bricht auch
// dieser Lauf vor dem Schreiben ab, liegt dort noch die Datei des früheren.
func (s *SQLite) AddAuditExportPending(ctx context.Context, p AuditExportPending) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	list, err := auditExportPending(ctx, tx)
	if err != nil {
		return err
	}
	for _, q := range list {
		if q == p {
			return nil
		}
	}
	if err := setAuditExportPending(ctx, tx, append(list, p)); err != nil {
		return err
	}
	return tx.Commit()
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
