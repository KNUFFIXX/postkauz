// Package httpmcp ist der MCP-Server über HTTP (`mcp --http`, S3.5 und
// S3.6, ADR-010): Streamable HTTP zustandslos (keine Session-ID, keine
// Cookies, kein HTTP+SSE), Antworten als JSON, nur POST /mcp. Daneben nur
// die Metadaten der geschützten Ressource (RFC 9728) und /healthz ohne
// Inhalt.
//
//   - PostKauz ist nur Resource Server: keine Login-Seite, keine
//     Token-Ausgabe, keine Client-Registrierung. Modus key: Bearer =
//     PostKauz-Schlüssel (nur Loopback und Tailnet); Modus oauth: Bearer =
//     Access-Token eines externen Autorisierungsservers (oauth.go).
//   - Jede Anfrage meldet sich neu an und bekommt einen eigenen MCP-Server
//     für ihren Schlüssel (internal/mcp, dieselben Tools wie über stdio).
//     Jeder Aufruf läuft mit Akteur agent durch ops.Executor.Execute; das
//     Audit nennt den Weg (params.via: transport http, Principal, bei OAuth
//     iss und sub), nie das Token.
//   - Härtung (A03 Teil C): Host-Allowlist statt des SDK-Schutzes gegen
//     DNS-Rebinding (Befund B1), Origin-Prüfung der Standardbibliothek,
//     Anfrage höchstens 1 MiB, Fristen 60 s, Rate-Begrenzung, keine
//     Server-Kennung.
//   - Der Prozess kennt kein Postfachpasswort (ADR-009).
package httpmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/time/rate"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/brand"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/mcp"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

const (
	transportHTTP = "http"
	// MaxRequestBytes: größte Anfrage (A03 Teil C, HTTP-Härtung).
	MaxRequestBytes = 1 << 20
	// Timeout: Lese- und Schreibfrist einer Anfrage.
	Timeout = 60 * time.Second
	// PathMCP: einziger Endpunkt für MCP.
	PathMCP = "/mcp"
	// PathHealth: Lebenszeichen ohne Inhalt (204).
	PathHealth = "/healthz"
	prmPath    = "/.well-known/oauth-protected-resource"
	// shutdownGrace: so lange laufen offene Anfragen nach dem Stopp weiter.
	shutdownGrace = 10 * time.Second
	// authTimeout: Frist einer Anmeldung (Discovery, JWKS, Datenbank);
	// danach 503 mit Retry-After. Nötig, weil mcpGate den Inhalt vor der
	// Anmeldung nicht liest und net/http eine Trennung des Clients dann
	// nicht bemerkt.
	authTimeout = 15 * time.Second
)

// Deps sind die Teile, die der HTTP-Server braucht.
type Deps struct {
	HTTP     config.HTTP
	Executor *ops.Executor
	Keys     ops.KeyRepo
	Audit    audit.Writer
	Version  string
	Log      *slog.Logger
	// MaxChars liefert das Zeichenbudget je Schlüsselname (Policy-Datei);
	// nil = Standard.
	MaxChars func(keyName string) int
	// HTTPClient für Discovery und JWKS des Autorisierungsservers (Tests:
	// Test-CA); nil = eigener Client mit Fristen und ohne Umleitungen
	// (newASClient).
	HTTPClient *http.Client
	// Now nur für Tests; nil = time.Now.
	Now func() time.Time
}

// Server ist der HTTP-Handler mit Konfiguration.
type Server struct {
	d       Deps
	handler http.Handler
	auth    authenticator
	hosts   map[string]bool
	prmURL  string
	limiter *rate.Limiter
}

type ctxKey struct{}

