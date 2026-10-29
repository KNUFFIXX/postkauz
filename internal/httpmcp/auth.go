package httpmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// principal ist das Ergebnis einer Anmeldung: der PostKauz-Schlüssel, für
// den die Anfrage läuft, und der Weg fürs Audit. Rechte kommen nur aus dem
// Schlüssel; ein Token erweitert nie (ADR-010 Punkt 5).
type principal struct {
	key    policy.Key
	origin ops.Origin
}

// Fehler der Anmeldung. Die Antwort nennt nie den Grund im Einzelnen, kein
// Token und kein Subjekt (Befund B2 des Spikes).
var (
	// errUnauthenticated → 401 mit WWW-Authenticate.
	errUnauthenticated = errors.New("invalid token")
	// errForbidden → 403 ohne Hinweis (unbekanntes Subjekt, ADR-010 Punkt 5).
	errForbidden = errors.New("forbidden")
	// errUnavailable → 503 (Autorisierungsserver nicht erreichbar,
	// Datenbank nicht lesbar).
	errUnavailable = errors.New("service unavailable")
	// errUnavailableVerified → 503 wie errUnavailable, aber erst nach
	// geglückter Prüfung des Tokens (Datenbank nicht lesbar). Nur dieser
	// Fall zieht nicht aus dem Vorrat für Fehlschläge (Server.mcpGate).
	errUnavailableVerified = fmt.Errorf("%w (token verified)", errUnavailable)
)

// deniedError → 403 mit Status denied und festem Code (Schlüssel
// widerrufen oder abgelaufen), wie die Ablehnung über stdio.
type deniedError struct{ code string }

func (e *deniedError) Error() string { return "denied (" + e.code + ")" }

// authenticator meldet ein Token an. w nimmt die Audit-Zeilen gescheiterter
// Anmeldungen auf; nil = keine Zeile (der Vorrat für Fehlschläge ist leer,
// Server.mcpGate).
type authenticator interface {
	authenticate(ctx context.Context, token string, w audit.Writer) (principal, error)
}

// keyCacheTTL: So lange gilt eine erfolgreiche Anmeldung mit einem
// Schlüssel ohne neuen Blick in die Datenbank (zustandslos heißt: jede
// Anfrage meldet sich an). Widerruf und Ablauf prüft der Executor trotzdem
// vor jedem Aufruf (ops.KeyGuard, 2 s).
const keyCacheTTL = 30 * time.Second

// keyCacheMax begrenzt den Zwischenspeicher (danach wird er geleert).
const keyCacheMax = 256

// keyAuth: Modus key (S3.5, R1). Bearer = PostKauz-Schlüssel; gescheiterte
// Anmeldungen schreibt ops.AuthenticateKeyAudited ins Audit (Weg http),
// solange der Vorrat für Fehlschläge reicht.
type keyAuth struct {
	repo ops.KeyRepo
	now  func() time.Time

	mu    sync.Mutex
	cache map[string]cachedKey // Hash des Schlüssels → Anmeldung
}

type cachedKey struct {
	at  time.Time
	key policy.Key
}

func (a *keyAuth) authenticate(ctx context.Context, token string, w audit.Writer) (principal, error) {
	now := a.now()
	h := ops.HashKey(token)
	a.mu.Lock()
	c, hit := a.cache[h]
	a.mu.Unlock()
	if hit && now.Sub(c.at) < keyCacheTTL && !now.Before(c.at) && (c.key.ExpiresAt.IsZero() || now.Before(c.key.ExpiresAt)) {
		return keyPrincipal(c.key), nil
	}
	k, err := ops.AuthenticateKeyAudited(ctx, a.repo, w, transportHTTP, token, now)
	switch {
	case err == nil:
	case errors.Is(err, ops.ErrKeyMissing), errors.Is(err, ops.ErrKeyInvalid):
		return principal{}, errUnauthenticated
	case errors.Is(err, ops.ErrKeyRevoked):
		return principal{}, &deniedError{code: "key_revoked"}
	case errors.Is(err, ops.ErrKeyExpired):
		return principal{}, &deniedError{code: "key_expired"}
	default:
		return principal{}, errUnavailable
	}
	a.mu.Lock()
	if a.cache == nil || len(a.cache) >= keyCacheMax {
		a.cache = map[string]cachedKey{}
	}
	a.cache[h] = cachedKey{at: now, key: k}
	a.mu.Unlock()
	return keyPrincipal(k), nil
}

func keyPrincipal(k policy.Key) principal {
	return principal{key: k, origin: ops.Origin{Transport: transportHTTP, Principal: k.Name}}
}

// auditDenied schreibt eine gescheiterte Anmeldung über OAuth ins Audit
// (keys.authenticate, denied): Weg, iss und sub, Schlüssel nur als Name;
// nie das Token. Scheitert das Schreiben, bleibt es bei der Ablehnung.
func auditDenied(ctx context.Context, w audit.Writer, now time.Time, keyName string, o ops.Origin, reason string) {
	if w == nil {
		return
	}
	id := ""
	if keyName != "" {
		id = "key:" + keyName
	}
	params, _ := json.Marshal(map[string]ops.Origin{"via": o})
	_ = w.Append(ctx, audit.Event{TS: now.UTC(), ActorType: audit.ActorAgent, KeyID: id, Operation: ops.OpAuthenticate,
		ParamsRedacted: params, Result: audit.ResultDenied, Reason: reason})
}

// bearerToken liest das Token aus "Authorization: Bearer <token>"; sonst "".
func bearerToken(header string) string {
	scheme, tok, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	tok = strings.TrimSpace(tok)
	if tok == "" || strings.ContainsAny(tok, " \t") {
		return ""
	}
	return tok
}
