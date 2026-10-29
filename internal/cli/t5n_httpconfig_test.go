package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Nachbesserung T5, Punkt 9 (h): Ein Fehler in [http] hält nur mcp --http
// auf und steht in doctor (Exit 1); version, accounts list und mcp über
// stdio laufen weiter (sync --watch startet nach einem Tippfehler also
// trotzdem).
func TestT5NHTTPConfigErrorOnlyStopsMCPHTTP(t *testing.T) {
	setupEnv(t)
	cfgFile := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("POSTKAUZ_CONFIG", cfgFile)
	for _, c := range []struct{ toml, want string }{
		{"[http]\nlisten = \"8787\"\n", "http.listen"},
		{"[http]\nlisten_typo = \"127.0.0.1:8787\"\n", "unknown keys: http.listen_typo"},
	} {
		if err := os.WriteFile(cfgFile, []byte(c.toml), 0o600); err != nil {
			t.Fatal(err)
		}
		if code, out, errs := run(t, "version"); code != ExitOK {
			t.Errorf("%q: version: %d %s %s", c.toml, code, out, errs)
		}
		if code, out, errs := run(t, "accounts", "list", "--json"); code != ExitOK {
			t.Errorf("%q: accounts list: %d %s %s", c.toml, code, out, errs)
		}
		if code, out, errs := run(t, "mcp", "--http"); code != ExitError || out != "" || !strings.Contains(errs, c.want) {
			t.Errorf("%q: mcp --http: %d %q %s", c.toml, code, out, errs)
		}
		// mcp über stdio hängt nicht an [http]: Er scheitert hier nur am
		// fehlenden Schlüssel, nicht an der Konfiguration.
		t.Setenv(EnvKey, "")
		if code, _, errs := run(t, "mcp"); code != ExitDenied || strings.Contains(errs, c.want) {
			t.Errorf("%q: mcp (stdio): %d %s", c.toml, code, errs)
		}
		code, out, errs := run(t, "doctor")
		if code != ExitError || !strings.Contains(out, "http:      INVALID: ") || !strings.Contains(out, c.want) ||
			!strings.Contains(errs, "[http] is invalid") || !strings.Contains(out, "database:  ok") {
			t.Errorf("%q: doctor: %d\n%s\n%s", c.toml, code, out, errs)
		}
		code, out, _ = run(t, "doctor", "--json")
		if code != ExitError || !strings.Contains(out, `"http_error":`) || strings.Contains(out, `"http":{`) || strings.Contains(out, `"config_error"`) {
			t.Errorf("%q: doctor --json: %d %s", c.toml, code, out)
		}
	}
}
