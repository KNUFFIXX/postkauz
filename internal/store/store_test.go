package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMigrationsAreContiguous(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 || ms[0].Version != 1 || ms[0].Name != "init" {
		t.Fatalf("migrations = %+v", ms)
	}
}

func openMem(t *testing.T) *SQLite {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMigrateCreatesSchemaAndIsIdempotent(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate must be a no-op: %v", err)
	}
	if v, err := s.SchemaVersion(ctx); err != nil || v != KnownSchemaVersion() || v < 2 {
		t.Fatalf("SchemaVersion = %d, %v", v, err)
	}
	want := []string{"accounts", "identities", "mailboxes", "messages", "message_mailboxes", "threads",
		"attachments", "bodies", "blobs", "tags", "message_tags", "drafts", "approvals", "jobs",
		"audit_events", "audit_exports", "api_keys", "policies", "meta", "fts_messages", "fts_trigram", "thread_refs", "index_queue"}
	for _, name := range want {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name=?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d, err=%v)", name, n, err)
		}
	}
}

func TestOpenOnDiskUsesWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	if st, err := os.Stat(dbPath); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("database file must be 0600: %v %v", st.Mode(), err)
	}
	var fk int
	_ = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign_keys must be on")
	}
}

func TestAuditEventsAreAppendOnly(t *testing.T) {
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (1,'agent','accounts.list','ok')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE audit_events SET result='denied'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("UPDATE must be blocked, got %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM audit_events`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Errorf("DELETE must be blocked, got %v", err)
	}
}

func TestForeignKeysAndChecks(t *testing.T) {
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO mailboxes(account_id, path, name) VALUES (999,'INBOX','INBOX')`); err == nil {
		t.Error("foreign key violation must fail")
	}
	if _, err := s.db.Exec(`INSERT INTO accounts(name,email,provider_type,host,port,username,credential_ref,created_at)
		VALUES ('a','a@example.invalid','pop3','h',993,'u','ref',1)`); err == nil {
		t.Error("unknown provider_type must fail the CHECK")
	}
}

// Die FTS5-Tabellen aus docs/datenmodell.md (Migration 0004): contentless
// mit Löschen, Wortindex mit Präfixen, Trigram für Teilwörter, dieselben
// Spalten in beiden Tabellen. Die Spalten befüllt internal/search
// (gefaltet und gestemmt); Ende-zu-Ende-Tests in search_test.go.
func TestFTS5Strategy(t *testing.T) {
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO fts_messages(rowid, subject, sender, recipients, body, attachments)
		VALUES (1, 'nebenkostenabrechn 2025', 'a exampl invalid', 'b exampl invalid', 'bitt pruf sie die zwangsvollstreck', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO fts_trigram(rowid, subject, sender, recipients, body, attachments)
		VALUES (1, 'nebenkostenabrechnung 2025', 'a@example.invalid', 'b@example.invalid', 'bitte prufen sie die zwangsvollstreckung', '')`); err != nil {
		t.Fatal(err)
	}
	count := func(q string, arg string) int {
		var n int
		if err := s.db.QueryRow(q, arg).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `{body} : "pruf"`); n != 1 {
		t.Errorf("stem match = %d", n)
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `"zw"*`); n != 1 {
		t.Errorf("prefix match = %d", n)
	}
	if n := count(`SELECT count(*) FROM fts_trigram WHERE fts_trigram MATCH ?`, `"vollstreck"`); n != 1 {
		t.Errorf("trigram infix match = %d", n)
	}
	if n := count(`SELECT count(*) FROM fts_trigram WHERE fts_trigram MATCH ?`, `{sender} : "example.invalid"`); n != 1 {
		t.Errorf("trigram column filter = %d", n)
	}
	if _, err := s.db.Exec(`DELETE FROM fts_messages WHERE rowid = 1`); err != nil {
		t.Fatalf("contentless_delete must allow DELETE: %v", err)
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `"pruf"`); n != 0 {
		t.Errorf("row still indexed after delete: %d", n)
	}
}

