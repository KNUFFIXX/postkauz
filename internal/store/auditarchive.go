package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// Rotation des Audits (T3, A03 E8, A14; docs/datenmodell.md,
// „Löschkonzept“): audit rotate kopiert die Einträge exportierter und
// bestätigter Tage in eine Archivdatenbank audit-archive-<von>-<bis>.db im
// Datenverzeichnis, hält Prüfsumme und Anker der Hash-Kette in
// audit_archives fest (Migration 0022, Abschnitt 3) und löscht die Zeilen
// danach in einer Transaktion (Trigger lösen, löschen, Trigger neu
// anlegen). Die Kette und die Zuordnung zu Exporttagen berechnet
// internal/ops (auditrotate.go).

// AuditArchivePrefix ist der Anfang jedes Archivnamens; Sicherungen
// (Paket B) erfassen audit-archive-*.db.
const AuditArchivePrefix = "audit-archive-"

// AuditArchiveName ist der Dateiname des Archivs der Tage from bis to.
func AuditArchiveName(from, to string) string { return AuditArchivePrefix + from + "-" + to + ".db" }

// AuditArchive ist eine Zeile der Tabelle audit_archives.
type AuditArchive struct {
	ID           int64
	File         string // Dateiname im Datenverzeichnis (ohne Pfad)
	FromDay      string // erster Exporttag (YYYY-MM-DD, UTC)
	ToDay        string // letzter Exporttag
	FirstEventID int64
	LastEventID  int64
	Events       int
	SHA256       string // Prüfsumme der Archivdatei
	ChainPrev    string // Kettenwert vor dem ersten Tag
	ChainHead    string // Anker: Kettenwert nach dem letzten Tag
	CreatedAt    time.Time
}

