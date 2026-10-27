package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// Migration 0018 (A1): started und ref_id; die Folge der IDs läuft weiter,
// höchstens ein Abschluss je Start, nie ein Verweis an einem Start-Eintrag.
func TestMigration0018AuditStarted(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 17); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.db.Exec(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'human', 'x.y', 'ok')`, i); err != nil {
			t.Fatal(err)
		}
	}
	// Ein zurückgerollter Eintrag zählt die Folge nicht hoch; die Folge
	// steht auf 5. Höher setzen, als hätte es Einträge gegeben, die es
	// nicht mehr gibt: Die Migration darf nicht darunter fallen.
	if _, err := s.db.Exec(`UPDATE sqlite_sequence SET seq = 9 WHERE name = 'audit_events'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	w := s.AuditWriter().(audit.IDWriter)
	start, err := w.AppendID(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorHuman, Operation: "x.y", Result: audit.ResultStarted})
	if err != nil || start != 10 {
		t.Fatalf("first id after 0018 = %d %v, want 10", start, err)
	}
	if _, err := w.AppendID(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorHuman, Operation: "x.y", Result: audit.ResultOK, RefID: start}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.AppendID(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorHuman, Operation: "x.y", Result: audit.ResultOK, RefID: start}); err == nil {
		t.Fatal("second end entry for the same start accepted")
	}
	if _, err := w.AppendID(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorHuman, Operation: "x.y", Result: audit.ResultStarted, RefID: 3}); err == nil {
		t.Fatal("start entry with ref_id accepted")
	}
	recs, err := s.AuditQuery(ctx, AuditFilter{AfterID: 9})
	if err != nil || len(recs) != 2 || recs[0].Result != audit.ResultStarted || recs[1].RefID != start {
		t.Fatalf("records = %+v %v", recs, err)
	}
	calls, err := s.AuditCalls(ctx, AuditFilter{AfterID: 9})
	if err != nil || len(calls) != 1 || calls[0].ID != start || calls[0].EndID != start+1 || calls[0].Result != audit.ResultOK {
		t.Fatalf("calls = %+v %v", calls, err)
	}
}

// Nachprüfung T1a: Ein zweiter Prozess migriert genau zwischen der ersten
// Schemaprüfung (unter der geteilten Sperre) und der exklusiven Sperre
// dieses Prozesses. Unter der exklusiven Sperre liest Migrate das Schema neu
// und migriert nicht noch einmal (kein zweiter Snapshot, kein Fehler).
func TestMigrateRereadsSchemaUnderExclusiveLock(t *testing.T) {
	ctx := context.Background()
	s, path := schema16(t, 3)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	var other string
	fired := false
	testHookUnlocked = func() {
		if fired {
			return // der zweite Prozess selbst
		}
		fired = true
		second, err := Open(path)
		if err != nil {
			t.Errorf("second open: %v", err)
			return
		}
		if err := second.Migrate(ctx); err != nil {
			t.Errorf("second migrate: %v", err)
		}
		other = second.LastSnapshot
		_ = second.Close()
	}
	t.Cleanup(func() { testHookUnlocked = nil })
	if err := first.Migrate(ctx); err != nil {
		t.Fatalf("migrate after another process migrated in between: %v", err)
	}
	if !fired || other == "" {
		t.Fatalf("hook fired %v, snapshot of the second process %q", fired, other)
	}
	if first.LastSnapshot != "" {
		t.Fatalf("first process migrated again (snapshot %q)", first.LastSnapshot)
	}
	if v, _ := first.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema = %d", v)
	}
	snaps, _ := filepath.Glob(path + ".pre-*.bak")
	if len(snaps) != 1 {
		t.Fatalf("snapshots = %v, want exactly one", snaps)
	}
}

// A4: remote_status sagt, wo eine Nachricht liegt. Nur importiert →
// imported_only; gespiegelt → present; auf dem Server gelöscht (ohne
// Import) → deleted_remote. Gleich in MessageByID, Suche und Thread.
func TestRemoteStatus(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acct := newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	if _, err := s.ApplyImport(ctx, ImportBatch{AccountID: acct,
		New: []ImportNew{{Msg: newMsg(0, "imp@example.org", true), Folders: []ImportFolderFlags{{Folder: "Archiv"}}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: []NewMessage{newMsg(7, "srv@example.org", false)}}); err != nil {
		t.Fatal(err)
	}
	status := func(mid string) (string, string, string) {
		t.Helper()
		id, ok, err := s.MessageByHeaderID(ctx, acct, mid)
		if err != nil || !ok {
			t.Fatalf("%s: %v %v", mid, ok, err)
		}
		m, err := s.MessageByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var hit string
		res, err := s.Search(ctx, SearchParams{AccountID: acct, Limit: 50, Now: time.Now()})
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range res.Hits {
			if h.ID == id {
				hit = h.RemoteStatus
			}
		}
		var thr string
		if _, msgs, err := s.ThreadByID(ctx, m.ThreadID); err == nil {
			for _, tm := range msgs {
				if tm.ID == id {
					thr = tm.RemoteStatus
				}
			}
		} else {
			t.Fatal(err)
		}
		return m.RemoteStatus, hit, thr
	}
	for mid, want := range map[string]string{"imp@example.org": RemoteImportedOnly, "srv@example.org": RemotePresent} {
		if a, b, c := status(mid); a != want || b != want || c != want {
			t.Fatalf("%s: %s/%s/%s, want %s", mid, a, b, c, want)
		}
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, Removed: []uint32{7}}); err != nil {
		t.Fatal(err)
	}
	if a, b, c := status("srv@example.org"); a != RemoteDeleted || b != RemoteDeleted || c != RemoteDeleted {
		t.Fatalf("after deletion on the server: %s/%s/%s", a, b, c)
	}
}
