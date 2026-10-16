package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

func TestAPIKeysStoreOnlyHashes(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	id, err := s.CreateAPIKey(ctx, APIKey{Name: "agent", KeyHash: "abc", Role: "reader", Scopes: []string{"mail:read"}, DryRunDefault: true, ExpiresAt: exp})
	if err != nil || id == 0 {
		t.Fatal(id, err)
	}
	if _, err := s.CreateAPIKey(ctx, APIKey{Name: "agent", KeyHash: "def", Role: "reader"}); !errors.Is(err, ErrKeyExists) {
		t.Fatalf("duplicate: %v", err)
	}
	k, err := s.APIKeyByName(ctx, "agent")
	if err != nil || k.KeyHash != "abc" || !k.DryRunDefault || !k.ExpiresAt.Equal(exp) || k.Scopes[0] != "mail:read" || k.CreatedAt.IsZero() {
		t.Fatalf("get: %+v %v", k, err)
	}
	if _, err := s.APIKeyByName(ctx, "nope"); !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("missing: %v", err)
	}
	if ok, err := s.RevokeAPIKey(ctx, id, time.Now()); !ok || err != nil {
		t.Fatalf("revoke: %v %v", ok, err)
	}
	if ok, _ := s.RevokeAPIKey(ctx, id, time.Now()); ok {
		t.Fatal("second revoke must report no change")
	}
	if err := s.TouchAPIKey(ctx, id, time.Now()); err != nil {
		t.Fatal(err)
	}
	list, _ := s.APIKeys(ctx)
	if len(list) != 1 || list[0].RevokedAt.IsZero() || list[0].LastUsedAt.IsZero() {
		t.Fatalf("list: %+v", list)
	}
}

func TestAuditQueryAndExports(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	w := s.AuditWriter().(interface {
		AppendID(context.Context, audit.Event) (int64, error)
	})
	base := time.Date(2026, 10, 15, 23, 0, 0, 0, time.UTC)
	for i, ev := range []audit.Event{
		{TS: base, ActorType: audit.ActorHuman, KeyID: "cli", Operation: "accounts.list", Result: audit.ResultOK},
		{TS: base.Add(2 * time.Hour), ActorType: audit.ActorAgent, KeyID: "key:a", Operation: "messages.get", Result: audit.ResultDenied, Reason: "missing_scope"},
		{TS: base.Add(3 * time.Hour), ActorType: audit.ActorAgent, KeyID: "key:a", Operation: "messages.search", Result: audit.ResultOK, Duration: 12 * time.Millisecond},
	} {
		id, err := w.AppendID(ctx, ev)
		if err != nil || id != int64(i+1) {
			t.Fatalf("append %d: id=%d %v", i, id, err)
		}
	}
	got, _ := s.AuditQuery(ctx, AuditFilter{Key: "key:a", Result: "ok"})
	if len(got) != 1 || got[0].Operation != "messages.search" || got[0].Duration != 12*time.Millisecond {
		t.Fatalf("filter: %+v", got)
	}
	got, _ = s.AuditQuery(ctx, AuditFilter{Since: base.Add(time.Hour), AfterID: 1, Limit: 1})
	if len(got) != 1 || got[0].ID != 2 || got[0].Reason != "missing_scope" {
		t.Fatalf("since/after/limit: %+v", got)
	}
	days, _ := s.AuditDays(ctx)
	if len(days) != 2 || days[0] != "2026-10-15" || days[1] != "2026-10-16" {
		t.Fatalf("days: %v", days)
	}
	e := AuditExport{Day: "2026-10-15", FirstEventID: 1, LastEventID: 1, SHA256: "x", Location: "/tmp/a", ExportedAt: time.Now()}
	_ = s.SaveAuditExport(ctx, e)
	e.LastEventID, e.SHA256 = 2, "y"
	if err := s.SaveAuditExport(ctx, e); err != nil {
		t.Fatal(err)
	}
	ex, _ := s.AuditExports(ctx)
	if len(ex) != 1 || ex[0].SHA256 != "y" || ex[0].LastEventID != 2 {
		t.Fatalf("exports: %+v", ex)
	}
}
