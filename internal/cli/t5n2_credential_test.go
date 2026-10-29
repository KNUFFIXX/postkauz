package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Zweite Nachbesserung T5, Punkt 4b: Auf dem VPS kommt das Passwort als
// systemd-Credential ($CREDENTIALS_DIRECTORY/POSTKAUZ_PASSWORD_<KONTO>,
// LoadCredential=) statt über die Umgebung. account add
// --password-from-env, account test und doctor nehmen es an; das Passwort
// steht in keiner Ausgabe.
func TestT5N2PasswordFromSystemdCredential(t *testing.T) {
	setupEnv(t)
	openSecrets = func(backend, dir string) (secrets.Store, error) {
		return secrets.WithEnv(secrets.Unavailable{Reason: "keyring unavailable: no D-Bus session (test)"}), nil
	}
	host, port := fakeIMAP(t, canary)
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", "")
	creds := t.TempDir()
	t.Setenv(secrets.EnvCredentialsDir, creds)
	if err := os.WriteFile(filepath.Join(creds, "POSTKAUZ_PASSWORD_DEMO"), []byte(canary+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	code, out, errs := run(t, "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-from-env")
	if code != ExitOK {
		t.Fatalf("add with credential: code=%d out=%q err=%q", code, out, errs)
	}
	if code, out, errs = run(t, "account", "test", "demo"); code != ExitOK {
		t.Fatalf("test with credential: %d %q %q", code, out, errs)
	}
	all := out + errs
	code, out, _ = run(t, "doctor", "--json")
	var r DoctorReport
	if json.Unmarshal([]byte(out), &r) != nil || len(r.Accounts) != 1 || r.Accounts[0].Password != "credential" {
		t.Errorf("doctor: code %d, accounts %+v", code, r.Accounts)
	}
	if strings.Contains(all+out, canary) {
		t.Fatal("canary in output")
	}

	// Die Umgebungsvariable geht vor.
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", "wrong-from-env")
	if code, _, _ = run(t, "account", "test", "demo"); code != ExitAuth {
		t.Errorf("environment variable before credential: account test %d, want %d", code, ExitAuth)
	}
}
