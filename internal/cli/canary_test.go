package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Kanarientest über alle Ausgabekanäle (S1.11, Akzeptanz S1 Punkt 8).
//
// Die gebaute Programmdatei (go build ./cmd/postkauz) läuft als eigener
// Prozess, wie beim Betreiber. Kanarienwerte:
//
//   - pw, pwBad: Passwörter zweier Konten (eines richtig, eines falsch);
//   - key: ein echter API-Schlüssel aus `key create`, wrongKey ein falscher;
//   - mail: ein Wort im Text einer Mail und ihres Anhangs.
//
// Passwörter und der falsche Schlüssel dürfen nirgends erscheinen, der
// Schlüssel nur in der Ausgabe von `key create`. Mailtext darf nur in den
// lesenden Kommandos stehen (show, search, thread, attach text, mailbox
// overview) und in MCP-Ergebnissen in der Untrusted-Hülle; nie im Audit,
// im Log, in den --json-Ausgaben der Admin-Kommandos, in Fehlermeldungen,
// in Absturzausgaben, im Audit-Export oder in doctor.
func TestCanaryAllChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the program; skipped with -short")
	}
	c := newCanaryEnv(t)
	addr := canaryServer(t, c)

	// --- Konten, Sync, Admin-Kommandos mit --json -------------------------
	c.mustRun("account add", 0, c.pw+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin", "--json")
	c.mustRun("account add (wrong password)", 0, c.pwBad+"\n", "account", "add", "bad", "--imap", addr, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin", "--json")
	c.mustRun("account test", 0, "", "account", "test", "demo", "--json")
	c.mustRun("account test (wrong password)", ExitError, "", "account", "test", "bad", "--json")
	c.mustRun("account test (wrong password, text)", ExitError, "", "account", "test", "bad")
	c.mustRun("sync", 0, "", "sync", "-a", "demo", "--json")
	c.mustRun("sync (text)", 0, "", "sync", "-a", "demo")
	c.mustRun("sync (wrong password)", ExitError, "", "sync", "-a", "bad", "--json")
	c.mustRun("accounts list", 0, "", "accounts", "list", "--json")
	c.mustRun("mailbox list", 0, "", "mailbox", "list", "-a", "demo", "--json")
	created := c.mustRun("key create", 0, "", "key", "create", "--name", "agent", "--role", "reader", "--json")
	var kc ops.KeysCreateOutput
	if err := json.Unmarshal([]byte(created.stdout), &kc); err != nil || !strings.HasPrefix(kc.Secret, ops.KeyPrefix) {
		t.Fatalf("key create: %v %s", err, created.stdout)
	}
	created.keyOK = true
	c.key = kc.Secret
	c.mustRun("key list", 0, "", "key", "list", "--json")
	// Entscheid 36 (S2.1): Passwort neu setzen nur über stdin; mit Schlüssel
	// verweigert (das Kanarien-Passwort auf stdin darf nirgends auftauchen),
	// im Trockenlauf ohne Wirkung, danach meldet sich "bad" mit dem
	// richtigen Passwort an.
	c.mustRun("account set --password-stdin (key)", ExitDenied, c.pw+"\n", "account", "set", "bad", "--password-stdin", "--key", "agent", "--json")
	c.mustRun("account set --password-stdin (dry run)", 0, c.pw+"\n", "account", "set", "bad", "--password-stdin", "--dry-run", "--json")
	c.mustRun("account test (still wrong)", ExitError, "", "account", "test", "bad", "--json")
	c.mustRun("account set --password-stdin", 0, c.pw+"\n", "account", "set", "bad", "--password-stdin", "--json")
	c.mustRun("account set --password-stdin (text)", 0, c.pw+"\n", "account", "set", "bad", "--password-stdin")
	c.mustRun("account test (password replaced)", 0, "", "account", "test", "bad", "--json")
	c.mustRun("account set --password-stdin (empty)", ExitUsage, "", "account", "set", "bad", "--password-stdin", "--json")
	c.mustRun("policy show", 0, "", "policy", "show", "--key", "agent", "--json")
	c.mustRun("import", 0, "", "import", "mbox", c.writeMbox(), "-a", "demo", "--mailbox", "Kanarien-Import", "--json")
	c.mustRun("import (text)", 0, "", "import", "mbox", c.writeMbox(), "-a", "demo", "--mailbox", "Kanarien-Import")
	c.mustRun("version", 0, "", "version", "--json")

	// --- Lesende Kommandos: hier darf (und muss) Mailtext stehen ----------
	var msg ops.MessageOutput
	show := c.mustRun("show --json", 0, "", "show", "kanarie@example.org", "-a", "demo", "--json")
	if err := json.Unmarshal([]byte(show.stdout), &msg); err != nil || msg.ID == 0 {
		t.Fatalf("show: %v %s", err, show.stdout)
	}
	id := fmt.Sprint(msg.ID)
	c.reading(c.mustRun("show", 0, "", "show", id), true)
	c.reading(show, true)
	c.reading(c.mustRun("thread", 0, "", "thread", "--message", id, "--strip-quotes"), true)
	c.reading(c.mustRun("search", 0, "", "search", c.mail, "-a", "demo", "--json"), false)
	c.reading(c.mustRun("search (text)", 0, "", "search", c.mail, "-a", "demo"), false)
	attList := c.mustRun("attach list", 0, "", "attach", "list", id, "--json") // Dateinamen, kein Text
	var al ops.AttachmentsListOutput
	if err := json.Unmarshal([]byte(attList.stdout), &al); err != nil || len(al.Attachments) != 1 {
		t.Fatalf("attach list: %v %s", err, attList.stdout)
	}
	attID := fmt.Sprint(al.Attachments[0].ID)
	c.reading(c.mustRun("attach text", 0, "", "attach", "text", attID), true)
	var boxes ops.MailboxesListOutput
	_ = json.Unmarshal([]byte(c.mustRun("mailbox list (ids)", 0, "", "mailbox", "list", "-a", "demo", "--json").stdout), &boxes)
	for _, b := range boxes.Mailboxes {
		if b.Name == "INBOX" {
			c.reading(c.mustRun("mailbox overview", 0, "", "mailbox", "overview", fmt.Sprint(b.ID), "-a", "demo", "--since", "3650d"), false)
		}
	}

	// --- Fehlermeldungen --------------------------------------------------
	c.mustRun("show (not found)", ExitError, "", "show", "999999")
	c.mustRun("attach text (invalid input)", ExitUsage, "", "attach", "text", attID, "--max-chars", "-1", "--json")
	c.mustRun("search (bad key name)", ExitUsage, "", "search", c.mail, "-a", "demo", "--key", c.key)
	c.mustRun("search (unknown key)", ExitError, "", "search", c.mail, "-a", "demo", "--key", "unbekannt")
	c.mustRun("mcp (wrong key)", ExitDenied, "", withEnv(EnvKey+"="+c.wrongKey), "mcp")
	c.mustRun("mcp (key as argument)", ExitUsage, "", "mcp", "--key", c.key)
	c.mustRun("account add (refused, password on stdin)", ExitUsage, c.pwBad+"\n", "account", "add", "x", "--imap", "imap.example.org:143",
		"--user", "u@example.org", "--tls", "none", "--insecure-test-server", "--password-stdin", "--json")

	// --- Abstürze (Panic mit Inhalt im Wert) -------------------------------
	for _, p := range []struct {
		when, op string
		args     []string
	}{
		{"cli", "messages.get", []string{"show", id}},
		{"handler", "messages.get", []string{"show", id, "--json"}},
		{"handler", "messages.search", []string{"search", c.mail, "-a", "demo"}},
		{"cli", "threads.get", []string{"thread", "--message", id, "--json"}},
		{"handler", "attachments.text", []string{"attach", "text", attID}},
		{"cli", "accounts.test", []string{"account", "test", "demo"}},
		{"handler", "keys.create", []string{"key", "create", "--name", "panic-" + p0(t), "--json"}},
	} {
		r := c.mustRun("panic "+p.when+":"+p.op, ExitError, "", append([]string{withEnv(ops.EnvTestPanic + "=" + p.when + ":" + p.op)}, p.args...)...)
		if !strings.Contains(r.stderr, "internal") || !strings.Contains(r.stderr, "panic value withheld") {
			t.Errorf("panic %s:%s: no crash report:\n%s", p.when, p.op, r.stderr)
		}
	}

	// --- MCP über stdio (eigener Prozess) -----------------------------------
	c.mcp(id, attID, "")
	c.mcp(id, attID, "handler:messages.get")

	// --- Audit, Export, doctor --------------------------------------------
	c.mustRun("audit", 0, "", "audit", "--since", "1h", "--limit", "1000", "--json")
	c.mustRun("audit (text)", 0, "", "audit", "--since", "1h", "--limit", "1000")
	c.mustRun("audit export", 0, "", "audit", "export", "--json")
	c.mustRun("audit verify", 0, "", "audit", "verify", "--json")
	c.mustRun("doctor", 0, "", "doctor")
	c.mustRun("doctor --json", 0, "", "doctor", "--json")
	sec := c.mustRun("doctor --secrets", 0, "", "doctor", "--secrets")
	if !strings.Contains(sec.stdout, "result: ok") || !strings.Contains(sec.stdout, "secrets in audit export") {
		t.Errorf("doctor --secrets:\n%s", sec.stdout)
	}
	c.mustRun("doctor --secrets --json", 0, "", "doctor", "--secrets", "--json")

	// --- Kanäle prüfen ------------------------------------------------------
	for _, r := range c.runs {
		c.checkOutput(r)
	}
	c.checkFiles()

	// --- audit verify: unverändert ok (Exit 0), eine Zeile verändert → mismatch (Exit 1)
	files, _ := filepath.Glob(filepath.Join(c.exportDir, "postkauz-audit-*.jsonl"))
	if len(files) == 0 {
		t.Fatal("no export files")
	}
	last := files[len(files)-1]
	b, _ := os.ReadFile(last)
	changed := bytes.Replace(b, []byte(`"result":"ok"`), []byte(`"result":"denied"`), 1)
	if bytes.Equal(changed, b) {
		t.Fatal("nothing to change in the export")
	}
	if err := os.WriteFile(last, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	r := c.run("audit verify (changed line)", "", "audit", "verify")
	if r.code != ExitError || !strings.HasPrefix(r.stdout, "mismatch:") || !strings.Contains(r.stdout, "export file was changed (line ") ||
		!strings.Contains(r.stdout, "hash chain is broken (line ") || !strings.Contains(r.stderr, "audit export does not match") {
		t.Fatalf("verify after change: %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
	_ = os.WriteFile(last, b, 0o600)
	if r := c.run("audit verify (restored)", "", "audit", "verify"); r.code != 0 || !strings.HasPrefix(r.stdout, "ok: ") {
		t.Fatalf("verify after restore: %d\n%s\n%s", r.code, r.stdout, r.stderr)
	}
}

// p0 liefert einen kurzen eindeutigen Namensteil.
func p0(t *testing.T) string {
	t.Helper()
	return randHex(t, 3)
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

type canaryRun struct {
	name           string
	code           int
	stdout, stderr string
	mailOK         bool // lesendes Kommando: Mailtext erlaubt (nur stdout)
	keyOK          bool // key create: Schlüssel erlaubt (nur stdout)
}

type canaryEnv struct {
	t                    *testing.T
	bin, dir, dataDir    string
	exportDir            string
	env                  []string
	pw, pwBad, mail, key string
	wrongKey             string
	runs                 []*canaryRun
	mcpOut               []string // Texte aus MCP-Ergebnissen mit Mailinhalt
}

func newCanaryEnv(t *testing.T) *canaryEnv {
	t.Helper()
	dir := t.TempDir()
	c := &canaryEnv{t: t, dir: dir, dataDir: filepath.Join(dir, "data"), exportDir: filepath.Join(dir, "zweiter-ort"),
		pw: "KanarienPasswort-" + randHex(t, 8), pwBad: "FalschesKanarienPasswort-" + randHex(t, 8),
		mail: "KANARIEMAILTEXT" + strings.ToUpper(randHex(t, 6)), wrongKey: ops.KeyPrefix + strings.Repeat("Q", 43)}
	c.bin = buildProgram(t, dir)
	cfg := filepath.Join(dir, "config.toml")
	// TOML-Literal (einfache Anführungszeichen): Backslashes unter Windows.
	if err := os.WriteFile(cfg, []byte("[secrets]\nbackend = \"insecure-file\"\n[audit]\nexport_dir = '"+c.exportDir+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "POSTKAUZ_") {
			c.env = append(c.env, kv)
		}
	}
	c.env = append(c.env, "POSTKAUZ_CONFIG="+cfg, "POSTKAUZ_DATA_DIR="+c.dataDir)
	return c
}

// buildProgram baut cmd/postkauz wie make build (ohne CGO).
func buildProgram(t *testing.T, dir string) string {
	t.Helper()
	goBin, err := exec.LookPath("go")
	if err != nil {
		goBin = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	bin := filepath.Join(dir, "postkauz")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command(goBin, "build", "-trimpath", "-o", bin, "./cmd/postkauz")
	cmd.Dir = filepath.Join("..", "..")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// withEnv markiert ein Argument als zusätzliche Umgebungsvariable.
func withEnv(kv string) string { return "\x00env:" + kv }

func (c *canaryEnv) run(name, stdin string, args ...string) *canaryRun {
	c.t.Helper()
	env := append([]string(nil), c.env...)
	var rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "\x00env:") {
			env = append(env, strings.TrimPrefix(a, "\x00env:"))
			continue
		}
		rest = append(rest, a)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.bin, rest...)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := &canaryRun{name: name, stdout: out.String(), stderr: errb.String()}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		r.code = ee.ExitCode()
	case err != nil:
		c.t.Fatalf("%s: %v", name, err)
	}
	c.runs = append(c.runs, r)
	return r
}

func (c *canaryEnv) mustRun(name string, code int, stdin string, args ...string) *canaryRun {
	c.t.Helper()
	r := c.run(name, stdin, args...)
	if r.code != code {
		c.t.Fatalf("%s: exit %d, want %d\nstdout:\n%s\nstderr:\n%s", name, r.code, code, r.stdout, r.stderr)
	}
	return r
}

// reading markiert ein lesendes Kommando; must: der Mailtext muss erscheinen.
func (c *canaryEnv) reading(r *canaryRun, must bool) {
	c.t.Helper()
	r.mailOK = true
	if must && !strings.Contains(r.stdout, c.mail) {
		c.t.Errorf("%s: mail text missing (the test would prove nothing):\n%s", r.name, r.stdout)
	}
	if strings.Contains(r.stdout, "{") && strings.Contains(r.stdout, c.mail) && !strings.Contains(r.stdout, `"untrusted":true`) {
		c.t.Errorf("%s: mail text in JSON without the untrusted envelope", r.name)
	}
}

// leaks nennt die Kanarienwerte, die in s stehen und dort nicht dürfen.
func (c *canaryEnv) leaks(s string, mailOK, keyOK bool) []string {
	var out []string
	for name, v := range map[string]string{"password": c.pw, "wrong password": c.pwBad, "wrong API key": c.wrongKey} {
		if strings.Contains(s, v) {
			out = append(out, name)
		}
	}
	if c.key != "" && !keyOK && strings.Contains(s, c.key) {
		out = append(out, "API key")
	}
	if !mailOK && strings.Contains(s, c.mail) {
		out = append(out, "mail text")
	}
	return out
}

func (c *canaryEnv) checkOutput(r *canaryRun) {
	c.t.Helper()
	if l := c.leaks(r.stdout, r.mailOK, r.keyOK); len(l) > 0 {
		c.t.Errorf("%s: stdout contains %v:\n%s", r.name, l, r.stdout)
	}
	if l := c.leaks(r.stderr, false, false); len(l) > 0 {
		c.t.Errorf("%s: stderr contains %v:\n%s", r.name, l, r.stderr)
	}
}

// checkFiles: Log, Audit-Export und alle Dateien des Datenverzeichnisses.
// Die Datenbank und die Blobs enthalten den Mailtext zu Recht (Spiegel),
// aber nie ein Passwort oder einen Schlüssel.
func (c *canaryEnv) checkFiles() {
	c.t.Helper()
	logb, err := os.ReadFile(filepath.Join(c.dataDir, "postkauz.log"))
	if err != nil || len(logb) == 0 {
		c.t.Fatalf("log: %v", err)
	}
	if l := c.leaks(string(logb), false, false); len(l) > 0 {
		c.t.Errorf("log contains %v", l)
	}
	if !bytes.Contains(logb, []byte(`"msg":"panic`)) {
		c.t.Errorf("log: no crash report")
	}
	n := 0
	_ = filepath.WalkDir(c.exportDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		n++
		b, _ := os.ReadFile(p)
		if l := c.leaks(string(b), false, false); len(l) > 0 {
			c.t.Errorf("audit export %s contains %v", filepath.Base(p), l)
		}
		return nil
	})
	if n < 2 {
		c.t.Errorf("audit export: %d files", n)
	}
	_ = filepath.WalkDir(c.dataDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		if l := c.leaks(string(b), true, false); len(l) > 0 {
			c.t.Errorf("data file %s contains %v", p, l)
		}
		return nil
	})
}

// mcp startet `postkauz mcp` als Kindprozess und ruft Tools und Resources.
// Mit panicAt wird der Abruf der Mail zum Absturz: Fehler ohne Inhalt.
func (c *canaryEnv) mcp(id, attID, panicAt string) {
	c.t.Helper()
	cmd := exec.Command(c.bin, "mcp")
	cmd.Env = append(append([]string(nil), c.env...), EnvKey+"="+c.key)
	if panicAt != "" {
		cmd.Env = append(cmd.Env, ops.EnvTestPanic+"="+panicAt)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "canary", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		c.t.Fatalf("mcp connect: %v (%s)", err, stderr.String())
	}
	call := func(tool string, args map[string]any, mailOK bool) string {
		c.t.Helper()
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			c.t.Fatalf("mcp %s: %v", tool, err)
		}
		var b strings.Builder
		for _, ct := range res.Content {
			if tc, ok := ct.(*sdk.TextContent); ok {
				b.WriteString(tc.Text)
			}
		}
		s := b.String()
		if l := c.leaks(s, mailOK, false); len(l) > 0 {
			c.t.Errorf("mcp %s (%s): result contains %v", tool, panicAt, l)
		}
		if mailOK && strings.Contains(s, c.mail) && !strings.Contains(s, `"untrusted":true`) {
			c.t.Errorf("mcp %s: mail text outside the envelope", tool)
		}
		return s
	}
	if panicAt != "" {
		s := call("mail_get_message", map[string]any{"id": json.Number(id)}, false)
		if !strings.Contains(s, `"code":"internal"`) {
			c.t.Errorf("mcp panic: %s", s)
		}
		// Der Server läuft weiter.
		call("mail_get_policy", map[string]any{}, false)
	} else {
		if s := call("mail_get_message", map[string]any{"id": json.Number(id)}, true); !strings.Contains(s, c.mail) {
			c.t.Errorf("mcp mail_get_message: mail text missing: %s", s)
		}
		call("mail_search", map[string]any{"query": c.mail}, true)
		call("mail_get_thread", map[string]any{"message": json.Number(id)}, true)
		if s := call("mail_get_attachment_text", map[string]any{"id": json.Number(attID)}, true); !strings.Contains(s, c.mail) {
			c.t.Errorf("mcp mail_get_attachment_text: mail text missing: %s", s)
		}
		call("mail_list_accounts", map[string]any{}, false)
		call("mail_list_attachments", map[string]any{"message": json.Number(id)}, false)
		call("mail_audit_log", map[string]any{"since": "1h"}, false)
		call("mail_get_policy", map[string]any{}, false)
		res, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "mail://demo/message/" + id})
		if err != nil || len(res.Contents) == 0 || !strings.Contains(res.Contents[0].Text, `"untrusted":true`) {
			c.t.Errorf("mcp resource: %v %+v", err, res)
		}
	}
	_ = cs.Close()
	_ = cmd.Wait()
	if l := c.leaks(stderr.String(), false, false); len(l) > 0 {
		c.t.Errorf("mcp stderr (%s) contains %v:\n%s", panicAt, l, stderr.String())
	}
}

