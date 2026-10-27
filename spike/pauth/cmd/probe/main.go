// probe misst einen Autorisierungsserver gegen die Prüfliste P-AUTH (A03 S3.0)
// und den Prototyp aus rs. Wegwerfcode; gibt nie ein Token aus.
//
//	. $WORK/env; go run ./cmd/probe -as authelia -issuer https://auth.pauth.test:9091 -tls $WORK/tls
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/KNUFFIXX/postkauz/spike/pauth/rs"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

const (
	redirectURI = "http://127.0.0.1:33418/callback"
	resourceURL = "https://mcp.pauth.test:8443/mcp"
	otherURL    = "https://other.pauth.test/mcp"
)

var (
	asName   = flag.String("as", "authelia", "authelia | dex")
	issuer   = flag.String("issuer", "", "issuer URL")
	tlsDir   = flag.String("tls", "", "directory with mcp.crt/mcp.key")
	user     = flag.String("user", "tester", "test user")
	scopes   = flag.String("scopes", "openid profile offline_access", "scopes for raw flows")
	password = os.Getenv("PAUTH_PASSWORD")
)

type asMeta struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	DeviceEndpoint        string `json:"device_authorization_endpoint"`
}

func main() {
	flag.Parse()
	ctx := context.Background()
	meta := discover()
	browser := newBrowser()

	section("PKCE")
	for _, m := range []string{"none", "plain", "S256"} {
		_, err := rawFlow(browser, meta, "claude-code", m, resourceURL)
		fmt.Printf("authorize with PKCE %-5s → %s\n", m, outcome(err))
	}

	section("Audience / RFC 8707")
	var accessSub string
	for _, res := range []string{resourceURL, ""} {
		tok, err := rawFlow(browser, meta, "claude-code", "S256", res)
		if err != nil {
			fmt.Printf("resource=%q: %v\n", res, err)
			continue
		}
		fmt.Printf("resource=%q → access token: %s\n", res, describe(tok.AccessToken))
		if id, ok := tok.Extra("id_token").(string); ok {
			fmt.Printf("resource=%q → id token:     %s\n", res, describe(id))
		}
		if accessSub == "" {
			accessSub = claim(tok.AccessToken, "sub")
		}
	}
	if *asName == "authelia" {
		tok, err := rawFlowExtra(browser, meta, "claude-code", "S256", "", url.Values{"audience": {resourceURL}})
		if err == nil {
			fmt.Printf("audience=%q (Authelia-Parameter) → access token: %s\n", resourceURL, describe(tok.AccessToken))
		} else {
			fmt.Printf("audience param: %v\n", err)
		}
	}
	if accessSub == "" {
		fmt.Println("no access token subject; stop")
		os.Exit(1)
	}

	section("Resource Server (Prototyp, in-process)")
	rsCfg := rs.Config{Resource: resourceURL, Issuer: *issuer, Principals: map[string]string{accessSub: "cloud-reader"},
		ClockSkew: 30 * time.Second, RequireATJWT: os.Getenv("PAUTH_REQUIRE_ATJWT") == "1"}
	h, err := rs.New(ctx, rsCfg)
	must(err)
	cert, err := tls.LoadX509KeyPair(*tlsDir+"/mcp.crt", *tlsDir+"/mcp.key")
	must(err)
	ln, err := tls.Listen("tcp", "127.0.0.1:8443", &tls.Config{Certificates: []tls.Certificate{cert}})
	must(err)
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()
	fmt.Printf("RS listens on %s (require at+jwt: %v)\n", resourceURL, rsCfg.RequireATJWT)

	// SDK-Client mit OAuth: entdeckt PRM, AS-Metadaten, macht PKCE und resource selbst.
	section("SDK client (AuthorizationCodeHandler, pre-registered client)")
	oh, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		PreregisteredClient: &oauthex.ClientCredentials{ClientID: "claude-code"},
		RedirectURL:         redirectURI,
		RequestRefreshToken: true,
		ScopeFilter: func(d []string) []string {
			fmt.Printf("scopes discovered from PRM/challenge: %v\n", d)
			return strings.Fields(*scopes)
		},
		AcceptUnadvertisedIss: true,
		AuthorizationCodeFetcher: func(ctx context.Context, a *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			q, err := browser.authorize(a.URL)
			if err != nil {
				return nil, err
			}
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
	})
	must(err)
	cl := mcp.NewClient(&mcp.Implementation{Name: "pauth-probe", Version: "0"}, nil)
	cs, err := cl.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: resourceURL, OAuthHandler: oh}, nil)
	if err != nil {
		fmt.Printf("SDK connect: FAIL %v\n", err)
	} else {
		tl, err := cs.ListTools(ctx, nil)
		fmt.Printf("tools/list: %d tools, err=%v, protocol %s\n", lenTools(tl), err, cs.InitializeResult().ProtocolVersion)
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "whoami"})
		if err == nil {
			b, _ := json.Marshal(res.StructuredContent)
			fmt.Printf("whoami: %s\n", b)
		} else {
			fmt.Printf("whoami: %v\n", err)
		}
		ts, _ := oh.TokenSource(ctx)
		if t, err := ts.Token(); err == nil {
			fmt.Printf("SDK token: %s; refresh token present: %v\n", describe(t.AccessToken), t.RefreshToken != "")
			refreshCheck(meta, t)
		}
		cs.Close()
	}

	section("Negative cases against RS")
	if tok, err := rawFlow(browser, meta, "other-client", "S256", otherURL); err == nil {
		fmt.Printf("other-client token (resource=%s): %s\n", otherURL, describe(tok.AccessToken))
		fmt.Printf("  → RS status %d (want 401)\n", rsStatus(tok.AccessToken))
		if id, ok := tok.Extra("id_token").(string); ok {
			fmt.Printf("  other-client id token → RS status %d (want 401)\n", rsStatus(id))
		}
	} else {
		fmt.Printf("other-client flow: %v\n", err)
	}
	if tok, err := rawFlow(browser, meta, "claude-code", "S256", resourceURL); err == nil {
		if id, ok := tok.Extra("id_token").(string); ok {
			fmt.Printf("claude-code id token → RS status %d (want 401)\n", rsStatus(id))
		}
		fmt.Printf("claude-code access token → RS status %d (want 200)\n", rsStatus(tok.AccessToken))
	}

	section("Device authorization (RFC 8628)")
	deviceCheck(meta)

	section("CIMD / URL als client_id (statisch eingetragen, Metadatendokument nicht vorhanden)")
	cimd := "https://client.pauth.test/oauth/client.json"
	tok, err := rawFlow(browser, meta, cimd, "S256", resourceURL)
	fmt.Printf("authorize with client_id=%s → %s\n", cimd, outcome(err))
	if err == nil {
		fmt.Printf("  token: %s\n", describe(tok.AccessToken))
	}
	_, err = rawFlow(browser, meta, "https://unknown.pauth.test/client.json", "S256", resourceURL)
	fmt.Printf("authorize with unregistered URL client_id → %s\n", outcome(err))
	fmt.Printf("consent clicks needed in this run: %d\n", consentClicks)
}

