package httpmcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Nachbesserung T5, Punkt 9 (a), ersetzt Festlegung 5: Anfragen ohne oder
// mit falschem Schlüssel sperren eine angemeldete Sitzung nicht aus;
// /healthz liegt außerhalb jeder Grenze.
func TestT5NRateNoLockoutByUnauthenticated(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	codes := map[int]int{}
	for i := 0; i < 130; i++ {
		res := post(t, ts.URL+PathMCP, initBody, nil) // ohne Bearer
		codes[res.StatusCode]++
		_ = res.Body.Close()
	}
	if codes[http.StatusTooManyRequests] == 0 || codes[http.StatusUnauthorized] > failPerMinute+2 {
		t.Errorf("130 anonymous requests: %v, want at most %d times 401, then 429", codes, failPerMinute)
	}
	res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"]))
	if res.StatusCode != http.StatusOK {
		t.Errorf("legitimate reader after 130 anonymous requests: %d, want 200", res.StatusCode)
	}
	for i := 0; i < 200; i++ {
		h, err := http.Get(ts.URL + PathHealth)
		if err != nil {
			t.Fatal(err)
		}
		_ = h.Body.Close()
		if h.StatusCode != http.StatusNoContent {
			t.Fatalf("/healthz %d after the flood: %d, want 204", i, h.StatusCode)
		}
	}
}

// Falsche Schlüssel schreiben höchstens so viele Audit-Zeilen, wie der
// Vorrat für Fehlschläge hergibt; danach 429 ohne Audit, der gültige
// Schlüssel kommt weiter durch.
func TestT5NRateWrongKeyAuditRows(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	codes := map[int]int{}
	for i := 0; i < 130; i++ {
		res := post(t, ts.URL+PathMCP, initBody, bearer(ops.KeyPrefix+"wrong-0123456789abcdef"))
		codes[res.StatusCode]++
		_ = res.Body.Close()
	}
	n := 0
	for _, r := range e.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "key_invalid" {
			n++
		}
	}
	if n == 0 || n > failPerMinute+2 || codes[http.StatusTooManyRequests] == 0 {
		t.Errorf("130 wrong keys: status codes %v, %d key_invalid audit rows, want 1 to %d rows and 429", codes, n, failPerMinute)
	}
	if res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"])); res.StatusCode != http.StatusOK {
		t.Errorf("legitimate reader after 130 wrong keys: %d, want 200", res.StatusCode)
	}
}

// Metadaten, unbekannte Pfade, 405/415 und fremde Hosts zählen zum selben
// Vorrat; der gültige Schlüssel bleibt frei.
func TestT5NRateOtherUnauthenticated(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	limited := 0
	for i := 0; i < 2*failPerMinute; i++ {
		var res *http.Response
		switch i % 4 {
		case 0:
			r, err := http.Get(ts.URL + prmPath)
			if err != nil {
				t.Fatal(err)
			}
			res = r
		case 1:
			r, err := http.Get(ts.URL + "/nothing")
			if err != nil {
				t.Fatal(err)
			}
			res = r
		case 2:
			res = post(t, ts.URL+PathMCP, initBody, map[string]string{"Content-Type": "text/plain"})
		case 3:
			req, _ := http.NewRequest(http.MethodPost, ts.URL+PathMCP, strings.NewReader(initBody))
			req.Host = "evil.example.org"
			req.Header.Set("Content-Type", "application/json")
			r, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			res = r
		}
		if res.StatusCode == http.StatusTooManyRequests {
			limited++
			if res.Header.Get("Retry-After") == "" {
				t.Error("429 without Retry-After")
			}
		}
		_ = res.Body.Close()
	}
	if limited == 0 {
		t.Errorf("%d unauthenticated requests without 429", 2*failPerMinute)
	}
	if res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"])); res.StatusCode != http.StatusOK {
		t.Errorf("legitimate reader: %d, want 200", res.StatusCode)
	}
}

// Nach der Anmeldung hat jeder Schlüssel seinen eigenen Vorrat
// ([http] max_requests_per_minute).
func TestT5NRatePerPrincipal(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, func(h *config.HTTP) { h.MaxRequestsPerMinute = 5 }, nil)
	codes := map[int]int{}
	for i := 0; i < 8; i++ {
		res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"]))
		codes[res.StatusCode]++
		if res.StatusCode == http.StatusTooManyRequests && res.Header.Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
	}
	if codes[http.StatusOK] != 5 || codes[http.StatusTooManyRequests] != 3 {
		t.Errorf("reader with 5 per minute, 8 requests: %v, want 5 times 200 and 3 times 429", codes)
	}
	if res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["assistant"])); res.StatusCode != http.StatusOK {
		t.Errorf("other key while the reader is limited: %d, want 200", res.StatusCode)
	}
}

// Modus oauth: unbekannte Subjekte mit gültigem Token schreiben höchstens
// so viele principal_unknown-Zeilen, wie der Vorrat hergibt; ein gültiges
// Token kommt weiter durch.
func TestT5NRateOAuthPrincipalUnknown(t *testing.T) {
	o := newOAuthEnv(t)
	tok := o.token(t, "user-unknown", nil)
	codes := map[int]int{}
	for i := 0; i < failPerMinute+15; i++ {
		code, _, _ := o.post(t, tok)
		codes[code]++
	}
	n := 0
	for _, r := range o.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "principal_unknown" {
			n++
		}
	}
	if n == 0 || n > failPerMinute+2 || codes[http.StatusTooManyRequests] == 0 {
		t.Errorf("unknown subject: status codes %v, %d principal_unknown rows, want 1 to %d rows and 429", codes, n, failPerMinute)
	}
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Errorf("legitimate token after the flood: %d %s", code, body)
	}
}
