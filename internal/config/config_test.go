package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsWithoutFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfig, filepath.Join(dir, "missing.toml"))
	t.Setenv(EnvDataDir, filepath.Join(dir, "data"))
	t.Setenv(EnvSecretsBackend, "")
	cfg, p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigExists || cfg.Secrets.Backend != BackendAuto || cfg.LogLevel != "info" {
		t.Fatalf("cfg=%+v paths=%+v", cfg, p)
	}
	if p.Database != filepath.Join(dir, "data", "postkauz.db") || p.Blobs != filepath.Join(dir, "data", "blobs") {
		t.Fatalf("paths = %+v", p)
	}
}

func TestLoadFileAndEnvPrecedence(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgFile, []byte("data_dir = \""+filepath.ToSlash(filepath.Join(dir, "fromfile"))+"\"\nlog_level = \"debug\"\n[secrets]\nbackend = \"keyring\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, cfgFile)
	t.Setenv(EnvDataDir, "")
	t.Setenv(EnvSecretsBackend, "")
	cfg, p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !p.ConfigExists || cfg.Secrets.Backend != BackendKeyring || cfg.LogLevel != "debug" || p.DataDir != filepath.Join(dir, "fromfile") {
		t.Fatalf("cfg=%+v paths=%+v", cfg, p)
	}
	// Umgebung schlägt Datei
	t.Setenv(EnvDataDir, filepath.Join(dir, "fromenv"))
	t.Setenv(EnvSecretsBackend, BackendInsecureFile)
	cfg, p, err = Load()
	if err != nil || p.DataDir != filepath.Join(dir, "fromenv") || cfg.Secrets.Backend != BackendInsecureFile {
		t.Fatalf("env override: cfg=%+v paths=%+v err=%v", cfg, p, err)
	}
}

func TestLoadRejectsUnknownKeysAndBadValues(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	for name, body := range map[string]string{
		"unknown key":   "data_dri = \"x\"\n",
		"bad backend":   "[secrets]\nbackend = \"plaintext\"\n",
		"bad log level": "log_level = \"loud\"\n",
		"invalid toml":  "data_dir = \n",
		"bare number":   "[imap]\ncommand_timeout = 30\n", // TOML-Zahl = Nanosekunden
		"negative":      "[imap]\ncommand_max_duration = \"-1m\"\n",
		"max < timeout": "[imap]\ncommand_timeout = \"2m\"\ncommand_max_duration = \"1m\"\n",
		"bad duration":  "[imap]\ncommand_timeout = \"zehn\"\n",
		"max msg < 0":   "[imap]\nmax_message_bytes = -1\n",
		"max msg 1":     "[imap]\nmax_message_bytes = 1\n",
		"max msg < MiB": "[imap]\nmax_message_bytes = 1048575\n",
		"max msg > cap": "[imap]\nmax_message_bytes = 268435457\n",
		"max msg text":  "[imap]\nmax_message_bytes = \"256MiB\"\n",
		// S2.1: [paths], [operator], [draft]
		"unknown draft key":  "[draft]\nmailbx = \"Drafts\"\n",
		"unknown paths key":  "[paths]\nattach = \"x\"\n",
		"unknown op key":     "[operator]\nmail = \"x@example.org\"\n",
		"ttl too short":      "[draft]\napproval_ttl = \"30s\"\n",
		"ttl too long":       "[draft]\napproval_ttl = \"9000h\"\n",
		"ttl negative":       "[draft]\napproval_ttl = \"-1h\"\n",
		"attach bytes < 0":   "[draft]\nmax_attachment_bytes = -1\n",
		"attach bytes > cap": "[draft]\nmax_attachment_bytes = 268435457\n",
		"quote placeholder":  "[draft]\nquote_line = \"Am {datum} schrieb {from}:\"\n",
		"quote two lines":    "[draft]\nquote_line = \"On {date}\\n{from} wrote:\"\n",
		"mailbox line break": "[draft]\nmailbox = \"Drafts\\nINBOX\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			f := filepath.Join(dir, strings.ReplaceAll(name, " ", "_")+".toml")
			if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv(EnvConfig, f)
			if _, _, err := Load(); err == nil {
				t.Error("expected error")
			}
		})
	}
}

