package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "modernc.org/sqlite" // reiner Go-Treiber, registriert "sqlite"; FTS5 ist enthalten
)

// SQLite ist die Store-Implementierung auf modernc.org/sqlite.
type SQLite struct {
	db *sql.DB
}

var _ Store = (*SQLite)(nil)

// Open öffnet (und erzeugt) die Datenbank unter path. ":memory:" ergibt eine
// private In-Memory-Datenbank (Tests). Die Datenbank darf nie in einem
// Cloud-synchronisierten Ordner liegen (docs/datenmodell.md).
func Open(path string) (*SQLite, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
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
	return &SQLite{db: db}, nil
}

// Close schließt die Datenbank.
func (s *SQLite) Close() error { return s.db.Close() }

// Migrate wendet ausstehende Migrationen je in einer eigenen Transaktion an.
func (s *SQLite) Migrate(ctx context.Context) error {
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
	for _, m := range ms {
		if m.Version <= have {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: migration %04d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(version, name, applied_at) VALUES (?,?,?)`,
			m.Version, m.Name, time.Now().UnixMilli()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
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
