package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Zweite Nachbesserung T3, Punkt 12: Der Startwert von id_floor_threads und
// id_floor_attachments aus Migration 0022 ist nicht nur max(id). Ein vor
// 0022 gelöschter Thread (Zusammenführen löscht oft den mit der größten ID)
// oder Anhang mit der größten ID, der im Audit als thread:<id> bzw.
// attachment:<id> steht, gibt seine ID nach 0022 nicht an eine neue Zeile ab.
func TestT3N2Floor0022CoversAuditTargets(t *testing.T) {
	cases := []struct {
		name, key string
		setup     string // läuft auf Schema 21; danach gibt es Thread 1 und Anhang 1
		want      int
	}{
		{"thread", "id_floor_threads",
			`INSERT INTO threads(id, account_id, subject_norm) VALUES (2, 1, 'b');
			 INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'threads.get', 'ok', '["thread:2"]');
			 DELETE FROM threads WHERE id = 2`, 2},
		{"thread far above", "id_floor_threads",
			`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'threads.get', 'ok', '["account:demo","thread:4711"]')`, 4711},
		{"attachment", "id_floor_attachments",
			`INSERT INTO attachments(id, message_id, part_id, content_type, size) VALUES (2, 1, '3', 'application/pdf', 1);
			 INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'attachments.text', 'ok', '["attachment:2"]');
			 DELETE FROM attachments WHERE id = 2`, 2},
		{"attachment far above", "id_floor_attachments",
			`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'human', 'attachments.save', 'ok', '["attachment:815","message:1"]')`, 815},
		// Andere Ziele heben die Höchststände von Threads und Anhängen nicht.
		{"other targets threads", "id_floor_threads",
			`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'messages.get', 'ok', '["message:900","account:demo","attachments:77"]')`, 1},
		{"other targets attachments", "id_floor_attachments",
			`INSERT INTO audit_events(ts, actor_type, operation, result, targets) VALUES (1, 'agent', 'messages.get', 'ok', '["message:900","thread:66"]')`, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err := s.migrateTo(ctx, 21); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
				`INSERT INTO threads(id,account_id,subject_norm) VALUES (1,1,'a')`,
				`INSERT INTO messages(id,account_id,thread_id,subject,created_at) VALUES (1,1,1,'a',1)`,
				`INSERT INTO attachments(id,message_id,part_id,content_type,size) VALUES (1,1,'2','text/plain',1)`,
				c.setup,
			} {
				if _, err := s.db.Exec(q); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := s.QueryIntForTest(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = ?`, c.key)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("%s = %d, want %d (the audit names the id as a target)", c.key, got, c.want)
			}
		})
	}
}
