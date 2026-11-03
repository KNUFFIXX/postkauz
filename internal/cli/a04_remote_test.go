package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/remote"
	"github.com/KNUFFIXX/postkauz/internal/secrets"
)

// A04 Teil 4: remote add|list|test|remove. Der Schlüssel kommt nur über
// stdin, liegt danach im Schlüsselbund (nie in der Konfiguration) und steht
// in keiner Ausgabe, keinem Log und keinem Audit-Eintrag.
func TestA04RemoteCommands(t *testing.T) {
	dataDir, kr := setupEnv(t)
	cfgFile := os.Getenv("POSTKAUZ_CONFIG")
	url := remoteStub(t)
	var all strings.Builder
	step := func(want int, stdin string, args ...string) string {
		t.Helper()
		code, out, errs := runIn(t, stdin, args...)
		all.WriteString(out + errs)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out + errs
	}

	// Kein Schlüssel im Argument: --key gibt es nicht, ein Schlüssel als
	// Argument ist ein Fehler der Argumente.
	step(ExitUsage, "", "remote", "add", "buero", "--url", url, remoteStubKey)
	// Kein Schlüssel, falsche Form, falsche Adresse.
	step(ExitUsage, "\n", "remote", "add", "buero", "--url", url, "--key-stdin")
	if out := step(ExitUsage, "geheim-KANARIE-ohne-praefix\n", "remote", "add", "buero", "--url", url, "--key-stdin"); strings.Contains(out, "geheim-KANARIE") {
		t.Fatalf("error repeats the key: %s", out)
	}
	step(ExitUsage, remoteStubKey+"\n", "remote", "add", "buero", "--url", "http://office-pc.example.ts.net/mcp", "--key-stdin")
	step(ExitUsage, remoteStubKey+"\n", "remote", "add", "buero", "--key-stdin")

	// Trockenlauf ändert nichts.
	out := step(ExitOK, remoteStubKey+"\n", "remote", "add", "buero", "--url", url, "--key-stdin", "--dry-run")
	if !strings.Contains(out, "dry run: would add remote buero") {
		t.Fatalf("dry run: %s", out)
	}
	if _, err := os.Stat(cfgFile); err == nil {
		t.Fatal("dry run wrote the configuration")
	}

	out = step(ExitOK, remoteStubKey+"\n", "remote", "add", "buero", "--url", url, "--key-stdin")
	for _, want := range []string{"remote buero added: " + url, "key: stored in remote/buero", "read back and compared", "next: postkauz remote test buero"} {
		if !strings.Contains(out, want) {
			t.Fatalf("remote add lacks %q:\n%s", want, out)
		}
	}
	if v, _ := kr.Get(remote.KeyRef("buero")); v.Reveal() != remoteStubKey {
		t.Fatal("key not in the keyring")
	}
	b, err := os.ReadFile(cfgFile)
	if err != nil || !strings.Contains(string(b), "[remote.buero]") || !strings.Contains(string(b), url) || strings.Contains(string(b), remoteStubKey) {
		t.Fatalf("configuration: %v\n%s", err, b)
	}
	out = step(ExitOK, "", "remote", "list")
	if !strings.Contains(out, "buero") || !strings.Contains(out, "stored") || !strings.Contains(out, url) {
		t.Fatalf("remote list: %s", out)
	}
	out = step(ExitOK, "", "remote", "test", "buero")
	for _, want := range []string{"server:   stub 1", "tools:    2", "key:      zweiter-pc (role assistant)", "ok: mcp --remote buero reaches the server"} {
		if !strings.Contains(out, want) {
			t.Fatalf("remote test lacks %q:\n%s", want, out)
		}
	}
	var js map[string]any
	if err := json.Unmarshal([]byte(step(ExitOK, "", "remote", "test", "buero", "--json")), &js); err != nil || js["tools"] != float64(2) {
		t.Fatalf("remote test --json: %v %v", err, js)
	}

	// Falscher Schlüssel: klarer Fehler mit Code.
	step(ExitOK, ops0Key("falsch")+"\n", "remote", "add", "buero", "--url", url, "--key-stdin")
	out = step(ExitError, "", "remote", "test", "buero", "--json")
	if !strings.Contains(out, `"code":"remote_key_refused"`) || !strings.Contains(out, "refused the API key (401") {
		t.Fatalf("wrong key: %s", out)
	}
	// Nicht erreichbar.
	step(ExitOK, remoteStubKey+"\n", "remote", "add", "tot", "--url", "http://127.0.0.1:1/mcp", "--key-stdin")
	if out := step(ExitError, "", "remote", "test", "tot", "--json"); !strings.Contains(out, `"code":"remote_unreachable"`) {
		t.Fatalf("unreachable: %s", out)
	}
	step(ExitError, "", "remote", "test", "fehlt")

	out = step(ExitOK, "", "remote", "remove", "buero")
	if !strings.Contains(out, "remote buero removed") {
		t.Fatalf("remove: %s", out)
	}
	if _, err := kr.Get(remote.KeyRef("buero")); err == nil {
		t.Fatal("key still in the keyring")
	}
	step(ExitError, "", "remote", "remove", "buero")
	// Wieder eintragen: Das Audit soll remote.add zeigen.
	step(ExitOK, remoteStubKey+"\n", "remote", "add", "buero", "--url", url, "--key-stdin")

	// Kanarie: Schlüssel weder in Ausgaben noch im Log noch im Audit.
	logb, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	auditOut := step(ExitOK, "", "audit", "--since", "1h", "--json")
	if !strings.Contains(auditOut, "remote.add") || !strings.Contains(auditOut, "remote:buero") {
		t.Fatalf("audit lacks remote.add: %s", auditOut)
	}
	for what, text := range map[string]string{"outputs": all.String(), "log": string(logb)} {
		for _, k := range []string{remoteStubKey, ops0Key("falsch")} {
			if strings.Contains(text, k) {
				t.Errorf("%s contain a key", what)
			}
		}
	}
	if strings.Contains(auditOut, "127.0.0.1") {
		t.Errorf("audit names the URL: %s", auditOut)
	}
}

