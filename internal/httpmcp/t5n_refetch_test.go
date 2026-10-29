package httpmcp

import (
	"net/http"
	"testing"
	"time"
)

// fakeClock stellt die Uhr von Server und Prüfer auf einen festen Wert;
// advance schiebt sie weiter.
func (o *oauthEnv) fakeClock() (advance func(time.Duration)) {
	now := time.Now()
	o.srv.d.Now = func() time.Time { return now }
	o.srv.auth.(*oauthAuth).now = o.srv.d.Now
	return func(d time.Duration) { now = now.Add(d) }
}

// Nachbesserung T5, Punkt 9 (c): Token mit fremdem iss oder abgelaufen
// lösen keinen JWKS-Abruf aus (Vorprüfung vor der Signatur).
func TestT5NJWKSRefetchPerBadToken(t *testing.T) {
	o := newOAuthEnv(t)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("valid: %d %s", code, body)
	}
	base := o.as.jwksHits.Load()
	other := genKey(t)
	for i := 0; i < 5; i++ {
		c := o.claims("user-reader", func(c map[string]any) {
			c["iss"] = "https://evil.example.org"
			c["exp"] = time.Now().Add(-time.Hour).Unix()
		})
		if code, _, _ := o.post(t, o.as.sign(t, other, "at+jwt", c)); code != http.StatusUnauthorized {
			t.Errorf("bad token %d: %d, want 401", i, code)
		}
	}
	if n := o.as.jwksHits.Load() - base; n > 1 {
		t.Errorf("5 bad tokens (foreign iss, expired, wrong signature) caused %d JWKS fetches, want at most 1", n)
	}
}

// Auch Token mit passendem iss und exp, aber falscher Signatur laden die
// JWKS höchstens einmal je 10 s; die bekannte kid bleibt 401.
func TestT5NJWKSRefetchThrottled(t *testing.T) {
	o := newOAuthEnv(t)
	advance := o.fakeClock()
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("valid: %d %s", code, body)
	}
	advance(jwksRefetch) // der erste Abruf liegt 10 s zurück
	base := o.as.jwksHits.Load()
	other := genKey(t)
	for i := 0; i < 5; i++ {
		if code, _, _ := o.post(t, o.as.sign(t, other, "at+jwt", o.claims("user-reader", nil))); code != http.StatusUnauthorized {
			t.Errorf("wrong signature %d: %d, want 401", i, code)
		}
	}
	if n := o.as.jwksHits.Load() - base; n > 1 {
		t.Errorf("5 tokens with a wrong signature caused %d JWKS fetches, want at most 1", n)
	}
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Errorf("valid token after the bad ones: %d %s", code, body)
	}
}

// Schlüsselwechsel: Eine neue kid gilt spätestens 10 s nach dem letzten
// Abruf; bis dahin 503 mit Retry-After (nicht 401, damit der Client nicht
// neu anmeldet).
func TestT5NJWKSRotationWithin10s(t *testing.T) {
	o := newOAuthEnv(t)
	advance := o.fakeClock()
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("before rotation: %d %s", code, body)
	}
	o.as.rotate(t)
	code, hdr, _ := o.post(t, o.token(t, "user-reader", nil))
	if code != http.StatusServiceUnavailable || hdr.Get("Retry-After") == "" {
		t.Errorf("new kid right after a fetch: %d, want 503 with Retry-After", code)
	}
	advance(jwksRefetch)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Errorf("new kid 10 s later: %d %s, want 200", code, body)
	}
}

func TestT5NPrecheckClaims(t *testing.T) {
	o := newOAuthEnv(t)
	now := time.Now()
	iss := o.as.srv.URL
	for name, c := range map[string]struct {
		mod func(map[string]any)
		ok  bool
	}{
		"valid":               {nil, true},
		"expired within skew": {func(c map[string]any) { c["exp"] = now.Add(-20 * time.Second).Unix() }, true},
		"exp as fraction":     {func(c map[string]any) { c["exp"] = float64(now.Add(time.Minute).Unix()) + 0.5 }, true},
		"exp far future":      {func(c map[string]any) { c["exp"] = 1e300 }, true},
		"expired":             {func(c map[string]any) { c["exp"] = now.Add(-time.Minute).Unix() }, false},
		"no exp":              {func(c map[string]any) { delete(c, "exp") }, false},
		"exp as text":         {func(c map[string]any) { c["exp"] = "tomorrow" }, false},
		"foreign iss":         {func(c map[string]any) { c["iss"] = "https://evil.example.org" }, false},
		"iss with slash":      {func(c map[string]any) { c["iss"] = iss + "/" }, false},
		"no iss":              {func(c map[string]any) { delete(c, "iss") }, false},
		"exp far in the past": {func(c map[string]any) { c["exp"] = -1e300 }, false},
	} {
		tok := o.as.sign(t, nil, "at+jwt", o.claims("user-reader", c.mod))
		if got := precheckClaims(tok, iss, now); got != c.ok {
			t.Errorf("%s: %v, want %v", name, got, c.ok)
		}
	}
	for _, raw := range []string{"", "a.b", "a.b.c", "x.e30.y", "a.b.c.d"} {
		if precheckClaims(raw, iss, now) {
			t.Errorf("%q accepted", raw)
		}
	}
}
