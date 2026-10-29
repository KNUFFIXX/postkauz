package httpmcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
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

// asTimeout: Frist für Verbindungsaufbau und Antwort des
// Autorisierungsservers (Discovery und JWKS).
const asTimeout = 10 * time.Second

// errASRedirect: Der Autorisierungsserver antwortete mit einer Umleitung.
// Discovery und JWKS folgen keiner Umleitung (der Aussteller steht fest in
// der Konfiguration; eine Umleitung wäre ein Fehler der Einrichtung).
var errASRedirect = errors.New("httpmcp: the authorization server answered with a redirect; redirects are not followed")

// newASClient baut den Client für Discovery und JWKS, wenn Deps.HTTPClient
// fehlt: Klon von http.DefaultTransport (Proxy aus der Umgebung, TLS wie
// üblich) mit eigenem DialContext, Fristen von asTimeout und ohne
// Umleitungen.
//
// Der eigene DialContext schaltet bewusst die SSRF-Sperre des MCP-SDK ab
// (oauthex: Discovery verweigert sonst private und CGNAT-Adressen, also
// genau 100.64.0.0/10 und fd7a:115c:a1e0::/48, wo der Aussteller im
// Tailnet liegt; ADR-010 Punkt 8). Die Sperre schützt Clients, die eine
// URL aus einer fremden Antwort abrufen. Hier steht der Aussteller fest in
// [http] issuer und kommt nie aus einer Anfrage; jwks_uri kommt aus den
// Metadaten genau dieses Ausstellers.
func newASClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	d := &net.Dialer{Timeout: asTimeout, KeepAlive: 30 * time.Second}
	tr.DialContext = d.DialContext
	return &http.Client{Transport: tr, Timeout: asTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errASRedirect }}
}

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
	client     *http.Client
	now        func() time.Time
	log        *slog.Logger

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
	failedAt time.Time
	// discovering ist offen, solange eine Discovery läuft.
	discovering chan struct{}
}

func (a *oauthAuth) authenticate(ctx context.Context, raw string, w audit.Writer) (principal, error) {
	// 1. JOSE-Typ at+jwt (RFC 9068): Ein ID-Token mit passender aud geht
	// sonst als Access-Token durch (Befund B5).
	if t := jwtType(raw); t != "at+jwt" && t != "application/at+jwt" {
		return principal{}, errUnauthenticated
	}
	// Vorprüfung von iss, exp, nbf und iat aus dem ungeprüften Inhalt:
	// Fremde oder abgelaufene Token lösen so weder Discovery noch
	// JWKS-Abruf aus; nbf und iat prüft nur diese Stelle (je 30 s).
	if !precheckClaims(raw, a.issuer, a.now()) {
		return principal{}, errUnauthenticated
	}
	v, err := a.getVerifier(ctx)
	if err != nil {
		return principal{}, errUnavailable
	}
	// 2. bis 4. Signatur, iss genau gleich, exp mit höchstens 30 s
	// Uhrabweichung (go-oidc). War die JWKS nicht abrufbar (keySet), liegt
	// es nicht am Token: 503 statt 401.
	st := &verifyState{}
	tok, err := v.Verify(context.WithValue(ctx, verifyStateKey{}, st), raw)
	if err != nil {
		if st.unavailable || ctx.Err() != nil {
			return principal{}, errUnavailable
		}
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
		auditDenied(ctx, w, now, "", o, "principal_unknown")
		return principal{}, errForbidden
	}
	k, err := ops.KeyByName(ctx, a.repo, name, now)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrKeyNotFound):
		auditDenied(ctx, w, now, "", o, "principal_unknown")
		return principal{}, errForbidden
	case errors.Is(err, ops.ErrKeyRevoked):
		auditDenied(ctx, w, now, name, o, "key_revoked")
		return principal{}, &deniedError{code: "key_revoked"}
	case errors.Is(err, ops.ErrKeyExpired):
		auditDenied(ctx, w, now, name, o, "key_expired")
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
//
// Die Discovery läuft im Hintergrund mit eigener Frist, unabhängig vom
// Kontext der Anfrage, die sie anstößt: Trennt dieser Client, läuft sie für
// die anderen weiter, und failedAt bleibt leer (früher sperrte ein
// Abbruch alle für 10 s). Wer wartet, wartet nur bis zum Ende seines
// eigenen Kontexts.
func (a *oauthAuth) getVerifier(ctx context.Context) (*oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	if a.verifier != nil {
		v := a.verifier
		a.mu.Unlock()
		return v, nil
	}
	now := a.now()
	if !a.failedAt.IsZero() && now.Sub(a.failedAt) < discoveryRetry && !now.Before(a.failedAt) {
		a.mu.Unlock()
		return nil, errUnavailable
	}
	done := a.discovering
	if done == nil {
		done = make(chan struct{})
		a.discovering = done
		go a.discover(context.WithoutCancel(ctx), done)
	}
	a.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.verifier == nil {
		return nil, errUnavailable
	}
	return a.verifier, nil
}

