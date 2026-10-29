package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Laufzeit der Migration 0017 an einer synthetischen Datenbank (A03_teile.md,
// Rahmen 4): 50.000 Nachrichten, 80.000 Ordnerkopien (alle in „Alle
// Nachrichten“, 30.000 zusätzlich in INBOX, wie bei Gmail), 20.000
// Audit-Zeilen. Nur mit POSTKAUZ_MEASURE_MIGRATION=1 (Ergebnis ins
// Protokoll docs/protokolle/T1a_migration.md); sonst übersprungen.
func TestMeasureMigration0017(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure migration 0017")
	}
	s, path := measureDB(t, 16)
	defer s.Close()
	took, links := measureFillAndMigrate(t, s, 17)
	fi, _ := os.Stat(path)
	t.Logf("migration 0017 (incl. snapshot): %s; messages 50000, links %d, audit 20000; db %d bytes; snapshot %s", took, links, fi.Size(), filepath.Base(s.LastSnapshot))
}

// Laufzeit der Migrationen aus T1b (0018 Neubau audit_events, 0019 jobs)
// an derselben synthetischen Datenbank, ausgehend von Schema 17 (Rahmen 4;
// Protokoll docs/protokolle/T1b_stand.md).
func TestMeasureMigrationsT1b(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure migrations 0018 and 0019")
	}
	ctx := context.Background()
	s, path := measureDB(t, 17)
	defer s.Close()
	var before int
	took, _ := measureFillAndMigrate(t, s, 0)
	_ = s.db.QueryRow(`SELECT count(*) FROM audit_events WHERE ref_id IS NULL`).Scan(&before)
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() || before != 20000 {
		t.Fatalf("schema %d, audit rows %d", v, before)
	}
	fi, _ := os.Stat(path)
	t.Logf("migrations 0018+0019 (incl. snapshot): %s; messages 50000, audit 20000; db %d bytes; snapshot %s", took, fi.Size(), filepath.Base(s.LastSnapshot))
}

func measureDB(t *testing.T, version int) (*SQLite, string) {
	t.Helper()
	dir := t.TempDir()
	if d := os.Getenv("POSTKAUZ_MEASURE_DIR"); d != "" {
		dir = d
	}
	path := filepath.Join(dir, "measure.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.migrateTo(context.Background(), version); err != nil {
		t.Fatal(err)
	}
	return s, path
}

// measureFillAndMigrate füllt die Datenbank (nach dem Grundbestand die
// Anweisungen extra) und migriert bis to (0 = alle).
func measureFillAndMigrate(t *testing.T, s *SQLite, to int, extra ...string) (time.Duration, int) {
	t.Helper()
	ctx := context.Background()
	fill := []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,uidvalidity,uidnext) VALUES (1,1,'INBOX','INBOX','inbox',7,30001), (2,1,'[Gmail]/All Mail','All Mail','all',8,50001)`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 50000)
		 INSERT INTO messages(id,account_id,message_id_header,subject,from_addr,size,snippet,created_at,date_header)
		 SELECT i, 1, 'm' || i || '@example.org', 'Betreff ' || i, 'a@example.org', 5000 + i % 7000, substr(hex(randomblob(100)),1,200), 1, 1700000000000 + i*1000 FROM n`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid,modseq,flag_seen,keywords) SELECT id, 2, id, id, id % 2, '[]' FROM messages`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid,modseq,flag_seen,keywords) SELECT id, 1, id, id, id % 2, '[]' FROM messages WHERE id <= 30000`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 20000)
		 INSERT INTO audit_events(ts,actor_type,api_key_id,scope,operation,params_redacted,targets,result,reason,dry_run,duration_ms)
		 SELECT 1700000000000 + i*60000, 'agent', 'k1', 'mail:read', 'messages.search', '{"fields":["query"]}', '[]', 'ok', NULL, 0, 12 FROM n`,
	}
	for _, q := range append(fill, extra...) {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var links int
	_ = s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&links)
	start := time.Now()
	if err := s.migrateTo(ctx, to); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	var after int
	_ = s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&after)
	if after != links {
		t.Fatalf("links %d -> %d", links, after)
	}
	return took, links
}
