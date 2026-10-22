package store

import (
	"context"
	"testing"
	"time"
)

// S2.5: Migration 0013 (Ablage der Entwürfe) und die Abfragen des Jobs
// draft_push.

func TestMigration0013DraftPush(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.MigrateToForTest(ctx, 12); err != nil {
		t.Fatal(err)
	}
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_uid,remote_state,pushed_content_hash,created_at,updated_at)
		VALUES (?,'new','pk.1@example.org','draft',7,'pushed','h1',1,1)`, acc); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 13 || KnownSchemaVersion() != 13 {
		t.Fatalf("schema %d, known %d", v, KnownSchemaVersion())
	}
	d, err := s.Draft(ctx, 1)
	if err != nil || d.RemoteUID != 7 || d.PushedDate != "" || d.RemoteUIDValidity != 0 || len(d.RemotePrevious) != 0 || d.PushNote != "" {
		t.Fatalf("old draft = %+v, %v", d, err)
	}
	// Neue Felder schreiben und lesen.
	d.PushedDate, d.RemoteUIDValidity, d.PushNote = "Thu, 22 Oct 2026 14:03:05 +0200", 42, "approval_stale"
	d.RemotePrevious = []RemoteCopy{{MailboxID: 3, UID: 5, UIDValidity: 42}}
	if err := s.DraftTx(ctx, func(x *DraftTx) error { return x.UpdateDraft(d) }); err != nil {
		t.Fatal(err)
	}
	got, err := s.Draft(ctx, 1)
	if err != nil || got.PushedDate != d.PushedDate || got.RemoteUIDValidity != 42 || got.PushNote != "approval_stale" ||
		len(got.RemotePrevious) != 1 || got.RemotePrevious[0] != (RemoteCopy{MailboxID: 3, UID: 5, UIDValidity: 42}) {
		t.Fatalf("draft = %+v, %v", got, err)
	}
	for _, bad := range []string{`UPDATE drafts SET push_note = 'irgendwas' WHERE id = 1`, `UPDATE drafts SET remote_previous = '{}' WHERE id = 1`} {
		if _, err := s.db.Exec(bad); err == nil {
			t.Errorf("accepted: %s", bad)
		}
	}
}

func TestDraftPushQueries(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	// Ohne wartende Entwürfe kein Job.
	if made, err := s.EnsureDraftPushJob(ctx, acc, time.Now()); err != nil || made {
		t.Fatalf("ensure without drafts = %v, %v", made, err)
	}
	for _, q := range []string{
		`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,created_at,updated_at) VALUES (?,'new','pk.1@example.org','draft','pending',1,1)`,
		`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,remote_uid,created_at,updated_at) VALUES (?,'new','pk.2@example.org','ready','pending',4,1,1)`,
		`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,created_at,updated_at,closed_at) VALUES (?,'new','pk.3@example.org','abandoned','pending',1,1,1)`,
		`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,created_at,updated_at) VALUES (?,'new','pk.4@example.org','draft','pushed',1,1)`,
		`INSERT INTO drafts(account_id,kind,message_id_header,status,remote_state,created_at,updated_at) VALUES (?,'new','pk.5@example.org','draft','taken_over',1,1)`,
	} {
		if _, err := s.db.Exec(q, acc); err != nil {
			t.Fatal(err)
		}
	}
	ids, withCopy, err := s.DraftPushCandidates(ctx, acc)
	if err != nil || len(ids) != 2 || ids[0] != 1 || ids[1] != 2 || withCopy != 1 {
		t.Fatalf("candidates = %v (%d with copy), %v", ids, withCopy, err)
	}
	if made, err := s.EnsureDraftPushJob(ctx, acc, time.Now()); err != nil || !made {
		t.Fatalf("ensure = %v, %v", made, err)
	}
	if made, err := s.EnsureDraftPushJob(ctx, acc, time.Now()); err != nil || made {
		t.Fatalf("second ensure = %v, %v (one open job per account)", made, err)
	}
	// CompleteJob kennt draft_push: Solange Entwürfe warten, bleibt er fällig.
	j, ok, err := s.ClaimJob(ctx, JobDraftPush, acc, "w1", time.Minute, false)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	if done, err := s.CompleteJob(ctx, j, "w1"); err != nil || done {
		t.Fatalf("complete with pending drafts = %v, %v", done, err)
	}
	if _, err := s.db.Exec(`UPDATE drafts SET remote_state = 'pushed' WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}
	if j, ok, err = s.ClaimJob(ctx, JobDraftPush, acc, "w1", time.Minute, false); err != nil || !ok {
		t.Fatalf("claim again = %v, %v", ok, err)
	}
	if done, err := s.CompleteJob(ctx, j, "w1"); err != nil || !done {
		t.Fatalf("complete = %v, %v", done, err)
	}
	// Hinweis an allen wartenden Entwürfen, approval_stale bleibt.
	if _, err := s.db.Exec(`UPDATE drafts SET remote_state = 'pending', push_note = CASE id WHEN 2 THEN 'approval_stale' END WHERE id IN (1, 2)`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.DraftTx(ctx, func(x *DraftTx) error {
		var err error
		n, err = x.SetPushNotePending(acc, "no_drafts_mailbox")
		return err
	}); err != nil || n != 1 {
		t.Fatalf("push note set on %d drafts, %v", n, err)
	}
	if d, _ := s.Draft(ctx, 2); d.PushNote != "approval_stale" {
		t.Fatalf("draft 2 note = %q", d.PushNote)
	}
}

func TestMirrorCopy(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncMailboxes(ctx, acc, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}, {Path: "Drafts", Name: "Drafts", Role: "drafts"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acc)
	var mb int64
	for _, b := range boxes {
		if b.Role == "drafts" {
			mb = b.ID
		}
	}
	read := func(uid int64) MirrorCopy {
		t.Helper()
		var mc MirrorCopy
		if err := s.DraftTx(ctx, func(x *DraftTx) error {
			var err error
			mc, err = x.MirrorCopy(mb, uid, "pk.1@example.org", "h1")
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return mc
	}
	if mc := read(5); !mc.MailboxFound || mc.Synced || mc.AtUID {
		t.Fatalf("unsynced = %+v", mc)
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acc, MailboxID: mb, New: []NewMessage{
		{Link: LinkFlags{UID: 5, Draft: true, Seen: true}, MessageID: "pk.1@example.org", ContentHash: "h1", Size: 10},
		{Link: LinkFlags{UID: 6}, MessageID: "pk.1@example.org", ContentHash: "h2", Size: 11},
	}, State: &MailboxState{UIDValidity: 9, UIDNext: 7, Phase: PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	mc := read(5)
	if !mc.Synced || mc.UIDValidity != 9 || mc.UIDNext != 7 || !mc.AtUID || mc.AtUIDMessageID != "pk.1@example.org" ||
		mc.AtUIDContentHash != "h1" || !mc.NewerSameID || !mc.SameIDAndHash {
		t.Fatalf("mirror = %+v", mc)
	}
	if mc := read(6); mc.NewerSameID || mc.AtUIDContentHash != "h2" {
		t.Fatalf("uid 6 = %+v", mc)
	}
	if mc := read(99); mc.AtUID {
		t.Fatalf("uid 99 = %+v", mc)
	}
	var none MirrorCopy
	if err := s.DraftTx(ctx, func(x *DraftTx) error {
		var err error
		none, err = x.MirrorCopy(9999, 5, "pk.1@example.org", "h1")
		return err
	}); err != nil || none.MailboxFound {
		t.Fatalf("unknown mailbox = %+v, %v", none, err)
	}
}
