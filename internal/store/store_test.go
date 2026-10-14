package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
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
	if v, err := s.SchemaVersion(ctx); err != nil || v != 1 {
		t.Fatalf("SchemaVersion = %d, %v", v, err)
	}
	want := []string{"accounts", "identities", "mailboxes", "messages", "message_mailboxes", "threads",
		"attachments", "bodies", "blobs", "tags", "message_tags", "drafts", "approvals", "jobs",
		"audit_events", "audit_exports", "api_keys", "policies", "meta", "fts_messages", "fts_trigram"}
	for _, name := range want {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name=?`, name).Scan(&n); err != nil || n != 1 {
			t.Errorf("table %s missing (n=%d, err=%v)", name, n, err)
		}
	}
}

func TestOpenOnDiskUsesWAL(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "mw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var mode string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
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

// Die FTS5-Strategie aus docs/datenmodell.md: contentless-Tabellen mit
// Löschen, deutsche Umlaute (unicode61 remove_diacritics) und Teilwörter
// (Trigram).
func TestFTS5Strategy(t *testing.T) {
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO fts_messages(rowid, subject, sender, recipients, body, attachments_text, body_stem)
		VALUES (1, 'Nebenkostenabrechnung 2025', 'a@example.invalid', 'b@example.invalid', 'Bitte prüfen Sie die Zwangsvollstreckung', '', 'bitt prüf zwangsvollstreck')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO fts_trigram(rowid, subject, sender, body)
		VALUES (1, 'Nebenkostenabrechnung 2025', 'a@example.invalid', 'Bitte prüfen Sie die Zwangsvollstreckung')`); err != nil {
		t.Fatal(err)
	}
	count := func(q string, arg string) int {
		var n int
		if err := s.db.QueryRow(q, arg).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `body: pruefen OR body: prüfen`); n != 1 {
		t.Errorf("umlaut match = %d", n)
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `body_stem: zwangsvollstreck*`); n != 1 {
		t.Errorf("stem prefix match = %d", n)
	}
	if n := count(`SELECT count(*) FROM fts_trigram WHERE fts_trigram MATCH ?`, `"vollstreck"`); n != 1 {
		t.Errorf("trigram infix match = %d", n)
	}
	if _, err := s.db.Exec(`DELETE FROM fts_messages WHERE rowid = 1`); err != nil {
		t.Fatalf("contentless_delete must allow DELETE: %v", err)
	}
	if n := count(`SELECT count(*) FROM fts_messages WHERE fts_messages MATCH ?`, `body: prüfen`); n != 0 {
		t.Errorf("row still indexed after delete: %d", n)
	}
}
