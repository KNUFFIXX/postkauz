package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// Kanarienwert als Passwort (Akzeptanz S1.1 / roadmap S1 Nr. 8): darf in
// keiner Datei des Datenverzeichnisses (Datenbank, WAL, Log, Audit) und in
// keiner Ausgabe auftauchen.
const canary = "CANARY-cli-pw-3e8b5f21-must-not-leak"

// setupEnv isoliert einen Test: eigenes Datenverzeichnis, keine
// Konfigurationsdatei, Schlüsselbund im Speicher.
func setupEnv(t *testing.T) (dataDir string, kr *memKeyring) {
	t.Helper()
	dir := t.TempDir()
	dataDir = filepath.Join(dir, "data")
	t.Setenv("POSTKAUZ_CONFIG", filepath.Join(dir, "missing-config.toml"))
	t.Setenv("POSTKAUZ_DATA_DIR", dataDir)
	t.Setenv("POSTKAUZ_SECRETS_BACKEND", "")
	kr = &memKeyring{m: map[string]secrets.Secret{}}
	orig := openSecrets
	openSecrets = func(backend, insecureDir string) (secrets.Store, error) {
		if backend == secrets.BackendInsecureFile {
			return orig(backend, insecureDir)
		}
		return secrets.WithEnv(kr), nil
	}
	t.Cleanup(func() { openSecrets = orig })
	return dataDir, kr
}

type memKeyring struct {
	mu sync.Mutex
	m  map[string]secrets.Secret
}

func (k *memKeyring) Get(ref string) (secrets.Secret, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if v, ok := k.m[ref]; ok {
		return v, nil
	}
	return "", secrets.ErrNotFound
}
func (k *memKeyring) Set(ref string, v secrets.Secret) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[ref] = v
	return nil
}
func (k *memKeyring) Delete(ref string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, ok := k.m[ref]; !ok {
		return secrets.ErrNotFound
	}
	delete(k.m, ref)
	return nil
}
func (k *memKeyring) Status() secrets.Status {
	return secrets.Status{Backend: "keyring", Available: true, Detail: "test keyring"}
}

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runIn(t, "", args...)
}

func runIn(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = RunWithInput(args, strings.NewReader(stdin), &out, &errb, "test")
	return code, out.String(), errb.String()
}

// fakeIMAP startet einen IMAP-Server im Prozess (ohne Docker).
func fakeIMAP(t *testing.T, password string) (host, port string) {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", password)
	for _, mb := range []string{"INBOX", "Sent", "Drafts", "Trash"} {
		_ = u.Create(mb, nil)
	}
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return imapmemserver.NewUserSession(u), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}},
		InsecureAuth: true,
		Logger:       log.New(io.Discard, "", 0),
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	host, port, _ = net.SplitHostPort(ln.Addr().String())
	return host, port
}

