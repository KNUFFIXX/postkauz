package httpmcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S3.5 Akzeptanz 1: zustandslos (keine Session-ID), tools/list folgt dem
// Schlüssel wie bei stdio, GET/DELETE → 405.
func TestHTTPStateless(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	for _, role := range testRoles {
		cs, ht := connect(t, ts.URL+PathMCP, nil, bearer(e.keys[role]))
		got := toolNames(t, cs)
		if want := e.stdioToolNames(t, role); !slices.Equal(got, want) {
			t.Errorf("%s: tools over http %v, over stdio %v", role, got, want)
		}
		if v := cs.InitializeResult(); v == nil || v.ServerInfo == nil || v.ServerInfo.Name == "" {
			t.Errorf("%s: no server info", role)
		}
		for _, h := range ht.seen {
			if h.Get("Mcp-Session-Id") != "" {
				t.Errorf("%s: response carries a session id", role)
			}
			if h.Get("Server") != "" || h.Get("Set-Cookie") != "" {
				t.Errorf("%s: response carries Server or Set-Cookie: %v", role, h)
			}
		}
	}
	res := post(t, ts.URL+PathMCP, initBody, bearer(e.keys["reader"]))
	if res.StatusCode != http.StatusOK || res.Header.Get("Mcp-Session-Id") != "" {
		t.Errorf("initialize: %d, session %q", res.StatusCode, res.Header.Get("Mcp-Session-Id"))
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("initialize: content type %q, want JSON (no SSE)", ct)
	}
	for _, m := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(m, ts.URL+PathMCP, nil)
		req.Header.Set("Authorization", "Bearer "+e.keys["reader"])
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = r.Body.Close()
		if r.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s /mcp: %d, want 405", m, r.StatusCode)
		}
	}
}

// S3.5 Akzeptanz 2: ohne Bearer → 401 mit WWW-Authenticate und
// resource_metadata; falscher Schlüssel → 401; widerrufener → denied.
func TestHTTPKeyAuth(t *testing.T) {
	e := newEnv(t)
	ts, s := e.start(t, nil, nil)
	url := ts.URL + PathMCP

	res := post(t, url, initBody, nil)
	wa := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != http.StatusUnauthorized || !strings.Contains(wa, `resource_metadata="`+s.PRMURL()+`"`) {
		t.Fatalf("no bearer: %d %q", res.StatusCode, wa)
	}
	for _, hdr := range []map[string]string{
		bearer(ops.KeyPrefix + "wrong-0123456789abcdef"),
		{"Authorization": "Basic " + e.keys["reader"]},
		{"Authorization": "Bearer"},
	} {
		res := post(t, url, initBody, hdr)
		if res.StatusCode != http.StatusUnauthorized || res.Header.Get("WWW-Authenticate") == "" {
			t.Errorf("%v: %d", hdr["Authorization"][:6], res.StatusCode)
		}
	}
	// Die gescheiterte Anmeldung steht im Audit (Weg http), ohne Schlüssel.
	found := false
	for _, r := range e.auditAll(t) {
		if r.Operation == ops.OpAuthenticate && r.Reason == "key_invalid" && strings.Contains(string(r.ParamsRedacted), `"http"`) {
			found = true
		}
	}
	if !found {
		t.Error("failed key login over http not in the audit")
	}

	// Widerruf: zuerst eine Sitzung, dann widerrufen. Ein Aufruf in der
	// Sitzung wird abgelehnt (Executor, key_revoked); eine neue Anmeldung
	// nach dem Zwischenspeicher bekommt 403 denied.
	ctx := context.Background()
	cs, _ := connect(t, url, nil, bearer(e.keys["assistant"]))
	k, _ := e.st.APIKeyByName(ctx, "assistant")
	if _, err := e.st.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(ops.DefaultKeyCheckTTL + 100*time.Millisecond)
	r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_list_accounts", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsError || !strings.Contains(textOf(r), `"key_revoked"`) {
		t.Errorf("call with revoked key: %s", textOf(r))
	}
	s.auth.(*keyAuth).cache = nil // Zwischenspeicher abgelaufen
	res = post(t, url, initBody, bearer(e.keys["assistant"]))
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusForbidden || !strings.Contains(string(body), `"denied"`) || !strings.Contains(string(body), `"key_revoked"`) {
		t.Errorf("revoked key: %d %s", res.StatusCode, body)
	}
}