// S2.1: Standards und Werte für [paths] attach_dir, [operator] name und
// [draft] (A02 E7, E10, E11, E12).
func TestLoadDraftPathsOperator(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	t.Setenv(EnvConfig, filepath.Join(dir, "fehlt.toml"))
	cfg, p, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Draft.ApprovalTTL != 168*time.Hour || cfg.Draft.MaxAttachmentBytes != 25<<20 || cfg.Draft.QuoteLine != "On {date}, {from} wrote:" ||
		cfg.Draft.Mailbox != "" || cfg.Operator.Name != "" || p.AttachDir != filepath.Join(dir, "attach") || cfg.Paths.AttachDir != p.AttachDir {
		t.Fatalf("defaults: %+v %+v", cfg.Draft, p)
	}
	f := filepath.Join(dir, "config.toml")
	body := "[paths]\nattach_dir = \"anhaenge\"\n[operator]\nname = \"Demo Operator\"\n" +
		"[draft]\napproval_ttl = \"24h\"\nmax_attachment_bytes = 1048576\nmailbox = \" Entwürfe \"\nquote_line = \"Am {date} schrieb {from}:\"\n"
	if err := os.WriteFile(f, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, f)
	cfg, p, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Draft.ApprovalTTL != 24*time.Hour || cfg.Draft.MaxAttachmentBytes != 1<<20 || cfg.Draft.Mailbox != "Entwürfe" ||
		cfg.Draft.QuoteLine != "Am {date} schrieb {from}:" || cfg.Operator.Name != "Demo Operator" || p.AttachDir != filepath.Join(dir, "anhaenge") {
		t.Fatalf("values: %+v %+v %+v", cfg.Draft, cfg.Operator, p)
	}
	abs := filepath.Join(t.TempDir(), "attach")
	if err := os.WriteFile(f, []byte("[paths]\nattach_dir = '"+abs+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, p, err = Load(); err != nil || p.AttachDir != abs {
		t.Fatalf("absolute attach_dir: %s, %v", p.AttachDir, err)
	}
}

func TestLoadIMAPTimeouts(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(f, []byte("[imap]\ncommand_timeout = \"45s\"\ncommand_max_duration = \"20m\"\nmax_message_bytes = 1048576\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, f)
	t.Setenv(EnvDataDir, dir)
	t.Setenv(EnvSecretsBackend, "")
	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IMAP.CommandTimeout != 45*time.Second || cfg.IMAP.CommandMaxDuration != 20*time.Minute || cfg.IMAP.MaxMessageBytes != 1<<20 {
		t.Fatalf("imap = %+v", cfg.IMAP)
	}
}

func TestEnsureDataDirIsPrivate(t *testing.T) {
	p := Paths{DataDir: filepath.Join(t.TempDir(), "a", "b")}
	if err := EnsureDataDir(p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p.DataDir)
	if err != nil || !st.IsDir() {
		t.Fatal(err)
	}
	if perm := st.Mode().Perm(); os.PathSeparator == '/' && perm != 0o700 {
		t.Errorf("perm = %o, want 700", perm)
	}
}

func TestCloudSyncWarning(t *testing.T) {
	if CloudSyncWarning(filepath.Join("C:", "Users", "x", "OneDrive", "postkauz")) == "" {
		t.Error("OneDrive must warn")
	}
	if CloudSyncWarning(filepath.Join("home", "x", ".local", "share", "postkauz")) != "" {
		t.Error("plain path must not warn")
	}
}

// A02 A3 (Frage 6c): Ein vorhandenes Datenverzeichnis und Log mit weiten
// Rechten werden beim Öffnen verschärft (Besitzerrechte bleiben); ein
// Fehler dabei wird gemeldet, nicht abgebrochen. Unter Windows geschieht
// nichts (dort gilt der Profilordner).
func TestRestrictPermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	p := Paths{DataDir: dir, Log: filepath.Join(dir, "postkauz.log")}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Log, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil { // umask umgehen
		t.Fatal(err)
	}
	if err := os.Chmod(p.Log, 0o644); err != nil {
		t.Fatal(err)
	}
	changes := RestrictPermissions(p)
	if runtime.GOOS == "windows" {
		if changes != nil {
			t.Fatalf("windows: %+v", changes)
		}
		return
	}
	if len(changes) != 2 || changes[0].Err != nil || changes[0].Old != 0o755 || changes[0].New != 0o700 ||
		changes[1].Err != nil || changes[1].Old != 0o644 || changes[1].New != 0o600 {
		t.Fatalf("changes = %+v", changes)
	}
	for path, want := range map[string]os.FileMode{dir: 0o700, p.Log: 0o600} {
		if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != want {
			t.Fatalf("%s: %v %v, want %04o", path, fi.Mode().Perm(), err, want)
		}
	}
	if c := RestrictPermissions(p); len(c) != 0 {
		t.Fatalf("second call changed %+v", c)
	}
	// Fehlende Pfade sind kein Fehler.
	if c := RestrictPermissions(Paths{DataDir: filepath.Join(dir, "missing")}); len(c) != 0 {
		t.Fatalf("missing: %+v", c)
	}
	// Scheitert chmod, kommt der Fehler zurück (der Aufrufer warnt im Log).
	_ = os.Chmod(dir, 0o750)
	orig := chmod
	chmod = func(string, os.FileMode) error { return os.ErrPermission }
	defer func() { chmod = orig }()
	if c := RestrictPermissions(p); len(c) != 1 || c[0].Path != dir || !errors.Is(c[0].Err, os.ErrPermission) {
		t.Fatalf("failing chmod: %+v", c)
	}
}

// Gegenprüfung 21.10.2026, Befund 7 (A3): RestrictPermissions folgt keinem
// Symlink (das Ziel kann ein fremdes, geteiltes Verzeichnis sein), ändert
// nichts, was einem anderen Nutzer gehört, und erhält Sonderbits wie Setgid
// (nur die Bits für Gruppe und andere fallen weg).
func TestRestrictPermissionsSymlinkOwnerSpecialBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permissions")
	}
	base := t.TempDir()
	shared := filepath.Join(base, "shared")
	if err := os.Mkdir(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(shared, 0o755)
	link := filepath.Join(base, "data")
	if err := os.Symlink(shared, link); err != nil {
		t.Fatal(err)
	}
	c := RestrictPermissions(Paths{DataDir: link})
	if len(c) != 1 || c[0].Skip != "symlink" || c[0].Err != nil || OctalMode(c[0].Old) != "0755" {
		t.Fatalf("symlink: %+v", c)
	}
	if fi, _ := os.Stat(shared); fi.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target changed to %04o", fi.Mode().Perm())
	}

	// Setgid bleibt erhalten: 2775 -> 2700.
	sg := filepath.Join(base, "setgid")
	if err := os.Mkdir(sg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(sg, 0o775|os.ModeSetgid); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(sg); fi.Mode()&os.ModeSetgid == 0 {
		t.Skip("file system does not keep the setgid bit")
	}
	c = RestrictPermissions(Paths{DataDir: sg})
	if len(c) != 1 || c[0].Err != nil || c[0].Skip != "" || OctalMode(c[0].Old) != "2775" || OctalMode(c[0].New) != "2700" {
		t.Fatalf("setgid: %+v", c)
	}
	if fi, _ := os.Stat(sg); fi.Mode()&os.ModeSetgid == 0 || fi.Mode().Perm() != 0o700 {
		t.Fatalf("setgid dir is %v", fi.Mode())
	}

	// Fremder Besitzer: nichts ändern, nur melden.
	other := filepath.Join(base, "other")
	if err := os.Mkdir(other, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(other, 0o755)
	orig := euid
	euid = func() int { return orig() + 4711 }
	defer func() { euid = orig }()
	c = RestrictPermissions(Paths{DataDir: other})
	if len(c) != 1 || c[0].Skip != "foreign_owner" || c[0].Err != nil {
		t.Fatalf("foreign owner: %+v", c)
	}
	if fi, _ := os.Stat(other); fi.Mode().Perm() != 0o755 {
		t.Fatalf("foreign directory changed to %04o", fi.Mode().Perm())
	}
}
