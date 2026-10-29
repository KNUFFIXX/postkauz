package httpmcp

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Zweite Nachbesserung T5, Punkt 1: Gültige Aufrufe ziehen nicht aus dem
// Vorrat für Fehlschläge. Vorher gab res.Cancel() den Platz nicht zurück
// (x/time/rate gibt eine sofort erfüllte Reservierung nie zurück); nach 35
// gültigen Aufrufen bekamen Metadaten, POST ohne Token und falsche
// Schlüssel 429 und es entstanden keine key_invalid-Zeilen mehr.
func TestT5N2ValidCallsDoNotDrainFailurePool(t *testing.T) {
	e := newEnv(t)
	ts, s := e.start(t, nil, nil)
	for i := 0; i < failPerMinute+5; i++ {
		res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"]))
		if res.StatusCode != http.StatusOK {
			t.Fatalf("valid reader call %d: %d, want 200", i, res.StatusCode)
		}
		_ = res.Body.Close()
	}
	if got := s.failures.Tokens(); got < failPerMinute-1 {
		t.Errorf("failure pool after %d valid calls: %.2f tokens, want about %d", failPerMinute+5, got, failPerMinute)
	}
	r, err := http.Get(ts.URL + prmPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Errorf("GET metadata after valid traffic: %d, want 200", r.StatusCode)
	}
	r2 := post(t, ts.URL+PathMCP, initBody, nil)
	_ = r2.Body.Close()
	if r2.StatusCode != http.StatusUnauthorized || !strings.Contains(r2.Header.Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("POST without token after valid traffic: %d (WWW-Authenticate %q), want 401 with resource_metadata",
			r2.StatusCode, r2.Header.Get("WWW-Authenticate"))
	}
	r3 := post(t, ts.URL+PathMCP, initBody, bearer(ops.KeyPrefix+"wrong-0123456789abcdef"))
	_ = r3.Body.Close()
	n := 0
	for _, a := range e.auditAll(t) {
		if a.Operation == ops.OpAuthenticate && a.Reason == "key_invalid" {
			n++
		}
	}
	if r3.StatusCode != http.StatusUnauthorized || n != 1 {
		t.Errorf("wrong key after valid traffic: %d with %d key_invalid rows, want 401 and 1 row", r3.StatusCode, n)
	}
	r4 := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["assistant"]))
	_ = r4.Body.Close()
	if r4.StatusCode != http.StatusOK {
		t.Errorf("valid assistant key: %d, want 200", r4.StatusCode)
	}
}

// Dauerbetrieb: ein gültiger Aufruf je Sekunde (unter dem Standard
// 120/min) lässt Metadaten weiter durch.
func TestT5N2SustainedValidTraffic(t *testing.T) {
	if testing.Short() {
		t.Skip("8 s real time")
	}
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	for i := 0; i < failPerMinute; i++ {
		_ = post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"])).Body.Close()
	}
	codes := map[int]int{}
	for i := 0; i < 8; i++ {
		time.Sleep(time.Second)
		_ = post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"])).Body.Close()
		r, err := http.Get(ts.URL + prmPath)
		if err != nil {
			t.Fatal(err)
		}
		codes[r.StatusCode]++
		_ = r.Body.Close()
	}
	if codes[http.StatusOK] != 8 {
		t.Errorf("metadata during 1 valid call/s for 8 s: %v, want 8 times 200", codes)
	}
}

// Modus oauth: Nach gültigem Verkehr bekommt ein abgelaufenes Token 401
// mit invalid_token (der Client erneuert dann), ein unbekanntes Subjekt
// 403 mit Audit-Zeile principal_unknown.
func TestT5N2OAuthExpiredAfterValidTraffic(t *testing.T) {
	o := newOAuthEnv(t)
	tok := o.token(t, "user-reader", nil)
	for i := 0; i < failPerMinute+5; i++ {
		if code, _, body := o.post(t, tok); code != http.StatusOK {
			t.Fatalf("valid %d: %d %s", i, code, body)
		}
	}
	exp := o.token(t, "user-reader", func(c map[string]any) {
		c["exp"] = time.Now().Add(-time.Hour).Unix()
		c["iat"] = time.Now().Add(-2 * time.Hour).Unix()
	})
	code, hdr, _ := o.post(t, exp)
	if code != http.StatusUnauthorized || !strings.Contains(hdr.Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Errorf("expired token after valid traffic: %d (WWW-Authenticate %q), want 401 invalid_token", code, hdr.Get("WWW-Authenticate"))
	}
	code2, _, _ := o.post(t, o.token(t, "user-unknown", nil))
	n := 0
	for _, r := range o.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "principal_unknown" {
			n++
		}
	}
	if code2 != http.StatusForbidden || n != 1 {
		t.Errorf("unknown subject after valid traffic: %d with %d principal_unknown rows, want 403 and 1 row", code2, n)
	}
}

// Ein 503 vor geglückter Signaturprüfung (unbekannte kid in der
// Neuladesperre) zählt wie ein Fehlschlag: 60 solche Token binnen 10 s
// bekommen ab dem leeren Vorrat 429, nicht unbegrenzt 503.
func TestT5N2UnknownKidThrottled(t *testing.T) {
	o := newOAuthEnv(t)
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("valid: %d %s", code, body)
	}
	hdr := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"at+jwt","kid":"zz-unknown"}`))
	codes := map[int]int{}
	for i := 0; i < 2*failPerMinute; i++ {
		j := o.junkToken(t)
		j = hdr + j[strings.Index(j, "."):]
		code, _, _ := o.post(t, j)
		codes[code]++
	}
	if codes[http.StatusTooManyRequests] == 0 || codes[http.StatusServiceUnavailable] > failPerMinute+2 {
		t.Errorf("%d tokens with an unknown kid: %v, want at most %d times 503, then 429", 2*failPerMinute, codes, failPerMinute)
	}
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Errorf("valid token after the unknown kids: %d %s", code, body)
	}
}