func textOf(r *sdk.CallToolResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*sdk.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// ADR-010 Punkt 9: Jeder Aufruf geht mit Akteur agent durch den Executor;
// das Audit nennt transport http und den Principal, nie den Schlüssel
// (Kanarientest: auch nicht im Log).
func TestHTTPAuditNamesTransport(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	cs, _ := connect(t, ts.URL+PathMCP, nil, bearer(e.keys["reader"]))
	ctx := context.Background()
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_list_accounts", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	var n int
	for _, r := range e.auditAll(t) {
		if r.Operation != "accounts.list" {
			continue
		}
		n++
		var p struct {
			Via ops.Origin `json:"via"`
		}
		_ = json.Unmarshal(r.ParamsRedacted, &p)
		if string(r.ActorType) != "agent" || r.KeyID != "key:reader" || p.Via.Transport != "http" || p.Via.Principal != "reader" {
			t.Errorf("audit entry %d: actor %s key %s params %s", r.ID, r.ActorType, r.KeyID, r.ParamsRedacted)
		}
	}
	if n != 2 {
		t.Errorf("%d audit entries for the call, want 2 (started, ok)", n)
	}
	// mail_audit_log zeigt den Weg.
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_audit_log", Arguments: map[string]any{"operation": "accounts.list"}})
	if err != nil || res.IsError || !strings.Contains(textOf(res), `"transport":"http"`) {
		t.Errorf("mail_audit_log: %v %s", err, textOf(res))
	}
	for name, plain := range e.keys {
		secret := strings.TrimPrefix(plain, ops.KeyPrefix)
		if strings.Contains(e.log.String(), secret) {
			t.Errorf("log contains the key %s", name)
		}
		for _, r := range e.auditAll(t) {
			if strings.Contains(string(r.ParamsRedacted), secret) || strings.Contains(r.KeyID, secret) {
				t.Errorf("audit entry %d contains the key %s", r.ID, name)
			}
		}
	}
	if !strings.Contains(e.log.String(), `"http request"`) || !strings.Contains(e.log.String(), `"key":"key:reader"`) {
		t.Error("log has no request line with the key id")
	}
}

// ADR-010 Punkt 2: feste Host-Allowlist (sonst 421); /healthz ohne
// Inhalt für jeden Host; Metadaten unter /.well-known.
func TestHTTPHostsHealthAndMetadata(t *testing.T) {
	e := newEnv(t)
	ts, s := e.start(t, func(h *config.HTTP) {
		h.Resource = "https://node.example.net/mcp"
		h.AllowedHosts = []string{"node.example.net"}
	}, nil)
	do := func(method, path, host string, hdr map[string]string, body string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.Host = host
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = r.Body.Close() })
		return r
	}
	jsonHdr := map[string]string{"Content-Type": "application/json", "Accept": "application/json, text/event-stream",
		"Authorization": "Bearer " + e.keys["reader"]}
	if r := do(http.MethodPost, PathMCP, "evil.example.org", jsonHdr, initBody); r.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("foreign host: %d, want 421", r.StatusCode)
	}
	if r := do(http.MethodPost, PathMCP, strings.TrimPrefix(ts.URL, "http://"), jsonHdr, initBody); r.StatusCode != http.StatusMisdirectedRequest {
		t.Errorf("listen address as host with own resource: %d, want 421", r.StatusCode)
	}
	if r := do(http.MethodPost, PathMCP, "NODE.example.net:443", jsonHdr, initBody); r.StatusCode != http.StatusOK {
		t.Errorf("allowed host: %d", r.StatusCode)
	}
	r := do(http.MethodGet, PathHealth, "anything.example.org", nil, "")
	b, _ := io.ReadAll(r.Body)
	if r.StatusCode != http.StatusNoContent || len(b) != 0 {
		t.Errorf("healthz: %d %q", r.StatusCode, b)
	}
	for _, p := range []string{prmPath, prmPath + "/mcp"} {
		r := do(http.MethodGet, p, "node.example.net", nil, "")
		var meta map[string]any
		_ = json.NewDecoder(r.Body).Decode(&meta)
		if r.StatusCode != http.StatusOK || meta["resource"] != "https://node.example.net/mcp" || meta["authorization_servers"] != nil {
			t.Errorf("%s: %d %v", p, r.StatusCode, meta)
		}
	}
	if s.PRMURL() != "https://node.example.net"+prmPath+"/mcp" {
		t.Errorf("prm url %s", s.PRMURL())
	}
	if r := do(http.MethodGet, "/other", "node.example.net", nil, ""); r.StatusCode != http.StatusNotFound {
		t.Errorf("unknown path: %d", r.StatusCode)
	}
}

