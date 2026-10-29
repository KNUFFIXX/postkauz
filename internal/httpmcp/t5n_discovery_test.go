package httpmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// junkToken: Kopf at+jwt und passende Angaben (iss, exp, aud), aber keine
// gültige Signatur; reicht, um die erste Discovery anzustoßen.
func (o *oauthEnv) junkToken(t *testing.T) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "RS256", "typ": "at+jwt", "kid": "k1"}) + "." + enc(o.claims("user-reader", nil)) + ".AAAA"
}

// Nachbesserung T5, Punkt 9 (b): Ein Client bricht seine Anfrage während
// der ersten Discovery ab (hier: Kontext schon beendet). Ein berechtigter
// Client bekommt danach trotzdem 200 (kein failedAt durch den Abbruch).
func TestT5NDiscoveryCancelledByClient(t *testing.T) {
	o := newOAuthEnv(t)
	a := o.srv.auth.(*oauthAuth)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.authenticate(ctx, o.junkToken(t), nil); err == nil {
		t.Fatal("cancelled request authenticated")
	}
	code, _, body := o.post(t, o.token(t, "user-reader", nil))
	if code != http.StatusOK {
		t.Errorf("legitimate token after a cancelled discovery: %d %s, want 200", code, strings.TrimSpace(body))
	}
}

// Ende zu Ende: POST mit leerem Inhalt, der Client trennt nach 200 ms,
// während die erste Discovery noch läuft (AS antwortet nach 1 s).
func TestT5NDiscoveryCancelledEndToEnd(t *testing.T) {
	o := newOAuthEnv(t)
	o.as.metaDelay.Store(int64(time.Second))
	req, _ := http.NewRequest(http.MethodPost, o.ts.URL+PathMCP, http.NoBody)
	req.Host = "node.example.net"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.junkToken(t))
	if res, err := (&http.Client{Timeout: 200 * time.Millisecond}).Do(req); err == nil {
		_ = res.Body.Close()
		t.Errorf("aborted client got %d", res.StatusCode)
	}
	time.Sleep(1500 * time.Millisecond)
	o.as.metaDelay.Store(0)
	code, _, body := o.post(t, o.token(t, "user-reader", nil))
	if code != http.StatusOK {
		t.Errorf("legitimate token after an aborted anonymous request: %d %s, want 200", code, strings.TrimSpace(body))
	}
}

// Wer auf eine laufende Discovery wartet, wartet nur bis zum Ende seines
// eigenen Kontexts (früher: Sperre über die ganze Discovery).
func TestT5NDiscoveryWaiterKeepsItsDeadline(t *testing.T) {
	o := newOAuthEnv(t)
	o.as.metaDelay.Store(int64(2 * time.Second))
	a := o.srv.auth.(*oauthAuth)
	first := make(chan error, 1)
	go func() {
		_, err := a.getVerifier(context.Background())
		first <- err
	}()
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := a.getVerifier(ctx); err == nil {
		t.Error("waiter got a verifier before the discovery ended")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("waiter blocked %v despite its 200 ms deadline", d)
	}
	if err := <-first; err != nil {
		t.Errorf("first caller: %v", err)
	}
}
