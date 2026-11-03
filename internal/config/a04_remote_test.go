package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A04 Teil 4: Adressen der Ziele von mcp --remote.
func TestA04CheckRemoteURL(t *testing.T) {
	for _, ok := range []string{
		"https://office-pc.example.ts.net/mcp",
		"https://office-pc.example.ts.net:8443/postkauz/mcp",
		"http://127.0.0.1:8787/mcp",
		"http://[::1]:8787/mcp",
		"http://LOCALHOST:8787/mcp",
	} {
		if _, err := CheckRemoteURL(ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for raw, want := range map[string]string{
		"":                                       "empty",
		"http://office-pc.example.ts.net/mcp":    "https",
		"http://100.64.0.1:8787/mcp":             "https",
		"ftp://office-pc.example.ts.net/mcp":     "https://",
		"https://office-pc.example.ts.net":       "path",
		"https://office-pc.example.ts.net/":      "path",
		"https://user:pw@example.org/mcp":        "user name",
		"https://office-pc.example.ts.net/mcp#x": "fragment",
		"https:///mcp":                           "host",
		"https://office-pc.example.ts.net/m cp":  "space",
		`https://office-pc.example.ts.net/"mcp`:  "quote",
		"https://office-pc.example.ts.net/mcp\n": "control",
	} {
		_, err := CheckRemoteURL(raw)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", raw, err, want)
		}
		// Die Meldung nennt nie die Adresse selbst (sie könnte ein
		// Passwort enthalten).
		if err != nil && strings.Contains(raw, "pw@") && strings.Contains(err.Error(), "pw") {
			t.Errorf("%q: error repeats the credentials: %v", raw, err)
		}
	}
}

// A04 Teil 4: remote add/remove ändern nur ihren Abschnitt; Kommentare,
// andere Abschnitte und CRLF bleiben; ein Fehler in [remote] hält nur die
// Fernbefehle auf (RemoteErr), nicht das Laden.
func TestA04RemoteFileEdits(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "sub", "config.toml")
	rf := RemoteFile{Path: f}
	// Datei fehlt: wird angelegt.
	if err := rf.SetRemote("buero", "https://office-pc.example.ts.net/mcp"); err != nil {
		t.Fatal(err)
	}
	got, err := rf.Remotes()
	if err != nil || got["buero"].URL != "https://office-pc.example.ts.net/mcp" {
		t.Fatalf("after add: %v %v", got, err)
	}
	if fi, err := os.Stat(f); err != nil || (fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/') {
		t.Fatalf("mode: %v %v", fi, err)
	}

	orig := "# meine Einstellungen\r\nlog_level = \"warn\"\r\n\r\n[remote.buero]\r\nurl = \"https://alt.example.ts.net/mcp\"\r\n\r\n# Kommentar bleibt\r\n[http]\r\nlisten = \"127.0.0.1:8787\"\r\n"
	if err := os.WriteFile(f, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rf.SetRemote("buero", "https://neu.example.ts.net/mcp"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(f)
	want := strings.Replace(orig, "https://alt.example.ts.net/mcp", "https://neu.example.ts.net/mcp", 1)
	if string(b) != want {
		t.Fatalf("replace changed more than the url:\n%q\nwant\n%q", b, want)
	}
	if err := rf.SetRemote("zweit", "http://127.0.0.1:8787/mcp"); err != nil {
		t.Fatal(err)
	}
	if got, _ := rf.Remotes(); len(got) != 2 || got["zweit"].URL != "http://127.0.0.1:8787/mcp" {
		t.Fatalf("two remotes: %v", got)
	}
	if ok, err := rf.RemoveRemote("zweit"); !ok || err != nil {
		t.Fatalf("remove zweit: %v %v", ok, err)
	}
	if b, _ := os.ReadFile(f); string(b) != want {
		t.Fatalf("add+remove did not restore the file:\n%q\nwant\n%q", b, want)
	}
	if ok, err := rf.RemoveRemote("fehlt"); ok || err != nil {
		t.Fatalf("remove unknown: %v %v", ok, err)
	}
	if ok, err := rf.RemoveRemote("buero"); !ok || err != nil {
		t.Fatalf("remove buero: %v %v", ok, err)
	}
	b, _ = os.ReadFile(f)
	if strings.Contains(string(b), "remote") || !strings.Contains(string(b), "# Kommentar bleibt") || !strings.Contains(string(b), "[http]") {
		t.Fatalf("after remove:\n%s", b)
	}

	// Andere Schreibweise (Punkt-Schlüssel): nicht still verdoppeln.
	if err := os.WriteFile(f, []byte("remote.buero.url = \"https://alt.example.ts.net/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rf.SetRemote("buero", "https://neu.example.ts.net/mcp"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatalf("dotted key: %v", err)
	}
	if _, err := rf.RemoveRemote("buero"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Fatalf("remove dotted key: %v", err)
	}
	// Syntaxfehler der Datei: nichts schreiben.
	if err := os.WriteFile(f, []byte("log_level = \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := rf.SetRemote("buero", "https://neu.example.ts.net/mcp"); err == nil {
		t.Fatal("broken file accepted")
	}
	if b, _ := os.ReadFile(f); string(b) != "log_level = \n" {
		t.Fatalf("broken file changed: %q", b)
	}
	// Ungültige Eingaben.
	if err := rf.SetRemote("Büro", "https://neu.example.ts.net/mcp"); err == nil {
		t.Fatal("bad name accepted")
	}
	if err := rf.SetRemote("buero", "http://neu.example.ts.net/mcp"); err == nil {
		t.Fatal("http to a non-loopback host accepted")
	}
}

func TestA04RemoteErrDoesNotStopLoad(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	f := filepath.Join(dir, "config.toml")
	t.Setenv(EnvConfig, f)
	for text, want := range map[string]string{
		"log_level = \"debug\"\n[remote.buero]\nurl = \"http://office-pc.example.ts.net/mcp\"\n":               "https",
		"log_level = \"debug\"\n[remote.buero]\nurl = \"https://x.example.ts.net/mcp\"\nkey = \"pk_geheim\"\n": "unknown keys: remote.buero.key",
		"log_level = \"debug\"\n[remote.Buero]\nurl = \"https://x.example.ts.net/mcp\"\n":                      "invalid remote name",
		"log_level = \"debug\"\n[remote.buero]\nurl = 5\n":                                                     "[remote]",
	} {
		if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg, _, err := Load()
		if err != nil || cfg.LogLevel != "debug" || cfg.RemoteErr == nil || !strings.Contains(cfg.RemoteErr.Error(), want) {
			t.Errorf("%q: load %v, RemoteErr %v", text, err, cfg.RemoteErr)
		}
		if cfg.RemoteErr != nil && strings.Contains(cfg.RemoteErr.Error(), "pk_geheim") {
			t.Errorf("RemoteErr repeats a value: %v", cfg.RemoteErr)
		}
	}
	if err := os.WriteFile(f, []byte("[remote.buero]\nurl = \"https://x.example.ts.net/mcp\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load()
	if err != nil || cfg.RemoteErr != nil || cfg.Remote["buero"].URL != "https://x.example.ts.net/mcp" {
		t.Fatalf("valid: %v %v %v", err, cfg.RemoteErr, cfg.Remote)
	}
}