// New baut den Server aus der geprüften Konfiguration ([http],
// config.LoadWith).
func New(d Deps) (*Server, error) {
	if d.Executor == nil || d.Keys == nil {
		return nil, errors.New("httpmcp: executor or key store missing")
	}
	if d.Log == nil {
		d.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	h := d.HTTP
	res, err := url.Parse(h.Resource)
	if err != nil || res.Host == "" {
		return nil, fmt.Errorf("httpmcp: invalid resource %q", h.Resource)
	}
	s := &Server{d: d, hosts: map[string]bool{}}
	hosts := h.AllowedHosts
	if len(hosts) == 0 {
		hosts = []string{res.Host}
		// Ohne eigene Resource-URL (nur Loopback) gilt auch localhost.
		if lh, port, err := net.SplitHostPort(h.Listen); err == nil && res.Host == h.Listen {
			for _, alt := range []string{"localhost", "127.0.0.1", "[::1]"} {
				if alt != lh {
					hosts = append(hosts, alt+":"+port)
				}
			}
		}
	}
	for _, v := range hosts {
		s.hosts[normHost(v)] = true
	}
	resPath := strings.TrimSuffix(res.Path, "/")
	s.prmURL = res.Scheme + "://" + res.Host + prmPath + resPath
	perMin := h.MaxRequestsPerMinute
	if perMin <= 0 {
		perMin = config.DefaultMaxRequestsPerMin
	}
	s.limiter = rate.NewLimiter(rate.Limit(float64(perMin)/60), perMin)

	switch h.Auth {
	case config.HTTPAuthKey:
		s.auth = &keyAuth{repo: d.Keys, audit: d.Audit, now: d.Now}
	case config.HTTPAuthOAuth:
		if d.HTTPClient == nil {
			d.HTTPClient = newASClient()
		}
		s.auth = &oauthAuth{issuer: h.Issuer, resource: h.Resource, principals: h.Principals, repo: d.Keys, audit: d.Audit,
			client: d.HTTPClient, now: d.Now, log: d.Log}
	default:
		return nil, fmt.Errorf("httpmcp: unknown auth mode %q", h.Auth)
	}

	meta := &oauthex.ProtectedResourceMetadata{Resource: h.Resource, BearerMethodsSupported: []string{"header"}, ResourceName: brand.Name}
	if h.Auth == config.HTTPAuthOAuth {
		meta.AuthorizationServers = []string{h.Issuer}
		meta.ScopesSupported = h.Scopes
	}
	prm, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	mcpHandler := sdk.NewStreamableHTTPHandler(s.serverFor, &sdk.StreamableHTTPOptions{
		Stateless: true, JSONResponse: true,
		// Ersetzt durch die Host-Allowlist (Befund B1): Hinter tailscale
		// serve oder Caddy auf Loopback lehnte der SDK-Schutz jede Anfrage
		// mit dem Namen des Knotens ab.
		DisableLocalhostProtection:   true,
		MaxRequestBodyBytes:          MaxRequestBytes,
		PropagateRequestCancellation: true,
	})
	cop := http.NewCrossOriginProtection()

	mux := http.NewServeMux()
	mux.Handle(PathMCP, s.mcpGate(cop.Handler(mcpHandler)))
	prmHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			plain(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write(prm)
	})
	mux.Handle(prmPath+resPath, prmHandler)
	if resPath != "" {
		mux.Handle(prmPath, prmHandler)
	}
	mux.HandleFunc(PathHealth, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET")
			plain(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) { plain(w, http.StatusNotFound, "not found") })
	s.handler = s.outer(mux)
	return s, nil
}

// Handler liefert den vollständigen HTTP-Handler (für httptest).
func (s *Server) Handler() http.Handler { return s.handler }

// PRMURL ist die Adresse der Metadaten (für WWW-Authenticate und Doku).
func (s *Server) PRMURL() string { return s.prmURL }

// outer: für jede Anfrage Rate-Begrenzung, Host-Allowlist (außer
// /healthz), feste Kopfzeilen, Größengrenze und ein Log ohne Token.
func (s *Server) outer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.d.Now()
		rw := &statusWriter{ResponseWriter: w}
		hd := rw.Header()
		hd.Set("Cache-Control", "no-store")
		hd.Set("X-Content-Type-Options", "nosniff")
		// Platz für den Principal, den mcpGate nach der Anmeldung einträgt.
		slot := new(*principal)
		defer func() {
			args := []any{"method", r.Method, "path", logPath(r.URL.Path), "status", rw.code(), "ms", s.d.Now().Sub(start).Milliseconds()}
			if p := *slot; p != nil {
				args = append(args, "key", p.key.ID)
			}
			s.d.Log.Info("http request", args...)
		}()
		if !s.limiter.Allow() {
			hd.Set("Retry-After", "1")
			plain(rw, http.StatusTooManyRequests, "too many requests")
			return
		}
		if r.URL.Path != PathHealth && !s.hosts[normHost(r.Host)] {
			plain(rw, http.StatusMisdirectedRequest, "misdirected request")
			return
		}
		r.Body = http.MaxBytesReader(rw, r.Body, MaxRequestBytes)
		next.ServeHTTP(rw, r.WithContext(context.WithValue(r.Context(), ctxKey{}, slot)))
	})
}

