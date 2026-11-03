package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// remoteStubKey: Schlüssel, den remoteStub annimmt (Kanarie: steht in
// keiner Ausgabe).
const remoteStubKey = ops.KeyPrefix + "KANARIE-remote-stub-0123456789"

// remoteStub ist ein kleiner MCP-Server über Streamable HTTP (zustandslos,
// JSON, wie mcp --http) mit zwei Tools, der nur remoteStubKey annimmt.
func remoteStub(t *testing.T) string {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: "stub", Version: "1"}, nil)
	type in struct {
		Text string `json:"text,omitempty"`
	}
	type out struct {
		Key  string `json:"key"`
		Role string `json:"role"`
	}
	sdk.AddTool(srv, &sdk.Tool{Name: "mail_get_policy", Description: "policy"}, func(context.Context, *sdk.CallToolRequest, in) (*sdk.CallToolResult, out, error) {
		return nil, out{Key: "zweiter-pc", Role: "assistant"}, nil
	})
	sdk.AddTool(srv, &sdk.Tool{Name: "mail_search", Description: "search"}, func(context.Context, *sdk.CallToolRequest, in) (*sdk.CallToolResult, out, error) {
		return nil, out{}, nil
	})
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+remoteStubKey {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL + "/mcp"
}
