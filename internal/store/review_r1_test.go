package store

import (
	"context"
	"strings"
	"testing"
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
