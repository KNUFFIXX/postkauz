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
	ctx := context.Background()
	dir := t.TempDir()
	if d := os.Getenv("POSTKAUZ_MEASURE_DIR"); d != "" {
		dir = d
	}
	path := filepath.Join(dir, "measure.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
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
	for _, q := range fill {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var links int
	_ = s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&links)
	fi, _ := os.Stat(path)
	start := time.Now()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	var after int
	_ = s.db.QueryRow(`SELECT count(*) FROM message_mailboxes`).Scan(&after)
	if after != links {
		t.Fatalf("links %d -> %d", links, after)
	}
	t.Logf("migration 0017 (incl. snapshot): %s; messages 50000, links %d, audit 20000; db before %d bytes; snapshot %s", took, links, fi.Size(), filepath.Base(s.LastSnapshot))
}
