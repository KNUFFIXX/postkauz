package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// freePort liefert eine freie Adresse auf Loopback.
func freePort(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

type bearerRT struct{ key string }

func (b bearerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.key)
	return http.DefaultTransport.RoundTrip(r)
}

// S3.5 (T5): `postkauz mcp --http` als Kindprozess. Ein Schlüssel als
// Bearer bekommt dieselben Tools wie über stdio; mail_sync_now reiht nur
// ein. Kanarientest (Akzeptanz 4): Der HTTP-Prozess braucht kein Passwort
// (der Speicher der Zugangsdaten ist während des Laufs weg), und weder
// Passwort noch Schlüssel stehen in Ausgabe oder Log.
func TestMCPHTTPChildProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	if runtime.GOOS == "windows" {
		t.Skip("stops the child with SIGINT")
	}
	dataDir, _ := setupEnv(t)
	t.Setenv("POSTKAUZ_SECRETS_BACKEND", "insecure-file")
	const pw = "kanarien-passwort-http-0815"
	host, port := fakeIMAP(t, pw)
	if code, out, errs := runIn(t, pw+"\n", "account", "add", "demo", "--imap", net.JoinHostPort(host, port), "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin", "--json"); code != ExitOK {
		t.Fatalf("account add: %d %s %s", code, out, errs)
	}
	key := newKey(t, "--name", "cloud-reader", "--role", "assistant")
	// Zugangsdaten weg: Braucht der HTTP-Prozess sie, scheitert der Aufruf.
	if err := os.RemoveAll(filepath.Join(dataDir, "insecure-secrets")); err != nil {
		t.Fatal(err)
	}

	addr := freePort(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "mcp", "--http", "--listen", addr)
	cmd.Env = append(os.Environ(), "POSTKAUZ_TEST_RUN_CLI=1", "POSTKAUZ_DATA_DIR="+dataDir, "POSTKAUZ_SECRETS_BACKEND=insecure-file")
	var stderr bytes.Buffer
	cmd.Stderr, cmd.Stdout = &stderr, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(syscall.SIGINT)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("mcp --http ended with %v (%s)", err, stderr.String())
			}
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			t.Error("mcp --http did not stop on SIGINT")
		}
	}
	t.Cleanup(stop)
	base := "http://" + addr
	deadline := time.Now().Add(20 * time.Second)
	for {
		r, err := http.Get(base + "/healthz")
		if err == nil {
			_ = r.Body.Close()
			if r.StatusCode == http.StatusNoContent {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("mcp --http did not come up: %v (%s)", err, stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "http-smoke", Version: "1"}, nil).Connect(ctx,
		&sdk.StreamableClientTransport{Endpoint: base + "/mcp", HTTPClient: &http.Client{Transport: bearerRT{key}},
			DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("connect: %v (%s)", err, stderr.String())
	}
	defer cs.Close()
	lt, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, tl := range lt.Tools {
		names[tl.Name] = true
	}
	if !names["mail_sync_now"] || !names["mail_create_draft"] || names["mail_move"] || names["mail_trash"] {
		t.Errorf("assistant tools over http: %v", names)
	}
	for _, call := range []struct {
		tool string
		args map[string]any
	}{
		{"mail_list_accounts", map[string]any{}},
		{"mail_sync_now", map[string]any{"account": "demo", "dry_run": false}},
		{"mail_search", map[string]any{"query": "kanarie"}},
	} {
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: call.tool, Arguments: call.args})
		if err != nil || res.IsError {
			var text string
			if res != nil && len(res.Content) > 0 {
				text = res.Content[0].(*sdk.TextContent).Text
			}
			t.Errorf("%s: %v %s", call.tool, err, text)
		}
		if call.tool == "mail_sync_now" && res != nil && !strings.Contains(res.Content[0].(*sdk.TextContent).Text, "queued") {
			t.Errorf("mail_sync_now: %s", res.Content[0].(*sdk.TextContent).Text)
		}
	}
	_ = cs.Close()
	stop()
	logData, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	secret := strings.TrimPrefix(key, "pk_key_")
	for name, s := range map[string]string{"stderr": stderr.String(), "log": string(logData)} {
		if strings.Contains(s, pw) || strings.Contains(s, secret) {
			t.Errorf("%s contains the password or the key", name)
		}
	}
	if !strings.Contains(string(logData), "mcp http server started") || !strings.Contains(string(logData), `"key":"key:cloud-reader"`) {
		t.Errorf("log lacks the start line or the key id:\n%s", logData)
	}
	// Audit: Start und Abschluss mit transport http.
	code, out, _ := run(t, "audit", "--raw", "--operation", "sync.request", "--json")
	if code != ExitOK || !strings.Contains(out, `"transport":"http"`) || !strings.Contains(out, `"principal":"cloud-reader"`) {
		t.Errorf("audit of sync.request: %d %s", code, out)
	}
}

// --http prüft die Adresse vor dem Start: 0.0.0.0 ohne
// allow_non_loopback bricht ab (S3.5 Akzeptanz 3); --listen ohne --http
// ist ein Bedienfehler.
func TestMCPHTTPRefusesNonLoopback(t *testing.T) {
	setupEnv(t)
	code, _, errs := run(t, "mcp", "--http", "--listen", "0.0.0.0:8787")
	if code != ExitUsage || !strings.Contains(errs, "allow_non_loopback") {
		t.Errorf("0.0.0.0: %d %s", code, errs)
	}
	code, _, errs = run(t, "mcp", "--listen", "127.0.0.1:8787")
	if code != ExitUsage || !strings.Contains(errs, "--http") {
		t.Errorf("--listen without --http: %d %s", code, errs)
	}
}

// doctor zeigt [http] ohne Geheimnisse, mit der Prüfung der Adresse.
func TestDoctorShowsHTTP(t *testing.T) {
	setupEnv(t)
	code, out, errs := run(t, "doctor", "--json")
	if code != ExitOK || !strings.Contains(out, `"listen":"127.0.0.1:8787"`) || !strings.Contains(out, `"auth":"key"`) ||
		!strings.Contains(out, `"listen_allowed":true`) {
		t.Fatalf("doctor --json: %d %s %s", code, out, errs)
	}
	_, out, _ = run(t, "doctor")
	if !strings.Contains(out, "http:      mcp --http on 127.0.0.1:8787, auth key, resource http://127.0.0.1:8787/mcp [http]") {
		t.Errorf("doctor text:\n%s", out)
	}
}
