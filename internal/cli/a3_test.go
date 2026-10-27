package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/prompt"
)

// A3: internal prompt-probe ist versteckt, legt nichts an und nennt bei
// einer Umleitung (kein Terminal, Hypothese a) Art von stdin und Fehler;
// den Inhalt von stdin gibt es nie aus (Kanarientest).
func TestPromptProbeWithoutTerminal(t *testing.T) {
	dataDir, _ := setupEnv(t)
	const canary = "Kanarie-Probe-0815"
	f, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(canary + "\n")
	_, _ = f.Seek(0, 0)
	defer f.Close()
	var out, errb bytes.Buffer
	code := RunWithInput([]string{"internal", "prompt-probe"}, f, &out, &errb, "test")
	if code != ExitError || strings.Contains(out.String()+errb.String(), canary) ||
		!strings.Contains(out.String(), "stdin:         disk (terminal: no)") ||
		!strings.Contains(out.String(), "error:         standard input is not a terminal") ||
		!strings.Contains(errb.String(), "prompt probe: standard input is not a terminal") {
		t.Fatalf("probe: %d\n%s\n%s", code, out.String(), errb.String())
	}
	_, _ = f.Seek(0, 0)
	out.Reset()
	errb.Reset()
	code = RunWithInput([]string{"internal", "prompt-probe", "--json", "--reader", "term"}, f, &out, &errb, "test")
	var p PromptProbe
	if code != ExitError || json.Unmarshal(out.Bytes(), &p) != nil || p.Report.Reader != "term" || p.Report.IsTerminal ||
		p.Env.StdinType != "disk" || strings.Contains(out.String(), canary) {
		t.Fatalf("probe --json: %d\n%s\n%s", code, out.String(), errb.String())
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("prompt-probe created the data directory: %v", err)
	}
	// Versteckt: weder in der Hilfe noch als Vorschlag.
	if _, help, _ := run(t, "--help"); strings.Contains(help, "internal") || strings.Contains(help, "prompt-probe") {
		t.Fatalf("help shows the hidden command:\n%s", help)
	}
	for _, args := range [][]string{{"internal", "prompt-probe", "--reader", "x"}, {"internal", "prompt-probe", "--timeout", "0s"}} {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
}

// A3: account add ohne Terminal meldet den Umweg wie bisher; ein Ablauf
// der Zeitgrenze nennt --password-stdin als Ausweg.
func TestPasswordPromptErrors(t *testing.T) {
	setupEnv(t)
	f, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out, errb bytes.Buffer
	code := RunWithInput([]string{"account", "add", "demo", "--imap", "127.0.0.1:1993", "--user", "demo@example.org"}, f, &out, &errb, "test")
	if code != ExitUsage || !strings.Contains(errb.String(), "standard input is not a terminal; use --password-stdin or --password-from-env") {
		t.Fatalf("account add: %d\n%s\n%s", code, out.String(), errb.String())
	}
	code, info := classify(fmt.Errorf("read: %w", prompt.ErrTimeout))
	if code != ExitError || info.Code != "prompt_timeout" || !strings.Contains(info.Message, "within 2m0s") || !strings.Contains(info.Hint, "--password-stdin") {
		t.Fatalf("timeout: %d %+v", code, info)
	}
}
