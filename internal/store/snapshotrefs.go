package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
)

// SnapshotContents ist, was eine Sicherung aus ihrem eigenen Schnappschuss
// liest (T3-Nachbesserung 3b, 5b): Die Liste der Blobs und Archive kommt
// aus demselben Stand wie die gesicherte Datenbank, nicht aus dem
// Datenverzeichnis, in dem purge, blob gc und audit rotate inzwischen
// weitergelaufen sein können.
type SnapshotContents struct {
	// Blobs: alle Hashes, auf die der Schnappschuss verweist
	// (BlobReferences).
	Blobs map[string]bool
	// Archives: die eingetragenen Archive (audit_archives).
	Archives []AuditArchive
}

// ReadSnapshot öffnet einen Schnappschuss (SnapshotTo) nur zum Lesen und
// liefert seine Verweise. Die Datei ändert sich dabei nicht (immutable:
// keine Sperrdatei, kein WAL, keine Prozesssperre), ihre Prüfsumme im
// Manifest bleibt gültig.
func ReadSnapshot(ctx context.Context, path string) (SnapshotContents, error) {
	var out SnapshotContents
	if _, err := os.Stat(path); err != nil {
		return out, fmt.Errorf("store: snapshot: %w", err)
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&immutable=1")
	if err != nil {
		return out, fmt.Errorf("store: snapshot: %w", err)
	}
	defer db.Close()
	// Dieselben Abfragen wie für die offene Datenbank (nur s.db wird
	// benutzt; keine Sperre, kein Pfad).
	s := &SQLite{db: db}
	if out.Blobs, err = s.BlobReferences(ctx); err != nil {
		return out, fmt.Errorf("store: snapshot: %w", err)
	}
	// audit_archives gibt es seit Migration 0022; ein älterer Stand hat
	// keine Archive.
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'audit_archives'`).Scan(&n); err != nil {
		return out, fmt.Errorf("store: snapshot: %w", err)
	}
	if n == 0 {
		return out, nil
	}
	if out.Archives, err = s.AuditArchives(ctx); err != nil {
		return out, fmt.Errorf("store: snapshot: %w", err)
	}
	return out, nil
}