func ops0Key(s string) string { return "pk_key_KANARIE-" + s + "-0123456789" }

// A04 Teil 4: mcp --remote reicht stdio 1:1 weiter, öffnet keine
// Datenbank und legt kein Datenverzeichnis an.
func TestA04MCPRemoteBridge(t *testing.T) {
	dataDir, kr := setupEnv(t)
	url := remoteStub(t)
	cfgFile := os.Getenv("POSTKAUZ_CONFIG")
	if err := os.WriteFile(cfgFile, []byte("[remote.buero]\nurl = \""+url+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := kr.Set(remote.KeyRef("buero"), secrets.Secret(remoteStubKey)); err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"desktop","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":"drei","method":"tools/call","params":{"name":"mail_get_policy","arguments":{}}}`,
	}, "\n") + "\n"
	code, out, errs := runIn(t, in, "mcp", "--remote", "buero")
	if code != ExitOK {
		t.Fatalf("exit %d\nstdout %s\nstderr %s", code, out, errs)
	}
	ids := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		var m struct {
			ID     any             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("not JSON-RPC: %s", l)
		}
		if m.Error != nil {
			t.Fatalf("error from the bridge: %s", l)
		}
		ids[strings.Trim(string(mustJSON(m.ID)), `"`)] = string(m.Result)
	}
	if !strings.Contains(ids["1"], `"protocolVersion"`) || !strings.Contains(ids["2"], `"mail_search"`) || !strings.Contains(ids["drei"], `zweiter-pc`) {
		t.Fatalf("answers: %v", ids)
	}
	if !strings.Contains(errs, "mcp --remote buero: forwarding to "+url) {
		t.Fatalf("stderr: %s", errs)
	}
	if strings.Contains(out+errs, remoteStubKey) {
		t.Fatal("key in the output")
	}
	if _, err := os.Stat(dataDir); err == nil {
		t.Fatal("mcp --remote created the data directory")
	}

	// Fehler als JSON-RPC-Fehler: falscher Schlüssel.
	_ = kr.Set(remote.KeyRef("buero"), secrets.Secret(ops0Key("falsch")))
	_, out, _ = runIn(t, in, "mcp", "--remote", "buero")
	if !strings.Contains(out, `"id":1`) || !strings.Contains(out, `"code":-32011`) || !strings.Contains(out, "refused the API key") {
		t.Fatalf("wrong key: %s", out)
	}
	// Ohne Schlüssel, unbekanntes Ziel, mit --http.
	_ = kr.Delete(remote.KeyRef("buero"))
	if code, _, errs := runIn(t, in, "mcp", "--remote", "buero"); code == ExitOK || !strings.Contains(errs, "remote add buero") {
		t.Fatalf("no key: %d %s", code, errs)
	}
	if code, _, errs := run(t, "mcp", "--remote", "fehlt"); code != ExitError || !strings.Contains(errs, "remote list") {
		t.Fatalf("unknown remote: %d %s", code, errs)
	}
	if code, _, _ := run(t, "mcp", "--remote", "buero", "--http"); code != ExitUsage {
		t.Fatalf("--remote with --http: %d", code)
	}
	if _, err := os.Stat(dataDir); err == nil {
		t.Fatal("mcp --remote created the data directory")
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// A04 Teil 4: doctor zeigt die Ziele und einen Fehler in [remote] (Exit 1),
// andere Kommandos laufen weiter.
func TestA04DoctorShowsRemote(t *testing.T) {
	setupEnv(t)
	cfgFile := os.Getenv("POSTKAUZ_CONFIG")
	if err := os.WriteFile(cfgFile, []byte("[remote.buero]\nurl = \"https://office-pc.example.ts.net/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "doctor"); code != ExitOK || !strings.Contains(out, "remote:    buero (mcp --remote") {
		t.Fatalf("doctor: %d %s %s", code, out, errs)
	}
	if err := os.WriteFile(cfgFile, []byte("[remote.buero]\nurl = \"http://office-pc.example.ts.net/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "doctor"); code != ExitError || !strings.Contains(out, "remote:    INVALID") {
		t.Fatalf("doctor with a broken [remote]: %d %s", code, out)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list with a broken [remote]: %d %s", code, errs)
	}
	if code, _, errs := run(t, "mcp", "--remote", "buero"); code == ExitOK || !strings.Contains(errs, "https") {
		t.Fatalf("mcp --remote with a broken [remote]: %d %s", code, errs)
	}
}
