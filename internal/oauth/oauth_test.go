package oauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

type fakeRefresher struct {
	calls int
	next  Token
	err   error
}

func (f *fakeRefresher) Refresh(_ context.Context, rt secrets.Secret) (Token, error) {
	f.calls++
	if rt.Reveal() != "refresh-1" {
		return Token{}, errors.New("unknown refresh token")
	}
	return f.next, f.err
}

func TestSourceRefreshAndCooldown(t *testing.T) {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	r := &fakeRefresher{next: Token{AccessToken: "access-2", Expiry: now.Add(time.Hour)}}
	s := NewSource(Token{AccessToken: "access-1", RefreshToken: "refresh-1", Expiry: now.Add(10 * time.Minute)}, r)
	s.Now = func() time.Time { return now }
	var stored []Token
	s.OnRefresh = func(t Token) error { stored = append(stored, t); return nil }
	ctx := context.Background()

	// gültig: kein Refresh
	if tok, err := s.Token(ctx); err != nil || tok.Reveal() != "access-1" || r.calls != 0 {
		t.Fatalf("valid token: %v calls=%d", err, r.calls)
	}
	// innerhalb der Marge vor Ablauf: Refresh, altes Refresh-Token bleibt
	now = now.Add(9 * time.Minute)
	if tok, err := s.Token(ctx); err != nil || tok.Reveal() != "access-2" || r.calls != 1 {
		t.Fatalf("refresh: %v calls=%d", err, r.calls)
	}
	if len(stored) != 1 || stored[0].RefreshToken.Reveal() != "refresh-1" {
		t.Fatalf("OnRefresh: %+v", stored)
	}

	// Ablauf und fehlschlagender Refresh: Fehler, danach Sperrzeit ohne neuen Versuch
	now = now.Add(2 * time.Hour)
	r.err = errors.New("invalid_grant: CANARY-provider-detail")
	_, err := s.Token(ctx)
	if !errors.Is(err, ErrRefresh) || strings.Contains(err.Error(), "CANARY") {
		t.Fatalf("refresh failure: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := s.Token(ctx); !errors.Is(err, ErrRefresh) {
			t.Fatal("cooldown must keep failing")
		}
	}
	if r.calls != 2 {
		t.Fatalf("no endless retry: calls=%d", r.calls)
	}
	// nach der Sperrzeit ein neuer Versuch
	now = now.Add(6 * time.Minute)
	r.err = nil
	r.next = Token{AccessToken: "access-3", Expiry: now.Add(time.Hour)}
	if tok, err := s.Token(ctx); err != nil || tok.Reveal() != "access-3" || r.calls != 3 {
		t.Fatalf("after cooldown: %v calls=%d", err, r.calls)
	}
}

func TestSourceWithoutRefreshToken(t *testing.T) {
	s := NewSource(Token{AccessToken: "a", Expiry: time.Now().Add(-time.Hour)}, &fakeRefresher{})
	if _, err := s.Token(context.Background()); !errors.Is(err, ErrRefresh) {
		t.Fatalf("err = %v", err)
	}
}
