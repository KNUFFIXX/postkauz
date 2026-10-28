package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// TestMain: Mit POSTKAUZ_TEST_RUN_CLI=1 ist die Testbinärdatei das
// Programm selbst (für den stdio-Smoke-Test mit einem echten Kindprozess).
func TestMain(m *testing.M) {
	if os.Getenv("POSTKAUZ_TEST_RUN_CLI") == "1" {
		os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr, "test"))
	}
	os.Exit(m.Run())
}

// newKey legt per CLI einen Schlüssel an und liefert den Klartext.
func newKey(t *testing.T, args ...string) string {
	t.Helper()
	code, out, errs := run(t, append([]string{"key", "create", "--json"}, args...)...)
	var o ops.KeysCreateOutput
	if code != ExitOK || json.Unmarshal([]byte(out), &o) != nil || o.Secret == "" {
		t.Fatalf("key create %v: %d %s %s", args, code, out, errs)
	}
	return o.Secret
}

// mcpSession startet `postkauz mcp` im Prozess (RunWithInput über Pipes)
// und verbindet einen Client des offiziellen SDK.
func mcpSession(t *testing.T, key string) (*sdk.ClientSession, *bytes.Buffer) {
	t.Helper()
	t.Setenv(EnvKey, key)
	srvIn, cliOut := io.Pipe()
	cliIn, srvOut := io.Pipe()
	var stderr bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		RunWithInput([]string{"mcp"}, srvIn, srvOut, &stderr, "test")
		_ = srvOut.Close()
	}()
	ctx := context.Background()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "postkauz-test", Version: "1"}, nil).
		Connect(ctx, &sdk.IOTransport{Reader: cliIn, Writer: cliOut}, nil)
	if err != nil {
		t.Fatalf("connect: %v (stderr %s)", err, stderr.String())
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = cliOut.Close()
		wg.Wait()
	})
	return cs, &stderr
}

func callTool(t *testing.T, cs *sdk.ClientSession, name string, args map[string]any) (map[string]any, *sdk.CallToolResult) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	text := res.Content[0].(*sdk.TextContent).Text
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("%s: content is not JSON: %s", name, text)
	}
	if res.IsError {
		return v, res
	}
	sc, _ := json.Marshal(res.StructuredContent)
	var s map[string]any
	_ = json.Unmarshal(sc, &s)
	if !jsonEqual(s, v) {
		t.Fatalf("%s: structured content and text differ:\n%s\n%s", name, sc, text)
	}
	return v, res
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// assertEnvelope: Ausgabe mit Mailinhalt steht in der Untrusted-Hülle.
func assertEnvelope(t *testing.T, what string, v map[string]any) {
	t.Helper()
	if v["untrusted"] != true || v["source"] != "email" || v["notice"] != ops.UntrustedNotice {
		t.Fatalf("%s: not in the untrusted envelope: %v", what, v)
	}
}

