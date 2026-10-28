// Package oauthtest stellt einen Fake-Autorisierungsserver (httptest) und
// einen Fake-Browser für Tests der OAuth2-Flows bereit (S3.7). Nur für
// Tests; kein Produktivcode importiert es.
package oauthtest

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// ClientID ist die Client-ID, die der Fake-AS annimmt.
const ClientID = "00000000-0000-4000-8000-00000000c1d1"

// AS ist der Fake-Autorisierungsserver.
type AS struct {
	*httptest.Server
	// Prefix steht vor jedem ausgegebenen Token (Kanarientests).
	Prefix string
	// RefreshLen: Länge der ausgegebenen Refresh-Token (0 = kurz).
	RefreshLen int
	// ExpiresIn: Lebensdauer der Access-Token in Sekunden (0 = 3600).
	ExpiresIn int
	// DevicePolls: Antworten auf die ersten Abfragen im Device-Code-Flow
	// (z. B. authorization_pending, slow_down); danach das Token.
	DevicePolls []string
	// DeviceInterval: interval in der Device-Code-Antwort (Sekunden).
	DeviceInterval int
	// DeviceExpiresIn: expires_in der Device-Code-Antwort (0 = 900,
	// negativ = weglassen).
	DeviceExpiresIn int
	// TokenDelay: /token antwortet erst nach dieser Zeit (oder bricht mit
	// der Anfrage ab).
	TokenDelay time.Duration
	// FailRefresh: Fehlercode für jeden Refresh ("" = Erfolg).
	FailRefresh string
	// Deny: /authorize leitet mit error=access_denied zurück.
	Deny bool
	// KeepUsedRefresh: Ein eingelöstes Refresh-Token bleibt gültig, wie bei
	// Microsoft (das alte wird beim Einlösen nicht widerrufen). Standard:
	// strenger, jedes Refresh-Token gilt nur einmal.
	KeepUsedRefresh bool

	mu       sync.Mutex
	codes    map[string]authReq
	refresh  map[string]bool
	n        int
	Requests map[string]int // Pfad oder grant_type → Anzahl
	Seen     []url.Values   // alle Formulare an /token (Prüfung: kein client_secret)
	Access   []string       // ausgegebene Access-Token
	devPolls int
}

type authReq struct{ challenge, redirect string }

// New startet den Fake-AS.
func New(t testing.TB) *AS {
	t.Helper()
	as := &AS{Prefix: "CANARY-tok", codes: map[string]authReq{}, refresh: map[string]bool{}, Requests: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/authorize", as.authorize)
	mux.HandleFunc("/token", as.token)
	mux.HandleFunc("/devicecode", as.devicecode)
	as.Server = httptest.NewServer(mux)
	t.Cleanup(as.Server.Close)
	return as
}

// Endpoint liefert die Endpunkte (für oauth.Microsoft.Endpoint).
func (as *AS) Endpoint(string) oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: as.URL + "/authorize", TokenURL: as.URL + "/token", DeviceAuthURL: as.URL + "/devicecode"}
}

// Count liefert die Zahl der Anfragen einer Art.
func (as *AS) Count(kind string) int {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.Requests[kind]
}

// LastAccess liefert das zuletzt ausgegebene Access-Token ("" = keins).
func (as *AS) LastAccess() string {
	as.mu.Lock()
	defer as.mu.Unlock()
	if len(as.Access) == 0 {
		return ""
	}
	return as.Access[len(as.Access)-1]
}

// Forms liefert eine Kopie der Formulare an /token.
func (as *AS) Forms() []url.Values {
	as.mu.Lock()
	defer as.mu.Unlock()
	return append([]url.Values(nil), as.Seen...)
}

// Browser ist ein Fake-Browser: Er ruft die Anmeldeadresse auf, folgt der
// Umleitung an den Loopback-Server und meldet den Fehler über errs.
func Browser(t testing.TB) func(string) error {
	return func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err != nil {
				t.Logf("fake browser: %v", err)
				return
			}
			_ = resp.Body.Close()
		}()
		return nil
	}
}

func (as *AS) next(kind string) string {
	as.n++
	return fmt.Sprintf("%s-%s-%d", as.Prefix, kind, as.n)
}

func (as *AS) refreshToken() string {
	rt := as.next("refresh")
	if as.RefreshLen > len(rt) {
		rt += strings.Repeat("x", as.RefreshLen-len(rt))
	}
	as.refresh[rt] = true
	return rt
}

