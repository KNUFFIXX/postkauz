package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.8: MCP-Sitzung mit einem assistant-Schlüssel über `postkauz mcp`:
// Entwurf anlegen (Trockenlauf und echt), ändern, vorlegen, Freigabe
// abfragen; eigener Text als source "draft", Zitat und Hook-Ausgabe in der
// Untrusted-Hülle; Freigeben und Senden gibt es nicht; das Audit enthält
// keinen Entwurfsinhalt.
func TestMCPAssistantSession(t *testing.T) {
	dataDir, _ := setupEnv(t)
	ph := seedS16(t)
	const subjCanary, textCanary, styleCanary = "KanarienBetreff-S28", "KanarienText-S28", "StilKanarie-S28"
	dir := t.TempDir()
	sig, style := filepath.Join(dir, "sig.txt"), filepath.Join(dir, "stil.md")
	_ = os.WriteFile(sig, []byte("Viele Grüße\nDemo"), 0o600)
	_ = os.WriteFile(style, []byte("Sie-Form, kurz. "+styleCanary+"\n"), 0o600)
	if code, out, errs := run(t, "identity", "add", "-a", "demo", "--email", "demo@example.org", "--name", "Demo",
		"--signature-file", sig, "--style-file", style); code != ExitOK {
		t.Fatalf("identity add: %d %s %s", code, out, errs)
	}
	withHook := !testing.Short()
	if withHook {
		// Prüf-Hook wie in S2.4: scheitert bei TODO im Betreff.
		self, err := filepath.Abs(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		writePolicy(t, dir, []string{self, "-test.run=^TestCLIHookHelper$", "--", "lint", filepath.Join(dir, "env.txt"),
			"{meta_json}", "{body_text}"})
	}
	key := newKey(t, "--name", "assistent", "--role", "assistant")
	cs, stderr := mcpSession(t, key)
	ctx := context.Background()

	init := cs.InitializeResult()
	if !strings.Contains(init.Instructions, "can create drafts in the Drafts folder") ||
		!strings.Contains(init.Instructions, "It cannot move, delete") || !strings.Contains(init.Instructions, "Nothing is ever sent") ||
		!strings.Contains(init.Instructions, "untrusted") {
		t.Fatalf("instructions: %s", init.Instructions)
	}
	var names []string
	for tl, err := range cs.Tools(ctx, nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tl.Name)
		if strings.Contains(tl.Name, "draft") || strings.Contains(tl.Name, "approval") || tl.Name == "mail_request_ready" {
			if !strings.Contains(tl.Description, "never sends") {
				t.Errorf("%s: description lacks never sends", tl.Name)
			}
		}
	}
	// 15 aus S2.8, seit T2 dazu mail_get_action, mail_list_actions,
	// mail_list_tags, mail_tag und mail_mark; nie mail_move/mail_trash.
	if len(names) != 20 || !slices.Contains(names, "mail_create_draft") || !slices.Contains(names, "mail_request_ready") {
		t.Fatalf("tools = %v", names)
	}
	for _, n := range names {
		for _, bad := range []string{"approve", "reject", "send", "delete", "move", "trash", "abandon", "sent"} {
			if strings.Contains(n, bad) {
				t.Fatalf("tool %s", n)
			}
		}
	}
	if _, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "mail_approve", Arguments: map[string]any{"approval_id": 1}}); err == nil {
		t.Fatal("an approve tool exists")
	}

	envelope := func(what string, v any) map[string]any {
		t.Helper()
		m, _ := v.(map[string]any)
		if m == nil || m["untrusted"] != true || m["source"] != "email" || m["notice"] != ops.UntrustedNotice {
			t.Fatalf("%s: not in the untrusted envelope: %v", what, v)
		}
		return m
	}
	answer := json.Number(ph["answer"])
	args := map[string]any{"kind": "reply", "in_reply_to": answer, "subject": "Re: " + subjCanary,
		"body_markdown": "Guten Tag,\n\n" + textCanary}

	// Trockenlauf: Der Schlüssel hat dry_run_default; ohne dry_run wird nichts gespeichert.
	v, _ := callTool(t, cs, "mail_create_draft", args)
	if v["dry_run"] != true || v["draft_id"] != nil || v["source"] != "draft" || !strings.Contains(v["preview"].(map[string]any)["text"].(string), textCanary) {
		t.Fatalf("dry run (key default): %v", v)
	}
	q := envelope("create original", v["original"])
	if q["part"] != "quote" || !strings.Contains(q["text"].(string), "> Der Monteur kommt am Freitag.") || strings.Contains(q["text"].(string), textCanary) {
		t.Fatalf("original = %v", q)
	}
	args["dry_run"] = true
	if v, _ = callTool(t, cs, "mail_create_draft", args); v["dry_run"] != true || v["draft_id"] != nil {
		t.Fatalf("dry run: %v", v)
	}
	if _, out, _ := run(t, "draft", "list"); !strings.Contains(out, "No drafts.") {
		t.Fatalf("dry run saved a draft: %s", out)
	}
	if v, res := callTool(t, cs, "mail_create_draft", map[string]any{"kind": "reply", "in_reply_to": answer, "dry_run": "no"}); !res.IsError || v["code"] != "invalid_input" {
		t.Fatalf("dry_run not boolean: %v", v)
	}

	// Echt: remote pending mit dem Weg (E1: der MCP-Prozess legt nichts ab).
	args["dry_run"] = false
	v, _ = callTool(t, cs, "mail_create_draft", args)
	if v["dry_run"] != false || v["draft_id"] != 1.0 || v["status"] != "draft" || v["remote"] != "pending" ||
		!strings.Contains(v["remote_note"].(string), "after the next sync") || !strings.HasPrefix(v["message_id"].(string), "pk.") {
		t.Fatalf("create = %v", v)
	}
	envelope("create original", v["original"])

	// Ändern: wieder mit Zitat in der Hülle.
	v, _ = callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "body_markdown": "Guten Tag,\n\n" + textCanary + " geändert",
		"dry_run": false})
	if v["changed"] != true || v["status"] != "draft" || v["source"] != "draft" {
		t.Fatalf("update = %v", v)
	}
	v, _ = callTool(t, cs, "mail_get_draft", map[string]any{"draft_id": 1})
	if v["source"] != "draft" || !strings.Contains(v["body_text"].(string), textCanary+" geändert") ||
		!strings.Contains(v["body_text"].(string), "Viele Grüße") || strings.Contains(v["body_text"].(string), "> ") {
		t.Fatalf("get = %v", v)
	}
	envelope("get original", v["original"])
	// Die Rohnachricht (eml) gibt es über MCP nicht.
	if v, res := callTool(t, cs, "mail_get_draft", map[string]any{"draft_id": 1, "eml": true}); !res.IsError || v["code"] != "invalid_input" {
		t.Fatalf("eml over MCP: %v", v)
	}
	v, _ = callTool(t, cs, "mail_list_drafts", map[string]any{"account": "demo"})
	if v["source"] != "draft" || len(v["drafts"].([]any)) != 1 {
		t.Fatalf("list = %v", v)
	}

	// Resources: Entwurf (nur unter seinem Konto) und Stil (Quelle config, keine Hülle).
	read := func(uri string) map[string]any {
		t.Helper()
		res, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: uri})
		if err != nil {
			t.Fatalf("read %s: %v", uri, err)
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(res.Contents[0].Text), &m); err != nil {
			t.Fatalf("read %s: %v", uri, err)
		}
		return m
	}
	if d := read("mail://demo/draft/1"); d["draft_id"] != 1.0 || d["source"] != "draft" {
		t.Fatalf("draft resource = %v", d)
	} else {
		envelope("draft resource", d["original"])
	}
	for _, bad := range []string{"mail://andere/draft/1", "mail://demo/draft/99", "mail://style/fremd@example.net"} {
		if _, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: bad}); err == nil {
			t.Errorf("read %s: no error", bad)
		}
	}
	st := read("mail://style/demo@example.org")
	if st["source"] != "config" || st["untrusted"] != nil || !strings.Contains(st["text"].(string), styleCanary) || st["has_style_file"] != true {
		t.Fatalf("style resource = %v", st)
	}
	if code, out, _ := run(t, "draft", "style", "-a", "demo"); code != ExitOK || !strings.Contains(out, styleCanary) || strings.Contains(out, style) {
		t.Fatalf("draft style: %d %s", code, out)
	}

	// Prompt draft_reply.
	pr, err := cs.GetPrompt(ctx, &sdk.GetPromptParams{Name: "draft_reply", Arguments: map[string]string{"message_id": ph["answer"],
		"intent": "Termin passt"}})
	if err != nil || !strings.Contains(pr.Messages[0].Content.(*sdk.TextContent).Text, "mail_create_draft") {
		t.Fatalf("prompt = %+v %v", pr, err)
	}

	// Prüf-Hook scheitert: Ausgabe in der eigenen Hülle, Entwurf bleibt draft.
	if withHook {
		callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "subject": "TODO " + subjCanary, "dry_run": false})
		v, _ = callTool(t, cs, "mail_request_ready", map[string]any{"draft_id": 1, "dry_run": false})
		h, _ := v["hook_output"].(map[string]any)
		if v["hook"] != "fail" || v["status"] != "draft" || h == nil || h["untrusted"] != true || h["source"] != "check_hook" ||
			!strings.Contains(h["text"].(string), hookReasonCanary) {
			t.Fatalf("request (hook fail) = %v", v)
		}
		callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "subject": "Re: " + subjCanary, "dry_run": false})
	}

	// Vorlegen (Trockenlauf, dann echt), Freigabe abfragen.
	if v, _ = callTool(t, cs, "mail_request_ready", map[string]any{"draft_id": 1, "dry_run": true}); v["dry_run"] != true || v["approval_id"] != nil {
		t.Fatalf("request dry run = %v", v)
	}
	v, _ = callTool(t, cs, "mail_request_ready", map[string]any{"draft_id": 1, "dry_run": false})
	if v["status"] != "pending_approval" || v["hook"] != "pass" || v["approval_id"] != 1.0 || v["hook_output"] != nil {
		t.Fatalf("request = %v", v)
	}
	// A2 (T1b): Das Ergebnis sagt, wie der Mensch freigibt.
	if s, _ := v["next_step"].(string); v["human_only"] != true || !strings.Contains(s, "postkauz approve 1") ||
		!strings.Contains(s, "cannot approve or send") {
		t.Fatalf("request next_step = %v / %v", v["next_step"], v["human_only"])
	}
	v, _ = callTool(t, cs, "mail_get_approval", map[string]any{"approval_id": 1})
	if v["status"] != "pending" || v["draft_status"] != "pending_approval" || v["decided_by"] != nil {
		t.Fatalf("approval = %v", v)
	}
	// Nur der Mensch gibt frei; eine Änderung danach ersetzt die Freigabe.
	if code, _, errs := run(t, "approve", "1", "--key", "assistent"); code != ExitDenied || !strings.Contains(errs, "human_only") {
		t.Fatalf("approve with key: %d %s", code, errs)
	}
	v, _ = callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "body_markdown": "Guten Tag,\n\nneu", "dry_run": false})
	if v["status"] != "draft" || v["approval_superseded"] != 1.0 {
		t.Fatalf("update after request = %v", v)
	}
	if v, _ = callTool(t, cs, "mail_get_approval", map[string]any{"approval_id": 1}); v["status"] != "superseded" {
		t.Fatalf("approval after change = %v", v)
	}
	// Nach dem Verwerfen durch den Menschen: draft_closed.
	if code, _, errs := run(t, "draft", "abandon", "1"); code != ExitOK {
		t.Fatalf("abandon: %d %s", code, errs)
	}
	if v, res := callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "subject": "x", "dry_run": false}); !res.IsError ||
		v["status"] != "denied" || v["reason"] != "draft_closed" {
		t.Fatalf("update closed draft = %v", v)
	}

	// Audit: jeder Aufruf mit Schlüssel und Akteur agent, ohne Entwurfsinhalt.
	_, out, _ := run(t, "audit", "--since", "10m", "--key-id", "key:assistent", "--json", "--limit", "1000")
	for _, op := range []string{"drafts.create", "drafts.update", "drafts.get", "drafts.list", "drafts.request_ready", "approvals.get",
		"drafts.style"} {
		if !strings.Contains(out, `"operation":"`+op+`"`) {
			t.Errorf("audit lacks %s", op)
		}
	}
	if !strings.Contains(out, `"actor":"agent"`) || !strings.Contains(out, `"dry_run":true`) {
		t.Fatalf("audit entries: %s", out)
	}
	logb, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	for _, leak := range []string{subjCanary, textCanary, styleCanary, hookReasonCanary, "Monteur", "demo@example.org", style, key} {
		if strings.Contains(out, leak) {
			t.Errorf("audit contains %q", leak)
		}
		if strings.Contains(string(logb), leak) || strings.Contains(stderr.String(), leak) {
			t.Errorf("log contains %q", leak)
		}
	}
}
