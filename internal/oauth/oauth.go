// Package oauth ist der Entwurf für OAuth2-Zugangsdaten (Prüfpunkt P3).
//
// Grundsätze (docs/sicherheit.md, Abschnitt 4): „bring your own client"
// (Client-ID/Secret des Nutzers, keine eigene verifizierte Google-App);
// Token werden wie Passwörter behandelt (Schlüsselbund); ein fehlgeschlagener
// Refresh erzeugt einen Fehler und einen Hinweis, keine Endlosschleife.
//
// Stand S1.1: Token-Typ und eine erneuernde TokenSource mit Sperrzeit nach
// Fehlern, getestet mit einem Fake-Refresher und einem Fake-IMAP-Server
// (internal/imapx). Der Autorisierungs-Flow (Loopback oder Device Code) und
// der HTTP-Refresher gegen den Token-Endpunkt fehlen noch.
package oauth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Token ist ein OAuth2-Token-Paar.
type Token struct {
	AccessToken  secrets.Secret
	RefreshToken secrets.Secret
	Expiry       time.Time // Nullzeit = unbekannt (gilt als gültig)
}

// Refresher tauscht ein Refresh-Token gegen ein neues Token (Token-Endpunkt
// des Anbieters). Liefert er kein neues Refresh-Token, bleibt das alte.
type Refresher interface {
	Refresh(ctx context.Context, refreshToken secrets.Secret) (Token, error)
}

// ErrRefresh: Erneuerung fehlgeschlagen. Der Aufrufer meldet das (Audit,
// doctor) und versucht es erst nach der Sperrzeit wieder.
var ErrRefresh = errors.New("oauth: token refresh failed")

// Source liefert ein gültiges Access-Token und erneuert es kurz vor Ablauf.
type Source struct {
	mu        sync.Mutex
	tok       Token
	refresher Refresher
	// Margin: so lange vor Ablauf wird erneuert (Standard 2 Minuten).
	Margin time.Duration
	// Cooldown: Sperrzeit nach einem fehlgeschlagenen Refresh (Standard 5 Minuten).
	Cooldown time.Duration
	// OnRefresh wird nach jedem erfolgreichen Refresh aufgerufen (Token im
	// Schlüsselbund aktualisieren).
	OnRefresh func(Token) error
	Now       func() time.Time

	failedAt time.Time
	lastErr  error
}

// NewSource legt eine Quelle für ein vorhandenes Token an.
func NewSource(tok Token, r Refresher) *Source { return &Source{tok: tok, refresher: r} }

func (s *Source) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Token liefert das Access-Token, nötigenfalls nach einem Refresh. Nach einem
// Fehlschlag liefert sie bis zum Ende der Sperrzeit sofort denselben Fehler.
func (s *Source) Token(ctx context.Context) (secrets.Secret, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	margin, cooldown := s.Margin, s.Cooldown
	if margin == 0 {
		margin = 2 * time.Minute
	}
	if cooldown == 0 {
		cooldown = 5 * time.Minute
	}
	now := s.now()
	if !s.tok.AccessToken.IsZero() && (s.tok.Expiry.IsZero() || now.Add(margin).Before(s.tok.Expiry)) {
		return s.tok.AccessToken, nil
	}
	if s.lastErr != nil && now.Sub(s.failedAt) < cooldown {
		return "", s.lastErr
	}
	if s.tok.RefreshToken.IsZero() {
		return "", fmt.Errorf("%w: no refresh token; authorize the account again", ErrRefresh)
	}
	nt, err := s.refresher.Refresh(ctx, s.tok.RefreshToken)
	if err == nil && nt.AccessToken.IsZero() {
		err = errors.New("empty access token")
	}
	if err != nil {
		// Den Fehlertext des Anbieters nicht weiterreichen (kann Token-Teile
		// oder Client-Daten enthalten), nur die Tatsache.
		s.failedAt, s.lastErr = now, fmt.Errorf("%w; retry after %s or authorize the account again", ErrRefresh, cooldown)
		return "", s.lastErr
	}
	if nt.RefreshToken.IsZero() {
		nt.RefreshToken = s.tok.RefreshToken
	}
	s.tok, s.lastErr = nt, nil
	if s.OnRefresh != nil {
		if err := s.OnRefresh(nt); err != nil {
			return "", fmt.Errorf("oauth: store refreshed token: %w", err)
		}
	}
	return nt.AccessToken, nil
}
