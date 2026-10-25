package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Import aus mbox und Maildir (S1.10, Migration 0007). Importierte Ordner
// und Labels stehen in import_folders/message_import_folders, getrennt von
// den Ordnern des Servers (ADR-003). Eine Nachricht, die nur importiert
// ist, trägt deleted_remote_at (der Server führt sie nicht); findet der
// Sync sie später auf dem Server, ordnet er sie zu und löscht die Marke.

// Import-Modi einer Quelle.
const (
	ImportContent = "content" // Inhalt speichern, Ordner zuordnen
	ImportLabels  = "labels"  // nur vorhandenen Nachrichten den Ordner zuordnen
)

// Stand einer Quelle.
const (
	ImportRunning = "running"
	ImportDone    = "done"
)

// ImportSource ist der gespeicherte Stand einer Quelle (import_sources).
type ImportSource struct {
	AccountID int64
	Source    string // absoluter Pfad
	Format    string // mbox | maildir
	Folder    string
	Mode      string // content | labels
	State     string // running | done
	Position  int64  // mbox: Byte-Position der nächsten Nachricht
	Cursor    string // maildir: letzter gespeicherter Name
	Size      int64
	MTime     int64 // Unix-Millisekunden
}

// ImportFolderFlags sind Ordner und Flags einer importierten Kopie.
type ImportFolderFlags struct {
	Folder                         string
	Seen, Flagged, Answered, Draft bool
	Keywords                       []string
}

// ImportNew ist eine neu importierte Nachricht mit ihren Ordnern (die erste
// Kopie und weitere Kopien desselben Stapels, z. B. Gmail-Labels).
type ImportNew struct {
	Msg     NewMessage // Flags der Nachricht: Msg.Link (UID ohne Bedeutung)
	Folders []ImportFolderFlags
}

// ImportLink ordnet eine vorhandene Nachricht einem importierten Ordner zu.
type ImportLink struct {
	MessageID int64
	ImportFolderFlags
}

// ImportBatch ist ein Stapel des Imports; ApplyImport schreibt ihn samt
// Stand der Quelle in einer Transaktion.
type ImportBatch struct {
	AccountID int64
	New       []ImportNew
	Links     []ImportLink
	Source    *ImportSource // nil = Stand nicht ändern
}

// ImportResult zählt, was ApplyImport geschrieben hat.
type ImportResult struct {
	New    int // neue Nachrichten
	Linked int // neue Ordnerzuordnungen vorhandener Nachrichten
}

