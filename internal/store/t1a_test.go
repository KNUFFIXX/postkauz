package store

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	// K6: Je Prüfung ein frisches Paar (Nachricht, Ordner). Das Paar (6,7)
	// gibt es schon; ein INSERT damit scheiterte am Primärschlüssel, auch
	// ohne CHECK bzw. UNIQUE.
	for _, q := range []string{
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (8,1,'Drei',1)`,
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (9,1,'Vier',1)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (8,7,0)`); err == nil ||
		!strings.Contains(err.Error(), "CHECK") {
		t.Fatalf("UID 0 must be rejected by CHECK after 0017: %v", err)
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (9,7,2)`); err == nil ||
		!strings.Contains(err.Error(), "UNIQUE") || !strings.Contains(err.Error(), "uid") {
		t.Fatalf("UNIQUE (mailbox_id, uid) lost: %v", err)
	}
	// Gegenprobe: Dieselben frischen Paare mit gültiger, freier UID gehen.
	for _, q := range []string{
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (8,7,4)`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (9,7,5)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
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

// K1: 0017 prüft die Fremdschlüssel nur in message_mailboxes. Ein
// Altfehler in einer anderen Tabelle hält sie nicht auf; ein Fehler in
// message_mailboxes bricht sie ab und nennt die Tabelle.
func TestMigration0017ForeignKeyCheckScope(t *testing.T) {
	ctx := context.Background()
	orphan := func(s *SQLite, q string) {
		t.Helper()
		conn, err := s.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
			t.Fatal(err)
		}
	}
	s, _ := schema16(t, 3)
	orphan(s, `INSERT INTO message_import_folders(message_id,folder_id,imported_at) VALUES (999,998,1)`)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("foreign key error in another table: %v", err)
	}

	s, _ = schema16(t, 3)
	orphan(s, `INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (999,7,4)`)
	err := s.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "table message_mailboxes") || !strings.Contains(err.Error(), "in messages") {
		t.Fatalf("err = %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 16 {
		t.Fatalf("schema = %d", v)
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

// A12.4: Kontolöschung atomar und unter der Kontosperre.
func TestDeleteAccountAtomicAndLocked(t *testing.T) {
	ctx := context.Background()
	s, path := schema16(t, 3)
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, 1); !errors.Is(err, ErrAccountHasMessages) {
		t.Fatalf("account with messages: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM messages`); err != nil {
		t.Fatal(err)
	}
	l, err := flock.TryLock(filepath.Join(filepath.Dir(path), flock.AccountLockName(1)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, 1); !errors.Is(err, ErrAccountBusy) {
		t.Fatalf("locked account: %v", err)
	}
	l.Release()
	if err := s.DeleteAccount(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, 1); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

// W1: Ein Prozess, der die Datenbank offen hat und das Schema geprüft hat
// (hier ein „alter“ mit Stand 16), verhindert jede Migration, solange er
// läuft; ein Prozess, der migriert, hält jeden anderen fern, bis er fertig
// ist. Danach liest der neue Prozess die Version unter der Sperre neu.
func TestMigrateWaitsForProcessLock(t *testing.T) {
	ctx := context.Background()
	s, path := schema16(t, 3)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	old, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	// Der alte Prozess besteht die Prüfung (sein Stand ist 16).
	if err := old.migrateTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	next, err := Open(path)
	if err != nil {
		t.Fatalf("a second process may open the database: %v", err)
	}
	defer next.Close()
	if err := next.Migrate(ctx); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("migrate while another process has the database open = %v", err)
	}
	if v, _ := next.SchemaVersion(ctx); v != 16 || next.LastSnapshot != "" {
		t.Fatalf("schema = %d, snapshot %q", v, next.LastSnapshot)
	}
	// Der neue Prozess hält danach wieder seine geteilte Sperre.
	if _, err := flock.TryLock(ProcessLockPath(path)); !errors.Is(err, flock.ErrLocked) {
		t.Fatalf("process lock not held after a refused migration: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}
	if err := next.Migrate(ctx); err != nil {
		t.Fatalf("migrate after the old process ended: %v", err)
	}
	if v, _ := next.SchemaVersion(ctx); v != KnownSchemaVersion() || next.LastSnapshot == "" {
		t.Fatalf("schema = %d, snapshot %q", v, next.LastSnapshot)
	}

	// Während einer Migration (exklusive Sperre) kommt niemand herein.
	defer func(w time.Duration) { openLockWait = w }(openLockWait)
	openLockWait = 100 * time.Millisecond
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	x, err := flock.TryLock(ProcessLockPath(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrDatabaseBusy) {
		t.Fatalf("open during a migration = %v", err)
	}
	x.Release()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if err := again.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
}

// processHelperEnv startet den Kindprozess für TestMigrateRefusedByOtherProcess.
const processHelperEnv = "POSTKAUZ_STORE_PROCESS_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(processHelperEnv) == "1" {
		os.Exit(runProcessHelper())
	}
	os.Exit(m.Run())
}

// runProcessHelper spielt eine ältere Version (Stand 16): öffnen, Schema
// prüfen, „ready“ melden und offen bleiben, bis stdin endet.
func runProcessHelper() int {
	st, err := Open(os.Getenv("PK_DB"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 3
	}
	defer st.Close()
	if err := st.migrateTo(context.Background(), 16); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 4
	}
	fmt.Println("ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0
}

// W1 mit echtem Kindprozess: Solange der alte Prozess läuft, migriert der
// neue nicht; nach seinem Ende schon.
func TestMigrateRefusedByOtherProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("child process")
	}
	ctx := context.Background()
	s, path := schema16(t, 3)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), processHelperEnv+"=1", "PK_DB="+path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("helper: %q %v", line, err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); !errors.Is(err, ErrMigrationBlocked) {
		t.Fatalf("migrate while the old process runs = %v", err)
	}
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper: %v", err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate after the old process ended: %v", err)
	}
	if v, _ := st.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema = %d", v)
	}
}
