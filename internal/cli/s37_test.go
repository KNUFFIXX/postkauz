package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
)

// S3.7: Microsoft-Konto per OAuth2 über die CLI, gegen den Fake-AS aus
// setupEnv. Kanarientest: Access- und Refresh-Token (Präfix CANARY-tok)
// erscheinen in keiner Ausgabe, keiner Datei des Datenverzeichnisses, im
// Audit nicht und nicht im Log.
func TestOAuth2AccountCLI(t *testing.T) {
	dataDir, kr := setupEnv(t)
	var outs []string
	do := func(want int, args ...string) (string, string) {
		t.Helper()
		code, out, errs := run(t, args...)
		outs = append(outs, out, errs)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\n%s\n%s", args, code, want, out, errs)
		}
		return out, errs
	}
	// Kein echter Server: Port 1 auf localhost (account test/sync laufen
	// hier nicht; die Anmeldung geht nur an den Fake-AS).
	out, _ := do(ExitOK, "account", "add", "ms", "--imap", "127.0.0.1:1", "--user", "ms@example.org", "--auth", "oauth2",
		"--client-id", oauthtest.ClientID, "--tenant", "consumers")
	if !strings.Contains(out, "sign in with: account login ms") || !strings.Contains(out, "tenant consumers") {
		t.Fatalf("add: %s", out)
	}
	if len(kr.m) != 0 {
		t.Fatalf("add stored something: %v", kr.m)
	}
	do(ExitUsage, "account", "add", "ms2", "--imap", "127.0.0.1:1", "--user", "ms@example.org", "--auth", "oauth2",
		"--client-id", oauthtest.ClientID, "--password-stdin")
	_, errs := do(ExitUsage, "account", "add", "ms3", "--imap", "127.0.0.1:1", "--user", "ms@example.org", "--auth", "oauth2")
	if !strings.Contains(errs, "client_id") {
		t.Fatalf("missing client id: %s", errs)
	}

	// doctor vor der Anmeldung: Token fehlt, Hinweis auf account login.
	out, _ = do(ExitOK, "doctor", "--json")
	if !strings.Contains(out, `"token":"missing"`) || !strings.Contains(out, "account login ms") {
		t.Fatalf("doctor before login: %s", out)
	}

	out, errs = do(ExitOK, "account", "login", "ms")
	if !strings.Contains(errs, "The browser opens the Microsoft sign-in") || !strings.Contains(errs, fakeAS.URL+"/authorize?") {
		t.Fatalf("prompt: %s", errs)
	}
	if !strings.Contains(out, "account ms: signed in; refresh token stored in the OS keyring as account/") {
		t.Fatalf("login: %s", out)
	}
	out, _ = do(ExitOK, "account", "login", "ms", "--json")
	var lo map[string]any
	if err := json.Unmarshal([]byte(out), &lo); err != nil || lo["refresh_token"] != "stored" || lo["method"] != "loopback" {
		t.Fatalf("login --json: %s", out)
	}

	out, _ = do(ExitOK, "doctor", "--json")
	var rep DoctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil || len(rep.Accounts) != 1 || rep.Accounts[0].Token != "stored" ||
		rep.Accounts[0].TokenExpiry == "" || rep.Accounts[0].Auth != "oauth2" {
		t.Fatalf("doctor: %s", out)
	}
	out, _ = do(ExitOK, "doctor")
	if !strings.Contains(out, "ms: OAuth2 token stored, last access token valid until") {
		t.Fatalf("doctor text: %s", out)
	}
	// doctor --secrets sucht das Refresh-Token in Log, Konfiguration und
	// Datenbank und findet es nicht.
	out, _ = do(ExitOK, "doctor", "--secrets")
	if !strings.Contains(out, "OAuth2 refresh token of account ms") && strings.Contains(out, "FAIL") {
		t.Fatalf("doctor --secrets: %s", out)
	}
	if strings.Contains(out, "FAIL") {
		t.Fatalf("doctor --secrets found a secret: %s", out)
	}

	// Passwort-Konto: account login lehnt ab.
	t.Setenv("POSTKAUZ_PASSWORD_PW", "x")
	do(ExitOK, "account", "add", "pw", "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none", "--insecure-test-server", "--password-from-env")
	do(ExitUsage, "account", "login", "pw")
	// Ein Agent mit Schlüssel darf sich nie anmelden (HumanOnly).
	do(ExitOK, "key", "create", "--name", "agent", "--role", "admin")
	_, errs = do(ExitDenied, "account", "login", "ms", "--key", "agent")
	if !strings.Contains(errs, "human_only") {
		t.Fatalf("login with key: %s", errs)
	}

	out, _ = do(ExitOK, "audit", "--json")
	if !strings.Contains(out, "accounts.login") {
		t.Fatalf("audit: %s", out)
	}
	do(ExitOK, "account", "remove", "ms")
	for k := range kr.m {
		if strings.HasSuffix(k, "/ms") {
			t.Fatalf("token left after remove: %v", k)
		}
	}

	for _, s := range outs {
		if strings.Contains(s, "CANARY-tok") || strings.Contains(s, "CANARY-device") {
			t.Fatalf("token in output: %s", s)
		}
	}
	_ = filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "CANARY-tok") {
			t.Errorf("token in file %s", p)
		}
		return nil
	})
}
