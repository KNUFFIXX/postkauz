package store

import (
	"context"
	"testing"
	"time"
)

// Gegenprüfung S2.3, W1: Migration 0012 ergänzt approvals.message_hash.
// Eine Freigabe aus Schema 11 bekommt den leeren Hash (nicht freizugeben, neu
// vorlegen); neue Freigaben speichern den Hash und lesen ihn zurück.
func TestMigration0012ApprovalMessageHash(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.MigrateToForTest(ctx, 11); err != nil {
		t.Fatal(err)
	}
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO drafts(account_id,kind,message_id_header,status,created_at,updated_at)
		VALUES (?,'new','pk.1@example.org','draft',1,1)`, acc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO approvals(subject_type, subject_id, description, requested_at, preview_hash, decision)
		VALUES ('draft', 1, 'draft 1', 1, 'p', 'pending')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 12 || KnownSchemaVersion() != 12 {
		t.Fatalf("schema %d, known %d", v, KnownSchemaVersion())
	}
	old, err := s.Approval(ctx, 1)
	if err != nil || old.MessageHash != "" || old.PreviewHash != "p" {
		t.Fatalf("old approval = %+v, %v", old, err)
	}
	if _, err := s.db.Exec(`UPDATE approvals SET decision = 'superseded' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	var id int64
	err = s.DraftTx(ctx, func(x *DraftTx) error {
		var err error
		id, err = x.InsertApproval(Approval{SubjectType: "draft", SubjectID: 1, Description: "draft 1", RequestedAt: time.Now(),
			PreviewHash: "p", MessageHash: "m"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if a, err := s.Approval(ctx, id); err != nil || a.MessageHash != "m" {
		t.Fatalf("new approval = %+v, %v", a, err)
	}
}
