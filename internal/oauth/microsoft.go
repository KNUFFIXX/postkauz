package oauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Microsoft-Anmeldung (E11, S3.7): öffentlicher Client des Nutzers („bring
// your own client"), PKCE S256, kein Client-Secret. Standard ist der
// Loopback-Flow (Browser, Umleitung an http://localhost:<freier Port>);
// für Rechner ohne Browser der Device-Code-Flow.

// MicrosoftScopes sind die Berechtigungen für IMAP: Postfachzugriff und ein
// Refresh-Token (offline_access).
var MicrosoftScopes = []string{"https://outlook.office.com/IMAP.AccessAsUser.All", "offline_access"}

// tenantRE: common | consumers | organizations | Mandanten-ID (GUID) oder
// Domäne des Mandanten.
var tenantRE = regexp.MustCompile(`^(common|consumers|organizations|[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}|[a-z0-9]([a-z0-9-]{0,62}\.)+[a-z]{2,63})$`)

// clientIDRE: Client-ID einer App-Registrierung (GUID).
var clientIDRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// DefaultTenant gilt, wenn das Konto keinen Mandanten nennt.
const DefaultTenant = "common"

// Client ist der öffentliche OAuth-Client eines Kontos (nur die Client-ID
// und der Mandant; kein Geheimnis).
type Client struct {
	ClientID string `json:"client_id"`
	Tenant   string `json:"tenant"`
}

// Validate prüft Client-ID und Mandant.
func (c Client) Validate() error {
	if !clientIDRE.MatchString(c.ClientID) {
		return errors.New("invalid client id: give the Application (client) ID of your app registration (a GUID)")
	}
	if !tenantRE.MatchString(c.Tenant) {
		return errors.New("invalid tenant: use common, consumers, organizations, a tenant id (GUID) or the tenant domain")
	}
	return nil
}

// Microsoft baut Konfigurationen und Flows. Der Nullwert spricht mit den
// echten Endpunkten von Microsoft und öffnet den Browser des Systems; Tests
// setzen Endpoint, HTTPClient und OpenBrowser.
type Microsoft struct {
	// Endpoint ersetzt die Endpunkte (Tests: Fake-AS mit httptest).
	Endpoint func(tenant string) oauth2.Endpoint
	// HTTPClient für Token- und Device-Code-Anfragen (nil = Standard mit
	// 60 s Frist).
	HTTPClient *http.Client
	// OpenBrowser öffnet eine Adresse im Browser (nil = OpenBrowser).
	OpenBrowser func(url string) error
	// LoginTimeout begrenzt den Loopback-Flow (0 = 5 Minuten).
	LoginTimeout time.Duration
	// Now ersetzt die Uhr (Tests).
	Now func() time.Time
}

func (m Microsoft) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Config liefert die oauth2-Konfiguration des Clients.
func (m Microsoft) Config(c Client) *oauth2.Config {
	tenant := c.Tenant
	if tenant == "" {
		tenant = DefaultTenant
	}
	ep := microsoft.AzureADEndpoint(tenant)
	if m.Endpoint != nil {
		ep = m.Endpoint(tenant)
	}
	// Öffentlicher Client: client_id im Formular, kein Basic-Auth-Kopf
	// (die automatische Erkennung probierte sonst zuerst den Kopf).
	ep.AuthStyle = oauth2.AuthStyleInParams
	return &oauth2.Config{ClientID: c.ClientID, Endpoint: ep, Scopes: MicrosoftScopes}
}

func (m Microsoft) ctx(ctx context.Context) context.Context {
	hc := m.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return context.WithValue(ctx, oauth2.HTTPClient, hc)
}

// Refresher liefert den Refresher gegen den Token-Endpunkt des Clients.
func (m Microsoft) Refresher(c Client) Refresher { return httpRefresher{m: m, cfg: m.Config(c)} }

type httpRefresher struct {
	m   Microsoft
	cfg *oauth2.Config
}

func (r httpRefresher) Refresh(ctx context.Context, rt secrets.Secret) (Token, error) {
	t, err := r.cfg.TokenSource(r.m.ctx(ctx), &oauth2.Token{RefreshToken: rt.Reveal()}).Token()
	if err != nil {
		return Token{}, endpointErr(err)
	}
	return fromOAuth2(t), nil
}

func fromOAuth2(t *oauth2.Token) Token {
	return Token{AccessToken: secrets.Secret(t.AccessToken), RefreshToken: secrets.Secret(t.RefreshToken), Expiry: t.Expiry}
}

var (
	errCodeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	aadstsRE  = regexp.MustCompile(`AADSTS[0-9]{4,8}`)
)

// endpointErr übersetzt einen Fehler von x/oauth2 in einen RefreshError ohne
// Anbietertext: RetrieveError.Error enthält den Antwortkörper, der Token
// enthalten kann; er gelangt nie weiter.
func endpointErr(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		code := re.ErrorCode
		if !errCodeRE.MatchString(code) {
			code = "http_" + strconv.Itoa(statusOf(re))
		}
		return &RefreshError{Code: code, AADSTS: aadstsRE.FindString(re.ErrorDescription)}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return err
	}
	// Netzfehler: Text ohne Token (Adresse, Ursache), trotzdem geschwärzt.
	return fmt.Errorf("oauth: token endpoint not reachable: %s", secrets.RedactTokens(firstLine(err.Error())))
}

