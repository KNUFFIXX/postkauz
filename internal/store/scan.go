package store

import (
	"bytes"
	"context"
	"fmt"
	"slices"
)

// ScanTables sind die Tabellen ohne Mailinhalt, die `doctor --secrets` nach
// Geheimnissen durchsucht (Audit, Konten, Schlüssel, Jobs, Import-Stand …).
// Tabellen mit Mailinhalt fehlen bewusst: Ein Passwort, das jemand per Mail
// geschickt hat, ist kein Leck des Programms.
var ScanTables = []string{"meta", "accounts", "identities", "mailboxes", "api_keys", "policies", "jobs",
	"audit_events", "audit_exports", "import_sources", "import_folders"}

// TableText liefert den Inhalt einer Tabelle aus ScanTables als Text: eine
// Zeile je Datensatz, Spalten durch Tabulator getrennt (für die Suche nach
// Geheimnissen, nie zur Ausgabe).
func (s *SQLite) TableText(ctx context.Context, table string) ([]byte, error) {
	if !slices.Contains(ScanTables, table) {
		return nil, fmt.Errorf("store: table %q is not scanned", table)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT * FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, v := range vals {
			if i > 0 {
				buf.WriteByte('\t')
			}
			switch x := v.(type) {
			case nil:
			case []byte:
				buf.Write(x)
			default:
				fmt.Fprint(&buf, x)
			}
		}
		buf.WriteByte('\n')
	}
	return buf.Bytes(), rows.Err()
}
