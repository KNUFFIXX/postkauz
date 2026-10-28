package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/oauth"
	"github.com/KNUFFIXX/postkauz/internal/oauth/oauthtest"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
	"github.com/KNUFFIXX/postkauz/internal/store"
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
	// K2: gespeichert wird unter der Sperrdatei neben der Datenbank.
	ref, _ := lo["credential_ref"].(string)
	var lockFound bool
	_ = filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Name() == oauth.LockName(ref) {
			lockFound = true
		}
		return nil
	})
	if ref == "" || !lockFound {
		t.Fatalf("no token lock file %s in the data directory", oauth.LockName(ref))
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

// Nachbesserung T1b, B-M4 und B-M3: doctor meldet einen gescheiterten
// Token-Refresh aus dem Audit (oauth.refresh), bis ein späteres ok oder
// eine neue Anmeldung ihn aufhebt; ein Lesefehler des Schlüsselbunds
// bleibt nicht still (doctor: Warnung; doctor --secrets: Skipped mit
// Grund).
func TestDoctorTokenRefreshAndKeyringErrors(t *testing.T) {
	dataDir, kr := setupEnv(t)
	if code, out, errs := run(t, "account", "add", "ms", "--imap", "127.0.0.1:1", "--user", "ms@example.org", "--auth", "oauth2",
		"--client-id", oauthtest.ClientID); code != ExitOK {
		t.Fatalf("add: %s %s", out, errs)
	}
	code, out, errs := run(t, "account", "login", "ms", "--json")
	var lo map[string]any
	if code != ExitOK || json.Unmarshal([]byte(out), &lo) != nil {
		t.Fatalf("login: %d %s %s", code, out, errs)
	}
	ref, _ := lo["credential_ref"].(string)
	appendRefresh := func(ts time.Time, res audit.Result, reason string) {
		t.Helper()
		st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer st.Close()
		if err := st.AuditWriter().Append(context.Background(), audit.Event{TS: ts, ActorType: audit.ActorSystem, KeyID: "system",
			Scope: "mail:sync", Operation: ops.OpOAuthRefresh, ParamsRedacted: []byte(`{"account":"ms"}`), Targets: []string{"account:ms"},
			Result: res, Reason: reason}); err != nil {
			t.Fatal(err)
		}
	}
	doctor := func() DoctorReport {
		t.Helper()
		_, out, _ := run(t, "doctor", "--json")
		var r DoctorReport
		if err := json.Unmarshal([]byte(out), &r); err != nil || len(r.Accounts) != 1 {
			t.Fatalf("doctor: %v\n%s", err, out)
		}
		return r
	}
	warned := func(r DoctorReport, s string) bool {
		for _, w := range r.Warnings {
			if strings.Contains(w, s) {
				return true
			}
		}
		return false
	}
	if r := doctor(); r.Accounts[0].TokenRefresh != nil || warned(r, "token refresh") {
		t.Fatalf("doctor without refresh: %+v", r)
	}
	appendRefresh(time.Now().Add(time.Second), audit.ResultError, "invalid_grant")
	r := doctor()
	if tr := r.Accounts[0].TokenRefresh; tr == nil || !tr.Failed || tr.Reason != "invalid_grant" ||
		!warned(r, "account ms: last token refresh failed (invalid_grant, ") || !warned(r, "sign in again with: postkauz account login ms") {
		t.Fatalf("doctor after a failed refresh: %+v %+v", r.Accounts[0].TokenRefresh, r.Warnings)
	}
	_, text, _ := run(t, "doctor")
	if !strings.Contains(text, "last token refresh failed") {
		t.Fatalf("doctor text: %s", text)
	}
	// Ein späteres ok hebt die Warnung auf.
	appendRefresh(time.Now().Add(2*time.Second), audit.ResultOK, "")
	if r := doctor(); r.Accounts[0].TokenRefresh == nil || r.Accounts[0].TokenRefresh.Failed || warned(r, "token refresh failed") {
		t.Fatalf("doctor after ok: %+v", r.Warnings)
	}
	// Ein Fehler, der älter ist als das gespeicherte Token (danach neu
	// angemeldet), warnt nicht.
	appendRefresh(time.Now().Add(-time.Hour), audit.ResultError, "invalid_grant")
	if r := doctor(); r.Accounts[0].TokenRefresh == nil || r.Accounts[0].TokenRefresh.Failed || warned(r, "token refresh failed") {
		t.Fatalf("doctor with an error older than the token: %+v", r.Warnings)
	}

	// B-M3/B-M4: Zeitüberschreitung des Schlüsselbunds.
	kr.mu.Lock()
	kr.failGet = map[string]error{ref: fmt.Errorf("%w (keyring did not answer within 10s)", secrets.ErrTimeout)}
	kr.mu.Unlock()
	if r := doctor(); !warned(r, "account ms: the OAuth2 token in the keyring") || !warned(r, "could not be read") {
		t.Fatalf("doctor with a keyring timeout: %+v", r.Warnings)
	}
	_, sec, _ := run(t, "doctor", "--secrets", "--json")
	if !strings.Contains(sec, "search for OAuth2 refresh token of account ms") || !strings.Contains(sec, "keyring: ") {
		t.Fatalf("doctor --secrets with a keyring timeout: %s", sec)
	}
}