// mcpGate: nur POST mit JSON, dann Anmeldung (Bearer). 401 trägt
// WWW-Authenticate mit resource_metadata (RFC 9728).
func (s *Server) mcpGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			plain(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			plain(w, http.StatusUnsupportedMediaType, "content type must be application/json")
			return
		}
		tok := bearerToken(r.Header.Get("Authorization"))
		if tok == "" {
			s.challenge(w, false)
			return
		}
		actx, cancel := context.WithTimeout(r.Context(), authTimeout)
		p, err := s.auth.authenticate(actx, tok)
		cancel()
		if err != nil {
			var de *deniedError
			switch {
			case errors.Is(err, errUnauthenticated):
				s.challenge(w, true)
			case errors.Is(err, errForbidden):
				plain(w, http.StatusForbidden, "forbidden")
			case errors.As(err, &de):
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				b, _ := json.Marshal(map[string]string{"status": "denied", "code": de.code,
					"message": "the API key behind this access was revoked or has expired; ask the user for a new key"})
				_, _ = w.Write(b)
			default:
				w.Header().Set("Retry-After", "10")
				plain(w, http.StatusServiceUnavailable, "service unavailable")
			}
			s.d.Log.Warn("http mcp: authentication failed", "status", authStatus(err))
			return
		}
		if slot, ok := r.Context().Value(ctxKey{}).(**principal); ok {
			*slot = &p
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, &p)))
	})
}

type principalKey struct{}

// authStatus ist der feste Grund einer gescheiterten Anmeldung fürs Log.
func authStatus(err error) string {
	var de *deniedError
	switch {
	case errors.Is(err, errUnauthenticated):
		return "invalid_token"
	case errors.Is(err, errForbidden):
		return "principal_unknown"
	case errors.As(err, &de):
		return de.code
	}
	return "unavailable"
}

// challenge antwortet 401 mit dem Verweis auf die Metadaten.
func (s *Server) challenge(w http.ResponseWriter, invalid bool) {
	v := "Bearer resource_metadata=" + strconv.Quote(s.prmURL)
	if invalid {
		v += `, error="invalid_token"`
	}
	w.Header().Set("WWW-Authenticate", v)
	plain(w, http.StatusUnauthorized, "unauthorized")
}

// serverFor baut den MCP-Server für den angemeldeten Schlüssel der Anfrage
// (zustandslos: einer je Anfrage).
func (s *Server) serverFor(r *http.Request) *sdk.Server {
	p, _ := r.Context().Value(principalKey{}).(*principal)
	if p == nil {
		return nil
	}
	maxChars := 0
	if s.d.MaxChars != nil {
		maxChars = s.d.MaxChars(p.key.Name)
	}
	o := p.origin
	srv, err := mcp.New(mcp.Options{Executor: s.d.Executor, Key: p.key, Version: s.d.Version, Log: s.d.Log, MaxChars: maxChars, Origin: &o})
	if err != nil {
		s.d.Log.Error("http mcp: building the server failed", "key", p.key.ID, "error_type", fmt.Sprintf("%T", err))
		return nil
	}
	return srv
}

// Serve bedient den Listener bis ctx endet; danach laufen offene
// Anfragen höchstens shutdownGrace weiter.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	hs := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       Timeout,
		WriteTimeout:      Timeout,
		IdleTimeout:       2 * Timeout,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(s.d.Log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return context.WithoutCancel(ctx) },
	}
	errc := make(chan error, 1)
	go func() { errc <- hs.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	err := hs.Shutdown(sctx)
	if e := <-errc; e != nil && !errors.Is(e, http.ErrServerClosed) && err == nil {
		err = e
	}
	return err
}

// ListenAndServe prüft die Adresse (CheckListen), lauscht und bedient bis
// ctx endet. ready (optional) erfährt die tatsächliche Adresse.
func (s *Server) ListenAndServe(ctx context.Context, ready func(addr string)) error {
	if err := CheckListen(s.d.HTTP); err != nil {
		return err
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.d.HTTP.Listen)
	if err != nil {
		return fmt.Errorf("httpmcp: listen on %s: %w", s.d.HTTP.Listen, err)
	}
	if ready != nil {
		ready(ln.Addr().String())
	}
	return s.Serve(ctx, ln)
}

// normHost vergleicht Host-Köpfe ohne Groß- und Kleinschreibung und ohne
// Standardport.
func normHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.TrimSuffix(h, ".")
	for _, p := range []string{":443", ":80"} {
		h = strings.TrimSuffix(h, p)
	}
	return strings.TrimSuffix(h, ".")
}

// logPath nennt nur bekannte Pfade, sonst "other" (kein Freitext aus der
// Anfrage im Log).
func logPath(p string) string {
	switch {
	case p == PathMCP, p == PathHealth, strings.HasPrefix(p, prmPath):
		return p
	}
	return "other"
}

func plain(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = io.WriteString(w, msg+"\n")
}

// statusWriter merkt sich den Status fürs Log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Unwrap lässt http.ResponseController an Flush und Fristen.
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
