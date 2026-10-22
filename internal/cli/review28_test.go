package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Gegenprüfung S2.8 über CLI und MCP.

// W1: Über MCP (Tool und Resource) gilt für einen Entwurf das Zeichenbudget
// der Policy-Datei; die CLI zeigt ohne --max-chars alles.
func TestReviewW1DraftBudgetOverMCP(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	dir := t.TempDir()
	pol := filepath.Join(dir, "policy.toml")
	if err := os.WriteFile(pol, []byte("[agent_defaults]\nmax_chars = 100\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTKAUZ_POLICY", pol)
	own := strings.Repeat("Eigener Text. ", 30)
	body := filepath.Join(dir, "text.md")
	if err := os.WriteFile(body, []byte(own), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "draft", "forward", ph["answer"], "--to", "z@example.org", "--body-file", body); code != ExitOK {
		t.Fatalf("draft reply: %d %s %s", code, out, errs)
	}
	check := func(what string, v map[string]any) {
		t.Helper()
		orig, _ := v["original"].(map[string]any)
		if v["body_text_truncated"] != true || utf8.RuneCountInString(v["body_text"].(string)) != 100 || v["body_markdown"] != nil ||
			v["body_source_omitted"] != true || orig == nil || orig["truncated"] != true || utf8.RuneCountInString(orig["text"].(string)) != 100 {
			t.Fatalf("%s: %v", what, v)
		}
	}
	cs, _ := mcpSession(t, newKey(t, "--name", "assistent", "--role", "assistant"))
	v, _ := callTool(t, cs, "mail_get_draft", map[string]any{"draft_id": 1})
	check("mail_get_draft", v)
	res, err := cs.ReadResource(t.Context(), &sdk.ReadResourceParams{URI: "mail://demo/draft/1"})
	if err != nil {
		t.Fatal(err)
	}
	var r map[string]any
	if err := json.Unmarshal([]byte(res.Contents[0].Text), &r); err != nil {
		t.Fatal(err)
	}
	check("resource", r)

	// CLI: ohne --max-chars ungekürzt, mit --max-chars gekürzt und markiert.
	_, out, _ := run(t, "draft", "show", "1", "--json")
	if strings.Contains(out, "body_text_truncated") || strings.Contains(out, "body_source_omitted") || !strings.Contains(out, strings.TrimSpace(own)) {
		t.Fatalf("draft show --json: %s", out)
	}
	if _, out, _ = run(t, "draft", "show", "1", "--max-chars", "10"); !strings.Contains(out, "[cut after --max-chars]") {
		t.Fatalf("draft show --max-chars: %s", out)
	}
}
