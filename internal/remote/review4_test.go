package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawBridge startet die Brücke zu url und liefert die Client-Seite als rohe
// JSON-RPC-Verbindung; done schließt, wenn Run endet.
func rawBridge(t *testing.T, b *Bridge) (sdk.Connection, <-chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ct, st := sdk.NewInMemoryTransports()
	local, err := st.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ct.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.Run(ctx, local)
	}()
	t.Cleanup(func() {
		_ = client.Close()
		_ = local.Close()
		cancel()
		<-done
	})
	return client, done
}

func call(t *testing.T, c sdk.Connection, id int64, method string, params any) {
	t.Helper()
	rid, _ := jsonrpc.MakeID(float64(id))
	p, _ := json.Marshal(params)
	if err := c.Write(context.Background(), &jsonrpc.Request{ID: rid, Method: method, Params: p}); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, c sdk.Connection, timeout time.Duration) *jsonrpc.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		m, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("no answer within %s: %v", timeout, err)
		}
		if r, ok := m.(*jsonrpc.Response); ok {
			return r
		}
	}
}

func wireErr(t *testing.T, r *jsonrpc.Response) *jsonrpc.Error {
	t.Helper()
	var we *jsonrpc.Error
	if !errors.As(r.Error, &we) {
		t.Fatalf("response without error: %+v", r)
	}
	return we
}

// W2: Hängt der POST einer Benachrichtigung, kommt ein folgender ping
// trotzdem sofort durch (vorher erst nach 30 s).
func TestReview4HangingNotificationDoesNotBlockPing(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "notifications/roots/list_changed") {
			<-r.Context().Done()
			return
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		s.ts.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(hang.Close)
	c, _ := rawBridge(t, &Bridge{Name: "buero", URL: hang.URL + "/mcp", Key: testKey, CallTimeout: 10 * time.Second, DrainTimeout: 200 * time.Millisecond})
	call(t, c, 1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "x", "version": "1"}})
	readResponse(t, c, 5*time.Second)
	_ = c.Write(context.Background(), &jsonrpc.Request{Method: "notifications/initialized"})
	_ = c.Write(context.Background(), &jsonrpc.Request{Method: "notifications/roots/list_changed"})
	start := time.Now()
	call(t, c, 2, "ping", map[string]any{})
	if r := readResponse(t, c, 5*time.Second); r.Error != nil || time.Since(start) > 2*time.Second {
		t.Fatalf("ping after a hanging notification: %v after %s", r.Error, time.Since(start))
	}
}

// K1: Ein Panic beim Weiterreichen ergibt -32603 mit derselben id.
func TestReview4PanicAnswersInternalError(t *testing.T) {
	s := newTestServer(t, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	testCallHook = func(r *jsonrpc.Request) {
		if r.Method == "tools/list" {
			panic("boom KANARIE")
		}
	}
	t.Cleanup(func() { testCallHook = nil })
	c, _ := rawBridge(t, &Bridge{Name: "buero", URL: s.ts.URL + "/mcp", Key: testKey, DrainTimeout: 200 * time.Millisecond})
	call(t, c, 7, "tools/list", map[string]any{})
	r := readResponse(t, c, 5*time.Second)
	if we := wireErr(t, r); we.Code != jsonrpc.CodeInternalError || r.ID.Raw() != int64(7) || strings.Contains(we.Message, "KANARIE") {
		t.Fatalf("panic: id %v error %+v", r.ID.Raw(), we)
	}
}

// K6: selbstsigniertes Zertifikat → nicht erreichbar (-32010).
func TestReview4SelfSignedCertificate(t *testing.T) {
	ts := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(ts.Close)
	_, err := connectBridge(t, ts.URL+"/mcp", testKey, nil, nil)
	if code, _ := ErrorCode(err); code != CodeUnreachable || !strings.Contains(err.Error(), "TLS certificate not accepted") {
		t.Fatalf("self-signed: code %d %v", code, err)
	}
}

// K6: Antwort über 16 MiB, als JSON und als SSE: Fehler „too large“.
func TestReview4TooLargeAnswer(t *testing.T) {
	big := strings.Repeat("x", DefaultMaxMessageBytes+1024)
	for _, ct := range []string{contentJSON, contentSSE} {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				ID any `json:"id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"text": big}})
			w.Header().Set("Content-Type", ct)
			if ct == contentSSE {
				_, _ = w.Write([]byte("event: message\ndata: " + string(body) + "\n\n"))
				return
			}
			_, _ = w.Write(body)
		}))
		c, _ := rawBridge(t, &Bridge{Name: "buero", URL: ts.URL + "/mcp", Key: testKey, DrainTimeout: 200 * time.Millisecond})
		call(t, c, 1, "ping", map[string]any{})
		r := readResponse(t, c, 20*time.Second)
		if we := wireErr(t, r); we.Code != CodeBadResponse || !strings.Contains(we.Message, "too large") {
			t.Errorf("%s: %+v", ct, we)
		}
		ts.Close()
	}
}

// K6: Ein Stapel (JSON-Array) in einer Antwort geht vollständig weiter.
func TestReview4BatchInAnswer(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID any `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		body, _ := json.Marshal([]any{
			map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "data": "vorher"}},
			map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{}},
		})
		w.Header().Set("Content-Type", contentJSON)
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	c, _ := rawBridge(t, &Bridge{Name: "buero", URL: ts.URL + "/mcp", Key: testKey, DrainTimeout: 200 * time.Millisecond})
	call(t, c, 3, "ping", map[string]any{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m1, err1 := c.Read(ctx)
	m2, err2 := c.Read(ctx)
	n, ok1 := m1.(*jsonrpc.Request)
	r, ok2 := m2.(*jsonrpc.Response)
	if err1 != nil || err2 != nil || !ok1 || n.Method != "notifications/message" || !ok2 || r.ID.Raw() != int64(3) || r.Error != nil {
		t.Fatalf("batch: %v %v %#v %#v", err1, err2, m1, m2)
	}
}

// K6: Endet stdin, während der Server nicht antwortet, endet die Brücke
// nach der Wartezeit (Standard 10 s), nicht erst nach CallTimeout.
func TestReview4StdinEndWithHangingServer(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the default drain timeout of 10 s")
	}
	// Den Inhalt lesen: Erst dann bemerkt net/http das Ende der Verbindung.
	ts := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(ts.Close)
	c, done := rawBridge(t, &Bridge{Name: "buero", URL: ts.URL + "/mcp", Key: testKey})
	call(t, c, 1, "tools/list", map[string]any{})
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	_ = c.Close()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the bridge did not end")
	}
	if d := time.Since(start); d < 9*time.Second || d > 13*time.Second {
		t.Fatalf("ended after %s, want about %s", d, drainTimeout)
	}
}
