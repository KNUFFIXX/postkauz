package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Gegenprüfung S2.6–S2.8, Runde 1.

// K6: Die Abfragen nach Kopien einer Message-ID im Entwürfe-Ordner nutzen
// den Index messages_message_id(account_id, message_id_header), statt alle
// Nachrichten des Ordners zu lesen.
func TestDraftCopyQueriesUseMessageIDIndex(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SyncMailboxes(ctx, acc, []MailboxSpec{{Path: "Drafts", Name: "Drafts", Role: "drafts"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acc)
	mb := boxes[0].ID
	for name, q := range map[string]struct {
		sql  string
		args []any
	}{
		"MirrorDraftCopies": {qMirrorDraftCopies, []any{mb, mb, "pk.1@example.org"}},
		"MirrorCopy":        {qMirrorCopySameID, []any{mb, mb, "pk.1@example.org", 3, mb, mb, "pk.1@example.org", "h"}},
		"MirrorOrphan":      {qMirrorOrphan, []any{mb, mb, "pk.1@example.org", "h"}},
	} {
		rows, err := s.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+q.sql, q.args...)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		_ = rows.Close()
		if !strings.Contains(strings.Join(plan, "\n"), "messages_message_id") {
			t.Errorf("%s does not use the index messages_message_id:\n%s", name, strings.Join(plan, "\n"))
		}
	}
}

// W1: Migration 0015 übernimmt updated_at als Zeit der letzten
// Inhaltsänderung; UpdateDraft schreibt sie, ohne Angabe gilt updated_at.
func TestMigration0015ContentChangedAt(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.MigrateToForTest(ctx, 14); err != nil {
		t.Fatal(err)
	}
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO drafts(account_id,kind,message_id_header,status,created_at,updated_at)
		VALUES (?,'new','pk.1@example.org','ready',1,2000)`, acc); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() || v < 15 {
		t.Fatalf("schema %d, known %d", v, KnownSchemaVersion())
	}
	d, err := s.Draft(ctx, 1)
	if err != nil || d.ContentChangedAt.UnixMilli() != 2000 || d.UpdatedAt.UnixMilli() != 2000 {
		t.Fatalf("old draft = %v / %v, %v", d.ContentChangedAt, d.UpdatedAt, err)
	}
	d.UpdatedAt = d.UpdatedAt.Add(time.Hour)
	if err := s.DraftTx(ctx, func(x *DraftTx) error { return x.UpdateDraft(d) }); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Draft(ctx, 1); got.ContentChangedAt.UnixMilli() != 2000 || !got.UpdatedAt.Equal(d.UpdatedAt) {
		t.Fatalf("after update: %v / %v", got.ContentChangedAt, got.UpdatedAt)
	}
}
