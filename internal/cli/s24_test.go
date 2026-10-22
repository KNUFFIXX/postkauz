package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// S2.4: Prüf-Hook über die CLI wie in der Abnahme D4 (Schritte 2 und 3):
// Hook aus der Policy-Datei, bestanden → Vorlage, gescheitert → Ausgabe des
// Hooks in `draft request`, Entwurf bleibt draft; im Audit und im Log nur
// das Ergebnis; der Hook sieht weder POSTKAUZ_KEY noch ein Kontopasswort.
// Als Hook dient die Testbinärdatei selbst (TestCLIHookHelper).

// hookReasonCanary steht in der Begründung des Fake-Hooks; sie darf nur in
// der Ausgabe von `draft request` erscheinen.
const hookReasonCanary = "HookBegruendungKanarie"

// TestCLIHookHelper ist kein Test, sondern der Fake-Hook (Rolle nach "--").
func TestCLIHookHelper(t *testing.T) {
	var a []string
	for i, v := range os.Args {
		if v == "--" && i+1 < len(os.Args) {
			a = os.Args[i+1:]
			break
		}
	}
	if len(a) < 4 || a[0] != "lint" {
		return
	}
	// lint <envfile> <meta_json> <body_text>
	_ = os.WriteFile(a[1], []byte(strings.Join(os.Environ(), "\n")), 0o600)
	var meta struct {
		Subject string `json:"subject"`
		Account string `json:"account"`
	}
	b, err := os.ReadFile(a[2])
	if err != nil || json.Unmarshal(b, &meta) != nil || meta.Account != "demo" {
		os.Stdout.WriteString("meta.json unreadable\n")
		os.Exit(2)
	}
	if text, err := os.ReadFile(a[3]); err != nil || !strings.HasPrefix(string(text), "Guten Tag") {
		os.Stdout.WriteString("body text unreadable or without greeting\n")
		os.Exit(2)
	}
	if strings.Contains(meta.Subject, "TODO") {
		os.Stdout.WriteString("subject contains TODO (" + hookReasonCanary + ")\n")
		os.Stderr.WriteString("second line on stderr\n")
		os.Exit(1)
	}
	os.Exit(0)
}

// writePolicy schreibt eine Policy-Datei mit check_hook und setzt
// POSTKAUZ_POLICY.
func writePolicy(t *testing.T, dir string, hook []string) {
	t.Helper()
	quoted := make([]string, len(hook))
	for i, h := range hook {
		quoted[i] = "'" + h + "'" // TOML-Literal: Backslashes unter Windows bleiben
	}
	p := filepath.Join(dir, "policy.toml")
	body := "[draft]\ncheck_hook = [" + strings.Join(quoted, ", ") + "]\ncheck_hook_timeout = \"60s\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTKAUZ_POLICY", p)
}

func TestDraftRequestRunsCheckHook(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	dataDir, _ := setupEnv(t)
	ph := seedS16(t)
	const keyCanary, pwCanary = "CANARY-s24-key-41d7", "CANARY-s24-pw-9e03"
	t.Setenv("POSTKAUZ_KEY", keyCanary)
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", pwCanary)
	dir := t.TempDir()
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	envFile := filepath.Join(dir, "hook-env.txt")
	writePolicy(t, dir, []string{self, "-test.run=^TestCLIHookHelper$", "--", "lint", envFile, "{meta_json}", "{body_text}"})
	answer := filepath.Join(dir, "antwort.md")
	if err := os.WriteFile(answer, []byte("Guten Tag,\n\nder Termin passt."), 0o600); err != nil {
		t.Fatal(err)
	}
	step := func(want int, args ...string) string {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out
	}

	step(ExitOK, "draft", "reply", ph["answer"], "--body-file", answer)
	// D4.2: bestanden.
	out := step(ExitOK, "draft", "request", "1")
	if !strings.Contains(out, "hook: pass\n") || !strings.Contains(out, "draft 1: status pending_approval, approval 1") {
		t.Fatalf("request (pass): %s", out)
	}
	// D4.3: Betreff mit TODO → fail mit Begründung, Status bleibt draft.
	step(ExitOK, "draft", "edit", "1", "--subject", "TODO Re: Heizung defekt")
	out = step(ExitOK, "draft", "request", "1")
	for _, want := range []string{"hook: fail\n", "subject contains TODO (" + hookReasonCanary + ")", "second line on stderr",
		"draft 1: status stays draft; fix the draft and request again"} {
		if !strings.Contains(out, want) {
			t.Fatalf("request (fail) lacks %q:\n%s", want, out)
		}
	}
	out = step(ExitOK, "draft", "request", "1", "--json")
	var r ops.DraftsRequestOutput
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Hook != "fail" || r.Status != "draft" || r.ApprovalID != 0 ||
		!strings.Contains(r.HookOutput, hookReasonCanary) || r.HookOutputTruncated {
		t.Fatalf("request --json: %v %s", err, out)
	}
	// Trockenlauf: Hook läuft mit (Festlegung S2.3), nichts wird gespeichert.
	out = step(ExitOK, "draft", "request", "1", "--dry-run")
	if !strings.Contains(out, "dry run: hook: fail") || !strings.Contains(out, hookReasonCanary) {
		t.Fatalf("request --dry-run: %s", out)
	}
	if out = step(ExitOK, "draft", "show", "1"); !strings.Contains(out, "hook:        fail") || !strings.Contains(out, "status draft\n") {
		t.Fatalf("draft show: %s", out)
	}

	// Kanarientest: Umgebung des Hooks ohne Schlüssel und Passwort.
	env, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{keyCanary, pwCanary, canary, "POSTKAUZ_KEY", "POSTKAUZ_PASSWORD", "POSTKAUZ_DATA_DIR"} {
		if strings.Contains(string(env), bad) {
			t.Fatalf("hook environment contains %q", bad)
		}
	}
	if !strings.Contains(string(env), "POSTKAUZ_DRAFT_META_JSON=") {
		t.Fatalf("hook environment lacks the file paths:\n%s", env)
	}

	// Audit und Log: nur das Ergebnis, nie die Begründung.
	out = step(ExitOK, "audit", "--since", "10m", "--json", "--limit", "1000")
	if strings.Contains(out, hookReasonCanary) || strings.Contains(out, "second line") || !strings.Contains(out, `"hook":"fail"`) {
		t.Fatalf("audit: %s", out)
	}
	logb, err := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logb), hookReasonCanary) || strings.Contains(string(logb), "second line") {
		t.Fatal("log contains the hook output")
	}

	// Programm fehlt → error, nicht pass; der Pfad steht nicht in der Meldung.
	writePolicy(t, dir, []string{filepath.Join(dir, "no-such-linter"), "{eml}"})
	step(ExitOK, "draft", "edit", "1", "--subject", "Re: Heizung defekt")
	out = step(ExitOK, "draft", "request", "1")
	if !strings.Contains(out, "hook: error") || !strings.Contains(out, "check hook error: program not found") ||
		strings.Contains(out, "no-such-linter") || !strings.Contains(out, "status stays draft") {
		t.Fatalf("request (missing program): %s", out)
	}
}