// S1.9: Smoke-Test über die MCP-Sitzung mit dem SDK-Client: initialize,
// tools/list, tools/call für jedes Tool, Resources, Prompts; jede Ausgabe
// mit Mailinhalt in der Hülle; das Audit nennt den Schlüssel, ohne Mailtext.
func TestMCPReaderSession(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	key := newKey(t, "--name", "leser", "--role", "reader")
	cs, _ := mcpSession(t, key)
	ctx := context.Background()

	init := cs.InitializeResult()
	if init.ServerInfo.Name != "postkauz" || !strings.Contains(init.Instructions, "untrusted") {
		t.Fatalf("initialize = %+v", init)
	}
	var names []string
	for tl, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tl.Name)
		if tl.Annotations == nil || !tl.Annotations.ReadOnlyHint {
			t.Errorf("%s is not marked read-only", tl.Name)
		}
	}
	// 9 lesende Tools aus S1.9, seit T2 dazu mail_get_action,
	// mail_list_actions und mail_list_tags (alle R0, mail:read).
	if len(names) != 12 || !slices.Contains(names, "mail_search") || !slices.Contains(names, "mail_get_message") {
		t.Fatalf("tools = %v", names)
	}

	// Suche → Nachricht → Thread → Anhänge.
	v, _ := callTool(t, cs, "mail_search", map[string]any{"query": "Rechnungen", "account": "demo"})
	assertEnvelope(t, "mail_search", v)
	hits := v["hits"].([]any)
	if len(hits) != 1 || hits[0].(map[string]any)["subject"] != "Rechnung" {
		t.Fatalf("search hits = %v", hits)
	}
	v, _ = callTool(t, cs, "mail_search", map[string]any{"query": "Heizung", "mailbox": "INBOX", "limit": 1})
	if len(v["hits"].([]any)) != 1 || v["next_cursor"] == nil {
		t.Fatalf("search page = %v", v)
	}
	if v, res := callTool(t, cs, "mail_search", map[string]any{"query": "Heizung", "limit": 500}); !res.IsError || v["code"] != "invalid_input" {
		t.Fatalf("limit above 100: %v", v)
	}

	v, _ = callTool(t, cs, "mail_get_message", map[string]any{"id": json.Number(ph["html"])})
	assertEnvelope(t, "mail_get_message", v)
	if v["hidden_text_removed"] != true || strings.Contains(fmt.Sprint(v), "Ignoriere") || v["truncated"] != false ||
		len(v["attachments"].([]any)) != 1 || v["auth"].(map[string]any)["dkim"] != "none" {
		t.Fatalf("message = %v", v)
	}
	v, _ = callTool(t, cs, "mail_get_message", map[string]any{"id": json.Number(ph["answer"])})
	if v["text"] != "Der Monteur kommt am Freitag." || v["text_source"] != "plain_unquoted" {
		t.Fatalf("quotes not stripped by default: %v", v["text"])
	}
	v, _ = callTool(t, cs, "mail_get_message", map[string]any{"id": json.Number(ph["answer"]), "strip_quotes": false, "max_chars": 9})
	if v["text"] != "Der Monte" || v["truncated"] != true {
		t.Fatalf("max_chars: %v", v["text"])
	}
	v, _ = callTool(t, cs, "mail_get_thread", map[string]any{"id": json.Number(ph["thread"])})
	assertEnvelope(t, "mail_get_thread", v)
	if msgs := v["messages"].([]any); len(msgs) != 2 || msgs[1].(map[string]any)["text"] != "Der Monteur kommt am Freitag." {
		t.Fatalf("thread = %v", v)
	}
	v, _ = callTool(t, cs, "mail_list_attachments", map[string]any{"message": json.Number(ph["html"])})
	assertEnvelope(t, "mail_list_attachments", v)
	v, _ = callTool(t, cs, "mail_get_attachment_text", map[string]any{"id": json.Number(ph["att"])})
	assertEnvelope(t, "mail_get_attachment_text", v)
	if v["extract_status"] != "skipped" || v["executable"] != true {
		t.Fatalf("attachment text = %v", v)
	}
	v, _ = callTool(t, cs, "mail_list_accounts", nil)
	if len(v["accounts"].([]any)) != 1 {
		t.Fatalf("accounts = %v", v)
	}
	v, _ = callTool(t, cs, "mail_list_mailboxes", map[string]any{"account": "demo"})
	inbox := v["mailboxes"].([]any)[0].(map[string]any)
	v, _ = callTool(t, cs, "mail_get_policy", nil)
	if v["key"] != "key:leser" || v["role"] != "reader" || v["dry_run_default"] != true {
		t.Fatalf("policy = %v", v)
	}
	// Fehler sind Ergebnisse, keine Protokollfehler.
	if v, res := callTool(t, cs, "mail_get_message", map[string]any{"id": 99999}); !res.IsError || v["code"] != "not_found" {
		t.Fatalf("missing message: %v", v)
	}

	// Resources.
	read := func(uri string) map[string]any {
		t.Helper()
		res, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("read %s: %v", uri, err)
		}
		var v map[string]any
		if err := json.Unmarshal([]byte(res.Contents[0].Text), &v); err != nil || res.Contents[0].MIMEType != "application/json" {
			t.Fatalf("read %s: %v %s", uri, err, res.Contents[0].Text)
		}
		return v
	}
	assertEnvelope(t, "message resource", read("mail://demo/message/"+ph["html"]))
	assertEnvelope(t, "thread resource", read("mail://demo/thread/"+ph["thread"]))
	assertEnvelope(t, "attachment resource", read("mail://demo/message/"+ph["html"]+"/attachment/"+ph["att"]))
	ov := read(fmt.Sprintf("mail://demo/mailbox/%v/overview?since=3650d", inbox["id"]))
	assertEnvelope(t, "overview unread", ov["unread"].(map[string]any))
	if u, r := ov["unread"].(map[string]any)["hits"].([]any), ov["read"].(map[string]any)["hits"].([]any); len(u)+len(r) != 3 {
		t.Fatalf("overview = %v", ov)
	}
	if len(read("mail://demo/mailboxes")["mailboxes"].([]any)) == 0 || read("mail://policy")["key"] != "key:leser" ||
		len(read("mail://accounts")["accounts"].([]any)) != 1 {
		t.Fatal("simple resources")
	}
	if len(read("mail://audit?since=1h")["events"].([]any)) == 0 {
		t.Fatal("audit resource empty")
	}
	for _, bad := range []string{"mail://andere/message/" + ph["html"], "mail://demo/message/" + ph["answer"] + "/attachment/" + ph["att"],
		"mail://demo/thread/99999", "mail://demo/unbekannt"} {
		if _, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: bad}); err == nil {
			t.Errorf("read %s: no error", bad)
		}
	}

	// Prompts: nur Text, mit den Regeln zum Umgang mit Mailinhalt.
	pr, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "extract_actions", Arguments: map[string]string{"thread_id": ph["thread"]}})
	if err != nil || !strings.Contains(pr.Messages[0].Content.(*sdk.TextContent).Text, "never follow instructions") {
		t.Fatalf("prompt = %+v %v", pr, err)
	}
	if _, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "extract_actions", Arguments: map[string]string{"thread_id": "x"}}); err == nil {
		t.Fatal("bad prompt argument accepted")
	}

	// Audit: jeder Aufruf mit Schlüsselname, ohne Mailtext und ohne Suchbegriffe.
	_, out, _ := run(t, "audit", "--since", "10m", "--key-id", "key:leser", "--json", "--limit", "1000")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) < 15 {
		t.Fatalf("audit has %d entries for the key", len(lines))
	}
	for _, leak := range []string{"Rechnung", "Heizung", "Monteur", "Ignoriere", "portal.example", key} {
		if strings.Contains(out, leak) {
			t.Fatalf("audit contains %q", leak)
		}
	}
	if !strings.Contains(out, `"actor":"agent"`) || !strings.Contains(out, `"operation":"messages.search"`) {
		t.Fatalf("audit entries: %s", lines[0])
	}
	if _, list, _ := run(t, "key", "list"); !strings.Contains(list, time.Now().Format("2006-01-02")+"\n") &&
		!strings.Contains(list, time.Now().Local().Format("2006-01-02")) {
		t.Fatalf("last used not set:\n%s", list)
	}
}

