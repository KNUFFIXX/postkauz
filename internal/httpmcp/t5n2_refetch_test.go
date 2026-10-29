package httpmcp

import (
	"net/http"
	"testing"
	"time"
)

// Zweite Nachbesserung T5, Punkt 5 (b): TestT5NJWKSRefetchPerBadToken blieb
// auch ohne Vorprüfung grün, weil die 10-s-Sperre das Fehlen verdeckte.
// Hier läuft die Uhr vor jedem Token 11 s weiter: Ein Token mit fremdem
// iss darf trotzdem keinen JWKS-Abruf auslösen.
func TestT5N2RefetchWithClock(t *testing.T) {
	o := newOAuthEnv(t)
	advance := o.fakeClock()
	if code, _, body := o.post(t, o.token(t, "user-reader", nil)); code != http.StatusOK {
		t.Fatalf("valid: %d %s", code, body)
	}
	base := o.as.jwksHits.Load()
	other := genKey(t)
	for i := 0; i < 5; i++ {
		advance(jwksRefetch + time.Second)
		c := o.claims("user-reader", func(c map[string]any) { c["iss"] = "https://evil.example.org" })
		if code, _, _ := o.post(t, o.as.sign(t, other, "at+jwt", c)); code != http.StatusUnauthorized {
			t.Errorf("foreign iss %d: %d, want 401", i, code)
		}
	}
	if n := o.as.jwksHits.Load() - base; n != 0 {
		t.Errorf("5 tokens with a foreign iss, 11 s apart, caused %d JWKS fetches, want 0", n)
	}
}