// AuditArchives liefert alle Archive, nach Tagen aufsteigend.
func (s *SQLite) AuditArchives(ctx context.Context) ([]AuditArchive, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, file, from_day, to_day, first_event_id, last_event_id, events, sha256,
		chain_prev, chain_head, created_at FROM audit_archives ORDER BY from_day, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditArchive
	for rows.Next() {
		var a AuditArchive
		var at int64
		if err := rows.Scan(&a.ID, &a.File, &a.FromDay, &a.ToDay, &a.FirstEventID, &a.LastEventID, &a.Events, &a.SHA256,
			&a.ChainPrev, &a.ChainHead, &at); err != nil {
			return nil, err
		}
		a.CreatedAt = unixMilliUTC(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// AuditArchivePath ist der Pfad eines Archivs neben der Datenbank.
func (s *SQLite) AuditArchivePath(file string) string {
	return filepath.Join(filepath.Dir(s.path), filepath.Base(file))
}

// createAuditTable übernimmt die Tabellendefinition von audit_events in
// ein angehängtes Schema (gleiche Spalten in gleicher Reihenfolge).
var createAuditTable = regexp.MustCompile(`^CREATE TABLE\s+("audit_events"|audit_events)`)

// WriteAuditArchive legt das Archiv path neu an (0600, O_EXCL: es darf
// nicht existieren) und kopiert die Zeilen ids aus audit_events Wert für
// Wert hinein, dazu info als Tabelle archive_info. Die Datenbank selbst
// ändert sich nicht. Liefert die SHA-256-Prüfsumme der fertigen Datei.
// Scheitert etwas, wird die Datei entfernt.
func (s *SQLite) WriteAuditArchive(ctx context.Context, path string, info map[string]string, ids []int64) (sum string, err error) {
	if s.path == "" {
		return "", errors.New("store: audit archive needs a database file")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("store: audit archive: %w", err)
	}
	_ = f.Close()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	var create string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'audit_events'`).Scan(&create); err != nil {
		return "", err
	}
	if !createAuditTable.MatchString(create) {
		return "", errors.New("store: audit archive: unexpected definition of audit_events")
	}
	create = createAuditTable.ReplaceAllString(create, `CREATE TABLE arc.audit_events`)

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `ATTACH DATABASE ? AS arc`, path); err != nil {
		return "", fmt.Errorf("store: audit archive: %w", err)
	}
	attached := true
	defer func() {
		if attached {
			_, _ = conn.ExecContext(context.WithoutCancel(ctx), `DETACH DATABASE arc`)
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	if err := fillAuditArchive(ctx, tx, create, info, ids); err != nil {
		_ = tx.Rollback()
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	// Erst lösen, dann die Prüfsumme: Danach schreibt nichts mehr hinein.
	if _, err := conn.ExecContext(ctx, `DETACH DATABASE arc`); err != nil {
		return "", err
	}
	attached = false
	return FileSHA256(path)
}

func fillAuditArchive(ctx context.Context, tx *sql.Tx, create string, info map[string]string, ids []int64) error {
	for _, q := range []string{create, `CREATE TABLE arc.archive_info (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("store: audit archive: %w", err)
		}
	}
	for k, v := range info {
		if _, err := tx.ExecContext(ctx, `INSERT INTO arc.archive_info(key, value) VALUES (?, ?)`, k, v); err != nil {
			return err
		}
	}
	copied := int64(0)
	for rest := ids; len(rest) > 0; {
		n := min(len(rest), purgeBatch)
		args := make([]any, n)
		for i, id := range rest[:n] {
			args[i] = id
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO arc.audit_events SELECT * FROM main.audit_events WHERE id IN (`+placeholders(n)+`)`, args...)
		if err != nil {
			return fmt.Errorf("store: audit archive: %w", err)
		}
		c, _ := res.RowsAffected()
		copied += c
		rest = rest[n:]
	}
	if copied != int64(len(ids)) {
		return fmt.Errorf("store: audit archive: copied %d of %d rows", copied, len(ids))
	}
	return nil
}

// FileSHA256 liefert die SHA-256-Prüfsumme einer Datei (hex).
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// CommitAuditRotation trägt das Archiv a ein und löscht die Zeilen ids aus
// audit_events, alles in einer Transaktion: Trigger audit_events_no_delete
// lösen, löschen, Trigger mit derselben Definition neu anlegen (E8). Wird
// nicht genau len(ids) gelöscht, bleibt alles, wie es war.
func (s *SQLite) CommitAuditRotation(ctx context.Context, a AuditArchive, ids []int64) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var trigger string
	if err := tx.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = 'audit_events_no_delete'`).Scan(&trigger); err != nil {
		return fmt.Errorf("store: audit rotation: append-only trigger: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_archives(file, from_day, to_day, first_event_id, last_event_id, events, sha256,
		chain_prev, chain_head, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, a.File, a.FromDay, a.ToDay, a.FirstEventID, a.LastEventID,
		a.Events, a.SHA256, a.ChainPrev, a.ChainHead, a.CreatedAt.UnixMilli()); err != nil {
		return fmt.Errorf("store: audit rotation: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER audit_events_no_delete`); err != nil {
		return err
	}
	deleted := int64(0)
	for rest := ids; len(rest) > 0; {
		n := min(len(rest), purgeBatch)
		args := make([]any, n)
		for i, id := range rest[:n] {
			args[i] = id
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM audit_events WHERE id IN (`+placeholders(n)+`)`, args...)
		if err != nil {
			return fmt.Errorf("store: audit rotation: %w", err)
		}
		c, _ := res.RowsAffected()
		deleted += c
		rest = rest[n:]
	}
	if deleted != int64(len(ids)) {
		return fmt.Errorf("store: audit rotation: deleted %d of %d rows; nothing was changed", deleted, len(ids))
	}
	if _, err := tx.ExecContext(ctx, trigger); err != nil {
		return err
	}
	return tx.Commit()
}

// AuditArchiveDB ist ein geöffnetes Archiv (nur lesen; die Datei ändert
// sich dabei nicht, immutable).
type AuditArchiveDB struct{ db *sql.DB }

// OpenAuditArchive öffnet ein Archiv nur zum Lesen. Fehlt die Tabelle
// audit_events, ist es kein Archiv.
func OpenAuditArchive(path string) (*AuditArchiveDB, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name IN ('audit_events', 'archive_info')`).Scan(&n); err != nil || n != 2 {
		_ = db.Close()
		if err == nil {
			err = errors.New("not an audit archive")
		}
		return nil, fmt.Errorf("store: audit archive: %w", err)
	}
	return &AuditArchiveDB{db: db}, nil
}

// Close schließt das Archiv.
func (a *AuditArchiveDB) Close() error { return a.db.Close() }

// AuditQuery liefert Einträge des Archivs wie SQLite.AuditQuery.
func (a *AuditArchiveDB) AuditQuery(ctx context.Context, f AuditFilter) ([]AuditRecord, error) {
	return auditQuery(ctx, a.db, f)
}

// AuditCountUpTo zählt die Einträge des Archivs bis einschließlich id.
func (a *AuditArchiveDB) AuditCountUpTo(ctx context.Context, id int64) (int, error) {
	var n int
	err := a.db.QueryRowContext(ctx, `SELECT count(*) FROM audit_events WHERE id <= ?`, id).Scan(&n)
	return n, err
}

// Info liefert die Tabelle archive_info.
func (a *AuditArchiveDB) Info(ctx context.Context) (map[string]string, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT key, value FROM archive_info`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
