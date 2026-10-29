package httpmcp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/config"
)

// Nachbesserung T5, Punkt 9 (i): Das Log nennt nur die bekannten Pfade
// wörtlich, alles andere als "other" (früher jeder Pfad, der mit
// /.well-known/oauth-protected-resource begann).
func TestT5NLogPath(t *testing.T) {
	e := newEnv(t)
	ts, s := e.start(t, func(h *config.HTTP) {
		h.Resource = "https://node.example.net/mcp"
		h.AllowedHosts = []string{"node.example.net"}
	}, nil)
	for p, want := range map[string]string{
		PathMCP:                       PathMCP,
		PathHealth:                    PathHealth,
		prmPath:                       prmPath,
		prmPath + "/mcp":              prmPath + "/mcp",
		prmPath + "/mcp/canary-x9q":   "other",
		prmPath + "canary-x9q":        "other",
		"/mcp/canary-x9q":             "other",
		"/healthz/canary-x9q":         "other",
		"/canary-x9q":                 "other",
		"/.well-known/canary-x9q":     "other",
		"/":                           "other",
		strings.ToUpper(PathMCP):      "other",
		prmPath + "/%63anary-x9q/mcp": "other",
	} {
		if got := s.logPath(p); got != want {
			t.Errorf("logPath(%q) = %q, want %q", p, got, want)
		}
	}
	for _, p := range []string{prmPath + "/mcp/canary-x9q", prmPath + "canary-x9q", "/canary-x9q"} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+p, nil)
		req.Host = "node.example.net"
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}
	if l := e.log.String(); strings.Contains(l, "canary-x9q") || !strings.Contains(l, `"path":"other"`) {
		t.Errorf("request path in the log:\n%s", l)
	}
}
