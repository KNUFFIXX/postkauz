package remote

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// testKey ist ein Kanarienwert: Er darf in keiner Ausgabe der Brücke stehen.
const testKey = secrets.Secret("pk_KANARIE_remote_0123456789abcdef")

type echoIn struct {
	Text string `json:"text"`
}
type echoOut struct {
	Text string `json:"text"`
}

// testServer ist ein SDK-Server hinter httptest mit Prüfung des Bearers,
// Mitschrift der Kopfzeilen und einstellbaren Störungen.
type testServer struct {
	t       *testing.T
	srv     *sdk.Server
	ts      *httptest.Server
	mu      sync.Mutex
	headers []http.Header
	methods []string
	// expireOnce: die nächste POST-Anfrage mit Session-ID bekommt 404.
	expireOnce atomic.Bool
	// waitCanceled: Das Tool wait sah den Abbruch.
	waitCanceled chan struct{}
	gets         atomic.Int32
}

func newTestServer(t *testing.T, opts *sdk.StreamableHTTPOptions) *testServer {
	t.Helper()
	s := &testServer{t: t, waitCanceled: make(chan struct{}, 1)}
	s.srv = sdk.NewServer(&sdk.Implementation{Name: "pk-test", Version: "9.9"}, nil)
	sdk.AddTool(s.srv, &sdk.Tool{Name: "echo", Description: "echo"}, func(_ context.Context, _ *sdk.CallToolRequest, in echoIn) (*sdk.CallToolResult, echoOut, error) {
		return nil, echoOut{Text: in.Text}, nil
	})
	sdk.AddTool(s.srv, &sdk.Tool{Name: "wait", Description: "wait until cancelled"}, func(ctx context.Context, _ *sdk.CallToolRequest, _ echoIn) (*sdk.CallToolResult, echoOut, error) {
		select {
		case <-ctx.Done():
			s.waitCanceled <- struct{}{}
		case <-time.After(20 * time.Second):
		}
		return nil, echoOut{}, nil
	})
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return s.srv }, opts)
	s.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.headers = append(s.headers, r.Header.Clone())
		s.methods = append(s.methods, r.Method)
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+string(testKey) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			s.gets.Add(1)
		}
		if r.Method == http.MethodPost && r.Header.Get("Mcp-Session-Id") != "" && s.expireOnce.CompareAndSwap(true, false) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.ts.Close)
	return s
}

func (s *testServer) seen() []http.Header {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]http.Header(nil), s.headers...)
}

// connectBridge verbindet einen SDK-Client über die Brücke mit url.
func connectBridge(t *testing.T, url string, key secrets.Secret, opts *sdk.ClientOptions, log *bytes.Buffer) (*sdk.ClientSession, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ct, st := sdk.NewInMemoryTransports()
	local, err := st.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var lg *slog.Logger
	if log != nil {
		lg = slog.New(slog.NewTextHandler(log, nil))
	}
	b := &Bridge{Name: "buero", URL: url, Key: key, Log: lg, CallTimeout: 10 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := b.Run(ctx, local); err != nil {
			t.Errorf("bridge: %v", err)
		}
	}()
	t.Cleanup(func() {
		_ = local.Close()
		cancel()
		<-done
	})
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "client", Version: "1"}, opts).Connect(ctx, ct, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

func echo(t *testing.T, cs *sdk.ClientSession, text string) {
	t.Helper()
	r, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "echo", Arguments: map[string]any{"text": text}})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if m, ok := r.StructuredContent.(map[string]any); !ok || m["text"] != text {
		t.Fatalf("echo result %#v", r.StructuredContent)
	}
}

// Wie mcp --http: zustandslos, Antworten als JSON, GET → 405.
func TestBridgeStatelessJSON(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	var log bytes.Buffer
	cs, err := connectBridge(t, s.ts.URL+"/mcp", testKey, nil, &log)
	if err != nil {
		t.Fatal(err)
	}
	if ir := cs.InitializeResult(); ir == nil || ir.ServerInfo.Name != "pk-test" {
		t.Fatalf("initialize: %+v", ir)
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatalf("tools: %+v %v", tools, err)
	}
	echo(t, cs, "Grüße über die Brücke")
	// Protokollversion nach initialize in jeder weiteren Anfrage.
	ver := cs.InitializeResult().ProtocolVersion
	var withVersion int
	for _, h := range s.seen() {
		if h.Get("Mcp-Protocol-Version") == ver {
			withVersion++
		}
		if h.Get("Mcp-Session-Id") != "" {
			t.Errorf("stateless server, but a session id was sent")
		}
	}
	if withVersion < 3 {
		t.Errorf("Mcp-Protocol-Version %q sent %d times", ver, withVersion)
	}
	if strings.Contains(log.String(), string(testKey)) {
		t.Fatalf("key in the log: %s", log.String())
	}
}