// Migration 0002 baut mailboxes neu (Rolle 'all'). Ein Bestand mit Version 1
// wird vorher per VACUUM INTO gesichert; Ordner und Ordnerzuordnungen der
// Nachrichten überleben den Umbau (kein CASCADE), Fremdschlüssel sind danach
// wieder an.
func TestMigration0002KeepsDataAndSnapshots(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if s.LastSnapshot != "" {
		t.Fatal("no snapshot for an empty database")
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,uidvalidity,uidnext) VALUES (7,1,'INBOX','INBOX','inbox',42,3)`,
		`INSERT INTO mailboxes(id,account_id,path,name) VALUES (8,1,'Neu','Neu')`,
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (5,1,'Hallo',1)`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (5,7,2)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO mailboxes(account_id,path,name,role) VALUES (1,'[Gmail]/All Mail','All Mail','all')`); err == nil {
		t.Fatal("schema 1 must reject role all")
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("version = %d", v)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM message_mailboxes WHERE message_id=5 AND mailbox_id=7 AND uid=2 AND flag_seen=0 AND keywords='[]'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("message_mailboxes row lost: n=%d err=%v", n, err)
	}
	var phase7, phase8 string
	_ = s.db.QueryRow(`SELECT sync_phase FROM mailboxes WHERE id=7`).Scan(&phase7)
	_ = s.db.QueryRow(`SELECT sync_phase FROM mailboxes WHERE id=8`).Scan(&phase8)
	if phase7 != "ready" || phase8 != "new" {
		t.Fatalf("sync_phase = %q, %q", phase7, phase8)
	}
	if _, err := s.db.Exec(`INSERT INTO mailboxes(account_id,path,name,role) VALUES (1,'[Gmail]/All Mail','All Mail','all')`); err != nil {
		t.Fatalf("role all must be accepted after 0002: %v", err)
	}
	var fk int
	_ = s.db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk)
	if fk != 1 {
		t.Fatal("foreign_keys must be on again after the migration")
	}
	if _, err := s.db.Exec(`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (5,999,3)`); err == nil {
		t.Fatal("foreign key on message_mailboxes.mailbox_id must still be enforced")
	}
	// Löschen eines Ordners nimmt seine Zuordnungen mit (CASCADE zeigt auf die neue Tabelle).
	if _, err := s.db.Exec(`DELETE FROM mailboxes WHERE id=7`); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&n)
	if n != 0 {
		t.Fatalf("cascade after rebuild: %d rows left", n)
	}

	// Snapshot: neben der Datenbank, Schema 1, enthält die Daten von vorher.
	if s.LastSnapshot == "" || !strings.Contains(filepath.Base(s.LastSnapshot), "pk.db.pre-0002-") {
		t.Fatalf("snapshot = %q", s.LastSnapshot)
	}
	if st, err := os.Stat(s.LastSnapshot); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("snapshot file: %v %v", st, err)
	}
	snap, err := Open(s.LastSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer snap.Close()
	if v, _ := snap.SchemaVersion(ctx); v != 1 {
		t.Fatalf("snapshot version = %d", v)
	}
	_ = snap.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&n)
	if n != 1 {
		t.Fatalf("snapshot rows = %d", n)
	}
}

// Migration 0003 (S1.6) auf einem Bestand von S1.5: Nachrichten und Texte
// bleiben, bekommen clean_version 0 (Neuaufbereitung beim Sync) und lassen
// sich nachträglich Threads zuordnen; attachments hat das neue Schema.
func TestMigration0003KeepsS15Data(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 2); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO messages(id,account_id,message_id_header,subject,internal_date,created_at) VALUES (5,1,'a@example.org','Hallo',1000,1)`,
		`INSERT INTO messages(id,account_id,message_id_header,subject,in_reply_to,internal_date,created_at) VALUES (6,1,'b@example.org','AW: Hallo','a@example.org',2000,1)`,
		`INSERT INTO bodies(message_id,text_plain) VALUES (5,'Text')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(s.LastSnapshot), "pk.db.pre-0003-") {
		t.Fatalf("snapshot = %q", s.LastSnapshot)
	}
	var text string
	var version int
	if err := s.db.QueryRow(`SELECT text_plain, clean_version FROM bodies WHERE message_id=5`).Scan(&text, &version); err != nil ||
		text != "Text" || version != 0 {
		t.Fatalf("body = %q, version %d, %v", text, version, err)
	}
	if n, err := s.ThreadUnassigned(ctx, 1, 100); err != nil || n != 2 {
		t.Fatalf("ThreadUnassigned = %d, %v", n, err)
	}
	a, _ := s.MessageByID(ctx, 5)
	b, _ := s.MessageByID(ctx, 6)
	if a.ThreadID == 0 || a.ThreadID != b.ThreadID {
		t.Fatalf("threads = %d, %d", a.ThreadID, b.ThreadID)
	}
	if n, _ := s.ThreadUnassigned(ctx, 1, 100); n != 0 {
		t.Fatalf("second run assigned %d", n)
	}
	if _, err := s.db.Exec(`INSERT INTO attachments(message_id,part_id,content_type,size,type_mismatch) VALUES (5,'2','application/pdf',10,1)`); err != nil {
		t.Fatalf("new attachments schema: %v", err)
	}
}

// Zwei Prozesse (hier: zwei Store-Instanzen auf derselben Datei) schreiben
// gleichzeitig, z. B. `sync --watch` und `account test`. Eine Transaktion,
// die erst liest und dann schreibt, darf nicht mit SQLITE_BUSY scheitern,
// weil die andere inzwischen geschrieben hat: Transaktionen beginnen deshalb
// mit BEGIN IMMEDIATE und warten (busy_timeout) statt abzubrechen.
func TestConcurrentWritersWaitInsteadOfBusy(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pk.db")
	a, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if _, err := a.db.Exec(`INSERT INTO meta(key, value) VALUES ('n', '0')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	txA, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := txA.Exec(`UPDATE meta SET value = '1' WHERE key = 'n'`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		txB, err := b.db.BeginTx(ctx, nil)
		if err != nil {
			done <- err
			return
		}
		var v string
		if err := txB.QueryRow(`SELECT value FROM meta WHERE key = 'n'`).Scan(&v); err != nil {
			_ = txB.Rollback()
			done <- err
			return
		}
		if _, err := txB.Exec(`UPDATE meta SET value = ? WHERE key = 'n'`, v+"+b"); err != nil {
			_ = txB.Rollback()
			done <- err
			return
		}
		done <- txB.Commit()
	}()
	<-started
	time.Sleep(200 * time.Millisecond) // B hat begonnen (und gelesen, falls nicht IMMEDIATE)
	if err := txA.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("second writer failed instead of waiting: %v", err)
	}
	var v string
	_ = a.db.QueryRow(`SELECT value FROM meta WHERE key = 'n'`).Scan(&v)
	if v != "1+b" {
		t.Fatalf("value = %q, want 1+b (B must read A's committed write)", v)
	}
}
