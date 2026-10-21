package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

// Kennung des Datenverzeichnisses und Umzug der Schlüsselbund-Verweise
// (S2.0/A2, Migration 0008).

// ErrNoDataDirID: Die Datenbank hat keine Kennung (Schema vor 8).
var ErrNoDataDirID = errors.New("store: database has no data directory id (schema older than 8)")

var dataDirIDRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

// DataDirID liefert die Kennung des Datenverzeichnisses für den
// Schlüsselbund-Verweis: die ersten 8 Hex der Kennung in meta.datadir_id,
// die Migration 0008 beim ersten Öffnen erzeugt.
func (s *SQLite) DataDirID(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'datadir_id'`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoDataDirID
	}
	if err != nil {
		return "", err
	}
	if !dataDirIDRE.MatchString(v) {
		return "", fmt.Errorf("store: meta.datadir_id is corrupt")
	}
	return v[:8], nil
}

// SetCredentialRef stellt den Schlüsselbund-Verweis eines Kontos um.
func (s *SQLite) SetCredentialRef(ctx context.Context, id int64, ref string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET credential_ref = ? WHERE id = ?`, ref, id)
	if err != nil {
		return fmt.Errorf("store: set credential ref: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: id %d", ErrAccountNotFound, id)
	}
	return nil
}

// metaCredentialRefsLeft: alte Verweise, deren Eintrag nach dem Umzug nicht
// gelöscht werden konnte (JSON-Liste; `doctor` warnt, solange er besteht).
const metaCredentialRefsLeft = "credential_refs_left"

// CredentialRefsLeft liefert die alten Verweise, deren Eintrag beim Umzug
// stehen geblieben ist.
func (s *SQLite) CredentialRefsLeft(ctx context.Context) ([]string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, metaCredentialRefsLeft).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	if err := json.Unmarshal([]byte(v), &out); err != nil {
		return nil, fmt.Errorf("store: meta.%s is corrupt", metaCredentialRefsLeft)
	}
	return out, nil
}

// AddCredentialRefLeft merkt sich einen alten Verweis, dessen Eintrag nicht
// gelöscht werden konnte (ohne Dubletten).
func (s *SQLite) AddCredentialRefLeft(ctx context.Context, ref string) error {
	left, err := s.CredentialRefsLeft(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(left, ref) {
		return nil
	}
	b, err := json.Marshal(append(left, ref))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaCredentialRefsLeft, string(b))
	return err
}
