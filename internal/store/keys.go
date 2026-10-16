package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Fehler der Schlüsselablage.
var (
	ErrKeyExists   = errors.New("store: api key already exists")
	ErrKeyNotFound = errors.New("store: api key not found")
)

// APIKey ist eine Zeile der Tabelle api_keys. Sie enthält nie den Klartext
// des Schlüssels, nur dessen SHA-256 (hex) in KeyHash (docs/sicherheit.md,
// Abschnitt 3). Nullzeiten bedeuten „nicht gesetzt".
type APIKey struct {
	ID            int64
	Name          string
	KeyHash       string
	Role          string
	Scopes        []string
	DryRunDefault bool
	ExpiresAt     time.Time
	LastUsedAt    time.Time
	RevokedAt     time.Time
	CreatedAt     time.Time
}

const keyCols = `id, name, key_hash, role, scopes, dry_run_default, expires_at, last_used_at, revoked_at, created_at`

func scanKey(sc interface{ Scan(...any) error }) (APIKey, error) {
	var k APIKey
	var scopes string
	var dry int
	var exp, used, rev sql.NullInt64
	var created int64
	if err := sc.Scan(&k.ID, &k.Name, &k.KeyHash, &k.Role, &scopes, &dry, &exp, &used, &rev, &created); err != nil {
		return k, err
	}
	if err := json.Unmarshal([]byte(scopes), &k.Scopes); err != nil {
		return k, fmt.Errorf("store: api key %s: scopes: %w", k.Name, err)
	}
	k.DryRunDefault = dry == 1
	k.ExpiresAt, k.LastUsedAt, k.RevokedAt = fromNullMilli(exp), fromNullMilli(used), fromNullMilli(rev)
	k.CreatedAt = unixMilliUTC(created)
	return k, nil
}

func fromNullMilli(v sql.NullInt64) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	return unixMilliUTC(v.Int64)
}

// CreateAPIKey legt einen Schlüssel an (nur Hash). Doppelte Namen ergeben
// ErrKeyExists; Namen bleiben auch nach dem Widerruf belegt, damit das
// Audit eindeutig bleibt.
func (s *SQLite) CreateAPIKey(ctx context.Context, k APIKey) (int64, error) {
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	scopes, err := json.Marshal(k.Scopes)
	if err != nil {
		return 0, err
	}
	created := k.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	var id int64
	err = s.db.QueryRowContext(ctx, `INSERT INTO api_keys(name, key_hash, role, scopes, dry_run_default, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?) RETURNING id`, k.Name, k.KeyHash, k.Role, string(scopes), boolInt(k.DryRunDefault),
		nullTime(k.ExpiresAt), created.UnixMilli()).Scan(&id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: api_keys.name") {
			return 0, fmt.Errorf("%w: %s", ErrKeyExists, k.Name)
		}
		return 0, fmt.Errorf("store: create api key: %w", err)
	}
	return id, nil
}

// APIKeys liefert alle Schlüssel (auch widerrufene und abgelaufene), nach Namen.
func (s *SQLite) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+keyCols+` FROM api_keys ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKey
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// APIKeyByName liefert einen Schlüssel per Namen.
func (s *SQLite) APIKeyByName(ctx context.Context, name string) (APIKey, error) {
	k, err := scanKey(s.db.QueryRowContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return APIKey{}, fmt.Errorf("%w: %s", ErrKeyNotFound, name)
	}
	return k, err
}

// RevokeAPIKey setzt revoked_at (nur wenn noch nicht gesetzt). Es meldet,
// ob der Schlüssel dadurch widerrufen wurde.
func (s *SQLite) RevokeAPIKey(ctx context.Context, id int64, at time.Time) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`, at.UnixMilli(), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// TouchAPIKey setzt last_used_at.
func (s *SQLite) TouchAPIKey(ctx context.Context, id int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`, at.UnixMilli(), id)
	return err
}
