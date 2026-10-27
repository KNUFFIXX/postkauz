// Package rs ist der Wegwerf-Prototyp für den Spike P-AUTH (A03 S3.0):
// PostKauz als reiner Resource Server hinter Streamable HTTP, zustandslos,
// mit Bearer-Token eines externen Autorisierungsservers. Nicht nach main.
package rs

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// Config beschreibt den Resource Server.
type Config struct {
	// Resource ist die eigene Resource-URL (RFC 9728), z. B. https://knoten/mcp.
	Resource string
	// Issuer des Autorisierungsservers (iss).
	Issuer string
	// JWKSURL; leer heißt: aus der OIDC-Discovery des Issuers.
	JWKSURL string
	// Principals ordnet `sub` einem PostKauz-Schlüsselnamen zu.
	Principals map[string]string
	// RequireATJWT verlangt den JOSE-Typ at+jwt (RFC 9068).
	RequireATJWT bool
	// ClockSkew ist die Uhrabweichung für exp (S3.6: 30 s).
	ClockSkew time.Duration
	// Scopes, die im Token stehen müssen (leer: keine).
	Scopes []string
	// AllowedHosts: erlaubte Host-Köpfe. Leer heißt: Host der Resource-URL.
	// Ersetzt den DNS-Rebinding-Schutz des SDK, der hinter einem Reverse-Proxy
	// auf Loopback (tailscale serve, Caddy) jeden fremden Host mit 403 ablehnt.
	AllowedHosts []string
	// SDKLocalhostProtection lässt den SDK-Schutz an (nur für den Vergleichstest).
	SDKLocalhostProtection bool
	// HTTPClient für Discovery und JWKS (Test-CA).
	HTTPClient *http.Client
	// Now nur für Tests.
	Now func() time.Time
}

// Feste Fehler: Die SDK-Middleware schreibt err.Error() in die Antwort,
// deshalb nie Details aus dem Token oder der Prüfung.
var (
	errInvalid   = fmt.Errorf("%w", auth.ErrInvalidToken)
	errForbidden = errors.New("forbidden")
)

type ctxKey struct{}

// Principal ist das Ergebnis der Prüfung.
type Principal struct {
	Subject  string
	ClientID string
	Key      string // PostKauz-Schlüssel; leer = unbekannt → 403
}

// New baut den HTTP-Handler: POST /mcp, PRM unter /.well-known, /healthz.
func New(ctx context.Context, cfg Config) (http.Handler, error) {
	if cfg.HTTPClient != nil {
		ctx = oidc.ClientContext(ctx, cfg.HTTPClient)
	}
	jwks := cfg.JWKSURL
	if jwks == "" {
		p, err := oidc.NewProvider(ctx, cfg.Issuer)
		if err != nil {
			return nil, fmt.Errorf("discovery: %w", err)
		}
		var m struct {
			JWKS string `json:"jwks_uri"`
		}
		if err := p.Claims(&m); err != nil {
			return nil, err
		}
		jwks = m.JWKS
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	skew := cfg.ClockSkew
	keySet := oidc.NewRemoteKeySet(ctx, jwks)
	v := oidc.NewVerifier(cfg.Issuer, keySet, &oidc.Config{
		SkipClientIDCheck:    true, // aud prüfen wir selbst gegen die Resource-URL
		SupportedSigningAlgs: []string{oidc.RS256, oidc.ES256, oidc.EdDSA, oidc.PS256},
		Now:                  func() time.Time { return now().Add(-skew) },
	})

	verifier := func(ctx context.Context, raw string, _ *http.Request) (*auth.TokenInfo, error) {
		if cfg.RequireATJWT && jwtType(raw) != "at+jwt" {
			return nil, errInvalid
		}
		tok, err := v.Verify(ctx, raw)
		if err != nil {
			return nil, errInvalid
		}
		if !oauthex.MatchesResource(tok.Audience, cfg.Resource) {
			return nil, errInvalid // Token für ein anderes Ziel (Akzeptanzkriterium 5)
		}
		var c struct {
			ClientID string `json:"client_id"`
			Azp      string `json:"azp"`
			Scope    string `json:"scope"`
			Scp      []string
		}
		_ = tok.Claims(&c)
		cid := c.ClientID
		if cid == "" {
			cid = c.Azp
		}
		p := Principal{Subject: tok.Subject, ClientID: cid, Key: cfg.Principals[tok.Subject]}
		return &auth.TokenInfo{
			Scopes:     strings.Fields(c.Scope),
			Expiration: tok.Expiry.Add(skew),
			UserID:     tok.Subject,
			Extra:      map[string]any{"principal": p},
		}, nil
	}

	prmURL := strings.TrimSuffix(origin(cfg.Resource), "/") + "/.well-known/oauth-protected-resource" + pathOf(cfg.Resource)
	bearer := auth.RequireBearerToken(verifier, &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: prmURL,
		Scopes:              cfg.Scopes,
	})

	srv := newMCPServer()
	mcpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, DisableLocalhostProtection: !cfg.SDKLocalhostProtection})
	hosts := cfg.AllowedHosts
	if len(hosts) == 0 {
		hosts = []string{strings.TrimPrefix(strings.TrimPrefix(origin(cfg.Resource), "https://"), "http://")}
	}

	mux := http.NewServeMux()
	mux.Handle("/mcp", hostGate(hosts, bearer(principalGate(mcpHandler))))
	prm := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.Resource,
		AuthorizationServers:   []string{cfg.Issuer},
		ScopesSupported:        cfg.Scopes,
		BearerMethodsSupported: []string{"header"},
	})
	mux.Handle("GET /.well-known/oauth-protected-resource"+pathOf(cfg.Resource), prm)
	mux.Handle("GET /.well-known/oauth-protected-resource", prm)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux, nil
}

// hostGate prüft den Host-Kopf gegen eine feste Liste (statt des SDK-Schutzes).
func hostGate(hosts []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, h := range hosts {
			if strings.EqualFold(r.Host, h) {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
	})
}

// principalGate: gültiges Token, aber kein Schlüssel zugeordnet → 403 ohne Hinweis.
func principalGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ti := auth.TokenInfoFromContext(r.Context())
		p, _ := ti.Extra["principal"].(Principal)
		if p.Key == "" {
			http.Error(w, errForbidden.Error(), http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

type whoamiOut struct {
	Key       string `json:"key"`
	Subject   string `json:"subject"`
	ClientID  string `json:"client_id"`
	Transport string `json:"transport"`
}

func newMCPServer() *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "postkauz-pauth-spike", Version: "0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "whoami", Description: "Shows the PostKauz key the token maps to."},
		func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, whoamiOut, error) {
			var p Principal
			if req.Extra != nil && req.Extra.TokenInfo != nil {
				p, _ = req.Extra.TokenInfo.Extra["principal"].(Principal)
			}
			return nil, whoamiOut{Key: p.Key, Subject: p.Subject, ClientID: p.ClientID, Transport: "http"}, nil
		})
	return s
}

// jwtType liest den JOSE-Kopf ohne Prüfung (nur für die typ-Regel).
func jwtType(raw string) string {
	h, _, ok := strings.Cut(raw, ".")
	if !ok {
		return ""
	}
	b, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return ""
	}
	var hdr struct {
		Typ string `json:"typ"`
	}
	_ = json.Unmarshal(b, &hdr)
	return strings.ToLower(hdr.Typ)
}

func origin(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return u
	}
	j := strings.Index(u[i+3:], "/")
	if j < 0 {
		return u
	}
	return u[:i+3+j]
}

func pathOf(u string) string {
	return strings.TrimPrefix(u, origin(u))
}