func TestAccountsListJSON(t *testing.T) {
	setupEnv(t)
	code, out, errs := run(t, "accounts", "list", "--json")
	if code != ExitOK || errs != "" {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
	if strings.TrimSpace(out) != `{"accounts":[]}` {
		t.Errorf("stdout = %q", out)
	}
}

func TestAccountsListHuman(t *testing.T) {
	setupEnv(t)
	code, out, _ := run(t, "accounts", "list")
	if code != ExitOK || !strings.Contains(out, "No accounts configured yet.") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestVersionJSON(t *testing.T) {
	setupEnv(t)
	code, out, _ := run(t, "version", "--json")
	var v map[string]any
	if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v["version"] != "test" || v["name"] != "postkauz" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, ok := v["schema_version"]; !ok {
		t.Error("schema_version missing")
	}
}

func TestDoctorFreshInstallCreatesNothing(t *testing.T) {
	dataDir, _ := setupEnv(t)
	code, out, _ := run(t, "doctor", "--json")
	var r DoctorReport
	if code != ExitOK || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if r.ConfigExists || r.DataDir != dataDir || r.Database != "missing" || r.Keyring != "available" || r.SchemaKnown < 1 {
		t.Errorf("report = %+v", r)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Error("doctor must not create the data directory")
	}
	code, out, _ = run(t, "doctor")
	for _, want := range []string{"version:", "config:", "data dir:", "database:", "schema:", "keyring:"} {
		if code != ExitOK || !strings.Contains(out, want) {
			t.Errorf("human doctor lacks %q: %s", want, out)
		}
	}
}

func TestDoctorReportsInvalidConfig(t *testing.T) {
	setupEnv(t)
	f := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(f, []byte("[secrets]\nbackend = \"plaintext\"\n"), 0o600)
	t.Setenv("POSTKAUZ_CONFIG", f)
	code, out, errs := run(t, "doctor")
	if code != ExitError || !strings.Contains(out, "error:") || !strings.Contains(errs, "configuration is invalid") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errs)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	setupEnv(t)
	code, _, errs := run(t, "bogus")
	if code != ExitUsage || !strings.Contains(errs, "error:") {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
}

func TestUnknownFlagJSONError(t *testing.T) {
	setupEnv(t)
	code, _, errs := run(t, "accounts", "list", "--nope", "--json")
	if code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
	var e map[string]ErrorInfo
	if err := json.Unmarshal([]byte(errs), &e); err != nil || e["error"].Code != "usage" {
		t.Fatalf("stderr = %q (%v)", errs, err)
	}
}

func TestEveryBindingResolves(t *testing.T) {
	setupEnv(t)
	root, err := NewRoot("test", &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		cmd, _, err := root.Find(b.path)
		if err != nil || cmd.Name() != b.path[len(b.path)-1] {
			t.Errorf("binding %s: command %v not found (%v)", b.op, b.path, err)
		}
	}
}

func TestAccountAddUsageErrors(t *testing.T) {
	setupEnv(t)
	cases := []struct {
		name  string
		stdin string
		args  []string
		code  int
		ecode string
	}{
		{"missing --imap", "", []string{"account", "add", "demo", "--user", "u", "--json"}, ExitUsage, "usage"},
		{"no prompt in json mode", "", []string{"account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u", "--insecure-test-server", "--json"}, ExitUsage, "usage"},
		{"stdin not a terminal", "", []string{"account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u", "--insecure-test-server"}, ExitUsage, ""},
		{"insecure on remote host", canary + "\n", []string{"account", "add", "demo", "--imap", "imap.example.org:993", "--user", "u", "--insecure-test-server", "--password-stdin", "--json"}, ExitUsage, "invalid_input"},
		{"plain text without flag", canary + "\n", []string{"account", "add", "demo", "--imap", "127.0.0.1:143", "--tls", "none", "--user", "u", "--password-stdin", "--json"}, ExitUsage, "invalid_input"},
		{"bad name", canary + "\n", []string{"account", "add", "Demo_1", "--imap", "127.0.0.1:993", "--user", "u", "--password-stdin", "--json"}, ExitUsage, "invalid_input"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			code, _, errs := runIn(t, c.stdin, c.args...)
			if code != c.code {
				t.Fatalf("code=%d stderr=%q", code, errs)
			}
			if c.ecode != "" {
				var e map[string]ErrorInfo
				if err := json.Unmarshal([]byte(errs), &e); err != nil || e["error"].Code != c.ecode {
					t.Fatalf("stderr = %q", errs)
				}
			}
			if strings.Contains(errs, canary) {
				t.Fatal("canary in error output")
			}
		})
	}
}

// Akzeptanz S1.1: Konto anlegen, testen, auflisten, entfernen; der
// Kanarienwert taucht nirgends auf (Datenbank, WAL, Log, Audit, Ausgaben).
func TestAccountLifecycleWithCanary(t *testing.T) {
	for _, backend := range []string{"keyring", "insecure-file"} {
		t.Run(backend, func(t *testing.T) {
			dataDir, kr := setupEnv(t)
			if backend == "insecure-file" {
				t.Setenv("POSTKAUZ_SECRETS_BACKEND", "insecure-file")
			}
			host, port := fakeIMAP(t, canary)
			var allOut strings.Builder
			step := func(stdin string, want int, args ...string) string {
				t.Helper()
				code, out, errs := runIn(t, stdin, args...)
				allOut.WriteString(out + errs)
				if code != want {
					t.Fatalf("%v: code=%d out=%q err=%q", args, code, out, errs)
				}
				return out
			}

			out := step(canary+"\n", ExitOK, "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin")
			if !strings.Contains(out, "account demo added") {
				t.Fatalf("add: %q", out)
			}
			if backend == "keyring" {
				if v, err := kr.Get("account/demo"); err != nil || v.Reveal() != canary {
					t.Fatal("password not in keyring")
				}
			}
			step(canary+"\n", ExitError, "account", "add", "demo", "--imap", host+":"+port, "--user", "x", "--tls", "none", "--insecure-test-server", "--password-stdin")

			out = step("", ExitOK, "accounts", "list", "--json")
			var list struct{ Accounts []map[string]any }
			if json.Unmarshal([]byte(out), &list) != nil || len(list.Accounts) != 1 || list.Accounts[0]["name"] != "demo" || list.Accounts[0]["email"] != "tester@example.invalid" {
				t.Fatalf("list: %s", out)
			}
			out = step("", ExitOK, "accounts", "list")
			if !strings.Contains(out, "demo") || !strings.Contains(out, "never") {
				t.Fatalf("list human: %s", out)
			}

			out = step("", ExitOK, "account", "test", "demo")
			if !strings.Contains(out, "login:         ok") || !strings.Contains(out, "INBOX") || !strings.Contains(out, "inbox") {
				t.Fatalf("test: %s", out)
			}
			out = step("", ExitOK, "account", "test", "demo", "--json")
			var tr map[string]any
			if json.Unmarshal([]byte(out), &tr) != nil || tr["login"] != "ok" {
				t.Fatalf("test json: %s", out)
			}

			out = step("", ExitOK, "doctor", "--json")
			var r DoctorReport
			if json.Unmarshal([]byte(out), &r) != nil || r.Database != "ok" || r.SchemaVersion != r.SchemaKnown || len(r.Accounts) != 1 || r.Accounts[0].Password != "stored" || r.Accounts[0].Status != "ok" {
				t.Fatalf("doctor: %s", out)
			}
			if backend == "insecure-file" && r.Keyring != "insecure" {
				t.Fatalf("doctor must flag the insecure backend: %+v", r)
			}

			step("", ExitOK, "account", "remove", "demo", "--dry-run")
			out = step("", ExitOK, "account", "remove", "demo")
			if !strings.Contains(out, "account demo removed; stored password deleted") {
				t.Fatalf("remove: %s", out)
			}
			step("", ExitError, "account", "test", "demo")
			if backend == "keyring" && len(kr.m) != 0 {
				t.Fatal("keyring entry left after remove")
			}

			// Kanarientest über alle Kanäle.
			if strings.Contains(allOut.String(), canary) {
				t.Fatal("canary in CLI output")
			}
			scanned := 0
			_ = filepath.WalkDir(dataDir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, _ := os.ReadFile(p)
				scanned++
				if bytes.Contains(b, []byte(canary)) {
					t.Errorf("canary found in %s", p)
				}
				return nil
			})
			if scanned < 2 {
				t.Fatalf("expected database and log in %s, scanned %d files", dataDir, scanned)
			}
			logb, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
			if !bytes.Contains(logb, []byte(`"op":"accounts.add"`)) {
				t.Errorf("log lacks operations: %s", logb)
			}
		})
	}
}

