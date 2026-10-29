package httpmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// ClockSkew: erlaubte Uhrabweichung beim Ablauf eines Tokens (ADR-010
// Punkt 5).
const ClockSkew = 30 * time.Second

// discoveryRetry: Nach einer gescheiterten Discovery wird frühestens so
// spät neu gefragt (der Autorisierungsserver soll nicht mit jeder Anfrage
// erneut belastet werden).
const discoveryRetry = 10 * time.Second

// discoveryTimeout begrenzt eine Discovery.
const discoveryTimeout = 10 * time.Second

// signingAlgs: angenommene Signaturverfahren (asymmetrisch; nie "none",
// nie HMAC).
var signingAlgs = []string{oidc.RS256, oidc.PS256, oidc.ES256, oidc.EdDSA}

// oauthAuth: Modus oauth (S3.6). Prüft das Access-Token eines externen
// Autorisierungsservers in der Reihenfolge aus ADR-010 Punkt 5 und ordnet
// das Subjekt einem PostKauz-Schlüssel zu. Die Kryptographie macht
// go-oidc (Signatur über die JWKS, Cache mit Neuladen bei unbekannter kid);
// hier steht nur, was geprüft wird.
type oauthAuth struct {
	issuer     string
	resource   string
	principals map[string]string
	repo       ops.KeyRepo
	audit      audit.Writer
	client     *http.Client
	now        func() time.Time
	log        *slog.Logger

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	failedAt time.Time
}

func (a *oauthAuth) authenticate(ctx context.Context, raw string) (principal, error) {
	// 1. JOSE-Typ at+jwt (RFC 9068): Ein ID-Token mit passender aud geht
	// sonst als Access-Token durch (Befund B5).
	if t := jwtType(raw); t != "at+jwt" && t != "application/at+jwt" {
		return principal{}, errUnauthenticated
	}
	v, err := a.getVerifier(ctx)
	if err != nil {
		return principal{}, errUnavailable
	}
	// 2. bis 4. Signatur, iss genau gleich, exp mit höchstens 30 s
	// Uhrabweichung (go-oidc).
	tok, err := v.Verify(ctx, raw)
	if err != nil {
		return principal{}, errUnauthenticated
	}
	// 5. aud enthält die eigene Resource-URL (Akzeptanzkriterium 5:
	// Token-Weitergabe abgelehnt).
	if !oauthex.MatchesResource(tok.Audience, a.resource) || tok.Subject == "" {
		return principal{}, errUnauthenticated
	}
	// 6. sub → PostKauz-Schlüssel; Rechte nur aus dem Schlüssel.
	now := a.now()
	o := ops.Origin{Transport: transportHTTP, Issuer: tok.Issuer, Subject: tok.Subject}
	name, ok := a.principals[tok.Subject]
	if !ok {
		auditDenied(ctx, a.audit, now, "", o, "principal_unknown")
		return principal{}, errForbidden
	}
	k, err := ops.KeyByName(ctx, a.repo, name, now)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrKeyNotFound):
		auditDenied(ctx, a.audit, now, "", o, "principal_unknown")
		return principal{}, errForbidden
	case errors.Is(err, ops.ErrKeyRevoked):
		auditDenied(ctx, a.audit, now, name, o, "key_revoked")
		return principal{}, &deniedError{code: "key_revoked"}
	case errors.Is(err, ops.ErrKeyExpired):
		auditDenied(ctx, a.audit, now, name, o, "key_expired")
		return principal{}, &deniedError{code: "key_expired"}
	default:
		return principal{}, errUnavailable
	}
	o.Principal = name
	return principal{key: k, origin: o}, nil
}

// getVerifier holt beim ersten Bedarf die Metadaten des
// Autorisierungsservers (RFC 8414, sonst OpenID-Discovery) und baut den
// Prüfer mit der JWKS daraus. Ist der Server beim Start noch nicht
// erreichbar, läuft PostKauz trotzdem; die Anfragen bekommen bis dahin 503.
func (a *oauthAuth) getVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.verifier != nil {
		return a.verifier, nil
	}
	now := a.now()
	if !a.failedAt.IsZero() && now.Sub(a.failedAt) < discoveryRetry && !now.Before(a.failedAt) {
		return nil, errUnavailable
	}
	dctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	jwks, err := discoverJWKS(dctx, a.issuer, a.client)
	if err != nil {
		a.failedAt = now
		a.log.Warn("http mcp: authorization server discovery failed", "issuer", a.issuer, "error", err)
		return nil, err
	}
	// Der Schlüsselsatz lebt so lange wie der Server (go-oidc nutzt den
	// Kontext nur für den HTTP-Client).
	keyCtx := context.Background()
	if a.client != nil {
		keyCtx = oidc.ClientContext(keyCtx, a.client)
	}
	a.verifier = oidc.NewVerifier(a.issuer, oidc.NewRemoteKeySet(keyCtx, jwks), &oidc.Config{
		SkipClientIDCheck:    true, // aud prüft authenticate gegen die Resource-URL
		SupportedSigningAlgs: signingAlgs,
		Now:                  func() time.Time { return a.now().Add(-ClockSkew) },
	})
	a.log.Info("http mcp: authorization server found", "issuer", a.issuer)
	return a.verifier, nil
}

// discoverJWKS liefert jwks_uri des Ausstellers: zuerst RFC 8414
// (/.well-known/oauth-authorization-server mit dem Pfad des Ausstellers),
// dann OpenID-Discovery. Der Aussteller in den Metadaten muss genau
// passen (oauthex.GetAuthServerMeta).
func discoverJWKS(ctx context.Context, issuer string, c *http.Client) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	if c == nil {
		c = http.DefaultClient
	}
	urls := []string{
		u.Scheme + "://" + u.Host + "/.well-known/oauth-authorization-server" + path,
		strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration",
	}
	var last error
	for _, m := range urls {
		meta, err := oauthex.GetAuthServerMeta(ctx, m, issuer, c)
		switch {
		case err != nil:
			last = err
		case meta == nil:
			last = fmt.Errorf("no metadata at %s", m)
		case meta.JWKSURI == "":
			last = fmt.Errorf("metadata at %s has no jwks_uri", m)
		default:
			return meta.JWKSURI, nil
		}
	}
	return "", last
}

// jwtType liest den JOSE-Kopf ohne Prüfung, nur für die typ-Regel (kein
// Krypto-Code; die Signatur prüft go-oidc).
func jwtType(raw string) string {
	h, _, ok := strings.Cut(raw, ".")
	if !ok || len(h) > 4096 {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return ""
	}
	var hdr struct {
		Typ string `json:"typ"`
	}
	if json.Unmarshal(b, &hdr) != nil {
		return ""
	}
	return strings.ToLower(hdr.Typ)
}
