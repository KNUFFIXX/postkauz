package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Zusatz (b) der Prüfsitzung zu T3: Eine Datenbank aus einem Zwischenbuild
// hat mailboxes.virtual_view schon auf Schema 20. 0021 lässt dann nur das
// ALTER aus (vorher: „duplicate column name“) und kennzeichnet den Bestand
// nach Namen wie sonst; die Datei 0021 bleibt unverändert.
func TestMigration0021WithExistingColumn(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		alter0021,
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,virtual_view) VALUES (1,1,'INBOX','INBOX','inbox',0), (2,1,'[Gmail]/Wichtig','Wichtig','custom',0)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate with existing column: %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema = %d", v)
	}
	var vv int
	if err := s.db.QueryRow(`SELECT virtual_view FROM mailboxes WHERE id = 2`).Scan(&vv); err != nil || vv != 1 {
		t.Fatalf("virtual_view = %d, %v", vv, err)
	}
	ms, _ := Migrations()
	if !strings.Contains(ms[20].SQL, alter0021) {
		t.Fatal("0021 no longer contains the statement the rewrite removes")
	}
}

// Steht die Spalte mit Werten außerhalb von 0/1 da (ohne CHECK angelegt),
// bricht 0021 ab, statt still zu bereinigen; nichts ist geändert.
func TestMigration0021ExistingColumnWithBadValues(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE mailboxes ADD COLUMN virtual_view INTEGER NOT NULL DEFAULT 0`,
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,virtual_view) VALUES (1,1,'INBOX','INBOX','inbox',2)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	err = s.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "0021") || !strings.Contains(err.Error(), "other than 0 or 1") {
		t.Fatalf("migrate = %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 20 {
		t.Fatalf("schema = %d", v)
	}
}
