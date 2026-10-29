package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Zweite Nachbesserung T5, Punkt 5 (a): Ein falscher Typ in [http]
// (max_requests_per_minute = "60") ließ accounts list und sync --watch mit
// "incompatible types" scheitern. Jetzt hält er nur mcp --http auf und
// steht in doctor; ein Syntaxfehler der Datei hält weiter jedes Kommando
// auf.
func TestT5N2HTTPTypeErrorOnlyStopsMCPHTTP(t *testing.T) {
	setupEnv(t)
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("POSTKAUZ_CONFIG", cfgFile)
	if err := os.WriteFile(cfgFile, []byte("[http]\nmax_requests_per_minute = \"60\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "accounts", "list", "--json"); code != ExitOK {
		t.Errorf("accounts list: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, "mcp", "--http"); code != ExitError || out != "" || !strings.Contains(errs, "incompatible types") {
		t.Errorf("mcp --http: %d %q %s", code, out, errs)
	}
	code, out, errs := run(t, "doctor")
	if code != ExitError || !strings.Contains(out, "http:      INVALID: ") || !strings.Contains(out, "database:  ok") {
		t.Errorf("doctor: %d\n%s\n%s", code, out, errs)
	}

	if err := os.WriteFile(cfgFile, []byte("[http]\nmax_requests_per_minute = \"60\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := run(t, "accounts", "list", "--json"); code == ExitOK || !strings.Contains(errs, "config") {
		t.Errorf("syntax error: accounts list %d %s, want a config error", code, errs)
	}
}
