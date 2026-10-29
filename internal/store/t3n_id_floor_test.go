package store

import (
	"context"
	"path/filepath"
	"testing"
)

// Nachbesserung T3, Punkt 8 (G2-4 der Nachprüfung): Der Höchststand aus
// Migration 0022 ist nicht nur max(id) beim Migrieren. Ein vor 0022
// gelöschter Ordner mit der größten ID, auf den ein offenes Aktionsziel
// (source_path danach NULL) noch zeigt, gibt seine ID nach 0022 nicht an
// einen neuen Ordner ab.
func TestT3NFloor0022CoversOpenActionTargets(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.migrateTo(ctx, 21); err != nil {
		t.Fatal(err)
	}
	acct, err := st.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "demo@example.org", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Rechnungen", Name: "Rechnungen"}}); err != nil {
		t.Fatal(err)
	}
	gone, err := st.QueryIntForTest(`SELECT id FROM mailboxes WHERE path = 'Rechnungen'`)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO messages(id, account_id, subject, created_at) VALUES (1, ?1, 'x', 1);
		INSERT INTO actions(id, account_id, kind, state, target_count, ids_hash, requested_by, created_at, updated_at)
		VALUES (1, ?1, 'mark', 'queued', 1, 'h', 'cli', 1, 1);
		INSERT INTO action_targets(action_id, message_id, mailbox_id, uid, uidvalidity) VALUES (1, 1, ?2, 7, 42);`,
		acct, gone); err != nil {
		t.Fatal(err)
	}
	// Der Ordner verschwindet vor 0022 vom Server (Ordnerabgleich löscht die Zeile).
	if _, _, err := st.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Privat", Name: "Privat"}}); err != nil {
		t.Fatal(err)
	}
	fresh, err := st.QueryIntForTest(`SELECT id FROM mailboxes WHERE path = 'Privat'`)
	if err != nil {
		t.Fatal(err)
	}
	if fresh <= gone {
		t.Fatalf("mailbox id %d reused (new %d) although an open action target still points at it", gone, fresh)
	}
}

// Jeder Verweis ohne Fremdschlüssel (oder mit abgeschalteten Fremd-
// schlüsseln verwaist) hebt den Startwert seines Höchststands; attachments
// und threads haben eigene Höchststände ab max(id).
func TestT3NFloor0022StartValues(t *testing.T) {
	cases := []struct {
		name, key string
		setup     string // läuft auf Schema 21 mit abgeschalteten Fremdschlüsseln
		want      int
	}{
		{"action target message", "id_floor_messages",
			`INSERT INTO actions(id, account_id, kind, state, target_count, ids_hash, requested_by, created_at, updated_at)
			 VALUES (1, 1, 'mark', 'done', 1, 'h', 'cli', 1, 1);
			 INSERT INTO action_targets(action_id, message_id, mailbox_id, uid, uidvalidity) VALUES (1, 70, 1, 7, 42)`, 70},
		{"draft reply", "id_floor_messages", draftSQL(`in_reply_to_msg`, `80`), 80},
		{"draft sent message", "id_floor_messages", draftSQL(`sent_message_id`, `90`), 90},
		{"sent check mark", "id_floor_messages", `INSERT INTO sent_check_marks(account_id, last_message_id) VALUES (1, 95)`, 95},
		{"action target mailbox", "id_floor_mailboxes",
			`INSERT INTO actions(id, account_id, kind, state, target_count, ids_hash, requested_by, created_at, updated_at)
			 VALUES (1, 1, 'mark', 'queued', 1, 'h', 'cli', 1, 1);
			 INSERT INTO action_targets(action_id, message_id, mailbox_id, uid, uidvalidity) VALUES (1, 3, 40, 7, 42)`, 40},
		{"action target folder", "id_floor_mailboxes",
			`INSERT INTO actions(id, account_id, kind, params, state, target_count, ids_hash, requested_by, created_at, updated_at)
			 VALUES (1, 1, 'move', '{"target_mailbox_id":45,"target_mailbox":"Alt"}', 'requested', 1, 'h', 'cli', 1, 1)`, 45},
		{"draft remote mailbox", "id_floor_mailboxes", draftSQL(`remote_mailbox_id`, `50`), 50},
		{"draft earlier copy", "id_floor_mailboxes",
			draftSQL(`remote_previous`, `'[{"mailbox_id":2,"uid":1,"uidvalidity":1},{"mailbox_id":55,"uid":2,"uidvalidity":1}]'`), 55},
		{"draft push attempt", "id_floor_mailboxes", draftSQL(`push_attempt`, `'{"mailbox_id":60,"content_hash":"c"}'`), 60},
		{"attachments", "id_floor_attachments", ``, 12},
		{"threads", "id_floor_threads", ``, 9},
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
			conn, err := s.db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{
				`PRAGMA foreign_keys=OFF`,
				`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
				`INSERT INTO mailboxes(id,account_id,path,name,role) VALUES (1,1,'INBOX','INBOX','inbox'), (2,1,'Drafts','Drafts','drafts')`,
				`INSERT INTO threads(id,account_id,subject_norm) VALUES (9,1,'x')`,
				`INSERT INTO messages(id,account_id,thread_id,subject,created_at) VALUES (1,1,9,'a',1), (3,1,9,'b',1)`,
				`INSERT INTO attachments(id,message_id,part_id,content_type,size) VALUES (12,3,'2','text/plain',1)`,
				c.setup,
				`PRAGMA foreign_keys=ON`,
			} {
				if q == "" {
					continue
				}
				if _, err := conn.ExecContext(ctx, q); err != nil {
					t.Fatalf("%s: %v", q, err)
				}
			}
			_ = conn.Close()
			if err := s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			got, err := s.QueryIntForTest(`SELECT CAST(value AS INTEGER) FROM meta WHERE key = ?`, c.key)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("%s = %d, want %d", c.key, got, c.want)
			}
		})
	}
}

