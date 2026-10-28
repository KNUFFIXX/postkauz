package store

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
)

// Laufzeit von retention.purge an der synthetischen Datenbank (A03_teile.md
// Rahmen 4, Brief Paket C): 50.000 Nachrichten mit Text und Volltextindex,
// 30.000 auf dem Server, 20.000 dort gelöscht (davon 5.000 auch
// importiert), 10.000 Anhänge, 20.000 Audit-Zeilen. Nur mit
// POSTKAUZ_MEASURE_MIGRATION=1.
func TestMeasurePurge(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure retention.purge")
	}
	ctx := context.Background()
	s, path := measureDB(t, 0)
	defer s.Close()
	old := time.Now().Add(-400 * 24 * time.Hour).UnixMilli()
	fill := []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role,uidvalidity,uidnext) VALUES (1,1,'INBOX','INBOX','inbox',7,30001)`,
		`INSERT INTO import_folders(id,account_id,path,created_at) VALUES (1,1,'Archiv',1)`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 50000)
		 INSERT INTO threads(id,account_id,subject_norm,message_count) SELECT i, 1, 'betreff ' || i, 1 FROM n`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 50000)
		 INSERT INTO messages(id,account_id,message_id_header,thread_id,subject,from_addr,size,snippet,created_at,date_header,deleted_remote_at)
		 SELECT i, 1, 'm' || i || '@example.org', i, 'Betreff ' || i, 'a@example.org', 5000, 'x', 1, 1700000000000 + i*1000,
		        CASE WHEN i > 30000 THEN ` + strconv.FormatInt(old, 10) + ` END FROM n`,
		`INSERT INTO bodies(message_id,text_plain) SELECT id, 'Text der Nachricht ' || id || ' mit Rechnung und Termin' FROM messages`,
		`INSERT INTO blobs(sha256,size,created_at) SELECT printf('%064x', id), 10, 1 FROM messages WHERE id % 5 = 0`,
		`INSERT INTO attachments(message_id,part_id,filename,content_type,size,sha256,extract_status) SELECT id, '2', 'a.pdf', 'application/pdf', 10, printf('%064x', id), 'skipped' FROM messages WHERE id % 5 = 0`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) SELECT id, 1, id FROM messages WHERE id <= 30000`,
		`INSERT INTO message_import_folders(message_id,folder_id,imported_at) SELECT id, 1, 1 FROM messages WHERE id > 45000`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 20000)
		 INSERT INTO audit_events(ts,actor_type,api_key_id,scope,operation,params_redacted,targets,result,reason,dry_run,duration_ms)
		 SELECT 1700000000000 + i*60000, 'agent', 'k1', 'mail:read', 'messages.search', '{"fields":["query"]}', '[]', 'ok', NULL, 0, 12 FROM n`,
	}
	for _, q := range fill {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	st := time.Now()
	for {
		n, err := s.IndexBatch(ctx, 1, 5000, search.NewStemmer().Build)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	t.Logf("indexed 50000 messages in %s", time.Since(st))

	start := time.Now()
	ids, c, err := s.PurgeCandidates(ctx, time.Now().Add(-30*24*time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	counted := time.Since(start)
	res, err := s.PurgeMessages(ctx, time.Now().Add(-30*24*time.Hour), ids)
	if err != nil {
		t.Fatal(err)
	}
	took := time.Since(start)
	var left, fts int
	_ = s.db.QueryRow(`SELECT count(*) FROM messages`).Scan(&left)
	_ = s.db.QueryRow(`SELECT count(*) FROM fts_messages_docsize`).Scan(&fts)
	if c.Gone != 20000 || c.KeptImported != 5000 || len(ids) != 15000 || res.Messages != 15000 || left != 35000 || fts != 35000 {
		t.Fatalf("counts %+v, ids %d, result %+v, left %d, fts %d", c, len(ids), res, left, fts)
	}
	fi, _ := os.Stat(path)
	t.Logf("purge: candidates in %s, 15000 of 50000 messages (3000 attachments, %d threads) deleted in %s total; db %d bytes (%s)",
		counted, res.ThreadsRemoved, took, fi.Size(), filepath.Base(path))
}