func (as *AS) authorize(w http.ResponseWriter, r *http.Request) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.Requests["/authorize"]++
	q := r.URL.Query()
	redirect := q.Get("redirect_uri")
	if q.Get("client_id") != ClientID || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" ||
		!strings.HasPrefix(redirect, "http://localhost:") || q.Get("response_type") != "code" {
		http.Error(w, "bad authorize request", http.StatusBadRequest)
		return
	}
	target, _ := url.Parse(redirect)
	v := url.Values{"state": {q.Get("state")}}
	if as.Deny {
		v.Set("error", "access_denied")
	} else {
		code := as.next("code")
		as.codes[code] = authReq{challenge: q.Get("code_challenge"), redirect: redirect}
		v.Set("code", code)
	}
	// Der Fake-Browser löst localhost auf 127.0.0.1 auf.
	target.Host = strings.Replace(target.Host, "localhost", "127.0.0.1", 1)
	target.RawQuery = v.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (as *AS) writeToken(w http.ResponseWriter, rt string) {
	at := as.next("access")
	as.Access = append(as.Access, at)
	exp := as.ExpiresIn
	if exp == 0 {
		exp = 3600
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": at, "refresh_token": rt, "token_type": "Bearer", "expires_in": exp,
		"scope": "https://outlook.office.com/IMAP.AccessAsUser.All"})
}

func oauthErr(w http.ResponseWriter, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code,
		"error_description": "AADSTS70000: test error with refresh_token=CANARY-leak-in-description"})
}

func (as *AS) token(w http.ResponseWriter, r *http.Request) {
	if as.TokenDelay > 0 {
		// Erst den Körper lesen: Erst dann merkt der Server, dass der Client
		// aufgibt (r.Context endet).
		_ = r.ParseForm()
		select {
		case <-time.After(as.TokenDelay):
		case <-r.Context().Done():
			return
		}
	}
	as.mu.Lock()
	defer as.mu.Unlock()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := r.PostForm
	as.Seen = append(as.Seen, f)
	grant := f.Get("grant_type")
	as.Requests[grant]++
	if f.Get("client_id") != ClientID || f.Get("client_secret") != "" || r.Header.Get("Authorization") != "" {
		oauthErr(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	switch grant {
	case "authorization_code":
		req, ok := as.codes[f.Get("code")]
		delete(as.codes, f.Get("code"))
		sum := sha256.Sum256([]byte(f.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != req.challenge || f.Get("redirect_uri") != req.redirect {
			oauthErr(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		as.writeToken(w, as.refreshToken())
	case "refresh_token":
		if as.FailRefresh != "" {
			oauthErr(w, as.FailRefresh, http.StatusBadRequest)
			return
		}
		rt := f.Get("refresh_token")
		if !as.refresh[rt] {
			oauthErr(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		// Microsoft ersetzt das Refresh-Token bei jedem Refresh.
		if !as.KeepUsedRefresh {
			delete(as.refresh, rt)
		}
		as.writeToken(w, as.refreshToken())
	case "urn:ietf:params:oauth:grant-type:device_code":
		if f.Get("device_code") != "CANARY-device-code" {
			oauthErr(w, "invalid_grant", http.StatusBadRequest)
			return
		}
		if as.devPolls < len(as.DevicePolls) {
			as.devPolls++
			oauthErr(w, as.DevicePolls[as.devPolls-1], http.StatusBadRequest)
			return
		}
		as.writeToken(w, as.refreshToken())
	default:
		oauthErr(w, "unsupported_grant_type", http.StatusBadRequest)
	}
}

func (as *AS) devicecode(w http.ResponseWriter, r *http.Request) {
	as.mu.Lock()
	defer as.mu.Unlock()
	as.Requests["/devicecode"]++
	_ = r.ParseForm()
	if r.PostForm.Get("client_id") != ClientID {
		oauthErr(w, "invalid_client", http.StatusUnauthorized)
		return
	}
	iv := as.DeviceInterval
	if iv == 0 {
		iv = 1
	}
	resp := map[string]any{"device_code": "CANARY-device-code", "user_code": "ABCD-EFGH",
		"verification_uri": "https://microsoft.com/devicelogin", "expires_in": 900, "interval": iv,
		"message": "To sign in, use a web browser to open the page and enter the code."}
	if as.DeviceExpiresIn > 0 {
		resp["expires_in"] = as.DeviceExpiresIn
	} else if as.DeviceExpiresIn < 0 {
		delete(resp, "expires_in")
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
