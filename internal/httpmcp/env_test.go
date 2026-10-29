package httpmcp

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/mcp"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// testEnv: Datenbank mit Schlüsseln je Rolle, Executor wie in der CLI
// (KeyGuard, Regeln), Log in einem Puffer (Kanarientest).
type testEnv struct {
	st   *store.SQLite
	x    *ops.Executor
	log  *syncBuffer
	keys map[string]string // Name → Klartext
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

var testRoles = []string{"reader", "assistant", "triage", "operator", "admin"}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	reg, err := ops.NewDefaultRegistry(ops.Deps{Accounts: st, Mailboxes: st, Messages: st, Search: st, Keys: st, Audit: st})
	if err != nil {
		t.Fatal(err)
	}
	rules := policy.DefaultRules()
	e := &testEnv{st: st, log: &syncBuffer{}, keys: map[string]string{},
		x: &ops.Executor{Registry: reg, Audit: st.AuditWriter(), Keys: &ops.KeyGuard{Repo: st}, Rules: &rules}}
	now := time.Now()
	for _, role := range testRoles {
		plain := ops.KeyPrefix + "test-" + role + "-0123456789abcdef"
		var scopes []string
		for _, s := range policy.Roles[role] {
			scopes = append(scopes, string(s))
		}
		if _, err := st.CreateAPIKey(ctx, store.APIKey{Name: role, KeyHash: ops.HashKey(plain), Role: role, Scopes: scopes, CreatedAt: now}); err != nil {
			t.Fatal(err)
		}
		e.keys[role] = plain
	}
	return e
}

func (e *testEnv) logger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(e.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// httpConfig: geprüfte Konfiguration wie aus config.LoadWith.
func httpConfig(t *testing.T, mod func(*config.HTTP)) config.HTTP {
	t.Helper()
	h := config.HTTP{}
	if mod != nil {
		mod(&h)
	}
	if err := config.CheckHTTP(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

// start baut den Server und startet ihn hinter httptest; listen und
// resource zeigen auf die Adresse des Testservers, Host wie bei
// tailscale serve über allowed_hosts.
func (e *testEnv) start(t *testing.T, mod func(*config.HTTP), deps func(*Deps)) (*httptest.Server, *Server) {
	t.Helper()
	ts := httptest.NewUnstartedServer(nil)
	addr := ts.Listener.Addr().String()
	h := httpConfig(t, func(h *config.HTTP) {
		h.Listen = addr
		if mod != nil {
			mod(h)
		}
	})
	d := Deps{HTTP: h, Executor: e.x, Keys: e.st, Audit: e.st.AuditWriter(), Version: "test", Log: e.logger()}
	if deps != nil {
		deps(&d)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	ts.Config.Handler = s.Handler()
	ts.Start()
	t.Cleanup(ts.Close)
	return ts, s
}

// headerTransport setzt feste Kopfzeilen (wie Claude Code mit headers).
type headerTransport struct {
	base http.RoundTripper
	hdr  map[string]string
	// seen sammelt die Antwortköpfe (Session-ID, Server-Kennung).
	mu   sync.Mutex
	seen []http.Header
}

func (h *headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	for k, v := range h.hdr {
		r.Header.Set(k, v)
	}
	res, err := h.base.RoundTrip(r)
	if err == nil {
		h.mu.Lock()
		h.seen = append(h.seen, res.Header.Clone())
		h.mu.Unlock()
	}
	return res, err
}

// connect öffnet eine MCP-Sitzung über HTTP mit festen Kopfzeilen.
func connect(t *testing.T, url string, base http.RoundTripper, hdr map[string]string) (*sdk.ClientSession, *headerTransport) {
	t.Helper()
	if base == nil {
		base = http.DefaultTransport
	}
	ht := &headerTransport{base: base, hdr: hdr}
	c := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil)
	cs, err := c.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: ht},
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, ht
}

func toolNames(t *testing.T, cs *sdk.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	return names
}

// stdioToolNames: dieselbe Liste über den Server, den `postkauz mcp`
// (stdio) baut, im Speicher verbunden.
func (e *testEnv) stdioToolNames(t *testing.T, role string) []string {
	t.Helper()
	ctx := context.Background()
	k, err := ops.AuthenticateKey(ctx, e.st, e.keys[role], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	srv, err := mcp.New(mcp.Options{Executor: e.x, Key: k, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ct, st := sdk.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	return toolNames(t, cs)
}

// post schickt eine rohe JSON-RPC-Anfrage an /mcp.
func post(t *testing.T, url, body string, hdr map[string]string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

const initBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`

func bearer(v string) map[string]string { return map[string]string{"Authorization": "Bearer " + v} }

// auditAll liest alle Audit-Einträge.
func (e *testEnv) auditAll(t *testing.T) []store.AuditRecord {
	t.Helper()
	recs, err := e.st.AuditQuery(context.Background(), store.AuditFilter{Limit: 10000})
	if err != nil {
		t.Fatal(err)
	}
	return recs
}
