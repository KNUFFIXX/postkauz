package httpmcp

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"strings"
	"time"
)

// maxClaimsBytes: größter angenommener Inhalt eines Tokens (Base64).
const maxClaimsBytes = 16 << 10

// jwtHeader: die Felder des JOSE-Kopfs, die PostKauz selbst liest.
type jwtHeader struct {
	Typ string `json:"typ"`
	Kid string `json:"kid"`
}

// parseHeader liest den JOSE-Kopf ohne Prüfung (kein Krypto-Code; die
// Signatur prüft go-oidc).
func parseHeader(raw string) (jwtHeader, bool) {
	h, _, ok := strings.Cut(raw, ".")
	if !ok || len(h) > 4096 {
		return jwtHeader{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(h)
	if err != nil {
		return jwtHeader{}, false
	}
	var hdr jwtHeader
	if json.Unmarshal(b, &hdr) != nil {
		return jwtHeader{}, false
	}
	return hdr, true
}

// precheckClaims prüft iss und exp aus dem noch ungeprüften Inhalt, bevor
// go-oidc die Signatur prüft. Grund (Nachbesserung T5, Punkt 9 c): go-oidc
// prüft die Signatur vor iss und exp und lädt bei jedem Fehlschlag die JWKS
// neu; ein Token mit fremdem Aussteller oder abgelaufen soll weder eine
// Discovery noch einen Abruf beim AS auslösen. Die Vorprüfung lehnt nur
// ab; was durchgeht, prüft go-oidc danach vollständig (über dieselben
// Bytes: kompakte Form, der Inhalt ist der signierte Teil).
func precheckClaims(raw, issuer string, now time.Time) bool {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(parts[1]) > maxClaimsBytes {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var c struct {
		Iss string   `json:"iss"`
		Exp *float64 `json:"exp"`
	}
	if json.Unmarshal(b, &c) != nil {
		return false
	}
	if c.Iss != issuer || c.Exp == nil {
		return false
	}
	return !numericDate(*c.Exp).Before(now.Add(-ClockSkew))
}

// numericDate: Sekunden seit 1970 (RFC 7519, auch mit Nachkommastellen).
// Werte jenseits von ±10^12 s (rund ±31.700 Jahre) werden darauf begrenzt.
func numericDate(v float64) time.Time {
	v = math.Max(math.Min(v, 1e12), -1e12)
	s, f := math.Modf(v)
	return time.Unix(int64(s), int64(f*1e9))
}
