package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// DefaultKeyCheckTTL: So lange gilt eine Prüfung des Schlüsselstatus als
// aktuell. Ein Widerruf wirkt spätestens nach dieser Zeit.
const DefaultKeyCheckTTL = 2 * time.Second

// KeyGuard prüft vor jedem Aufruf, ob der API-Schlüssel des Aufrufers noch
// gilt (nicht widerrufen, nicht abgelaufen). Lange laufende Oberflächen
// (MCP-Server, später REST) melden den Schlüssel nur beim Start an; ohne
// diese Prüfung arbeitete ein widerrufener Schlüssel bis zum Neustart
// weiter. Der Status kommt aus der Datenbank und wird höchstens TTL lang
// zwischengespeichert; ein Lesefehler lässt den Aufruf scheitern (fail
// closed). Gilt nur für Schlüssel mit ID „key:<name>"; die CLI ohne --key
// (Mensch, „cli") prüft nichts.
type KeyGuard struct {
	Repo KeyRepo
	// TTL: Höchstalter einer Prüfung; 0 = DefaultKeyCheckTTL.
	TTL time.Duration

	mu    sync.Mutex
	cache map[string]keyCheck
}

type keyCheck struct {
	at  time.Time
	key store.APIKey
}

// Check liefert nil, wenn der Schlüssel gilt, sonst ErrKeyRevoked,
// ErrKeyExpired oder ErrKeyInvalid (gelöscht) mit dem Namen, oder den
// Lesefehler.
func (g *KeyGuard) Check(ctx context.Context, k policy.Key, now time.Time) error {
	name, ok := strings.CutPrefix(k.ID, "key:")
	if !ok {
		return nil
	}
	if !k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt) {
		return fmt.Errorf("%w: %s", ErrKeyExpired, name)
	}
	ttl := g.TTL
	if ttl <= 0 {
		ttl = DefaultKeyCheckTTL
	}
	g.mu.Lock()
	c, hit := g.cache[name]
	g.mu.Unlock()
	if !hit || now.Sub(c.at) >= ttl || now.Before(c.at) {
		key, err := g.Repo.APIKeyByName(ctx, name)
		if errors.Is(err, store.ErrKeyNotFound) {
			return fmt.Errorf("%w: %s", ErrKeyInvalid, name)
		}
		if err != nil {
			return fmt.Errorf("check API key: %w", err)
		}
		c = keyCheck{at: now, key: key}
		g.mu.Lock()
		if g.cache == nil {
			g.cache = map[string]keyCheck{}
		}
		g.cache[name] = c
		g.mu.Unlock()
	}
	switch KeyStatus(c.key, now) {
	case "revoked":
		return fmt.Errorf("%w: %s", ErrKeyRevoked, name)
	case "expired":
		return fmt.Errorf("%w: %s", ErrKeyExpired, name)
	}
	return nil
}

// keyReason ist der Audit-Grund einer gescheiterten Schlüsselprüfung.
func keyReason(err error) string {
	switch {
	case errors.Is(err, ErrKeyRevoked):
		return "key_revoked"
	case errors.Is(err, ErrKeyExpired):
		return "key_expired"
	case errors.Is(err, ErrKeyInvalid):
		return "key_invalid"
	}
	return "key_check_failed"
}
