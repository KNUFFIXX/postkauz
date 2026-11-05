package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A06 Teil 1: Ein Entwurf mit Empfängernamen voller Sonderzeichen lässt
// sich über CLI und MCP ändern und vorlegen; beide zeigen den Namen lesbar
// und, wo nötig, in Anführungszeichen (kein encoded-word). Vorher gab
// draft show den Namen ungequotet aus, und draft edit bzw. draft request
// scheiterten mit invalid_address, weil der Neubau den Text wieder parste.
func TestA06AddressNamesCLIAndMCP(t *testing.T) {
	setupEnv(t)
	seedS16(t)
	step := func(want int, args ...string) string {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out + errs
	}
	body := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(body, []byte("Guten Tag"), 0o600); err != nil {
		t.Fatal(err)
	}
	const wantTo = `to:          "Nachname, Vorname" <h@example.com>, x@example.com`
	const wantCc = `cc:          "Müller (Büro)" <m@example.com>`
	out := step(ExitOK, "draft", "new", "-a", "demo", "--to", `"Nachname, Vorname" <h@example.com>`, "--to", `"x@example.com" <x@example.com>`,
		"--cc", `"Müller (Büro)" <m@example.com>`, "--subject", "Heizung", "--body-file", body)
	if !strings.Contains(out, wantTo) || !strings.Contains(out, wantCc) || strings.Contains(out, "=?") {
		t.Fatalf("draft new:\n%s", out)
	}
	if out := step(ExitOK, "draft", "edit", "1", "--subject", "Heizung Haus 2"); !strings.Contains(out, wantTo) {
		t.Fatalf("draft edit:\n%s", out)
	}
	if out := step(ExitOK, "draft", "request", "1", "--dry-run"); strings.Contains(out, "invalid_address") {
		t.Fatalf("draft request --dry-run:\n%s", out)
	}
	if out := step(ExitOK, "draft", "show", "1"); !strings.Contains(out, wantTo) || !strings.Contains(out, wantCc) {
		t.Fatalf("draft show:\n%s", out)
	}
	var shown struct {
		To []string `json:"to"`
		Cc []string `json:"cc"`
	}
	if out := step(ExitOK, "draft", "show", "1", "--json"); json.Unmarshal([]byte(out), &shown) != nil ||
		len(shown.To) != 2 || shown.To[0] != `"Nachname, Vorname" <h@example.com>` || shown.To[1] != "x@example.com" ||
		len(shown.Cc) != 1 || shown.Cc[0] != `"Müller (Büro)" <m@example.com>` {
		t.Fatalf("draft show --json: %+v\n%s", shown, out)
	}

	// MCP: Der Agent bekommt dieselben Texte und kann sie zurückgeben.
	key := newKey(t, "--name", "assistent", "--role", "assistant")
	cs, _ := mcpSession(t, key)
	v, _ := callTool(t, cs, "mail_get_draft", map[string]any{"draft_id": 1})
	to, _ := v["to"].([]any)
	if len(to) != 2 || to[0] != `"Nachname, Vorname" <h@example.com>` || to[1] != "x@example.com" {
		t.Fatalf("mail_get_draft to = %v", v["to"])
	}
	v, res := callTool(t, cs, "mail_update_draft", map[string]any{"draft_id": 1, "to": to, "subject": "Heizung Haus 3", "dry_run": false})
	if res.IsError || v["changed"] != true {
		t.Fatalf("mail_update_draft with the shown to: %v", v)
	}
	if v, res := callTool(t, cs, "mail_request_ready", map[string]any{"draft_id": 1, "dry_run": true}); res.IsError {
		t.Fatalf("mail_request_ready (dry run): %v", v)
	}
}
