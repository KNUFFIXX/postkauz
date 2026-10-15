package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

func TestAccountsCRUD(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	a := Account{Name: "demo", Email: "demo@example.invalid", Host: "127.0.0.1", Port: 11143, TLS: "none",
		Username: "tester", AuthType: "password", CredentialRef: "account/demo", InsecureTestServer: true}
	id, err := s.CreateAccount(ctx, a)
	if err != nil || id == 0 {
		t.Fatalf("create: id=%d err=%v", id, err)
	}
	if _, err := s.CreateAccount(ctx, a); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("duplicate: %v", err)
	}
	got, err := s.AccountByName(ctx, "demo")
	if err != nil || got.ID != id || !got.InsecureTestServer || got.Status != "new" || got.Capabilities != "{}" || got.CreatedAt.IsZero() {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := s.AccountByName(ctx, "nope"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("missing: %v", err)
	}

	// Ungelesen-Summe und letzter Sync kommen aus den Ordnern.
	now := time.Now().UnixMilli()
	if _, err := s.db.Exec(`INSERT INTO mailboxes(account_id, path, name, role, unread, last_sync_at) VALUES (?,?,?,?,?,?),(?,?,?,?,?,?)`,
		id, "INBOX", "INBOX", "inbox", 3, now, id, "Archive", "Archive", "archive", 2, now-1000); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAccounts(ctx)
	if err != nil || len(list) != 1 || list[0].UnreadTotal != 5 || list[0].LastSyncAt.UnixMilli() != now {
		t.Fatalf("list: %+v %v", list, err)
	}

	if err := s.UpdateAccountProbe(ctx, id, `{"idle":true}`, "ok", ""); err != nil {
		t.Fatal(err)
	}
	got, _ = s.AccountByName(ctx, "demo")
	if got.Status != "ok" || got.Capabilities != `{"idle":true}` {
		t.Fatalf("probe update: %+v", got)
	}

	if n, err := s.CountMessages(ctx, id); err != nil || n != 0 {
		t.Fatalf("count: %d %v", n, err)
	}
	if err := s.DeleteAccount(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAccount(ctx, id); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if list, _ := s.ListAccounts(ctx); len(list) != 0 {
		t.Fatalf("list after delete: %+v", list)
	}
}

func TestSQLiteAuditWriter(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	w := s.AuditWriter()
	ev := audit.Event{TS: time.UnixMilli(1000), ActorType: audit.ActorHuman, KeyID: "cli", Scope: "admin",
		Operation: "accounts.add", ParamsRedacted: []byte(`{"fields":["host"]}`), Targets: []string{"account:demo"},
		Result: audit.ResultOK, Reason: "ok", DryRun: true, Duration: 5 * time.Millisecond}
	if err := w.Append(ctx, ev); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(ctx, audit.Event{TS: time.UnixMilli(2000), ActorType: audit.ActorAgent, Operation: "accounts.list", Result: audit.ResultDenied}); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(ctx, audit.Event{TS: time.UnixMilli(3000), ActorType: audit.ActorAgent, Operation: "x.y", Result: audit.ResultOK, ParamsRedacted: []byte(`{nope`)}); err == nil {
		t.Error("invalid params JSON must be rejected")
	}
	if err := w.Append(ctx, audit.Event{TS: time.UnixMilli(3000), ActorType: "robot", Operation: "x.y", Result: audit.ResultOK}); err == nil {
		t.Error("unknown actor type must fail the CHECK")
	}
	evs, err := s.AuditEvents(ctx, 0, 10)
	if err != nil || len(evs) != 2 {
		t.Fatalf("events: %+v %v", evs, err)
	}
	if e := evs[0]; e.Operation != "accounts.add" || !e.DryRun || e.KeyID != "cli" || len(e.Targets) != 1 || e.Targets[0] != "account:demo" || e.TS.UnixMilli() != 1000 {
		t.Fatalf("event 0: %+v", e)
	}
	if e := evs[1]; e.Result != audit.ResultDenied || e.KeyID != "" || len(e.Targets) != 0 {
		t.Fatalf("event 1: %+v", e)
	}
}

func TestMigrateRefusesNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO schema_migrations(version, name, applied_at) VALUES (?, 'future', 0)`, KnownSchemaVersion()+1); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Migrate on newer schema = %v", err)
	}
	_ = s.Close()
}
