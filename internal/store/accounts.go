package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Fehler der Kontenablage.
var (
	ErrAccountExists   = errors.New("store: account already exists")
	ErrAccountNotFound = errors.New("store: account not found")
)

// Account ist eine Zeile der Tabelle accounts. Sie enthält nie ein Passwort
// oder Token, nur CredentialRef (Name im Schlüsselbund).
type Account struct {
	ID                 int64
	Name               string
	Email              string
	Provider           string // imap | jmap
	Host               string
	Port               int
	TLS                string // tls | starttls | none
	Username           string
	AuthType           string // password | app_password | oauth2
	CredentialRef      string
	InsecureTestServer bool
	Capabilities       string // JSON
	Status             string // new | ok | error | disabled
	LastError          string
	CreatedAt          time.Time
}

// AccountSummary ergänzt ein Konto um Zähler aus Ordnern und Nachrichten.
type AccountSummary struct {
	Account
	LastSyncAt  time.Time // Nullzeit = nie
	UnreadTotal int
}

const accountCols = `id, name, email, provider_type, host, port, tls, username, auth_type, credential_ref,
	insecure_test_server, capabilities, status, COALESCE(last_error,''), created_at`

func scanAccount(sc interface{ Scan(...any) error }, a *Account, extra ...any) error {
	var created int64
	var insecure int
	dest := append([]any{&a.ID, &a.Name, &a.Email, &a.Provider, &a.Host, &a.Port, &a.TLS, &a.Username,
		&a.AuthType, &a.CredentialRef, &insecure, &a.Capabilities, &a.Status, &a.LastError, &created}, extra...)
	if err := sc.Scan(dest...); err != nil {
		return err
	}
	a.InsecureTestServer = insecure == 1
	a.CreatedAt = time.UnixMilli(created).UTC()
	return nil
}

// CreateAccount legt ein Konto an und liefert seine ID.
func (s *SQLite) CreateAccount(ctx context.Context, a Account) (int64, error) {
	if a.Provider == "" {
		a.Provider = "imap"
	}
	if a.Capabilities == "" {
		a.Capabilities = "{}"
	}
	if a.Status == "" {
		a.Status = "new"
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(name, email, provider_type, host, port, tls, username,
		auth_type, credential_ref, insecure_test_server, capabilities, status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.Name, a.Email, a.Provider, a.Host, a.Port, a.TLS, a.Username, a.AuthType, a.CredentialRef,
		boolInt(a.InsecureTestServer), a.Capabilities, a.Status, a.CreatedAt.UnixMilli())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed: accounts.name") {
			return 0, fmt.Errorf("%w: %s", ErrAccountExists, a.Name)
		}
		return 0, fmt.Errorf("store: create account: %w", err)
	}
	return res.LastInsertId()
}

// AccountByName liefert ein Konto per Name.
func (s *SQLite) AccountByName(ctx context.Context, name string) (Account, error) {
	var a Account
	err := scanAccount(s.db.QueryRowContext(ctx, `SELECT `+accountCols+` FROM accounts WHERE name = ?`, name), &a)
	if errors.Is(err, sql.ErrNoRows) {
		return a, fmt.Errorf("%w: %s", ErrAccountNotFound, name)
	}
	return a, err
}

// ListAccounts liefert alle Konten, nach Namen sortiert, mit Ungelesen-Summe
// und letztem Sync-Zeitpunkt aus den Ordnern.
func (s *SQLite) ListAccounts(ctx context.Context) ([]AccountSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountCols+`,
		COALESCE((SELECT MAX(last_sync_at) FROM mailboxes m WHERE m.account_id = accounts.id), 0),
		COALESCE((SELECT SUM(unread) FROM mailboxes m WHERE m.account_id = accounts.id), 0)
		FROM accounts ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AccountSummary
	for rows.Next() {
		var sum AccountSummary
		var lastSync int64
		if err := scanAccount(rows, &sum.Account, &lastSync, &sum.UnreadTotal); err != nil {
			return nil, err
		}
		if lastSync > 0 {
			sum.LastSyncAt = time.UnixMilli(lastSync).UTC()
		}
		out = append(out, sum)
	}
	return out, rows.Err()
}

// CountMessages zählt die lokal archivierten Nachrichten eines Kontos.
func (s *SQLite) CountMessages(ctx context.Context, accountID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE account_id = ?`, accountID).Scan(&n)
	return n, err
}

// DeleteAccount entfernt ein Konto. Ordner, Nachrichten und Entwürfe hängen
// per ON DELETE CASCADE daran; der Aufrufer (accounts.remove) verweigert
// deshalb das Entfernen, solange Nachrichten archiviert sind (ADR-003).
func (s *SQLite) DeleteAccount(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete account: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: id %d", ErrAccountNotFound, id)
	}
	return nil
}

// UpdateAccountProbe speichert das Ergebnis einer Verbindungsprüfung:
// erkannte Fähigkeiten (JSON), Status und letzten Fehler (ohne Geheimnisse).
func (s *SQLite) UpdateAccountProbe(ctx context.Context, id int64, capabilities, status, lastError string) error {
	var le any
	if lastError != "" {
		le = lastError
	}
	if capabilities == "" {
		capabilities = "{}"
	}
	_, err := s.db.ExecContext(ctx, `UPDATE accounts SET capabilities = ?, status = ?, last_error = ? WHERE id = ?`,
		capabilities, status, le, id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ExecForTest führt rohes SQL aus. Nur für Tests anderer Pakete, die
// Zustände herstellen müssen, für die es (noch) keine Operation gibt.
func (s *SQLite) ExecForTest(query string, args ...any) error {
	_, err := s.db.Exec(query, args...)
	return err
}

// QueryIntForTest liefert das Ergebnis einer Zählabfrage. Nur für Tests
// anderer Pakete.
func (s *SQLite) QueryIntForTest(query string, args ...any) (int, error) {
	var n int
	err := s.db.QueryRow(query, args...).Scan(&n)
	return n, err
}

// DBForTest gibt die Datenbank frei. Nur für Tests.
func (s *SQLite) DBForTest() *sql.DB { return s.db }
