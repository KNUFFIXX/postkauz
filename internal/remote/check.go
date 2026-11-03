package remote

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// TestResult ist das Ergebnis von remote test: nur Zahlen und Namen, kein
// Inhalt einer Mail.
type TestResult struct {
	ProtocolVersion string `json:"protocol_version"`
	ServerName      string `json:"server_name"`
	ServerVersion   string `json:"server_version"`
	Tools           int    `json:"tools"`
	// KeyName und Role: aus mail_get_policy (Name des Schlüssels, nie der
	// Schlüssel); leer, wenn der Server das Tool nicht anbietet.
	KeyName string `json:"key_name,omitempty"`
	Role    string `json:"role,omitempty"`
	// ToolNames: Namen der Tools, sortiert.
	ToolNames []string `json:"tool_names"`
}

// Error ist ein Fehler von remote test mit dem JSON-RPC-Code der Brücke
// (CodeUnreachable …) und dem Text, den auch der MCP-Client sähe.
type Error struct {
	Code    int64
	Message string
}

func (e *Error) Error() string { return e.Message }

// testTimeout begrenzt remote test.
const testTimeout = 60 * time.Second

// Check meldet sich als MCP-Client durch dieselbe Brücke an, die
// `mcp --remote` nutzt (initialize, tools/list, mail_get_policy), und
// liefert, was der Server über den Schlüssel sagt.
func Check(ctx context.Context, name, rawURL string, key secrets.Secret, hc *http.Client, log *slog.Logger) (TestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	ct, st := sdk.NewInMemoryTransports()
	local, err := st.Connect(ctx)
	if err != nil {
		return TestResult{}, err
	}
	b := &Bridge{Name: name, URL: rawURL, Key: key, HTTPClient: hc, Log: log, CallTimeout: testTimeout}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = crash.Do("remote test bridge", func() error { return b.Run(ctx, local) })
	}()
	defer func() {
		_ = local.Close()
		cancel()
		<-done
	}()
	c := sdk.NewClient(&sdk.Implementation{Name: "postkauz-remote-test", Version: "1"}, nil)
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		return TestResult{}, testErr(err)
	}
	defer cs.Close()
	var res TestResult
	if ir := cs.InitializeResult(); ir != nil {
		res.ProtocolVersion = ir.ProtocolVersion
		if ir.ServerInfo != nil {
			res.ServerName, res.ServerVersion = ir.ServerInfo.Name, ir.ServerInfo.Version
		}
	}
	hasPolicy := false
	for tool, err := range cs.Tools(ctx, nil) {
		if err != nil {
			return TestResult{}, testErr(err)
		}
		res.ToolNames = append(res.ToolNames, tool.Name)
		if tool.Name == "mail_get_policy" {
			hasPolicy = true
		}
	}
	sort.Strings(res.ToolNames)
	res.Tools = len(res.ToolNames)
	if res.ToolNames == nil {
		res.ToolNames = []string{}
	}
	if hasPolicy {
		r, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_get_policy", Arguments: map[string]any{}})
		if err != nil {
			return TestResult{}, testErr(err)
		}
		var p struct {
			Key  string `json:"key"`
			Role string `json:"role"`
		}
		if b, err := json.Marshal(r.StructuredContent); err == nil && json.Unmarshal(b, &p) == nil {
			res.KeyName, res.Role = p.Key, p.Role
		}
	}
	return res, nil
}

// testErr macht aus einem Fehler des SDK-Clients den Fehler der Brücke.
func testErr(err error) error {
	if code, ok := ErrorCode(err); ok {
		var be *bridgeError
		var msg string
		if errors.As(err, &be) {
			msg = be.msg
		} else {
			msg = jsonrpcMessage(err)
		}
		return &Error{Code: code, Message: msg}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeUnreachable, Message: "no answer from the server within " + testTimeout.String()}
	}
	return &Error{Code: CodeBadResponse, Message: "the MCP handshake failed"}
}

func jsonrpcMessage(err error) string {
	var we *jsonrpc.Error
	if errors.As(err, &we) {
		return we.Message
	}
	return "the MCP handshake failed"
}