// MessageByHeaderID sucht die älteste Nachricht des Kontos mit dieser
// Message-ID (ohne spitze Klammern).
func (s *SQLite) MessageByHeaderID(ctx context.Context, accountID int64, messageID string) (int64, bool, error) {
	if messageID == "" {
		return 0, false, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM messages WHERE account_id = ? AND message_id_header = ? ORDER BY id LIMIT 1`,
		accountID, messageID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// MessageByContentHash sucht die älteste Nachricht des Kontos mit einem der
// Hashes (content_hash; der Import prüft Varianten der Zeilenenden am Ende).
func (s *SQLite) MessageByContentHash(ctx context.Context, accountID int64, hashes ...string) (int64, bool, error) {
	if len(hashes) == 0 {
		return 0, false, nil
	}
	args := []any{accountID}
	for _, h := range hashes {
		args = append(args, h)
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM messages WHERE account_id = ? AND content_hash IN (`+placeholders(len(hashes))+`)
		ORDER BY id LIMIT 1`, args...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// MessageNearImport sucht für den Import eine gespeicherte Nachricht mit
// dieser Message-ID, die 1 bis maxExtra Bytes KLEINER ist als die
// importierte Kopie und denselben Betreff und dieselbe Date-Kopfzeile hat
// (subject bereinigt wie beim Speichern; dateHeader Nullzeit = keine).
// Das ist dieselbe Nachricht, die Thunderbird beim Speichern am Ende
// ergänzt hat (fehlendes Zeilenende, abschließende MIME-Grenze; Restfall
// von C2), nachdem der Sync die Fassung des Servers gespeichert hatte.
// Vorrang: kleinste Differenz, dann niedrigste ID. Nie ein Entwurf (wie
// FindLinkCandidateUpTo) und nur mit Rohmail.
func (s *SQLite) MessageNearImport(ctx context.Context, accountID int64, messageID string, size, maxExtra int64, subject string, dateHeader time.Time) (int64, bool, error) {
	if messageID == "" || maxExtra <= 0 {
		return 0, false, nil
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT m.id FROM messages m
		WHERE m.account_id = ? AND m.message_id_header = ? AND m.size BETWEEN ? AND ? AND m.blob_sha256 IS NOT NULL
		  AND COALESCE(m.subject, '') = ? AND m.date_header IS ?
		  AND `+notDraftCond+`
		ORDER BY ? - m.size, m.id LIMIT 1`, accountID, messageID, size-maxExtra, size-1, subject, nullTime(dateHeader), size).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// ImportSourceState liefert den gespeicherten Stand einer Quelle.
func (s *SQLite) ImportSourceState(ctx context.Context, accountID int64, source, folder, mode string) (ImportSource, bool, error) {
	src := ImportSource{AccountID: accountID, Source: source, Folder: folder, Mode: mode}
	var cursor sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT format, state, position, cursor, size, mtime FROM import_sources
		WHERE account_id = ? AND source = ? AND folder = ? AND mode = ?`, accountID, source, folder, mode).Scan(
		&src.Format, &src.State, &src.Position, &cursor, &src.Size, &src.MTime)
	if errors.Is(err, sql.ErrNoRows) {
		return src, false, nil
	}
	src.Cursor = cursor.String
	return src, err == nil, err
}

// ApplyImport schreibt einen Stapel atomar: neue Nachrichten (mit Thread,
// Text, Anhängen, Ordnern), Ordner vorhandener Nachrichten und den Stand
// der Quelle. Flags vorhandener Nachrichten ändern sich nur, wenn der
// Server sie nicht führt (nur importiert): dann gilt ein Flag, wenn eine
// Kopie es trägt. Sonst gewinnt der Server (ADR-003).
func (s *SQLite) ApplyImport(ctx context.Context, b ImportBatch) (res ImportResult, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	now := time.Now().UnixMilli()
	folders := map[string]int64{}
	folderID := func(path string) (int64, error) {
		if id, ok := folders[path]; ok {
			return id, nil
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO import_folders(account_id, path, created_at) VALUES (?,?,?)
			ON CONFLICT(account_id, path) DO NOTHING`, b.AccountID, path, now); err != nil {
			return 0, fmt.Errorf("store: import folder %q: %w", path, err)
		}
		var id int64
		if err := tx.QueryRowContext(ctx, `SELECT id FROM import_folders WHERE account_id = ? AND path = ?`, b.AccountID, path).Scan(&id); err != nil {
			return 0, err
		}
		folders[path] = id
		return id, nil
	}
	link := func(mid int64, f ImportFolderFlags) (bool, error) {
		fid, err := folderID(f.Folder)
		if err != nil {
			return false, err
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO message_import_folders(message_id, folder_id, flag_seen, flag_flagged,
			flag_answered, flag_draft, keywords, imported_at) VALUES (?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`,
			mid, fid, b2i(f.Seen), b2i(f.Flagged), b2i(f.Answered), b2i(f.Draft), keywordsJSON(f.Keywords), now)
		if err != nil {
			return false, fmt.Errorf("store: import link %d: %w", mid, err)
		}
		n, err := r.RowsAffected()
		return n > 0, err
	}
	for _, n := range b.New {
		mid, err := insertMessage(ctx, tx, b.AccountID, n.Msg, now, now)
		if err != nil {
			return res, err
		}
		for _, f := range n.Folders {
			if _, err := link(mid, f); err != nil {
				return res, err
			}
			if err := mergeImportFlags(ctx, tx, mid, f); err != nil {
				return res, err
			}
		}
		if err := assignThread(ctx, tx, b.AccountID, mid, threadMessage(n.Msg)); err != nil {
			return res, fmt.Errorf("store: thread: %w", err)
		}
		res.New++
	}
	for _, l := range b.Links {
		added, err := link(l.MessageID, l.ImportFolderFlags)
		if err != nil {
			return res, err
		}
		if !added {
			continue
		}
		res.Linked++
		if err := mergeImportFlags(ctx, tx, l.MessageID, l.ImportFolderFlags); err != nil {
			return res, err
		}
	}
	if src := b.Source; src != nil {
		var finished any
		if src.State == ImportDone {
			finished = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO import_sources(account_id, source, format, folder, mode, state, position,
			cursor, size, mtime, started_at, updated_at, finished_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(account_id, source, folder, mode) DO UPDATE SET format = excluded.format, state = excluded.state,
			position = excluded.position, cursor = excluded.cursor, size = excluded.size, mtime = excluded.mtime,
			updated_at = excluded.updated_at, finished_at = excluded.finished_at`,
			b.AccountID, src.Source, src.Format, src.Folder, src.Mode, src.State, src.Position, nullStr(src.Cursor),
			src.Size, src.MTime, now, now, finished); err != nil {
			return res, fmt.Errorf("store: import source: %w", err)
		}
	}
	return res, tx.Commit()
}

// mergeImportFlags überträgt Flags und Keywords einer importierten Kopie auf
// die Nachricht, solange der Server sie nicht führt (keine Zuordnung in
// message_mailboxes). Ein Flag bleibt gesetzt, wenn eine Kopie es trägt.
func mergeImportFlags(ctx context.Context, tx *sql.Tx, mid int64, f ImportFolderFlags) error {
	_, err := tx.ExecContext(ctx, `UPDATE messages SET flag_seen = MAX(flag_seen, ?), flag_flagged = MAX(flag_flagged, ?),
		flag_answered = MAX(flag_answered, ?), flag_draft = MAX(flag_draft, ?),
		keywords = (SELECT json_group_array(value) FROM (SELECT value FROM json_each(messages.keywords)
		            UNION SELECT value FROM json_each(?) ORDER BY 1))
		WHERE id = ? AND NOT EXISTS (SELECT 1 FROM message_mailboxes WHERE message_id = messages.id)`,
		b2i(f.Seen), b2i(f.Flagged), b2i(f.Answered), b2i(f.Draft), keywordsJSON(f.Keywords), mid)
	if err != nil {
		return fmt.Errorf("store: import flags %d: %w", mid, err)
	}
	return nil
}

// ImportFolders liefert die importierten Ordner einer Nachricht.
func (s *SQLite) ImportFolders(ctx context.Context, messageID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT f.path FROM message_import_folders mf JOIN import_folders f ON f.id = mf.folder_id
		WHERE mf.message_id = ? ORDER BY f.path`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// importFolderFilter ist die Bedingung für in:/mailbox: über importierte
// Ordner (Pfad ohne Groß-/Kleinschreibung, Unterordner, Rolle "import").
const importFolderFilter = `SELECT mf.message_id FROM import_folders f JOIN message_import_folders mf ON mf.folder_id = f.id
			WHERE f.path = ? COLLATE NOCASE OR f.path LIKE ? ESCAPE '\' OR f.path LIKE ? ESCAPE '\' OR 'import' = ?`
