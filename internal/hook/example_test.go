package hook_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/hook"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Die Beispiel-Hooks aus deploy/hooks prüfen die dokumentierte Regel:
// Betreff mit „TODO" oder fehlende Anrede → fail. meta.json kommt aus
// ops.HookMeta, also genau in der Form, die der Runner im Betrieb schreibt.
// check-example.sh läuft überall mit sh; check-example.ps1 nur, wenn
// powershell.exe (Windows PowerShell 5.1) oder pwsh auf PATH liegt.

type exampleCase struct {
	name, subject, text, want, says string
}

var exampleCases = []exampleCase{
	{"greeting ok", "Re: Termin", "Hallo Anna,\n\nder Termin passt.", hook.Pass, ""},
	{"todo in subject", "TODO Re: Termin", "Hallo Anna,\n\nder Termin passt.", hook.Fail, "subject contains TODO"},
	{"todo inside quotes", `Fwd: "Plan" TODO`, "Guten Tag,\nanbei.", hook.Fail, "subject contains TODO"},
	{"lower-case todo passes", "Re: todo-Liste", "Guten Tag,\nanbei.", hook.Pass, ""},
	{"greeting missing", "Re: Termin", "Der Termin passt.\n\nOn Wed, … wrote:", hook.Fail, "greeting missing"},
	{"both problems", "TODO", "Termin passt.", hook.Fail, "greeting missing"},
	{"empty text", "Re: Termin", "", hook.Fail, "greeting missing"},
	{"blank lines and spaces first", "Re: Termin", "\r\n\r\n   Sehr geehrter Herr Beispiel,\r\n", hook.Pass, ""},
	{"liebe", "Grüße", "Liebe Erika,", hook.Pass, ""},
	{"lieber", "Grüße", "Lieber Bert,", hook.Pass, ""},
	{"dear", "Re: Offer", "Dear Ms Example,", hook.Pass, ""},
	{"hi", "Re: Offer", "Hi Bert!", hook.Pass, ""},
	{"lower-case greeting fails", "Re: Termin", "hallo Anna,", hook.Fail, "greeting missing"},
}

func exampleInput(t *testing.T, c exampleCase) hook.Input {
	t.Helper()
	m := &compose.Message{Raw: []byte("Subject: x\r\n\r\nx\r\n"), Subject: c.subject, BodyText: c.text, BodyHTML: "<p>x</p>",
		From: compose.Address{Name: "Demo", Address: "demo@example.org"}, To: []compose.Address{{Address: "a@example.org"}}}
	meta, err := ops.HookMeta(ops.DraftCheckInput{Draft: store.Draft{ID: 1, Kind: "reply"}, Account: "demo", EML: m.Raw, Message: m})
	if err != nil {
		t.Fatal(err)
	}
	return hook.Input{EML: m.Raw, BodyHTML: m.BodyHTML, BodyText: m.BodyText, Meta: meta}
}

func runExamples(t *testing.T, cmd []string) {
	for _, c := range exampleCases {
		t.Run(c.name, func(t *testing.T) {
			res, err := hook.Runner{Command: cmd, TempDir: t.TempDir()}.Run(context.Background(), exampleInput(t, c))
			if err != nil {
				t.Fatal(err)
			}
			if res.Status != c.want || (c.says != "" && !strings.Contains(res.Output, c.says)) {
				t.Fatalf("status %s, output %q; want %s with %q", res.Status, res.Output, c.want, c.says)
			}
			if c.name == "both problems" && !strings.Contains(res.Output, "subject contains TODO") {
				t.Fatalf("output %q lacks the subject problem", res.Output)
			}
		})
	}
}

func examplePath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "deploy", "hooks", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExampleHookShell(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh on PATH")
	}
	runExamples(t, []string{sh, examplePath(t, "check-example.sh"), "{meta_json}", "{body_text}"})
}

func TestExampleHookPowerShell(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	ps := "powershell.exe"
	if runtime.GOOS != "windows" {
		ps = "pwsh"
	}
	path, err := exec.LookPath(ps)
	if err != nil {
		t.Skipf("no %s on PATH", ps)
	}
	runExamples(t, []string{path, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File",
		examplePath(t, "check-example.ps1"), "{meta_json}", "{body_text}"})
}
