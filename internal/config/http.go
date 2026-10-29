package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// HTTP: Abschnitt [http] der Konfiguration (T5, ADR-010): `mcp --http`
// spricht Streamable HTTP zustandslos. Keine Geheimnisse in diesem
// Abschnitt (Schlüssel kommen als Bearer, Token vom externen
// Autorisierungsserver).
type HTTP struct {
	// Listen: Adresse und Port (Standard 127.0.0.1:8787). Andere als
	// Loopback-Adressen nur mit AllowNonLoopback (E9).
	Listen string `toml:"listen"`
	// AllowNonLoopback erlaubt eine Adresse außerhalb von Loopback. Im
	// Modus key zusätzlich nur Tailnet-Adressen (100.64.0.0/10,
	// fd7a:115c:a1e0::/48; ADR-010 Punkt 4).
	AllowNonLoopback bool `toml:"allow_non_loopback"`
	// AllowedHosts: erlaubte Host-Köpfe (ADR-010 Punkt 2; sonst 421).
	// Leer = Host aus Resource (ohne resource zusätzlich localhost:<port>).
	AllowedHosts []string `toml:"allowed_hosts"`
	// Auth: "key" (Bearer = PostKauz-Schlüssel, Standard) oder "oauth"
	// (Bearer = Access-Token eines externen Autorisierungsservers).
	Auth string `toml:"auth"`
	// Resource: eigene Resource-URL (RFC 9728), z. B.
	// https://knoten.tailnet.ts.net/mcp; Pflicht bei oauth. Leer =
	// http://<listen>/mcp.
	Resource string `toml:"resource"`
	// Issuer: Aussteller (iss) des Autorisierungsservers; Pflicht bei oauth.
	Issuer string `toml:"issuer"`
	// Scopes: scopes_supported in den Metadaten (PRM); leer = openid,
	// offline_access. Rechte kommen nie aus dem Token, nur aus dem
	// PostKauz-Schlüssel.
	Scopes []string `toml:"scopes"`
	// Principals ordnet das Subjekt (sub) eines Tokens einem
	// PostKauz-Schlüsselnamen zu ([http.principals], nur oauth).
	Principals map[string]string `toml:"principals"`
	// MaxRequestsPerMinute: Rate-Begrenzung für alle Anfragen
	// zusammen (Standard 120).
	MaxRequestsPerMinute int `toml:"max_requests_per_minute"`

	// resourceDefault: Resource stammt aus Listen (nicht aus der Datei);
	// ein neuer Listen-Wert (mcp --http --listen) bildet sie dann neu.
	resourceDefault bool
}

// Standards und Grenzen für [http].
const (
	DefaultHTTPListen          = "127.0.0.1:8787"
	HTTPAuthKey                = "key"
	HTTPAuthOAuth              = "oauth"
	DefaultMaxRequestsPerMin   = 120
	MaxMaxRequestsPerMin       = 10000
	httpMaxPrincipals          = 100
	httpPathMCP                = "/mcp"
	httpDefaultScopesOpenID    = "openid"
	httpDefaultScopesOffline   = "offline_access"
	httpMaxAllowedHosts        = 20
	httpMaxScopes              = 20
	httpMaxValueLength         = 512
	httpMaxPrincipalNameLength = 63
)

var (
	httpKeyNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	httpScopeRE   = regexp.MustCompile(`^[\x21\x23-\x5b\x5d-\x7e]{1,128}$`)
	httpHostRE    = regexp.MustCompile(`^[A-Za-z0-9.\-\[\]:]{1,255}$`)
)

