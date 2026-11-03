package httpmcp

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/remote"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// viaBridge verbindet einen SDK-Client über die Brücke von mcp --remote
// (remote.Bridge) mit dem HTTP-Server.
func viaBridge(t *testing.T, url string, key secrets.Secret, log *lockedBuffer) (*sdk.ClientSession, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	ct, st := sdk.NewInMemoryTransports()
	local, err := st.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b := &remote.Bridge{Name: "buero", URL: url, Key: key, Log: slog.New(slog.NewTextHandler(log, nil)), CallTimeout: 30 * time.Second}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.Run(ctx, local)
	}()
	t.Cleanup(func() {
		_ = local.Close()
		cancel()
		<-done
	})
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "claude-desktop-test", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err == nil {
		t.Cleanup(func() { _ = cs.Close() })
	}
	return cs, err
}

// A04 Teil 4: mcp --remote gegen mcp --http (in-process): Die Tools sind
// dieselben wie über stdio, ein Suchaufruf geht durch und steht mit dem Weg
// http im Audit; ein falscher Schlüssel ergibt einen klaren Fehler; der
// Schlüssel steht in keiner Ausgabe, keinem Log und keinem Audit-Eintrag.
func TestA04BridgeAgainstHTTPServer(t *testing.T) {
	e := newEnv(t)
	ts, _ := e.start(t, nil, nil)
	key := secrets.Secret(e.keys["assistant"])
	var bridgeLog lockedBuffer
	cs, err := viaBridge(t, ts.URL+PathMCP, key, &bridgeLog)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := toolNames(t, cs), e.stdioToolNames(t, "assistant"); !slices.Equal(got, want) {
		t.Fatalf("tools via the bridge %v, over stdio %v", got, want)
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "mail_search", Arguments: map[string]any{"query": "Rechnung"}})
	if err != nil || res.IsError {
		t.Fatalf("mail_search: %v %+v", err, res)
	}
	pol, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "mail_get_policy", Arguments: map[string]any{}})
	if err != nil || pol.IsError {
		t.Fatalf("mail_get_policy: %v %+v", err, pol)
	}
	searched := false
	for _, r := range e.auditAll(t) {
		if r.Operation == "messages.search" && strings.Contains(string(r.ParamsRedacted), `"http"`) {
			searched = true
		}
	}
	if !searched {
		t.Fatal("the search via the bridge is not in the audit with transport http")
	}

	// remote test (remote.Check) über dieselbe Brücke.
	chk, err := remote.Check(context.Background(), "buero", ts.URL+PathMCP, key, nil, nil)
	if err != nil || chk.Tools != len(e.stdioToolNames(t, "assistant")) || chk.Role != "assistant" || !strings.Contains(chk.KeyName, "assistant") {
		t.Fatalf("check: %+v %v", chk, err)
	}

	// Falscher Schlüssel: klarer Fehler, ohne den Schlüssel zu nennen.
	wrong := secrets.Secret(ops.KeyPrefix + "KANARIE-falsch-0123456789abcdef")
	_, err = viaBridge(t, ts.URL+PathMCP, wrong, &bridgeLog)
	if err == nil || !strings.Contains(err.Error(), "refused the API key (401") {
		t.Fatalf("wrong key: %v", err)
	}
	if code, _ := remote.ErrorCode(err); code != remote.CodeUnauthorized {
		t.Fatalf("wrong key: code %d", code)
	}
	_, err2 := remote.Check(context.Background(), "buero", ts.URL+PathMCP, wrong, nil, nil)

	// Kanarie: weder der richtige noch der falsche Schlüssel irgendwo.
	var audit strings.Builder
	for _, ev := range e.auditAll(t) {
		audit.WriteString(string(ev.ParamsRedacted))
		audit.WriteString(ev.Operation)
	}
	for _, k := range []secrets.Secret{key, wrong} {
		for what, text := range map[string]string{"bridge log": bridgeLog.String(), "server log": e.log.String(),
			"error": err.Error(), "check error": err2.Error(), "audit": audit.String()} {
			if strings.Contains(text, k.Reveal()) {
				t.Errorf("%s contains a key", what)
			}
		}
	}
}