// P4: kein Schlüsselbund (Linux ohne Secret Service). `account add` scheitert
// mit Hinweis; mit --password-from-env und POSTKAUZ_PASSWORD_<NAME> geht es.
func TestNoKeyringFallback(t *testing.T) {
	setupEnv(t)
	openSecrets = func(backend, dir string) (secrets.Store, error) {
		return secrets.WithEnv(secrets.Unavailable{Reason: "keyring unavailable: no D-Bus session (test)"}), nil
	}
	host, port := fakeIMAP(t, canary)
	code, _, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin", "--json")
	var e map[string]ErrorInfo
	if code != ExitError || json.Unmarshal([]byte(errs), &e) != nil || e["error"].Code != "secret_store_unavailable" || !strings.Contains(e["error"].Message, "insecure-file") {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
	code, out, _ := run(t, "doctor", "--json")
	var r DoctorReport
	if code != ExitOK || json.Unmarshal([]byte(out), &r) != nil || r.Keyring != "unavailable" || len(r.Warnings) == 0 {
		t.Fatalf("doctor: %s", out)
	}

	t.Setenv("POSTKAUZ_PASSWORD_DEMO", canary)
	code, out, errs = run(t, "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-from-env")
	if code != ExitOK || !strings.Contains(out, "POSTKAUZ_PASSWORD_DEMO") {
		t.Fatalf("env add: code=%d out=%q err=%q", code, out, errs)
	}
	if code, out, errs = run(t, "account", "test", "demo"); code != ExitOK {
		t.Fatalf("test with env password: %d %q %q", code, out, errs)
	}
	if strings.Contains(out+errs, canary) {
		t.Fatal("canary in output")
	}
}

func TestAccountTestErrors(t *testing.T) {
	setupEnv(t)
	host, port := fakeIMAP(t, "right-password")
	if code, _, errs := runIn(t, "wrong-password\n", "account", "add", "demo", "--imap", host+":"+port, "--user", "tester@example.invalid", "--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("add: %d %s", code, errs)
	}
	code, _, errs := run(t, "account", "test", "demo", "--json")
	var e map[string]ErrorInfo
	if code != ExitError || json.Unmarshal([]byte(errs), &e) != nil || e["error"].Code != "auth" || strings.Contains(errs, "wrong-password") {
		t.Fatalf("auth: code=%d %s", code, errs)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().String()
	ln.Close()
	if code, _, errs := runIn(t, "pw\n", "account", "add", "gone", "--imap", closed, "--user", "u", "--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("add gone: %d %s", code, errs)
	}
	if code, _, errs := run(t, "account", "test", "gone"); code != ExitConnection {
		t.Fatalf("closed port: code=%d %s", code, errs)
	}
	code, out, _ := run(t, "doctor", "--json")
	if code != ExitOK || !strings.Contains(out, `"status":"error"`) {
		t.Fatalf("doctor after failed test: %s", out)
	}
	if _, _, errs := run(t, "account", "test", "nope"); !strings.Contains(errs, "not found") {
		t.Fatalf("unknown account: %s", errs)
	}
}