// writeMbox schreibt eine mbox-Datei mit einer Kanarien-Mail für den Import.
func (c *canaryEnv) writeMbox() string {
	c.t.Helper()
	p := filepath.Join(c.dir, "import.mbox")
	body := "From - Thu Oct 15 10:00:00 2026\n" +
		"Message-ID: <kanarie-import@example.org>\nFrom: Archiv <archiv@example.org>\nTo: <b@example.org>\n" +
		"Subject: Importierte Nachricht\nDate: Thu, 15 Oct 2026 10:00:00 +0200\n\n" +
		"Alter Text mit " + c.mail + " aus dem Archiv.\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		c.t.Fatal(err)
	}
	return p
}

// canaryServer: IMAP-Server im Prozess mit einer Mail, deren Text und
// Anhang den Kanarien-Mailtext tragen, und einer Antwort darauf.
func canaryServer(t *testing.T, c *canaryEnv) string {
	t.Helper()
	u := imapmemserver.NewUser("tester@example.invalid", c.pw)
	for _, mb := range []string{"INBOX", "Sent"} {
		_ = u.Create(mb, nil)
	}
	msgs := []string{
		"Message-ID: <kanarie@example.org>\r\nFrom: Absender <a@example.org>\r\nTo: Empfang <b@example.org>\r\n" +
			"Subject: Kanarientest\r\nDate: Mon, 12 Oct 2026 10:00:00 +0200\r\nMIME-Version: 1.0\r\n" +
			"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
			"--b1\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nHallo, hier steht " + c.mail + " im Text.\r\n" +
			"--b1\r\nContent-Type: text/plain; name=\"notiz.txt\"\r\nContent-Disposition: attachment; filename=\"notiz.txt\"\r\n\r\n" +
			"Notiz mit " + c.mail + " im Anhang.\r\n--b1--\r\n",
		"Message-ID: <kanarie-antwort@example.org>\r\nIn-Reply-To: <kanarie@example.org>\r\nReferences: <kanarie@example.org>\r\n" +
			"From: Empfang <b@example.org>\r\nTo: Absender <a@example.org>\r\nSubject: Re: Kanarientest\r\n" +
			"Date: Mon, 12 Oct 2026 11:00:00 +0200\r\n\r\nDanke.\r\n",
	}
	for _, m := range msgs {
		if _, err := u.Append("INBOX", litReader{bytes.NewReader([]byte(m))}, &imap.AppendOptions{}); err != nil {
			t.Fatal(err)
		}
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
	return ln.Addr().String()
}
