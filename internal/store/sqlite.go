package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // reiner Go-Treiber, registriert "sqlite"; FTS5 ist enthalten
)

// ErrSchemaTooNew: Die Datenbank wurde von einer neueren Programmversion migriert.
var ErrSchemaTooNew = errors.New("store: database schema is newer than this program")

// KnownSchemaVersion ist die höchste eingebettete Migration.
func KnownSchemaVersion() int {
	ms, err := Migrations()
	if err != nil {
		return 0
	}
	return len(ms)
}

func unixMilliUTC(ms int64) time.Time { return time.UnixMilli(ms).UTC() }

// SQLite ist die Store-Implementierung auf modernc.org/sqlite.
type SQLite struct {
	db   *sql.DB
	path string // "" bei :memory:
	// LastSnapshot ist der Pfad des Snapshots, den das letzte Migrate vor
	// einer Migration angelegt hat ("" = keiner nötig).
	LastSnapshot string
}

var _ Store = (*SQLite)(nil)

// Open öffnet (und erzeugt) die Datenbank unter path. ":memory:" ergibt eine
// private In-Memory-Datenbank (Tests). Die Datenbank darf nie in einem
// Cloud-synchronisierten Ordner liegen (docs/datenmodell.md).
func Open(path string) (*SQLite, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	// Schreibende Transaktionen beginnen mit BEGIN IMMEDIATE: Sie holen die
	// Schreibsperre sofort und warten sonst bis busy_timeout. Mit dem
	// Standard (DEFERRED) liest eine Transaktion erst und scheitert beim
	// ersten Schreiben sofort mit SQLITE_BUSY, wenn ein anderer Prozess
	// (z. B. `sync --watch` neben `account test`) inzwischen geschrieben
	// hat; busy_timeout hilft dann nicht. Alle BeginTx im Store schreiben.
	q.Set("_txlock", "immediate")
	if path != ":memory:" {
		q.Add("_pragma", "journal_mode(WAL)")
	}
	dsn := "file:" + path + "?" + q.Encode()
	if path == ":memory:" {
		dsn = "file::memory:?" + q.Encode()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1) // jede Verbindung hätte sonst eine eigene Datenbank
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	if path != ":memory:" {
		// Nur für den Nutzer lesbar (docs/sicherheit.md, Abschnitt 10);
		// SQLite legt Dateien sonst nach umask an (meist 0644).
		for _, f := range []string{path, path + "-wal", path + "-shm"} {
			_ = os.Chmod(f, 0o600)
		}
	}
	st := &SQLite{db: db}
	if path != ":memory:" {
		st.path = path
	}
	return st, nil
}

// Close schließt die Datenbank.
func (s *SQLite) Close() error { return s.db.Close() }

// Migrate wendet ausstehende Migrationen je in einer eigenen Transaktion an.
// Hat die Datenbank schon ein Schema (Bestand), legt es vorher einen
// Snapshot per VACUUM INTO neben der Datenbankdatei an
// (<db>.pre-NNNN-<Zeit>.bak, 0600; docs/datenmodell.md, Migrationen).
func (s *SQLite) Migrate(ctx context.Context) error {
	return s.migrateTo(ctx, 0)
}

// fkOffDirective in der ersten Zeile einer Migration schaltet die
// Fremdschlüssel für deren Dauer ab (Tabellenumbau ohne CASCADE-Folgen);
// danach muss PRAGMA foreign_key_check leer sein.
const fkOffDirective = "-- postkauz:foreign_keys=off"

// migrateTo migriert bis einschließlich Version max (0 = alle).
func (s *SQLite) migrateTo(ctx context.Context, max int) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		return err
	}
	have, err := s.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	ms, err := Migrations()
	if err != nil {
		return err
	}
	// Schutz vor stiller Beschädigung: Eine ältere Binärdatei fasst eine
	// Datenbank mit neuerem Schema nicht an (docs/datenmodell.md, Migrationen).
	if have > len(ms) {
		return fmt.Errorf("%w: database has schema version %d, this program knows up to %d; use a newer version",
			ErrSchemaTooNew, have, len(ms))
	}
	if max <= 0 || max > len(ms) {
		max = len(ms)
	}
	s.LastSnapshot = ""
	if have > 0 && have < max && s.path != "" {
		if s.LastSnapshot, err = s.snapshot(ctx, have+1); err != nil {
			return err
		}
	}
	for _, m := range ms {
		if m.Version <= have || m.Version > max {
			continue
		}
		if err := s.apply(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

// snapshot schreibt eine konsistente Kopie der Datenbank (VACUUM INTO).
func (s *SQLite) snapshot(ctx context.Context, next int) (string, error) {
	name := fmt.Sprintf("%s.pre-%04d-%s.bak", s.path, next, time.Now().UTC().Format("20060102T150405.000Z"))
	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, name); err != nil {
		return "", fmt.Errorf("store: snapshot before migration %04d: %w", next, err)
	}
	_ = os.Chmod(name, 0o600)
	return filepath.Clean(name), nil
}

// apply wendet eine Migration an: eigene Verbindung (PRAGMA foreign_keys
// gilt je Verbindung und nur außerhalb einer Transaktion), eine Transaktion.
func (s *SQLite) apply(ctx context.Context, m Migration) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	fkOff := strings.HasPrefix(m.SQL, fkOffDirective)
	if fkOff {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			return err
		}
		defer func() { _, _ = conn.ExecContext(context.Background(), `PRAGMA foreign_keys=ON`) }()
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
	}
	if fkOff {
		rows, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		bad := rows.Next()
		_ = rows.Close()
		if bad {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %04d_%s: foreign key check failed", m.Version, m.Name)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?,?,?)`,
		m.Version, m.Name, time.Now().UnixMilli()); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SchemaVersion liefert die höchste angewendete Migration.
func (s *SQLite) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	if err != nil {
		// Tabelle fehlt noch: leere Datenbank.
		var n int
		if e := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='schema_migrations'`).Scan(&n); e == nil && n == 0 {
			return 0, nil
		}
		return 0, err
	}
	return int(v.Int64), nil
}
