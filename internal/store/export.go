package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Lesen für messages.export (S3.3, E6): Ordner mit Zählern und die
// Nachrichten eines Ordners, ohne Inhalt (Betreff und Adressen bleiben in
// der Datenbank; nur der Absender wird für die mbox-Trennzeile gelesen).

// Arten eines Export-Ordners.
const (
	// ExportMailbox: ein Serverordner (message_mailboxes).
	ExportMailbox = "mailbox"
	// ExportImported: ein Import-Ordner, nur mit Nachrichten ohne
	// Serverkopie (sonst stünden sie doppelt im Export).
	ExportImported = "imported"
	// ExportNoFolder: Nachrichten ohne Serverkopie und ohne Import-Ordner
	// (auf dem Server gelöscht).
	ExportNoFolder = "none"
)

// ExportFilter schränkt den Export ein.
type ExportFilter struct {
	// Mailbox: nur dieser Serverordner (Pfad); leer = alle Ordner samt
	// Import-Ordnern und Nachrichten ohne Ordner.
	Mailbox string
	// Since: nur Nachrichten ab diesem Zeitpunkt (INTERNALDATE, sonst
	// Date-Kopf); Nullzeit = alle.
	Since time.Time
}

// ExportFolder ist ein Ordner des Exports mit Zählern (Nachrichten im
// Filter, davon ohne Rohmail).
type ExportFolder struct {
	Kind string
	ID   int64
	Path string
	// Segments sind die Teile des Pfads (Serverordner: getrennt mit dem
	// Trennzeichen des Servers, das vor dem Blattnamen steht, etwa "." bei
	// "INBOX.Archiv"; Import-Ordner: "/").
	Segments []string
	Messages int
	NoBlob   int
}

// pathSegments teilt einen Serverpfad am Trennzeichen vor dem Blattnamen
// (die Datenbank führt das Trennzeichen nicht; "Archiv/2024" mit Blatt
// "2024" → "/").
func pathSegments(path, leaf string) []string {
	if leaf == "" || len(path) <= len(leaf)+1 || !strings.HasSuffix(path, leaf) {
		return []string{path}
	}
	sep := path[len(path)-len(leaf)-1 : len(path)-len(leaf)]
	return strings.Split(path, sep)
}

// ExportItem ist eine Nachricht für den Export.
type ExportItem struct {
	ID           int64
	MessageID    string
	DateHeader   time.Time
	InternalDate time.Time
	From         string
	BlobSHA256   string
}

// exportSince ist die Bedingung für ExportFilter.Since (m = messages).
func exportSince(f ExportFilter) (string, []any) {
	if f.Since.IsZero() {
		return "", nil
	}
	return ` AND COALESCE(m.internal_date, m.date_header, 0) >= ?`, []any{f.Since.UnixMilli()}
}

const (
	noServerCopy   = `NOT EXISTS (SELECT 1 FROM message_mailboxes mm WHERE mm.message_id = m.id)`
	noImportFolder = `NOT EXISTS (SELECT 1 FROM message_import_folders mif WHERE mif.message_id = m.id)`
)

