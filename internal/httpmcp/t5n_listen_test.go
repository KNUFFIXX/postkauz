package httpmcp

import (
	"net"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/config"
)

// Nachbesserung T5, Prüfung F6 (kein Handlungsbedarf): CheckHTTP (Syntax)
// und CheckListen (Politik) für Grenzfälle, mit allow_non_loopback. Hält
// den geprüften Stand fest: localhost erlaubt (Festlegung 8, nur klein
// geschrieben), IPv4-gemappte Adressen zählen wie die IPv4-Adresse, Zonen
// lehnt CheckHTTP ab, 0.0.0.0 und :: nur im Modus oauth.
func TestT5NListenEdgeCases(t *testing.T) {
	type want struct{ syntax, key, oauth bool } // CheckHTTP; CheckListen je Modus
	for l, w := range map[string]want{
		"localhost:8787":           {true, true, true},
		"LOCALHOST:8787":           {false, false, false},
		"[::ffff:127.0.0.1]:8787":  {true, true, true},
		"[::ffff:100.64.1.1]:8787": {true, true, true},
		"[fe80::1%eth0]:8787":      {false, false, true},
		"100.64.0.1:8787":          {true, true, true},
		"100.127.255.255:8787":     {true, true, true},
		"[fd7a:115c:a1e0::5]:8787": {true, true, true},
		"0.0.0.0:8787":             {true, false, true},
		"[::]:8787":                {true, false, true},
		"127.0.0.2:8787":           {true, true, true},
	} {
		for _, auth := range []string{config.HTTPAuthKey, config.HTTPAuthOAuth} {
			h := config.HTTP{Listen: l, Auth: auth, AllowNonLoopback: true}
			if auth == config.HTTPAuthOAuth {
				h.Resource, h.Issuer, h.Principals = "https://n.example.net/mcp", "https://as.example.net", map[string]string{"s": "reader"}
			}
			if ok := config.CheckHTTP(&h) == nil; ok != w.syntax {
				t.Errorf("%s %s: CheckHTTP ok=%v, want %v", l, auth, ok, w.syntax)
			}
			wantListen := w.key
			if auth == config.HTTPAuthOAuth {
				wantListen = w.oauth
			}
			if ok := CheckListen(h) == nil; ok != wantListen {
				t.Errorf("%s %s: CheckListen ok=%v, want %v", l, auth, ok, wantListen)
			}
		}
	}
	// [::ffff:127.0.0.1] lauscht auf 127.0.0.1.
	ln, err := net.Listen("tcp", "[::ffff:127.0.0.1]:0")
	if err != nil {
		t.Skipf("[::ffff:127.0.0.1]: %v", err)
	}
	defer ln.Close()
	if a, ok := ln.Addr().(*net.TCPAddr); !ok || !a.IP.Equal(net.IPv4(127, 0, 0, 1)) {
		t.Errorf("[::ffff:127.0.0.1]:0 listens on %s, want 127.0.0.1", ln.Addr())
	}
}
