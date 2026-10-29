package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T3-Nachbesserung 3(b), 5(b): ReadSnapshot liest Verweise und Archive aus
// einem Schnappschuss, ohne ihn zu ändern (Prüfsumme gleich, keine
// Nebendateien wie -wal, -shm oder .lock).
func TestT3NReadSnapshotLeavesFileUnchanged(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(ctx, Account{Name: "a", Email: "a@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "u", AuthType: "password", CredentialRef: "account/a"}); err != nil {
		t.Fatal(err)
	}
	sum := strings.Repeat("a", 64)
	if err := s.ExecForTest(`INSERT INTO blobs(sha256, size, created_at) VALUES (?, 1, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecForTest(`INSERT INTO messages(account_id, blob_sha256, created_at) VALUES (1, ?, 1)`, sum); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecForTest(`INSERT INTO audit_archives(file, from_day, to_day, first_event_id, last_event_id, events, sha256,
		chain_prev, chain_head, created_at) VALUES ('audit-archive-2025-01-01-2025-01-31.db', '2025-01-01', '2025-01-31', 1, 2, 2, ?, 'p', 'h', 1)`,
		sum); err != nil {
		t.Fatal(err)
	}
	snapDir := t.TempDir()
	snap := filepath.Join(snapDir, "snap.db")
	if err := s.SnapshotTo(ctx, snap); err != nil {
		t.Fatal(err)
	}
	before, err := FileSHA256(snap)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadSnapshot(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Blobs[sum] || len(got.Archives) != 1 || got.Archives[0].File != "audit-archive-2025-01-01-2025-01-31.db" {
		t.Fatalf("ReadSnapshot = %+v", got)
	}
	after, err := FileSHA256(snap)
	if err != nil || after != before {
		t.Fatalf("snapshot changed by ReadSnapshot: %s -> %s (%v)", before, after, err)
	}
	entries, _ := os.ReadDir(snapDir)
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("files next to the snapshot: %v", names)
	}
	if _, err := ReadSnapshot(ctx, filepath.Join(snapDir, "missing.db")); err == nil {
		t.Fatal("ReadSnapshot of a missing file succeeded")
	}
}