// Mit Sitzung und SSE: Session-ID weitergeführt, Antworten als SSE,
// Server-Nachricht über den GET-Strom, 404 → neue Sitzung, Abbruch.
func TestBridgeStatefulSSE(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{})
	changed := make(chan struct{}, 4)
	opts := &sdk.ClientOptions{ToolListChangedHandler: func(context.Context, *sdk.ToolListChangedRequest) { changed <- struct{}{} }}
	cs, err := connectBridge(t, s.ts.URL+"/mcp", testKey, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	echo(t, cs, "eins")
	var sid string
	for _, h := range s.seen() {
		if v := h.Get("Mcp-Session-Id"); v != "" {
			sid = v
		}
	}
	if sid == "" {
		t.Fatal("no session id passed on")
	}
	// GET-Strom: Eine Änderung der Tools erreicht den Client.
	deadline := time.Now().Add(5 * time.Second)
	for s.gets.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.gets.Load() == 0 {
		t.Fatal("no GET stream opened")
	}
	time.Sleep(100 * time.Millisecond)
	sdk.AddTool(s.srv, &sdk.Tool{Name: "neu", Description: "neu"}, func(context.Context, *sdk.CallToolRequest, echoIn) (*sdk.CallToolResult, echoOut, error) {
		return nil, echoOut{}, nil
	})
	select {
	case <-changed:
	case <-time.After(5 * time.Second):
		t.Fatal("tools/list_changed did not arrive over the GET stream")
	}

	// Abgelaufene Sitzung: neue Anmeldung, die Anfrage gelingt.
	s.expireOnce.Store(true)
	echo(t, cs, "zwei")
	var newSID string
	for _, h := range s.seen() {
		if v := h.Get("Mcp-Session-Id"); v != "" && v != sid {
			newSID = v
		}
	}
	if newSID == "" {
		t.Fatal("no new session after 404")
	}

	// Abbruch: notifications/cancelled kommt an, das Tool sieht ihn.
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "wait", Arguments: map[string]any{"text": ""}})
		errc <- err
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case <-s.waitCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not see the cancellation")
	}
	if err := <-errc; err == nil {
		t.Fatal("cancelled call returned no error")
	}
	echo(t, cs, "nach dem Abbruch")
}

// Fehler kommen als JSON-RPC-Fehler mit verständlichem Text, nie mit dem
// Schlüssel.
func TestBridgeErrors(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	wrong := secrets.Secret("pk_FALSCH_KANARIE_0123456789")
	_, err := connectBridge(t, s.ts.URL+"/mcp", wrong, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "refused the API key (401") || !strings.Contains(err.Error(), "remote add buero") {
		t.Fatalf("wrong key: %v", err)
	}
	if strings.Contains(err.Error(), string(wrong)) {
		t.Fatalf("error repeats the key: %v", err)
	}
	if code, ok := ErrorCode(err); !ok || code != CodeUnauthorized {
		t.Fatalf("code %d %v (%v)", code, ok, err)
	}

	// Nicht erreichbar.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL + "/mcp"
	dead.Close()
	_, err = connectBridge(t, url, testKey, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "is not reachable (connection refused)") {
		t.Fatalf("unreachable: %v", err)
	}

	// Falsche Adresse, widerrufener Schlüssel, Umleitung, Überlast.
	for _, c := range []struct {
		h    http.HandlerFunc
		want string
		code int64
	}{
		{func(w http.ResponseWriter, _ *http.Request) { http.NotFound(w, nil) }, "no MCP endpoint at this URL (404)", CodeBadResponse},
		{func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":"denied","code":"key_revoked","message":"x"}`))
		}, "revoked or has expired (403 key_revoked)", CodeForbidden},
		{func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.org/mcp", http.StatusFound)
		}, "redirects (302)", CodeBadResponse},
		{func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "7")
			http.Error(w, "x", http.StatusTooManyRequests)
		}, "try again in 7 s", CodeRateLimited},
		{func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "x", http.StatusServiceUnavailable) }, "temporarily unavailable (503)", CodeUnavailable},
	} {
		ts := httptest.NewServer(c.h)
		_, err := connectBridge(t, ts.URL+"/mcp", testKey, nil, nil)
		ts.Close()
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v", c.want, err)
			continue
		}
		if code, _ := ErrorCode(err); code != c.code {
			t.Errorf("%q: code %d, want %d", c.want, code, c.code)
		}
	}
}

// remote test (Check) über dieselbe Brücke.
func TestCheck(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	res, err := Check(context.Background(), "buero", s.ts.URL+"/mcp", testKey, nil, nil)
	if err != nil || res.Tools != 2 || res.ServerName != "pk-test" || res.ProtocolVersion == "" || res.Role != "" {
		t.Fatalf("check: %+v %v", res, err)
	}
	_, err = Check(context.Background(), "buero", s.ts.URL+"/mcp", "pk_falsch", nil, nil)
	var re *Error
	if !errors.As(err, &re) || re.Code != CodeUnauthorized {
		t.Fatalf("check with wrong key: %v", err)
	}
}