// discover führt eine Discovery aus und trägt das Ergebnis ein: den Prüfer
// oder den Zeitpunkt des Fehlschlags. done schließt danach.
func (a *oauthAuth) discover(ctx context.Context, done chan struct{}) {
	defer close(done)
	dctx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()
	jwks, err := discoverJWKS(dctx, a.issuer, a.client)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.discovering = nil
	if err != nil {
		a.failedAt = a.now()
		a.log.Warn("http mcp: authorization server discovery failed", "issuer", a.issuer, "error", err)
		return
	}
	// Der Schlüsselsatz lebt so lange wie der Server (go-oidc nutzt den
	// Kontext nur für den HTTP-Client, newJWKSClient mit Frist).
	keyCtx := oidc.ClientContext(context.Background(), newJWKSClient(a.client, func() time.Time { return a.now() }))
	a.verifier = oidc.NewVerifier(a.issuer, &keySet{inner: oidc.NewRemoteKeySet(keyCtx, jwks)}, &oidc.Config{
		SkipClientIDCheck:    true, // aud prüft authenticate gegen die Resource-URL
		SupportedSigningAlgs: signingAlgs,
		Now:                  func() time.Time { return a.now().Add(-ClockSkew) },
	})
	a.log.Info("http mcp: authorization server found", "issuer", a.issuer)
}

// discoverJWKS liefert jwks_uri des Ausstellers: zuerst RFC 8414
// (/.well-known/oauth-authorization-server mit dem Pfad des Ausstellers),
// dann OpenID-Discovery. Der Aussteller in den Metadaten muss genau
// passen (oauthex.GetAuthServerMeta), jwks_uri muss https sein
// (checkJWKSURI). c = nil: newASClient.
func discoverJWKS(ctx context.Context, issuer string, c *http.Client) (string, error) {
	u, err := url.Parse(issuer)
	if err != nil {
		return "", err
	}
	if err := checkIssuerHost(u); err != nil {
		return "", err
	}
	path := strings.TrimSuffix(u.Path, "/")
	if c == nil {
		c = newASClient()
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
		case checkJWKSURI(meta.JWKSURI) != nil:
			return "", checkJWKSURI(meta.JWKSURI)
		default:
			return meta.JWKSURI, nil
		}
	}
	return "", last
}

// checkIssuerHost: Der Aussteller braucht einen DNS-Namen; eine
// IP-Adresse geht nur auf Loopback (Tests, lokaler Betrieb). Grund:
// tailscale serve bekommt Zertifikate nur für MagicDNS-Namen, und das SDK
// lehnt private und CGNAT-Adressen in der URL ohnehin ab
// (checkHTTPSOrLoopback); so ist der Fehler eindeutig.
func checkIssuerHost(u *url.URL) error {
	ip, err := netip.ParseAddr(u.Hostname())
	if err != nil || ip.Unmap().IsLoopback() {
		return nil
	}
	return fmt.Errorf("httpmcp: issuer %q uses an IP address; use the DNS name of the authorization server (tailscale serve certificates are issued for MagicDNS names only)", u.Redacted())
}

// jwtType liest den Typ aus dem JOSE-Kopf ohne Prüfung, nur für die
// typ-Regel.
func jwtType(raw string) string {
	h, _ := parseHeader(raw)
	return strings.ToLower(h.Typ)
}