// draftSQL legt einen Entwurf an, dessen Spalte col den Wert val hat.
func draftSQL(col, val string) string {
	return `INSERT INTO drafts(id, account_id, kind, message_id_header, created_at, updated_at, ` + col + `)
		VALUES (1, 1, 'new', 'd1@example.org', 1, 1, ` + val + `)`
}

// Nach dem Löschen des größten Anhangs bzw. Threads (Ersetzen der Anhänge,
// Zusammenführen von Threads) vergibt der Store deren ID nicht neu.
func TestT3NAttachmentAndThreadIDsAreNotReused(t *testing.T) {
	ctx := context.Background()
	s := openMem(t)
	if _, err := s.db.Exec(`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1);
		INSERT INTO messages(id,account_id,subject,created_at) VALUES (1,1,'a',1)`); err != nil {
		t.Fatal(err)
	}
	att := func(parts ...string) int64 {
		t.Helper()
		var as []NewAttachment
		for _, p := range parts {
			as = append(as, NewAttachment{PartID: p, ContentType: "text/plain", Size: 1})
		}
		if err := upsertAttachments(ctx, s.db, 1, as, 1, true); err != nil {
			t.Fatal(err)
		}
		var id int64
		if err := s.db.QueryRow(`SELECT coalesce(max(id), 0) FROM attachments`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	before := att("2", "3")
	att("2")
	if after := att("2", "4"); after <= before {
		t.Fatalf("attachment id %d reused (largest before %d)", after, before)
	}

	x := &txIndex{ctx: ctx, q: s.db, account: 1}
	a, err := x.Create("a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := x.Create("b")
	if err != nil {
		t.Fatal(err)
	}
	if err := x.Merge(a, []int64{b}); err != nil {
		t.Fatal(err)
	}
	c, err := x.Create("c")
	if err != nil {
		t.Fatal(err)
	}
	if c <= b {
		t.Fatalf("thread id %d reused (merged away %d)", c, b)
	}
}
