package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.3: Entwürfe über die CLI wie in der Abnahme D4 (Grundform; S2.7
// erweitert die Kommandos). Texte und Exit-Codes, die das Abnahmeskript
// vergleicht, stehen hier fest.
func TestDraftCommandsLikeD4(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	const word = "Entwurfswortkanarie"
	answer := write("antwort.md", "Guten Tag,\n\nder Termin passt. "+word)
	other := write("anders.md", "Guten Tag,\n\nder Termin passt leider nicht.")
	step := func(want int, args ...string) (string, string) {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out, errs
	}

	// Identität mit Signatur.
	sig := write("signatur.txt", "Mit freundlichen Grüßen\nDemo")
	out, _ := step(ExitOK, "identity", "add", "-a", "demo", "--email", "demo@example.org", "--name", "Demo Büro", "--signature-file", sig)
	if !strings.Contains(out, "identity 1 of account demo: Demo Büro <demo@example.org> (signature text, default)") {
		t.Fatalf("identity add: %s", out)
	}
	out, _ = step(ExitOK, "identity", "list")
	if !strings.Contains(out, "demo@example.org") || !strings.Contains(out, "yes") {
		t.Fatalf("identity list: %s", out)
	}

	// D4.1: Antwort anlegen.
	out, _ = step(ExitOK, "draft", "reply", ph["answer"], "--body-file", answer)
	for _, want := range []string{"draft 1 created (status draft, remote pending)", "subject:     AW: Heizung defekt",
		"in-reply-to: <antwort@example.org>", "to:          Bert <b@example.org>", "Mit freundlichen Grüßen", "> Der Monteur kommt am Freitag.",
		"remote: " + ops.RemoteNotePending, "nothing is sent"} {
		if !strings.Contains(out, want) {
			t.Fatalf("draft reply lacks %q:\n%s", want, out)
		}
	}
	out, _ = step(ExitOK, "draft", "show", "1")
	if !strings.Contains(out, "references:  <antwort@example.org>") || !strings.Contains(out, word) {
		t.Fatalf("draft show: %s", out)
	}
	// D4.2: vorlegen.
	out, _ = step(ExitOK, "draft", "request", "1")
	if !strings.Contains(out, "hook: pass") || !strings.Contains(out, "draft 1: status pending_approval, approval 1") {
		t.Fatalf("draft request: %s", out)
	}
	// D4.4: freigeben, ändern, alte Freigabe ist veraltet.
	out, _ = step(ExitOK, "approve", "1")
	if !strings.Contains(out, "approval 1 approved by") || !strings.Contains(out, "draft 1 is ready") {
		t.Fatalf("approve: %s", out)
	}
	out, _ = step(ExitOK, "draft", "edit", "1", "--body-file", other)
	if !strings.Contains(out, "draft 1 changed (status ready -> draft, remote pending)") || !strings.Contains(out, "approval 1 superseded") {
		t.Fatalf("draft edit: %s", out)
	}
	_, errs := step(ExitDenied, "approve", "1")
	if !strings.Contains(errs, "approval_stale") {
		t.Fatalf("approve stale: %s", errs)
	}
	_, errs = step(ExitDenied, "approve", "1", "--json")
	if !strings.Contains(errs, `"code":"denied"`) || !strings.Contains(errs, "approval_stale") {
		t.Fatalf("approve stale --json: %s", errs)
	}
	// D4.5: Schlüssel sieht human_only, approve mit Schlüssel verweigert.
	step(ExitOK, "key", "create", "--name", "test-agent", "--role", "reader")
	step(ExitOK, "key", "create", "--name", "test-assistant", "--role", "assistant")
	out, _ = step(ExitOK, "policy", "show", "--key", "test-agent")
	if !strings.Contains(out, "drafts.approve: human_only") {
		t.Fatalf("policy show: %s", out)
	}
	out, _ = step(ExitOK, "draft", "request", "1")
	if !strings.Contains(out, "approval 2") {
		t.Fatalf("request again: %s", out)
	}
	for _, k := range []string{"test-agent", "test-assistant"} {
		_, errs = step(ExitDenied, "approve", "2", "--key", k)
		if !strings.Contains(errs, "denied (human_only)") {
			t.Fatalf("approve --key %s: %s", k, errs)
		}
	}
	// Assistent darf anlegen und vorlegen, aber nicht verwerfen.
	out, _ = step(ExitOK, "draft", "new", "-a", "demo", "--to", "a@example.org", "--subject", "Neu", "--key", "test-assistant", "--dry-run=false")
	if !strings.Contains(out, "draft 2 created") {
		t.Fatalf("draft new with key: %s", out)
	}
	_, errs = step(ExitDenied, "draft", "abandon", "2", "--key", "test-assistant")
	if !strings.Contains(errs, "human_only") {
		t.Fatalf("abandon with key: %s", errs)
	}
	_, errs = step(ExitDenied, "draft", "new", "-a", "demo", "--key", "test-agent")
	if !strings.Contains(errs, "missing_scope") {
		t.Fatalf("reader creates a draft: %s", errs)
	}
	out, _ = step(ExitOK, "draft", "list")
	if !strings.Contains(out, "pending_approval") || !strings.Contains(out, "Neu") {
		t.Fatalf("draft list: %s", out)
	}
	out, _ = step(ExitOK, "approval", "show", "2")
	if !strings.Contains(out, "approval 2: pending (draft 1, status pending_approval)") {
		t.Fatalf("approval show: %s", out)
	}
	out, _ = step(ExitOK, "reject", "2", "--reason", "Ton zu knapp")
	if !strings.Contains(out, "approval 2 rejected") || !strings.Contains(out, "draft 1 is draft") {
		t.Fatalf("reject: %s", out)
	}
	// D4.9: verwerfen, danach Änderung draft_closed.
	out, _ = step(ExitOK, "draft", "abandon", "1")
	if !strings.Contains(out, "draft 1: abandoned (was draft)") {
		t.Fatalf("abandon: %s", out)
	}
	_, errs = step(ExitDenied, "draft", "edit", "1", "--subject", "x")
	if !strings.Contains(errs, "draft_closed") {
		t.Fatalf("edit closed: %s", errs)
	}
	out, _ = step(ExitOK, "draft", "mark-sent", "2")
	if !strings.Contains(out, "draft 2: sent_by_human (was draft)") {
		t.Fatalf("mark-sent: %s", out)
	}
	step(ExitError, "draft", "show", "99")
	step(ExitUsage, "draft", "edit", "2")

	// D4.6: Audit mit Übergängen, ohne Inhalt.
	out, _ = step(ExitOK, "audit", "--since", "10m", "--json", "--limit", "1000")
	for _, leak := range []string{word, "Termin", "Heizung", "Ton zu knapp", "b@example.org", "Demo Büro"} {
		if strings.Contains(out, leak) {
			t.Fatalf("audit contains %q", leak)
		}
	}
	var transitions int
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var ev struct {
			Actor, Operation, Reason string
			Params                   map[string]any
		}
		if json.Unmarshal([]byte(l), &ev) == nil && ev.Reason == "transition" {
			transitions++
			if ev.Params["draft_id"] == nil || ev.Params["to"] == nil {
				t.Errorf("transition without draft id or status: %s", l)
			}
		}
	}
	if transitions < 9 {
		t.Fatalf("transitions in the audit: %d\n%s", transitions, out)
	}
	out, _ = step(ExitOK, "audit", "--since", "10m", "--operation", "drafts.request_ready")
	if !strings.Contains(out, `"hook":"pass"`) || !strings.Contains(out, "draft:1") {
		t.Fatalf("audit text: %s", out)
	}
}
