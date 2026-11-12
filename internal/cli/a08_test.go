package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/compose"
)

// A08 Teil 1 über die CLI: doctor nennt die Regeln der Anhang-Sperre;
// attach_dir umfasst das Datenverzeichnis (wie im Dauerbetrieb ein großer
// Arbeitsordner), Datenbank und Log sind gesperrt (own_data), ebenso
// gesperrte Namen; eine harmlose Datei geht. Alles im Trockenlauf, keine
// Ablage. Pfade sind Platzhalter unter t.TempDir().
func TestA08CLIAttachGuard(t *testing.T) {
	setupEnv(t)
	work := filepath.Join(t.TempDir(), "Arbeit")
	data := filepath.Join(work, "pkdaten")
	t.Setenv("POSTKAUZ_DATA_DIR", data)
	cfg := filepath.Join(work, "pkconf", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "[paths]\nattach_dir = '" + work + "'\n\n[draft]\nattach_deny_files = [\"*.xlsm\"]\n"
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTKAUZ_CONFIG", cfg)
	addTestAccount(t, "demo")
	if code, out, errs := run(t, "identity", "add", "-a", "demo", "--email", "probe@example.org"); code != ExitOK {
		t.Fatalf("identity add: %d %s %s", code, out, errs)
	}
	for _, f := range []string{"Mieter/Brief.pdf", "liste.xlsm", "probe.kdbx", "pkconf/notiz.txt"} {
		p := filepath.Join(work, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("Probe A08\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code, out, errs := run(t, "doctor")
	want := "attach_guard: " + itoa(len(compose.DefaultDenyDirs)) + " dir rules, " + itoa(len(compose.DefaultDenyFiles)+1) + " file rules\n"
	if !strings.Contains(out, want) {
		t.Fatalf("doctor (%d) lacks %q:\n%s%s", code, want, out, errs)
	}
	_, out, _ = run(t, "doctor", "--json")
	var rep struct {
		AttachGuard struct {
			DirRules   int `json:"dir_rules"`
			FileRules  int `json:"file_rules"`
			ExtraFiles int `json:"extra_files"`
		} `json:"attach_guard"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	g := rep.AttachGuard
	if g.DirRules != len(compose.DefaultDenyDirs) || g.FileRules != len(compose.DefaultDenyFiles)+1 || g.ExtraFiles != 1 {
		t.Fatalf("doctor --json attach_guard = %+v\n%s", g, out)
	}

	draft := func(file string) (int, string, string) {
		return run(t, "draft", "new", "-a", "demo", "--to", "b@example.org", "--subject", "Probe", "--attach", file, "--dry-run")
	}
	if code, out, errs := draft("Mieter/Brief.pdf"); code != ExitOK {
		t.Fatalf("harmless file: %d %s %s", code, out, errs)
	}
	for _, c := range []struct{ file, rule string }{
		{"pkdaten/postkauz.db", compose.RuleOwnData},
		{filepath.Join(data, "postkauz.log"), compose.RuleOwnData},
		{"pkconf/notiz.txt", compose.RuleOwnData},
		{"probe.kdbx", compose.RuleFile},
		{"liste.xlsm", compose.RuleFile},
		{"probe.txt:stream", compose.RuleStream},
	} {
		code, out, errs := draft(c.file)
		if code == ExitOK || !strings.Contains(errs, "attach_denied") || !strings.Contains(errs, "rule: "+c.rule) ||
			strings.Contains(errs+out, work) {
			t.Errorf("%s: %d\n%s%s", c.file, code, out, errs)
		}
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
