package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// A14 (T3): Der Snapshot vor einer Migration hat von Anfang an die Rechte
// 0600, nicht erst nach einem chmod; geprüft direkt nach VACUUM INTO.
func TestSnapshotCreatedWithOwnerOnlyMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes are not meaningful on Windows")
	}
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	var mode os.FileMode
	testHookSnapshotWritten = func(name string) {
		if fi, err := os.Stat(name); err == nil {
			mode = fi.Mode().Perm()
		}
	}
	defer func() { testHookSnapshotWritten = nil }()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if s.LastSnapshot == "" || mode != 0o600 {
		t.Fatalf("snapshot %q had mode %o right after VACUUM INTO, want 0600", s.LastSnapshot, mode)
	}
}

// A14 (T3): Nach einer erfolgreichen Migration bleiben nur die zwei
// neuesten Snapshots dieser Datenbank; fremde Dateien (andere Datenbank,
// anderes Muster) bleiben unberührt. Scheitert eine Migration, wird nichts
// aufgeräumt.
func TestPruneSnapshotsKeepsLastTwo(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "pk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 20); err != nil {
		t.Fatal(err)
	}
	old := []string{
		"pk.db.pre-0010-20261001T100000.000Z.bak",
		"pk.db.pre-0017-20261020T100000.000Z.bak",
		"pk.db.pre-0019-20261025T100000.000Z.bak",
	}
	foreign := []string{
		"other.db.pre-0002-20200101T000000.000Z.bak", // andere Datenbank
		"pk.db.pre-0003-kaputt.bak",                  // anderes Muster
		"pk.db.pre-0004-20200101T000000.000Z.bak.txt",
		"pk.db.bak",
	}
	for _, n := range append(slices.Clone(old), foreign...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var got []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{filepath.Base(s.LastSnapshot), old[2]}
	for _, n := range append(want, foreign...) {
		if !slices.Contains(got, n) {
			t.Errorf("%s was removed; files now %v", n, got)
		}
	}
	for _, n := range old[:2] {
		if slices.Contains(got, n) {
			t.Errorf("%s was kept; only the last %d snapshots stay", n, SnapshotsKept)
		}
	}
}

// Scheitert die Migration, bleiben alle Snapshots liegen (der neue ist dann
// der Weg zurück).
func TestPruneSnapshotsNotAfterFailedMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 16); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"pk.db.pre-0010-20261001T100000.000Z.bak", "pk.db.pre-0012-20261002T100000.000Z.bak"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Migration 0017 bricht bei UID 0 ab (migrationChecks).
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role) VALUES (1,1,'INBOX','INBOX','inbox')`,
		`INSERT INTO messages(id,account_id,created_at) VALUES (1,1,1)`,
		`INSERT INTO message_mailboxes(message_id,mailbox_id,uid) VALUES (1,1,0)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("migration 0017 must fail with UID 0")
	}
	entries, _ := os.ReadDir(dir)
	n := 0
	for _, e := range entries {
		if snapshotName.MatchString(e.Name()[len("pk.db"):]) {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("%d snapshots after a failed migration, want all 3", n)
	}
}