// ExportFolders liefert die Ordner eines Kontos in der Reihenfolge des
// Exports: Serverordner nach Pfad, dann Import-Ordner (nur Nachrichten ohne
// Serverkopie), dann die Nachrichten ohne jeden Ordner. Ordner ohne
// Nachricht im Filter fehlen. Mit Filter.Mailbox nur dieser Serverordner
// (ErrMailboxNotFound, wenn es ihn nicht gibt).
func (s *SQLite) ExportFolders(ctx context.Context, accountID int64, f ExportFilter) ([]ExportFolder, error) {
	since, sinceArgs := exportSince(f)
	var out []ExportFolder
	scan := func(kind, q string, args ...any) error {
		rows, err := s.db.QueryContext(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := ExportFolder{Kind: kind}
			var leaf string
			if err := rows.Scan(&e.ID, &e.Path, &leaf, &e.Messages, &e.NoBlob); err != nil {
				return err
			}
			switch kind {
			case ExportMailbox:
				e.Segments = pathSegments(e.Path, leaf)
			case ExportImported:
				e.Segments = strings.Split(e.Path, "/")
			}
			if e.Messages > 0 {
				out = append(out, e)
			}
		}
		return rows.Err()
	}
	mbCond, mbArgs := "", []any{}
	if f.Mailbox != "" {
		var id int64
		err := s.db.QueryRowContext(ctx, `SELECT id FROM mailboxes WHERE account_id = ? AND path = ?`, accountID, f.Mailbox).Scan(&id)
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("%w: %s", ErrMailboxNotFound, f.Mailbox)
		}
		if err != nil {
			return nil, err
		}
		mbCond, mbArgs = ` AND mb.id = ?`, []any{id}
	}
	args := append(append([]any{accountID}, mbArgs...), sinceArgs...)
	if err := scan(ExportMailbox, `SELECT mb.id, mb.path, mb.name, count(m.id), count(m.id) - count(m.blob_sha256)
		FROM mailboxes mb JOIN message_mailboxes mm ON mm.mailbox_id = mb.id JOIN messages m ON m.id = mm.message_id
		WHERE mb.account_id = ?`+mbCond+since+` GROUP BY mb.id ORDER BY mb.path, mb.id`, args...); err != nil {
		return nil, fmt.Errorf("store: export folders: %w", err)
	}
	if f.Mailbox != "" {
		return out, nil
	}
	args = append([]any{accountID}, sinceArgs...)
	if err := scan(ExportImported, `SELECT f.id, f.path, '', count(m.id), count(m.id) - count(m.blob_sha256)
		FROM import_folders f JOIN message_import_folders mif ON mif.folder_id = f.id JOIN messages m ON m.id = mif.message_id
		WHERE f.account_id = ? AND `+noServerCopy+since+` GROUP BY f.id ORDER BY f.path, f.id`, args...); err != nil {
		return nil, fmt.Errorf("store: export folders: %w", err)
	}
	if err := scan(ExportNoFolder, `SELECT 0, '', '', count(m.id), count(m.id) - count(m.blob_sha256) FROM messages m
		WHERE m.account_id = ? AND `+noServerCopy+` AND `+noImportFolder+since, args...); err != nil {
		return nil, fmt.Errorf("store: export folders: %w", err)
	}
	return out, nil
}

// ExportMessages liefert die Nachrichten eines Ordners aus ExportFolders
// nach ID (ganz gelesen: Die Lesetransaktion bleibt nicht offen, während
// der Export Dateien schreibt).
func (s *SQLite) ExportMessages(ctx context.Context, accountID int64, folder ExportFolder, f ExportFilter) ([]ExportItem, error) {
	since, sinceArgs := exportSince(f)
	const cols = `SELECT m.id, COALESCE(m.message_id_header, ''), m.date_header, m.internal_date, m.from_addr,
		COALESCE(m.blob_sha256, '') FROM messages m`
	var q string
	var args []any
	switch folder.Kind {
	case ExportMailbox:
		q = cols + ` JOIN message_mailboxes mm ON mm.message_id = m.id WHERE m.account_id = ? AND mm.mailbox_id = ?`
		args = []any{accountID, folder.ID}
	case ExportImported:
		q = cols + ` JOIN message_import_folders mif ON mif.message_id = m.id WHERE m.account_id = ? AND mif.folder_id = ? AND ` + noServerCopy
		args = []any{accountID, folder.ID}
	case ExportNoFolder:
		q = cols + ` WHERE m.account_id = ? AND ` + noServerCopy + ` AND ` + noImportFolder
		args = []any{accountID}
	default:
		return nil, fmt.Errorf("store: export: unknown folder kind %q", folder.Kind)
	}
	rows, err := s.db.QueryContext(ctx, q+since+` ORDER BY m.id`, append(args, sinceArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("store: export messages: %w", err)
	}
	defer rows.Close()
	var out []ExportItem
	for rows.Next() {
		var it ExportItem
		var dh, idate sql.NullInt64
		if err := rows.Scan(&it.ID, &it.MessageID, &dh, &idate, &it.From, &it.BlobSHA256); err != nil {
			return nil, err
		}
		if dh.Valid {
			it.DateHeader = unixMilliUTC(dh.Int64)
		}
		if idate.Valid {
			it.InternalDate = unixMilliUTC(idate.Int64)
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
