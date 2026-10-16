package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// fakeKeyRepo liefert einen Schlüssel per Namen und zählt die Abfragen.
type fakeKeyRepo struct {
	KeyRepo
	key     store.APIKey
	err     error
	lookups int
}

func (f *fakeKeyRepo) APIKeyByName(_ context.Context, name string) (store.APIKey, error) {
	f.lookups++
	if f.err != nil {
		return store.APIKey{}, f.err
	}
	if name != f.key.Name {
		return store.APIKey{}, store.ErrKeyNotFound
	}
	return f.key, nil
}

// Widerruf und Ablauf wirken während einer laufenden Sitzung, spätestens
// nach der TTL; ohne Datenbank kein Aufruf (fail closed).
func TestKeyGuard(t *testing.T) {
	t0 := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	repo := &fakeKeyRepo{key: store.APIKey{Name: "agent", ExpiresAt: t0.Add(time.Hour)}}
	g := &KeyGuard{Repo: repo, TTL: 2 * time.Second}
	ctx := context.Background()
	k := policy.Key{ID: "key:agent", Name: "agent", ExpiresAt: t0.Add(time.Hour)}
	if err := g.Check(ctx, k, t0); err != nil {
		t.Fatal(err)
	}
	repo.key.RevokedAt = t0.Add(time.Second)
	if err := g.Check(ctx, k, t0.Add(time.Second)); err != nil || repo.lookups != 1 {
		t.Fatalf("within TTL: %v, lookups %d", err, repo.lookups)
	}
	if err := g.Check(ctx, k, t0.Add(2*time.Second)); !errors.Is(err, ErrKeyRevoked) {
		t.Fatalf("after TTL: %v", err)
	}
	repo.key.RevokedAt = time.Time{}
	if err := g.Check(ctx, k, t0.Add(time.Hour)); !errors.Is(err, ErrKeyExpired) {
		t.Fatalf("expired: %v", err)
	}
	if err := g.Check(ctx, policy.Key{ID: "key:weg", Name: "weg"}, t0); !errors.Is(err, ErrKeyInvalid) {
		t.Fatalf("unknown: %v", err)
	}
	repo.err = errors.New("database is locked")
	if err := g.Check(ctx, k, t0.Add(10*time.Second)); err == nil || keyReason(err) != "key_check_failed" {
		t.Fatalf("lookup error: %v", err)
	}
	if err := g.Check(ctx, policy.Key{ID: "cli"}, t0); err != nil {
		t.Fatalf("cli: %v", err)
	}
}

// Im Executor: Der Handler läuft mit einem widerrufenen Schlüssel nicht,
// das Audit nennt den Grund.
func TestExecutorRejectsRevokedKey(t *testing.T) {
	ran := 0
	reg := NewRegistry()
	if err := reg.Register(newTestOp("m.read", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) {
		ran++
		return map[string]any{}, nil
	})); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	repo := &fakeKeyRepo{key: store.APIKey{Name: "agent"}}
	au := &audit.Memory{}
	x := &Executor{Registry: reg, Audit: au, Keys: &KeyGuard{Repo: repo, TTL: time.Second}, Now: func() time.Time { return now }}
	req := Request{Key: policy.Key{ID: "key:agent", Name: "agent", Scopes: policy.Roles["reader"]}, Actor: audit.ActorAgent, Operation: "m.read"}
	if _, err := x.Execute(context.Background(), req); err != nil || ran != 1 {
		t.Fatalf("active key: %v ran=%d", err, ran)
	}
	repo.key.RevokedAt = now
	now = now.Add(time.Second)
	if _, err := x.Execute(context.Background(), req); !errors.Is(err, ErrKeyRevoked) || ran != 1 {
		t.Fatalf("revoked key: %v ran=%d", err, ran)
	}
	ev := au.Events()
	if last := ev[len(ev)-1]; last.Result != audit.ResultDenied || last.Reason != "key_revoked" || last.KeyID != "key:agent" {
		t.Fatalf("audit = %+v", last)
	}
}