// CheckHTTP prüft [http] und setzt die Standards (Load ruft es auf und
// merkt einen Fehler in Config.HTTPErr; mcp --http erneut nach --listen). Ob die Adresse erlaubt ist (Loopback,
// Tailnet), entscheidet httpmcp.CheckListen beim Start.
func CheckHTTP(h *HTTP) error {
	h.Listen = strings.TrimSpace(h.Listen)
	if h.Listen == "" {
		h.Listen = DefaultHTTPListen
	}
	host, port, err := net.SplitHostPort(h.Listen)
	if err != nil || port == "" {
		return fmt.Errorf("config: http.listen %q must be host:port, e.g. %s", h.Listen, DefaultHTTPListen)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("config: http.listen %q has an invalid port", h.Listen)
	}
	if host != "localhost" && net.ParseIP(host) == nil {
		return fmt.Errorf("config: http.listen %q: give an IP address or localhost, not a host name", h.Listen)
	}
	switch h.Auth {
	case "":
		h.Auth = HTTPAuthKey
	case HTTPAuthKey, HTTPAuthOAuth:
	default:
		return fmt.Errorf("config: http.auth %q is invalid (expected key or oauth)", h.Auth)
	}
	h.Resource = strings.TrimSpace(h.Resource)
	if h.Resource == "" || h.resourceDefault {
		if h.Auth == HTTPAuthOAuth {
			return fmt.Errorf("config: http.resource is required with http.auth = \"oauth\" (the URL clients use, e.g. https://<node>/mcp)")
		}
		h.Resource = "http://" + h.Listen + httpPathMCP
		h.resourceDefault = true
	}
	if err := checkHTTPURL("http.resource", h.Resource, h.Auth == HTTPAuthOAuth); err != nil {
		return err
	}
	h.Issuer = strings.TrimSpace(h.Issuer)
	if h.Auth == HTTPAuthOAuth {
		if h.Issuer == "" {
			return fmt.Errorf("config: http.issuer is required with http.auth = \"oauth\" (the issuer URL of the authorization server)")
		}
		if err := checkHTTPURL("http.issuer", h.Issuer, true); err != nil {
			return err
		}
		if len(h.Principals) == 0 {
			return fmt.Errorf("config: http.auth = \"oauth\" needs at least one entry in [http.principals] (subject = key name)")
		}
	}
	if len(h.Principals) > httpMaxPrincipals {
		return fmt.Errorf("config: [http.principals] has more than %d entries", httpMaxPrincipals)
	}
	for sub, name := range h.Principals {
		if strings.TrimSpace(sub) == "" || len(sub) > httpMaxValueLength || strings.ContainsAny(sub, "\r\n\x00") {
			return fmt.Errorf("config: [http.principals] has an invalid subject")
		}
		if !httpKeyNameRE.MatchString(name) {
			return fmt.Errorf("config: [http.principals] maps a subject to an invalid key name (lowercase letters, digits and '-', max %d)", httpMaxPrincipalNameLength)
		}
	}
	if len(h.AllowedHosts) > httpMaxAllowedHosts {
		return fmt.Errorf("config: http.allowed_hosts has more than %d entries", httpMaxAllowedHosts)
	}
	for i, v := range h.AllowedHosts {
		v = strings.TrimSpace(v)
		if !httpHostRE.MatchString(v) {
			return fmt.Errorf("config: http.allowed_hosts entry %q is invalid (host or host:port)", h.AllowedHosts[i])
		}
		h.AllowedHosts[i] = v
	}
	if len(h.Scopes) == 0 {
		h.Scopes = []string{httpDefaultScopesOpenID, httpDefaultScopesOffline}
	}
	if len(h.Scopes) > httpMaxScopes {
		return fmt.Errorf("config: http.scopes has more than %d entries", httpMaxScopes)
	}
	for _, s := range h.Scopes {
		if !httpScopeRE.MatchString(s) {
			return fmt.Errorf("config: http.scopes entry %q is invalid", s)
		}
	}
	switch v := h.MaxRequestsPerMinute; {
	case v == 0:
		h.MaxRequestsPerMinute = DefaultMaxRequestsPerMin
	case v < 1 || v > MaxMaxRequestsPerMin:
		return fmt.Errorf("config: http.max_requests_per_minute %d is out of range (1 to %d; 0 = default %d)", v, MaxMaxRequestsPerMin, DefaultMaxRequestsPerMin)
	}
	return nil
}

// checkHTTPURL prüft eine absolute URL ohne Nutzerangaben, Query und
// Fragment; strict verlangt https (außer für Loopback).
func checkHTTPURL(name, raw string, strict bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("config: %s %q must be an absolute http(s) URL", name, raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(raw) > httpMaxValueLength {
		return fmt.Errorf("config: %s %q must not carry user info, a query or a fragment", name, raw)
	}
	if strict && u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("config: %s %q must use https (http only for 127.0.0.1, ::1 or localhost)", name, raw)
	}
	return nil
}

func isLoopbackHost(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}
