package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/flock"
)

// schema16 legt eine Datenbank mit Schema 16 und einer Ordnerkopie an.
func schema16(t *testing.T, uid int) (*SQLite, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err := s.migrateTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,uidvalidity,uidnext) VALUES (7,1,'INBOX','INBOX','inbox',42,3)`,
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (5,1,'Hallo',1)`,
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (6,1,'Zwei',1)`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid,modseq,flag_seen,flag_flagged,keywords) VALUES (5,7,2,9,1,1,'["$x"]')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (6,7,?)`, uid); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// Migration 0017 (A6): CHECK uid > 0, Daten unverändert; mit einer Zeile
// uid <= 0 bricht sie ab, ohne etwas zu ändern.
func TestMigration0017UIDPositive(t *testing.T) {
	ctx := context.Background()
	s, _ := schema16(t, 3)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM message_mailboxes WHERE message_id=5 AND mailbox_id=7 AND uid=2 AND modseq=9
		AND flag_seen=1 AND flag_flagged=1 AND flag_answered=0 AND keywords='["$x"]'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("row changed by 0017: n=%d err=%v", n, err)
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (6,7,0)`); err == nil {
		t.Fatal("UID 0 must be rejected after 0017")
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (6,7,2)`); err == nil {
		t.Fatal("UNIQUE (mailbox_id, uid) lost")
	}
	if _, err := s.db.Exec(`DELETE FROM messages WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM message_mailboxes WHERE message_id=5`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("ON DELETE CASCADE lost: %d", n)
	}

	for _, bad := range []int{0, -1} {
		s, _ := schema16(t, bad)
		err := s.Migrate(ctx)
		if err == nil || !strings.Contains(err.Error(), "UID 0 or less; nothing was changed") {
			t.Fatalf("uid %d: err = %v", bad, err)
		}
		if v, _ := s.SchemaVersion(ctx); v != 16 {
			t.Fatalf("uid %d: schema = %d, want 16", bad, v)
		}
		if err := s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&n); err != nil || n != 2 {
			t.Fatalf("uid %d: rows = %d", bad, n)
		}
	}
}

// T1a Punkt 7: Keine Migration, solange ein Sync oder Import eine
// Kontosperre im Datenverzeichnis hält; die Sperren gelten während der
// Migration.
func TestMigrateRefusesWhileAccountLocked(t *testing.T) {
	ctx := context.Background()
	s, path := schema16(t, 3)
	lock, err := flock.TryLock(filepath.Join(filepath.Dir(path), flock.AccountLockName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("migrate with a held lock = %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 16 || s.LastSnapshot != "" {
		t.Fatalf("schema = %d, snapshot %q", v, s.LastSnapshot)
	}
	lock.Release()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema = %d", v)
	}
	// Nach der Migration ist die Sperre wieder frei.
	l, err := flock.TryLock(filepath.Join(filepath.Dir(path), flock.AccountLockName(1)))
	if err != nil {
		t.Fatalf("lock still held after migration: %v", err)
	}
	// Ohne anstehende Migration stört eine gehaltene Sperre nicht.
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("no pending migration: %v", err)
	}
	l.Release()
}

// T1a Punkt 7: Ein anderer Programmstand mit gleicher Nummer, anderem Namen.
func TestMigrateRefusesForeignMigrationName(t *testing.T) {
	ctx := context.Background()
	s, _ := schema16(t, 3)
	if _, err := s.db.Exec(`UPDATE schema_migrations SET name = 'something_else' WHERE version = 16`); err != nil {
		t.Fatal(err)
	}
	err := s.Migrate(ctx)
	if !errors.Is(err, ErrMigrationMismatch) || !strings.Contains(err.Error(), "migration 0016") {
		t.Fatalf("err = %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 16 {
		t.Fatalf("schema = %d", v)
	}
}

// A13.4: Ziele für Aktionen nur aus message_mailboxes. Ein importierter
// Ordner namens INBOX erscheint in Locations (UID 0), nie in ServerCopies.
func TestServerCopiesNeverImportFolders(t *testing.T) {
	ctx := context.Background()
	s, _ := schema16(t, 3)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO import_folders(id,account_id,path,created_at) VALUES (1,1,'INBOX',1)`,
		`INSERT INTO message_import_folders(message_id,folder_id,imported_at) VALUES (5,1,1)`,
		`INSERT INTO message_import_folders(message_id,folder_id,imported_at) VALUES (6,1,1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role) VALUES (8,1,'Neu','Neu','custom')`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (6,8,4)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	m, err := s.MessageByID(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	importUID0 := false
	for _, l := range m.Locations {
		importUID0 = importUID0 || (l.Role == "import" && l.Mailbox == "INBOX" && l.UID == 0)
	}
	if !importUID0 {
		t.Fatalf("locations = %+v", m.Locations)
	}
	cs, err := s.ServerCopies(ctx, 5)
	if err != nil || len(cs) != 1 || cs[0].Mailbox != "INBOX" || cs[0].UID != 2 || cs[0].UIDValidity != 42 || cs[0].Role != "inbox" {
		t.Fatalf("server copies of 5 = %+v %v", cs, err)
	}
	// Ordner ohne bekannte UIDVALIDITY (noch nie gewählt): kein Ziel.
	cs, err = s.ServerCopies(ctx, 6)
	if err != nil || len(cs) != 1 || cs[0].MailboxID != 7 {
		t.Fatalf("server copies of 6 = %+v %v", cs, err)
	}
}
