package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// BlobRow ist eine Zeile der Tabelle blobs (für `doctor --blobs`).
type BlobRow struct {
	SHA256     string
	Size       int64
	Compressed bool
}

// BlobRows liefert alle Zeilen der Tabelle blobs (nur Hash, Größe, Form;
// kein Inhalt). Die Spalten gibt es seit Schema 1, die Abfrage läuft also
// auch vor einer Migration. Eine Abfrage für alle Zeilen: Bei 50.000 Blobs
// sind das wenige Megabyte im Speicher.
func (s *SQLite) BlobRows(ctx context.Context) ([]BlobRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT sha256, size, compressed FROM blobs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BlobRow
	for rows.Next() {
		var b BlobRow
		if err := rows.Scan(&b.SHA256, &b.Size, &b.Compressed); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// DraftAttachmentHashes liefert die Hashes aller Anhänge aller Entwürfe
// (drafts.attachment_blobs, jeder Status), jeden Hash einmal. Dateianhänge
// von Entwürfen liegen im Blob-Store ohne Zeile in blobs (A03 A13.3); sie
// sind trotzdem verwiesen. Andere Tabellen mit attachment_blobs gibt es
// nicht (Migrationen 0001, 0010, 0014: nur drafts).
func (s *SQLite) DraftAttachmentHashes(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT attachment_blobs FROM drafts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var atts []struct {
			SHA256 string `json:"sha256"`
		}
		if err := json.Unmarshal([]byte(raw), &atts); err != nil {
			return nil, fmt.Errorf("store: drafts.attachment_blobs: %w", err)
		}
		for _, a := range atts {
			if a.SHA256 != "" && !seen[a.SHA256] {
				seen[a.SHA256] = true
				out = append(out, a.SHA256)
			}
		}
	}
	return out, rows.Err()
}