func statusOf(re *oauth2.RetrieveError) int {
	if re.Response != nil {
		return re.Response.StatusCode
	}
	return 0
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// tokenFromFlow prüft ein Ergebnis des Flows.
func tokenFromFlow(t *oauth2.Token) (Token, error) {
	if t.AccessToken == "" {
		return Token{}, &RefreshError{Code: ReasonEmpty}
	}
	if t.RefreshToken == "" {
		return Token{}, errors.New("oauth: the provider returned no refresh token; the app registration must allow offline_access")
	}
	return fromOAuth2(t), nil
}

// ErrLoginTimeout: Im Browser kam binnen der Frist keine Anmeldung zurück.
var ErrLoginTimeout = errors.New("oauth: no sign-in arrived in time")

// ErrLoginDenied: Der Anbieter meldete einen Fehler statt eines Codes
// (z. B. access_denied, weil die Anmeldung abgebrochen wurde).
var ErrLoginDenied = errors.New("oauth: sign-in was not completed")

// Prompt ist eine Mitteilung an den Menschen während der Anmeldung (Adresse
// zum Öffnen, Code zum Eintippen). Sie enthält nie ein Token.
type Prompt struct {
	// URL ist die Adresse, die der Mensch öffnet (Loopback: Anmeldeseite;
	// Device-Code: Seite zur Code-Eingabe).
	URL string
	// UserCode ist der Code zum Eintippen (nur Device-Code).
	UserCode string
	// Expires: Ablauf der Anmeldung.
	Expires time.Time
	// BrowserOpened: Der Browser wurde geöffnet (Loopback).
	BrowserOpened bool
}

// LoginLoopback führt den Loopback-Flow aus: freier Port auf 127.0.0.1 (und
// ::1, falls vorhanden), Umleitung http://localhost:<port>, zufälliger
// state, PKCE S256, Frist LoginTimeout. Den Browser öffnet OpenBrowser;
// show bekommt die Adresse in jedem Fall (falls kein Browser aufgeht).
// PostKauz zeigt keine eigene Anmeldeseite.
func (m Microsoft) LoginLoopback(ctx context.Context, c Client, show func(Prompt)) (Token, error) {
	timeout := m.LoginTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Token{}, fmt.Errorf("oauth: no free local port for the sign-in: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	lns := []net.Listener{ln}
	// Manche Browser lösen localhost zuerst als ::1 auf.
	if ln6, err := net.Listen("tcp", "[::1]:"+strconv.Itoa(port)); err == nil {
		lns = append(lns, ln6)
	}

	cfg := m.Config(c)
	cfg.RedirectURL = "http://localhost:" + strconv.Itoa(port)
	state, err := randomHex(32)
	if err != nil {
		return Token{}, err
	}
	verifier := oauth2.GenerateVerifier()
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.URL.Path != "/" || subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(state)) != 1 {
			http.Error(w, "unknown request", http.StatusBadRequest)
			return
		}
		res := result{code: q.Get("code")}
		if e := q.Get("error"); e != "" || res.code == "" {
			if !errCodeRE.MatchString(e) {
				e = "no_code"
			}
			res = result{err: fmt.Errorf("%w (%s)", ErrLoginDenied, e)}
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if res.err != nil {
			_, _ = fmt.Fprintln(w, "Sign-in was not completed. Return to the terminal.")
		} else {
			_, _ = fmt.Fprintln(w, "Sign-in received. You can close this window and return to the terminal.")
		}
		select {
		case done <- res:
		default:
		}
	})}
	for _, l := range lns {
		go func() { _ = srv.Serve(l) }()
	}
	defer func() { _ = srv.Close() }()

	open := m.OpenBrowser
	if open == nil {
		open = OpenBrowser
	}
	opened := open(authURL) == nil
	if show != nil {
		show(Prompt{URL: authURL, Expires: m.now().Add(timeout), BrowserOpened: opened})
	}

	var res result
	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return Token{}, fmt.Errorf("%w (%s)", ErrLoginTimeout, timeout)
		}
		return Token{}, ctx.Err()
	case res = <-done:
	}
	if res.err != nil {
		return Token{}, res.err
	}
	t, err := cfg.Exchange(m.ctx(ctx), res.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Token{}, endpointErr(err)
	}
	return tokenFromFlow(t)
}

// LoginDeviceCode führt den Device-Code-Flow aus (RFC 8628): Code holen,
// show zeigt Adresse und Code, dann fragt die Bibliothek im Abstand
// interval nach (authorization_pending: weiter; slow_down: 5 s länger).
func (m Microsoft) LoginDeviceCode(ctx context.Context, c Client, show func(Prompt)) (Token, error) {
	cfg := m.Config(c)
	hctx := m.ctx(ctx)
	da, err := cfg.DeviceAuth(hctx)
	if err != nil {
		return Token{}, endpointErr(err)
	}
	if show != nil {
		uri := da.VerificationURIComplete
		if uri == "" {
			uri = da.VerificationURI
		}
		show(Prompt{URL: uri, UserCode: da.UserCode, Expires: da.Expiry})
	}
	t, err := cfg.DeviceAccessToken(hctx, da)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Token{}, ErrLoginTimeout
		}
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "access_denied" || re.ErrorCode == "expired_token") {
			return Token{}, fmt.Errorf("%w (%s)", ErrLoginDenied, re.ErrorCode)
		}
		return Token{}, endpointErr(err)
	}
	return tokenFromFlow(t)
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
