package store

import (
	"context"
	"os"
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

// A14 (Migration 0022): Nach dem Löschen der größten Nachricht bzw. des
// größten Ordners vergibt der Store deren ID nicht neu.
func TestIDsAreNotReusedAfterDelete(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`); err != nil {
		t.Fatal(err)
	}
	insert := func() int64 {
		t.Helper()
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		id, err := insertMessage(ctx, tx, 1, NewMessage{Subject: "x"}, 1, nil)
		if err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b := insert(), insert()
	if _, err := s.db.Exec(`DELETE FROM messages WHERE id = ?`, b); err != nil {
		t.Fatal(err)
	}
	if c := insert(); c <= b || c <= a {
		t.Fatalf("message ids %d %d then %d: reused", a, b, c)
	}

	box := func(paths ...string) map[string]int64 {
		t.Helper()
		var specs []MailboxSpec
		for _, p := range paths {
			specs = append(specs, MailboxSpec{Path: p, Name: p, Role: map[bool]string{true: "inbox", false: "custom"}[p == "INBOX"]})
		}
		if _, _, err := s.SyncMailboxes(ctx, 1, specs); err != nil {
			t.Fatal(err)
		}
		mbs, err := s.Mailboxes(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]int64{}
		for _, m := range mbs {
			ids[m.Path] = m.ID
		}
		return ids
	}
	first := box("INBOX", "Alt")
	box("INBOX")
	second := box("INBOX", "Neu")
	if second["Neu"] <= first["Alt"] {
		t.Fatalf("mailbox id of Neu = %d, Alt had %d: reused", second["Neu"], first["Alt"])
	}
}

// Laufzeit der Migration 0022 (Höchststand der IDs, Quellpfad je Ziel,
// Archive der Audit-Rotation) an der synthetischen Datenbank, ausgehend
// von Schema 21; nur mit POSTKAUZ_MEASURE_MIGRATION=1 (Rahmen 4).
func TestMeasureMigration0022(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure migration 0022")
	}
	s, path := measureDB(t, 21)
	defer s.Close()
	took, links := measureFillAndMigrate(t, s, 22)
	fi, _ := os.Stat(path)
	var floor string
	_ = s.db.QueryRow(`SELECT value FROM meta WHERE key = 'id_floor_messages'`).Scan(&floor)
	t.Logf("migration 0022 (incl. snapshot): %s; messages 50000, links %d, audit 20000; db %d bytes; id_floor_messages %s; snapshot %s",
		took, links, fi.Size(), floor, filepath.Base(s.LastSnapshot))
}
