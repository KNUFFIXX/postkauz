package store

import (
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Nachprüfung der Prüfsitzung zu fd70f19, Punkt 1: Eine Datenbank, die
// 0.3.0-alpha.4 (da5386b) auf Schema 20 gebracht hat, bekommt virtual_view
// über Migration 0021; danach laufen Ordnerabgleich und ServerCopies, und
// der Bestand ist nach Namen gekennzeichnet. (Mit der Spalte als Nachtrag in
// 0020 scheiterten beide: Der Läufer vergleicht nur Nummern.)
func TestSchema20OfAlpha4Migrates(t *testing.T) {
	ctx := context.Background()
	f, err := os.Open(filepath.Join("testdata", "t2_schema20", "postkauz.db.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "postkauz.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.SchemaVersion(ctx); v != 20 {
		t.Fatalf("fixture schema = %d", v)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema after migrate = %d", v)
	}
	acct, err := s.AccountByName(ctx, "demo")
	if err != nil {
		t.Fatal(err)
	}
	cs, err := s.ServerCopies(ctx, 1)
	if err != nil || len(cs) != 3 {
		t.Fatalf("server copies = %+v %v", cs, err)
	}
	for _, c := range cs {
		if c.Virtual != (c.Mailbox == "[Gmail]/Wichtig") {
			t.Fatalf("virtual of %s = %v", c.Mailbox, c.Virtual)
		}
	}
	if _, _, err := s.SyncMailboxes(ctx, acct.ID, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "[Gmail]/Alle Nachrichten", Name: "Alle Nachrichten", Role: "all"},
		{Path: "[Gmail]/Wichtig", Name: "Wichtig", Role: "custom", Virtual: true},
		{Path: "[Gmail]/Papierkorb", Name: "Papierkorb", Role: "trash"},
		{Path: "[Gmail]/Markiert", Name: "Markiert", Role: "custom", Virtual: true}}); err != nil {
		t.Fatal(err)
	}
}
