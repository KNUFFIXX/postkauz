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
// von Schema 21; nur mit POSTKAUZ_MEASURE_MIGRATION=1 (Rahmen 4). Seit der
// Nachbesserung T3 (Punkt 8) mit Threads, Anhängen, Aktionen und Entwürfen,
// deren Verweise die Startwerte der Höchststände mitbestimmen.
func TestMeasureMigration0022(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure migration 0022")
	}
	s, path := measureDB(t, 21)
	defer s.Close()
	took, links := measureFillAndMigrate(t, s, 22,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 50000)
		 INSERT INTO threads(id,account_id,subject_norm,message_count) SELECT i, 1, 'betreff ' || i, 1 FROM n`,
		`UPDATE messages SET thread_id = id`,
		`INSERT INTO attachments(message_id,part_id,filename,content_type,size,extract_status) SELECT id, '2', 'a.pdf', 'application/pdf', 10, 'skipped' FROM messages WHERE id % 5 = 0`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 1000)
		 INSERT INTO actions(id,account_id,kind,params,state,target_count,ids_hash,requested_by,created_at,updated_at)
		 SELECT i, 1, 'move', '{"target_mailbox_id":1,"target_mailbox":"INBOX"}', CASE WHEN i % 10 = 0 THEN 'queued' ELSE 'done' END, 20, 'h', 'cli', 1, 1 FROM n`,
		`INSERT INTO action_targets(action_id,message_id,mailbox_id,uid,uidvalidity,result)
		 SELECT 1 + (id - 1) / 20, id, 2, id, 8, CASE WHEN (1 + (id - 1) / 20) % 10 = 0 THEN NULL ELSE 'ok' END FROM messages WHERE id <= 20000`,
		`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 500)
		 INSERT INTO drafts(id,account_id,kind,in_reply_to_msg,message_id_header,remote_mailbox_id,remote_previous,created_at,updated_at)
		 SELECT i, 1, 'reply', i * 10, 'd' || i || '@example.org', 2, '[{"mailbox_id":2,"uid":1,"uidvalidity":8}]', 1, 1 FROM n`,
	)
	fi, _ := os.Stat(path)
	floors := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM meta WHERE key GLOB 'id_floor_*'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var k, v string
		_ = rows.Scan(&k, &v)
		floors[k] = v
	}
	_ = rows.Close()
	t.Logf("migration 0022 (incl. snapshot): %s; messages 50000, links %d, threads 50000, attachments 10000, actions 1000 with 20000 targets, drafts 500, audit 20000; db %d bytes; floors %v; snapshot %s",
		took, links, fi.Size(), floors, filepath.Base(s.LastSnapshot))
}