// A03 Teil C, HTTP-Härtung: Content-Type, 1 MiB, Origin-Prüfung,
// Rate-Begrenzung.
func TestHTTPHardening(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, func(h *config.HTTP) { h.MaxRequestsPerMinute = 30 }, nil)
	url := ts.URL + PathMCP
	key := bearer(e.keys["reader"])
	if r := post(t, url, initBody, map[string]string{"Authorization": key["Authorization"], "Content-Type": "text/plain"}); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain: %d, want 415", r.StatusCode)
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"x":"` + strings.Repeat("a", MaxRequestBytes) + `"}}`
	if r := post(t, url, big, key); r.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("more than 1 MiB: %d, want 413", r.StatusCode)
	}
	cross := map[string]string{"Authorization": key["Authorization"], "Origin": "https://evil.example.org", "Sec-Fetch-Site": "cross-site"}
	if r := post(t, url, initBody, cross); r.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: %d, want 403", r.StatusCode)
	}
	limited := false
	for range 40 {
		r := post(t, url, initBody, key)
		if r.StatusCode == http.StatusTooManyRequests {
			limited = r.Header.Get("Retry-After") != ""
			break
		}
	}
	if !limited {
		t.Error("no 429 after more requests than max_requests_per_minute")
	}
}

// E9, ADR-010 Punkt 4: Loopback immer, sonst nur mit allow_non_loopback;
// im Modus key nur Tailnet-Adressen.
func TestCheckListen(t *testing.T) {
	cases := []struct {
		listen, auth string
		allow, ok    bool
	}{
		{"127.0.0.1:8787", "key", false, true},
		{"[::1]:8787", "key", false, true},
		{"localhost:8787", "key", false, true},
		{"0.0.0.0:8787", "key", false, false},
		{"0.0.0.0:8787", "key", true, false},
		{"192.168.1.20:8787", "key", true, false},
		{"100.101.102.103:8787", "key", false, false},
		{"100.101.102.103:8787", "key", true, true},
		{"[fd7a:115c:a1e0::1]:8787", "key", true, true},
		{"0.0.0.0:8787", "oauth", false, false},
		{"0.0.0.0:8787", "oauth", true, true},
	}
	for _, c := range cases {
		err := CheckListen(config.HTTP{Listen: c.listen, Auth: c.auth, AllowNonLoopback: c.allow})
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrListenNotAllowed)) {
			t.Errorf("%s auth=%s allow=%v: %v", c.listen, c.auth, c.allow, err)
		}
	}
}

// ListenAndServe bricht bei einer verbotenen Adresse ab, bevor gelauscht
// wird (S3.5 Akzeptanz 3), und endet mit dem Kontext.
func TestListenAndServe(t *testing.T) {
	e := newEnv(t)
	h := httpConfig(t, func(h *config.HTTP) { h.Listen = "0.0.0.0:8787" })
	h.Listen = "0.0.0.0:0"
	s, err := New(Deps{HTTP: h, Executor: e.x, Keys: e.st})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ListenAndServe(context.Background(), nil); !errors.Is(err, ErrListenNotAllowed) {
		t.Fatalf("0.0.0.0 without allow_non_loopback: %v", err)
	}
	h = httpConfig(t, nil)
	h.Listen = "127.0.0.1:0"
	s, _ = New(Deps{HTTP: h, Executor: e.x, Keys: e.st})
	ctx, cancel := context.WithCancel(context.Background())
	addr := make(chan string, 1)
	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe(ctx, func(a string) { addr <- a }) }()
	a := <-addr
	r, err := http.Get("http://" + a + PathHealth)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Body.Close()
	if r.StatusCode != http.StatusNoContent {
		t.Errorf("healthz: %d", r.StatusCode)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve ended with %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not stop")
	}
}