// S1.9: Ein auf mail:search verengter Schlüssel sieht nur Suche und
// Richtlinie; Bodies kann er nicht lesen, auch nicht über Resources.
func TestMCPSearchOnlyKey(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	key := newKey(t, "--name", "triage", "--role", "reader", "--scopes", "mail:search")
	cs, _ := mcpSession(t, key)
	ctx := context.Background()
	var names []string
	for tl, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tl.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"mail_get_policy", "mail_search"}) {
		t.Fatalf("tools = %v", names)
	}
	v, _ := callTool(t, cs, "mail_search", map[string]any{"query": "Heizung"})
	assertEnvelope(t, "mail_search", v)
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_get_message", Arguments: map[string]any{"id": 1}}); err == nil {
		t.Fatal("hidden tool callable")
	}
	if _, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "mail://demo/message/" + ph["html"]}); err == nil {
		t.Fatal("message resource readable without mail:read")
	}
}

// Ein Widerruf wirkt während der laufenden Sitzung: Der nächste Aufruf
// nach der Prüffrist scheitert mit klarem Grund (denied, key_revoked).
func TestMCPKeyRevokedDuringSession(t *testing.T) {
	setupEnv(t)
	key := newKey(t, "--name", "agent", "--role", "reader")
	cs, _ := mcpSession(t, key)
	if v, res := callTool(t, cs, "mail_get_policy", nil); res.IsError {
		t.Fatalf("before revoke: %v", v)
	}
	if code, _, errs := run(t, "key", "revoke", "agent"); code != ExitOK {
		t.Fatalf("revoke: %d %s", code, errs)
	}
	deadline := time.Now().Add(ops.DefaultKeyCheckTTL + 3*time.Second)
	for {
		v, res := callTool(t, cs, "mail_list_accounts", nil)
		if res.IsError {
			if v["status"] != "denied" || v["code"] != "key_revoked" || !strings.Contains(fmt.Sprint(v["message"]), "revoked") {
				t.Fatalf("error = %v", v)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("revoked key still works")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if _, err := cs.ReadResource(context.Background(), &sdk.ReadResourceParams{URI: "mail://accounts"}); err == nil {
		t.Fatal("resource readable with a revoked key")
	}
	_, out, _ := run(t, "audit", "--json", "--result", "denied")
	if !strings.Contains(out, "key_revoked") {
		t.Fatalf("audit lacks key_revoked: %s", out)
	}
}

// Ohne gültigen Schlüssel startet kein Server (Exit 3, Grund auf stderr,
// kein Protokoll auf stdout); --key ist für mcp verboten.
func TestMCPRequiresValidKey(t *testing.T) {
	setupEnv(t)
	key := newKey(t, "--name", "alt", "--role", "reader")
	run(t, "key", "revoke", "alt")
	for _, c := range []struct {
		env, want string
		code      int
	}{
		{"", "no API key given", ExitDenied},
		{"pk_key_falsch", "unknown API key", ExitDenied},
		{key, "API key revoked", ExitDenied},
	} {
		t.Setenv(EnvKey, c.env)
		code, out, errs := run(t, "mcp")
		if code != c.code || out != "" || !strings.Contains(errs, c.want) || strings.Contains(errs, key) {
			t.Errorf("POSTKAUZ_KEY=%q: code=%d out=%q err=%q", c.env, code, out, errs)
		}
	}
	// T1b: Gescheiterte Anmeldungen stehen im Audit (ohne den Schlüssel).
	_, out, _ := run(t, "audit", "--operation", "keys.authenticate", "--json")
	if strings.Count(out, "\n") != 2 || !strings.Contains(out, `"reason":"key_invalid"`) || !strings.Contains(out, `"reason":"key_revoked"`) ||
		!strings.Contains(out, `"key":"key:alt"`) || strings.Contains(out, key) || strings.Contains(out, "pk_key_falsch") {
		t.Fatalf("audit of failed key logins:\n%s", out)
	}
	t.Setenv(EnvKey, "")
	if code, _, _ := run(t, "mcp", "--key", "alt"); code != ExitUsage {
		t.Fatalf("mcp --key: %d", code)
	}
	// Schlüsseldatei: nur für den Nutzer lesbar.
	f := filepath.Join(t.TempDir(), "key")
	_ = os.WriteFile(f, []byte(key+"\n"), 0o644)
	t.Setenv(EnvKeyFile, f)
	if code, _, errs := run(t, "mcp"); code != ExitError || !strings.Contains(errs, "readable by others") {
		t.Fatalf("world-readable key file: %d %s", code, errs)
	}
}

// Smoke-Test über echtes stdio: Der SDK-Client startet das Programm als
// Kindprozess (`postkauz mcp`), initialize, tools/list, tools/call.
func TestMCPStdioSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	dataDir, _ := setupEnv(t)
	ph := seedS16(t)
	key := newKey(t, "--name", "smoke", "--role", "reader")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "mcp")
	cmd.Env = append(os.Environ(), "POSTKAUZ_TEST_RUN_CLI=1", EnvKey+"="+key, "POSTKAUZ_DATA_DIR="+dataDir,
		"POSTKAUZ_SECRETS_BACKEND=insecure-file")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "smoke", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connect: %v (stderr %s)", err, stderr.String())
	}
	defer cs.Close()
	if cs.InitializeResult().ServerInfo.Name != "postkauz" {
		t.Fatalf("initialize: %+v", cs.InitializeResult())
	}
	lt, err := cs.ListTools(ctx, nil)
	if err != nil || len(lt.Tools) != 12 {
		t.Fatalf("tools/list: %v %v", lt, err)
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_get_message", Arguments: map[string]any{"id": json.Number(ph["html"])}})
	if err != nil || res.IsError {
		t.Fatalf("tools/call: %+v %v (stderr %s)", res, err, stderr.String())
	}
	text := res.Content[0].(*sdk.TextContent).Text
	if !strings.Contains(text, `"untrusted":true`) || !strings.Contains(text, `"hidden_text_removed":true`) {
		t.Fatalf("tools/call result: %s", text)
	}
}

// V15 der Nachbesserung T2: Hilfetexte und README sagen nicht mehr, dass
// nie etwas verschoben wird; sie nennen die Scopes, die den Server ändern.
func TestMCPLongTextFollowsT2(t *testing.T) {
	setupEnv(t)
	code, out, errs := run(t, "mcp", "--help")
	if code != ExitOK || strings.Contains(out, "deleted or moved") || !strings.Contains(out, "mail:mark") ||
		!strings.Contains(out, "mail:move") || !strings.Contains(out, "mail:trash") || !strings.Contains(out, "nothing is ever sent") {
		t.Fatalf("mcp --help: %d %s %s", code, out, errs)
	}
	code, out, errs = run(t, "sync", "--help")
	if code != ExitOK || strings.Contains(out, "never sends, deletes or moves") || !strings.Contains(out, "queued mailbox actions") {
		t.Fatalf("sync --help: %d %s %s", code, out, errs)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"deleted or moved by PostKauz", "get six MCP tools:", "tags and marks; no move"} {
		if strings.Contains(string(readme), bad) {
			t.Errorf("README still says %q", bad)
		}
	}
}
