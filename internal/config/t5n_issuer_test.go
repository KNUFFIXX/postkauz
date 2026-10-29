package config

import (
	"strings"
	"testing"
)

// Nachbesserung T5, Punkt 1: Der Aussteller braucht einen DNS-Namen; eine
// IP-Adresse geht nur auf Loopback (tailscale serve stellt Zertifikate nur
// für MagicDNS-Namen aus, die Discovery des SDK lehnt private Adressen in
// der URL ab). Der Fehler steht schon beim Laden fest (doctor zeigt ihn).
func TestT5NIssuerNeedsDNSName(t *testing.T) {
	for _, c := range []struct {
		issuer string
		ok     bool
	}{
		{"https://auth.example.net:8443", true},
		{"https://127.0.0.1:9091", true},
		{"http://[::1]:9091", true},
		{"https://100.101.102.103:8443", false},
		{"https://[fd7a:115c:a1e0::1]:8443", false},
		{"https://192.0.2.10", false},
	} {
		h := HTTP{Listen: "127.0.0.1:8787", Auth: HTTPAuthOAuth, Resource: "https://node.example.net/mcp",
			Issuer: c.issuer, Principals: map[string]string{"s": "reader"}}
		err := CheckHTTP(&h)
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v, want ok=%v", c.issuer, err, c.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "DNS name") {
			t.Errorf("%s: unexpected error %v", c.issuer, err)
		}
	}
}
