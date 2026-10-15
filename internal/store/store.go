// Package store kapselt die lokale Datenbank (SQLite, ohne CGO) und ihre
// Migrationen. Die Datenbank ist die Wahrheit für Beobachtung, Archiv,
// Index, Freigaben und Audit (ADR-003); Rohmails liegen im Blob-Store.
//
// Stand S1.1: Migrationsläufer mit Schema-Schutz, Konten (accounts.go) und
// SQLite-Audit-Writer (audit.go). Nachrichten, Suche und Blobs folgen in S1.2ff.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Store ist die schmale Schnittstelle, gegen die Dienste programmieren.
// Sie wächst mit den Schnitten; heute nur Lebenszyklus und Schemastand.
type Store interface {
	// Migrate wendet alle noch nicht angewendeten Migrationen an (idempotent).
	Migrate(ctx context.Context) error
	// SchemaVersion liefert die höchste angewendete Migration (0 = leer).
	SchemaVersion(ctx context.Context) (int, error)
	Close() error
}

// Migration ist eine eingebettete SQL-Datei.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// Migrations liefert alle eingebetteten Migrationen aufsteigend. Lücken oder
// doppelte Versionsnummern sind ein Fehler.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		m := migrationName.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("store: invalid migration name %q (expected 0001_name.sql)", e.Name())
		}
		v, _ := strconv.Atoi(m[1])
		b, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: m[2], SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i, m := range out {
		if m.Version != i+1 {
			return nil, fmt.Errorf("store: migrations not contiguous: position %d has version %d", i+1, m.Version)
		}
	}
	return out, nil
}
