package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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
		!strings.Contains(out.String(), "hash match:    not checked") ||
		!strings.Contains(out.String(), "note:          test values only: never enter a real password here") ||
		!strings.Contains(errb.String(), "prompt probe: standard input is not a terminal") {
		t.Fatalf("probe: %d\n%s\n%s", code, out.String(), errb.String())
	}
	_, _ = f.Seek(0, 0)
	out.Reset()
	errb.Reset()
	code = RunWithInput([]string{"internal", "prompt-probe", "--json", "--reader", "term", "--expect-sha256", strings.Repeat("ab", 32)}, f, &out, &errb, "test")
	var p PromptProbe
	if code != ExitError || json.Unmarshal(out.Bytes(), &p) != nil || p.Report.Reader != "term" || p.Report.IsTerminal ||
		p.Env.StdinType != "disk" || strings.Contains(out.String(), canary) {
		t.Fatalf("probe --json: %d\n%s\n%s", code, out.String(), errb.String())
	}
	// W2: kein Hash in der Ausgabe, hash_match nur bei gelesener Eingabe,
	// die Warnung steht auch im JSON.
	if strings.Contains(out.String(), "sha256") || p.HashMatch != nil || p.Note != probeNote ||
		!strings.Contains(out.String(), `"note":"test values only: never enter a real password here"`) {
		t.Fatalf("probe --json hash/note: %s", out.String())
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("prompt-probe created the data directory: %v", err)
	}
	// Versteckt: weder in der Hilfe noch als Vorschlag.
	if _, help, _ := run(t, "--help"); strings.Contains(help, "internal") || strings.Contains(help, "prompt-probe") {
		t.Fatalf("help shows the hidden command:\n%s", help)
	}
	for _, args := range [][]string{{"internal", "prompt-probe", "--reader", "x"}, {"internal", "prompt-probe", "--timeout", "0s"},
		{"internal", "prompt-probe", "--expect-sha256", "abc"}, {"internal", "prompt-probe", "--expect-sha256", strings.Repeat("g", 64)}} {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Errorf("%v: %d", args, code)
		}
	}
}

// Restpunkt der Nachprüfung (W2): Mit gesetztem hash_match (true und
// false) steht in Text und JSON nur das Ergebnis des Vergleichs, weder
// ein Hash der Eingabe noch der vorgegebene Hash noch das Wort sha256.
func TestPromptProbeOutputWithHashMatch(t *testing.T) {
	const value = "Testwert-Prüfung-4711"
	sum := sha256.Sum256([]byte(value))
	expect := hex.EncodeToString(sum[:])
	for _, match := range []bool{true, false} {
		want := expect
		if !match {
			want = strings.Repeat("ab", 32)
		}
		m := prompt.MatchesSHA256([]byte(value), want)
		if m != match {
			t.Fatalf("MatchesSHA256 = %v, want %v", m, match)
		}
		p := PromptProbe{Note: probeNote, HashMatch: &m,
			Report: prompt.Report{Reader: prompt.ReaderConsole, Reads: 1, Units: 23, Bytes: 23, Runes: 21, NonASCII: 1, Terminator: "crlf"}}
		for _, asJSON := range []bool{false, true} {
			var out bytes.Buffer
			if err := writePromptProbe(&app{stdout: &out}, p, asJSON); err != nil {
				t.Fatal(err)
			}
			s := out.String()
			if strings.Contains(strings.ToLower(s), "sha256") || strings.Contains(strings.ToLower(s), "sha-256") ||
				strings.Contains(s, expect) || strings.Contains(s, want) || strings.Contains(s, value) || strings.Contains(s, "4711") {
				t.Fatalf("match=%v json=%v: output carries a hash or the value:\n%s", match, asJSON, s)
			}
			text := map[bool]string{true: "hash match:    yes\n", false: "hash match:    no\n"}[match]
			if asJSON {
				text = fmt.Sprintf(`"hash_match":%v`, match)
			}
			if !strings.Contains(s, text) || !strings.Contains(s, probeNote) {
				t.Fatalf("match=%v json=%v: want %q:\n%s", match, asJSON, text, s)
			}
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
	// K8: Tippfehler im Rückweg-Schalter ist ein Eingabefehler vor der Aufforderung.
	t.Setenv(prompt.EnvReader, "trem")
	out.Reset()
	errb.Reset()
	code = RunWithInput([]string{"account", "add", "demo", "--imap", "127.0.0.1:1993", "--user", "demo@example.org"}, f, &out, &errb, "test")
	if code != ExitUsage || strings.Contains(errb.String(), "Password") || !strings.Contains(errb.String(), `unknown password reader "trem" (POSTKAUZ_PASSWORD_READER must be console or term)`) {
		t.Fatalf("account add with unknown reader: %d\n%s\n%s", code, out.String(), errb.String())
	}
	code, info := classify(fmt.Errorf("read: %w", prompt.ErrTimeout))
	if code != ExitError || info.Code != "prompt_timeout" || info.Message != "no input at the hidden prompt within 2m0s" ||
		!strings.Contains(info.Hint, "--password-stdin") || !strings.Contains(info.Hint, "README, Troubleshooting (prompt_timeout)") {
		t.Fatalf("timeout: %d %+v", code, info)
	}
	// K7: allgemein (account add und set, jedes System); das README hat die
	// Zeile, auf die der Hinweis zeigt.
	if strings.Contains(info.Hint, "Get-Clipboard") || strings.Contains(info.Hint, "account add") {
		t.Fatalf("hint is specific: %q", info.Hint)
	}
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil || !bytes.Contains(readme, []byte("ends with `prompt_timeout`")) {
		t.Fatalf("README row for prompt_timeout missing: %v", err)
	}
}