// --- Browser-Ersatz: folgt Umleitungen und meldet sich mit dem Testkonto an ---

type browser struct {
	c        *http.Client
	clientID string
}

func newBrowser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{c: &http.Client{Jar: jar, Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// authorize läuft bis zur Umleitung auf redirectURI und liefert deren Query.
func (b *browser) authorize(start string) (url.Values, error) {
	cur := start
	if su, err := url.Parse(start); err == nil {
		b.clientID = su.Query().Get("client_id")
	}
	for range 20 {
		if os.Getenv("PAUTH_DEBUG") != "" {
			fmt.Fprintf(os.Stderr, "  GET %s %s\n", hostPath(cur), qkeys(cur))
		}
		resp, err := b.c.Get(cur)
		if err != nil {
			return nil, err
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc, _ := resp.Request.URL.Parse(resp.Header.Get("Location"))
			if strings.HasPrefix(loc.String(), redirectURI) {
				q := loc.Query()
				if q.Get("error") != "" {
					return nil, fmt.Errorf("error=%s (%s)", q.Get("error"), q.Get("error_description"))
				}
				return q, nil
			}
			cur = loc.String()
			continue
		}
		u := resp.Request.URL
		switch {
		case resp.StatusCode == 200 && u.Query().Get("flow_id") != "" && strings.Contains(u.Path, "/consent/"): // Authelia-Zustimmung
			next, err := b.autheliaConsent(u)
			if err != nil {
				return nil, err
			}
			cur = next
		case resp.StatusCode == 200 && u.Query().Get("flow_id") != "": // Authelia-Portal
			next, err := b.autheliaLogin(u)
			if err != nil {
				return nil, err
			}
			cur = next
		case resp.StatusCode == 200 && strings.Contains(u.Path, "/login"): // Dex-Formular
			form := url.Values{"login": {*user}, "password": {password}}
			if *asName == "dex" {
				form.Set("login", "tester@example.com")
			}
			r2, err := b.c.PostForm(u.String(), form)
			if err != nil {
				return nil, err
			}
			r2.Body.Close()
			loc, _ := r2.Request.URL.Parse(r2.Header.Get("Location"))
			if r2.StatusCode < 300 || r2.StatusCode >= 400 {
				return nil, fmt.Errorf("dex login: status %d", r2.StatusCode)
			}
			if strings.HasPrefix(loc.String(), redirectURI) {
				return loc.Query(), nil
			}
			cur = loc.String()
		default:
			_ = body
			return nil, fmt.Errorf("stuck at %s%s [%s]: status %d", u.Host, u.Path, qkeysAll(u), resp.StatusCode)
		}
	}
	return nil, errors.New("too many redirects")
}

func (b *browser) autheliaLogin(portal *url.URL) (string, error) {
	q := portal.Query()
	payload := map[string]any{"username": *user, "password": password, "keepMeLoggedIn": false,
		"flow": q.Get("flow"), "flowID": q.Get("flow_id")}
	if q.Get("user_code") != "" {
		payload["userCode"] = q.Get("user_code")
	}
	bs, _ := json.Marshal(payload)
	api := portal.Scheme + "://" + portal.Host + "/api/firstfactor"
	resp, err := b.c.Post(api, "application/json", bytes.NewReader(bs))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Status  string `json:"status"`
		Message string `json:"message"`
		Data    struct {
			Redirect string `json:"redirect"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Status != "OK" || r.Data.Redirect == "" {
		return "", fmt.Errorf("authelia firstfactor: %d %s %s", resp.StatusCode, r.Status, r.Message)
	}
	return r.Data.Redirect, nil
}

// autheliaConsent klickt „Zustimmen“ (Authelia verlangt das bei offline_access).
func (b *browser) autheliaConsent(page *url.URL) (string, error) {
	fid := page.Query().Get("flow_id")
	bs, _ := json.Marshal(map[string]any{"flow_id": fid, "client_id": b.clientID, "consent": true, "pre_configure": false})
	resp, err := b.c.Post(page.Scheme+"://"+page.Host+"/api/oidc/consent", "application/json", bytes.NewReader(bs))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Status string `json:"status"`
		Data   struct {
			RedirectURI string `json:"redirect_uri"`
		} `json:"data"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&r)
	if r.Status != "OK" || r.Data.RedirectURI == "" {
		return "", fmt.Errorf("authelia consent: %d %s", resp.StatusCode, r.Status)
	}
	consentClicks++
	return r.Data.RedirectURI, nil
}

var consentClicks int

// rawFlow: Authorization Code mit wählbarem PKCE-Verfahren und resource.
func rawFlow(b *browser, m asMeta, clientID, pkce, resource string) (*oauth2.Token, error) {
	return rawFlowExtra(b, m, clientID, pkce, resource, nil)
}

func rawFlowExtra(b *browser, m asMeta, clientID, pkce, resource string, extra url.Values) (*oauth2.Token, error) {
	cfg := oauth2.Config{ClientID: clientID, RedirectURL: redirectURI, Scopes: strings.Fields(*scopes),
		Endpoint: oauth2.Endpoint{AuthURL: m.AuthorizationEndpoint, TokenURL: m.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	verifier := oauth2.GenerateVerifier()
	state := hex.EncodeToString([]byte(time.Now().String()))[:32]
	opts := []oauth2.AuthCodeOption{}
	switch pkce {
	case "S256":
		opts = append(opts, oauth2.S256ChallengeOption(verifier))
	case "plain":
		opts = append(opts, oauth2.SetAuthURLParam("code_challenge", verifier), oauth2.SetAuthURLParam("code_challenge_method", "plain"))
	}
	if resource != "" {
		opts = append(opts, oauth2.SetAuthURLParam("resource", resource))
	}
	for k, vs := range extra {
		opts = append(opts, oauth2.SetAuthURLParam(k, vs[0]))
	}
	q, err := b.authorize(cfg.AuthCodeURL(state, opts...))
	if err != nil {
		return nil, err
	}
	if q.Get("state") != state {
		return nil, errors.New("state mismatch")
	}
	ex := []oauth2.AuthCodeOption{}
	if pkce != "none" {
		ex = append(ex, oauth2.VerifierOption(verifier))
	}
	if resource != "" {
		ex = append(ex, oauth2.SetAuthURLParam("resource", resource))
	}
	return cfg.Exchange(context.Background(), q.Get("code"), ex...)
}

func refreshCheck(m asMeta, t *oauth2.Token) {
	if t.RefreshToken == "" {
		fmt.Println("refresh: no refresh token")
		return
	}
	cfg := oauth2.Config{ClientID: "claude-code", Endpoint: oauth2.Endpoint{TokenURL: m.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	old := *t
	old.Expiry = time.Now().Add(-time.Hour)
	rctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 20 * time.Second})
	start := time.Now()
	nt, err := cfg.TokenSource(rctx, &old).Token()
	fmt.Printf("refresh took %s\n", time.Since(start).Round(time.Millisecond))
	if err != nil {
		fmt.Printf("refresh: FAIL %v\n", err)
		return
	}
	fmt.Printf("refresh → new access token: %s; RS status %d\n", describe(nt.AccessToken), rsStatus(nt.AccessToken))
}

func deviceCheck(m asMeta) {
	if m.DeviceEndpoint == "" {
		fmt.Println("no device_authorization_endpoint")
		return
	}
	cfg := oauth2.Config{ClientID: "claude-code", Scopes: strings.Fields(*scopes),
		Endpoint: oauth2.Endpoint{DeviceAuthURL: m.DeviceEndpoint, TokenURL: m.TokenEndpoint, AuthStyle: oauth2.AuthStyleInParams}}
	da, err := cfg.DeviceAuth(context.Background(), oauth2.SetAuthURLParam("resource", resourceURL))
	if err != nil {
		fmt.Printf("device authorization: FAIL %v\n", err)
		return
	}
	fmt.Printf("device authorization: user_code present %v, verification_uri %s, complete uri present %v, interval %d s\n",
		da.UserCode != "", hostPath(da.VerificationURI), da.VerificationURIComplete != "", da.Interval)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(da.Interval+3)*time.Second)
	defer cancel()
	_, err = cfg.DeviceAccessToken(ctx, da)
	fmt.Printf("device token poll without approval → %v\n", oneLine([]byte(fmt.Sprint(err))))
}

// --- Hilfen ---

func discover() asMeta {
	var m asMeta
	for _, p := range []string{"/.well-known/oauth-authorization-server", "/.well-known/openid-configuration"} {
		resp, err := http.Get(strings.TrimSuffix(*issuer, "/") + p)
		if err != nil {
			continue
		}
		if resp.StatusCode == 200 {
			_ = json.NewDecoder(resp.Body).Decode(&m)
			resp.Body.Close()
			fmt.Printf("discovery via %s\n", p)
			return m
		}
		resp.Body.Close()
	}
	must(errors.New("no metadata"))
	return m
}

func rsStatus(token string) int {
	req, _ := http.NewRequest(http.MethodPost, resourceURL, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return -1
	}
	resp.Body.Close()
	return resp.StatusCode
}

// describe zeigt Form und Claims, nie das Token. Undurchsichtige Token werden
// nur mit Länge und SHA-256-Präfix genannt.
func describe(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		s := sha256.Sum256([]byte(tok))
		return fmt.Sprintf("OPAQUE (len %d, sha256 %x…)", len(tok), s[:4])
	}
	hdr, _ := base64.RawURLEncoding.DecodeString(parts[0])
	pl, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var h map[string]any
	var c map[string]any
	_ = json.Unmarshal(hdr, &h)
	_ = json.Unmarshal(pl, &c)
	var life any
	if e, ok := c["exp"].(float64); ok {
		if i, ok := c["iat"].(float64); ok {
			life = e - i
		}
	}
	return fmt.Sprintf("JWT typ=%v alg=%v kid?=%v | iss=%v aud=%v sub=%v client_id=%v azp=%v scope=%v scp=%v lifetime=%vs jti?=%v",
		h["typ"], h["alg"], h["kid"] != nil, c["iss"], c["aud"], c["sub"], c["client_id"], c["azp"], c["scope"], c["scp"], life, c["jti"] != nil)
}

func claim(tok, name string) string {
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		return ""
	}
	pl, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c map[string]any
	_ = json.Unmarshal(pl, &c)
	s, _ := c[name].(string)
	return s
}

func outcome(err error) string {
	if err == nil {
		return "code issued and exchanged (accepted)"
	}
	return "rejected: " + oneLine([]byte(err.Error()))
}

func oneLine(b []byte) string {
	s := strings.Join(strings.Fields(string(b)), " ")
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

func hostPath(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return "?"
	}
	return p.Host + p.Path
}

func lenTools(t *mcp.ListToolsResult) int {
	if t == nil {
		return 0
	}
	return len(t.Tools)
}

func section(s string) { fmt.Printf("\n## %s\n", s) }

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func qkeys(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	var ks []string
	for k, v := range p.Query() {
		if k == "flow" || k == "workflow" || k == "consent_id" || k == "flow_id" {
			ks = append(ks, k+"="+v[0])
		} else {
			ks = append(ks, k)
		}
	}
	return strings.Join(ks, ",")
}

// qkeysAll zeigt error-Parameter mit Wert, sonst nur Namen.
func qkeysAll(u *url.URL) string {
	var ks []string
	for k, v := range u.Query() {
		if strings.HasPrefix(k, "error") {
			ks = append(ks, k+"="+v[0])
		} else {
			ks = append(ks, k)
		}
	}
	return strings.Join(ks, ",")
}
