package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// A08 Teil 1: Anhang-Sperre. Alle Pfade sind Platzhalter unter t.TempDir().

// a08Tree legt unter attach eine Datei je Pfad an (Ordner nach Bedarf).
func a08Tree(t *testing.T, attach string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(attach, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("Probe A08\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// wantGuard prüft eine Ablehnung attach_denied mit Regelart; die Meldung
// nennt weder Pfad noch Dateinamen.
func wantGuard(t *testing.T, err error, rule, secret string) {
	t.Helper()
	var e *Error
	if err == nil {
		t.Fatalf("no error, want attach_denied/%s", rule)
	}
	r, _, ok := GuardOf(err)
	if !ok || r != rule {
		t.Fatalf("error = %v (rule %q), want attach_denied/%s", err, r, rule)
	}
	if !strings.Contains(err.Error(), "attach_denied") || !strings.Contains(err.Error(), "rule: "+rule) {
		t.Fatalf("message %q lacks reason or rule", err)
	}
	if secret != "" && strings.Contains(err.Error(), secret) {
		t.Fatalf("message %q names the path or file", err)
	}
	if !asError(err, &e) || e.Field != "attachments[0]" {
		t.Fatalf("field = %+v", e)
	}
}

func asError(err error, e **Error) bool {
	x, ok := err.(*Error)
	if ok {
		*e = x
	}
	return ok
}

func TestA08GuardTable(t *testing.T) {
	base := t.TempDir()
	attach := filepath.Join(base, "Arbeit")
	nfd := norm.NFD.String("Passwörter")
	a08Tree(t, attach,
		"_secrets/a.txt", "_SECRETS/b.txt", nfd+"/c.txt", ".ssh/id.txt", "Kunden/.git/config",
		"vault.kdbx", "Vault.KDBX", "SECRETS.md", "SECRETS.md.bak_20261105_x", "token.json", "config.toml.bak",
		"claude_desktop_config_X.json", "id_ed25519.pub", "Ablage/mcp-token.json", "x.kdbx.bak", "alt.env.old", "secretary_letter.pdf",
		"Mieter/Brief 2026-11-12.pdf", "rechnung_key_account.pdf", "Schlüsselübergabe Protokoll.pdf", "liste.xlsm",
		"pkdata/notiz.txt", "Exporte/x.txt")
	if nfd == "Passwörter" {
		t.Fatal("NFD form equals NFC form; the test would not test anything")
	}
	symlinks := runtime.GOOS != "windows"
	if symlinks {
		if err := os.Symlink(filepath.Join(attach, "_secrets", "a.txt"), filepath.Join(attach, "harmlos.pdf")); err != nil {
			symlinks = false
		}
		if err := os.Symlink(filepath.Join(attach, ".ssh"), filepath.Join(attach, "ok")); err != nil {
			symlinks = false
		}
	}
	// PostKauz-Datenverzeichnis und export_dir liegen unter attach_dir.
	g, err := NewAttachGuard(nil, []string{"*.xlsm"}, OwnPaths{DataDir: filepath.Join(attach, "pkdata"),
		ExportDir: filepath.Join(attach, "Exporte")})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path, rule string // rule "" = erlaubt
		link       bool
	}{
		{"_secrets/a.txt", RuleDir, false},
		{"_SECRETS/b.txt", RuleDir, false},
		{nfd + "/c.txt", RuleDir, false},
		{norm.NFC.String(nfd) + "/c.txt", RuleDir, false}, // NFC angefragt, NFD auf der Platte: fehlt, aber gesperrt
		{".ssh/id.txt", RuleDir, false},
		{"Kunden/.git/config", RuleDir, false},
		{"_secrets/fehlt.txt", RuleDir, false}, // fehlt: trotzdem gesperrt, nicht attach_not_found
		{"vault.kdbx", RuleFile, false},
		{"Vault.KDBX", RuleFile, false},
		{"SECRETS.md", RuleFile, false},
		{"SECRETS.md.bak_20261105_x", RuleFile, false},
		{"token.json", RuleFile, false},
		{"config.toml.bak", RuleFile, false},
		{"claude_desktop_config_X.json", RuleFile, false},
		{"id_ed25519.pub", RuleFile, false},
		{"Ablage/mcp-token.json", RuleFile, false},
		{"x.kdbx.bak", RuleFile, false},
		{"alt.env.old", RuleFile, false},
		{"secretary_letter.pdf", RuleFile, false}, // bewusst: *secret*
		{"liste.xlsm", RuleFile, false},           // eigene Erweiterung
		{"pkdata/notiz.txt", RuleOwnData, false},
		{"Exporte/x.txt", RuleOwnData, false},
		{"brief.pdf:stream", RuleStream, false},
		{"Mieter/brief.pdf:Zone.Identifier:$DATA", RuleStream, false},
		{"harmlos.pdf", RuleDir, true},
		{"ok/id.txt", RuleDir, true},
		{"Mieter/Brief 2026-11-12.pdf", "", false},
		{"rechnung_key_account.pdf", "", false},
		{"Schlüsselübergabe Protokoll.pdf", "", false},
	} {
		t.Run(c.path, func(t *testing.T) {
			if c.link && !symlinks {
				t.Skip("symlinks not available")
			}
			for _, p := range []string{filepath.FromSlash(c.path), filepath.Join(attach, filepath.FromSlash(c.path))} {
				data, _, err := LoadFile("attachments[0]", attach, p, 0, g)
				if c.rule == "" {
					if err != nil || string(data) != "Probe A08\n" {
						t.Fatalf("%s: %v", p, err)
					}
					continue
				}
				wantGuard(t, err, c.rule, attach)
				if data != nil {
					t.Fatal("data returned for a blocked file")
				}
			}
		})
	}
	// Ohne Erweiterung ist .xlsm erlaubt (die Standardlisten kennen es nicht).
	if _, _, err := LoadFile("attachments[0]", attach, "liste.xlsm", 0, nil); err != nil {
		t.Fatalf("xlsm without extension: %v", err)
	}
}

// Liegt attach_dir im Datenverzeichnis (Standard <Daten>/attach), sperrt
// das Datenverzeichnis nicht alles; die einzelnen Orte (Datenbank, Blobs)
// bleiben gesperrt, auch wenn sie über einen Symlink erreichbar wären.
func TestA08OwnDataAroundAttachDir(t *testing.T) {
	data := t.TempDir()
	attach := filepath.Join(data, "attach")
	a08Tree(t, attach, "brief.pdf", "export/x.txt")
	if err := os.WriteFile(filepath.Join(data, "pk.db"), []byte("SQLite"), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := NewAttachGuard(nil, nil, OwnPaths{DataDir: data, ConfigDir: data, ExportDir: filepath.Join(attach, "export"),
		Entries: []string{filepath.Join(data, "pk.db")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadFile("attachments[0]", attach, "brief.pdf", 0, g); err != nil {
		t.Fatalf("attach_dir inside the data directory: %v", err)
	}
	_, _, err = LoadFile("attachments[0]", attach, "export/x.txt", 0, g)
	wantGuard(t, err, RuleOwnData, "")
	// attach_dir gleich dem Datenverzeichnis: Die Datenbank bleibt gesperrt.
	_, _, err = LoadFile("attachments[0]", data, "pk.db", 0, g)
	wantGuard(t, err, RuleOwnData, "")
	if _, _, err := LoadFile("attachments[0]", data, "attach/brief.pdf", 0, g); err != nil {
		t.Fatalf("attach_dir = data directory, ordinary file: %v", err)
	}
}

// Build: Datei per Path wie LoadFile; Bytes einer lokalen Datei (Origin
// SourceFile, etwa ein vor A08 gespeicherter Entwurf) werden nach dem Namen
// geprüft; Blob- und weitergeleitete Anhänge nicht.
func TestA08Build(t *testing.T) {
	attach := t.TempDir()
	a08Tree(t, attach, "_secrets/a.txt", "brief.txt")
	in := baseInput(KindNew)
	in.AttachDir = attach
	in.Attachments = []Attachment{{Path: "_secrets/a.txt"}}
	_, err := Build(in)
	wantGuard(t, err, RuleDir, "_secrets")
	for _, c := range []struct {
		origin string
		denied bool
	}{{SourceFile, true}, {"", false}, {SourceForward, false}} {
		in.Attachments = []Attachment{{Filename: "Vault.kdbx", Data: []byte("x"), Origin: c.origin}}
		m, err := Build(in)
		if c.denied {
			wantGuard(t, err, RuleFile, "Vault")
			continue
		}
		if err != nil || len(m.Attachments) != 1 || m.Attachments[0].Source != map[string]string{"": SourceBlob, SourceForward: SourceForward}[c.origin] {
			t.Fatalf("origin %q: %v", c.origin, err)
		}
	}
	in.Attachments = []Attachment{{Filename: "brief.txt", Data: []byte("x"), Origin: SourceFile}}
	if m, err := Build(in); err != nil || m.Attachments[0].Source != SourceFile {
		t.Fatalf("file origin allowed name: %v", err)
	}
	// Der Name im Audit ist auf 40 Zeichen gekürzt.
	long := strings.Repeat("x", 50) + ".kdbx"
	in.Attachments = []Attachment{{Filename: long, Data: []byte("x"), Origin: SourceFile}}
	_, err = Build(in)
	if _, name, ok := GuardOf(err); !ok || len([]rune(name)) != 40 {
		t.Fatalf("name %q (%v)", name, err)
	}
}

// Windows-Pfade: Laufwerk und Gerätepräfix sind kein Datenstrom, jeder
// weitere Doppelpunkt schon; der Name zählt nach dem letzten Trenner.
func TestA08WindowsPaths(t *testing.T) {
	for _, c := range []struct {
		p    string
		want bool
	}{
		{`C:\Users\probe\Arbeit\brief.pdf`, false},
		{`C:brief.pdf`, false},
		{`\\?\C:\Arbeit\brief.pdf`, false},
		{`C:\Arbeit\brief.pdf:stream`, true},
		{`C:\Arbeit\brief.pdf::$DATA`, true},
		{`brief.pdf:stream`, true},
		{`Mieter/brief.pdf`, false},
	} {
		if got := hasStream(c.p); got != c.want {
			t.Errorf("hasStream(%q) = %v", c.p, got)
		}
	}
	var g *AttachGuard
	for _, c := range []struct {
		p    string
		want bool
	}{
		{`C:\Arbeit\_secrets\token.json`, true},
		{`C:\Arbeit\Mieter\Brief.pdf`, false},
		{`C:\Arbeit\VAULT.KDBX`, true},
		{`D:\Ablage\rechnung_key_account.pdf`, false},
	} {
		if got := g.DeniedName(c.p); got != c.want {
			t.Errorf("DeniedName(%q) = %v", c.p, got)
		}
	}
	// Ordner: Rückstriche trennen auch auf Unix (eine Datei mit Rückstrich
	// im Namen wird so eher gesperrt als durchgelassen).
	parts, ok := components(`C:\Arbeit`, `C:\Arbeit\Mieter\_Secrets\a.txt`)
	if runtime.GOOS == "windows" {
		if !ok || len(parts) != 3 || !g.orDefault().denyDir(parts[1]) {
			t.Fatalf("components = %q %v", parts, ok)
		}
	}
	if !g.orDefault().denyDir("_Secrets") || !g.orDefault().denyDir(norm.NFD.String("PASSWÖRTER")) {
		t.Fatal("dir names must match without case and in NFD")
	}
}

func TestA08NewAttachGuard(t *testing.T) {
	d, f := (*AttachGuard)(nil).Rules()
	if d != len(DefaultDenyDirs) || f != len(DefaultDenyFiles) {
		t.Fatalf("default rules = %d, %d", d, f)
	}
	g, err := NewAttachGuard([]string{"Steuer", "_SECRETS"}, []string{"*.xlsm", "*.KDBX"}, OwnPaths{})
	if err != nil {
		t.Fatal(err)
	}
	// Doppelte (ohne Groß-/Kleinschreibung) zählen nicht doppelt.
	if d, f := g.Rules(); d != len(DefaultDenyDirs)+1 || f != len(DefaultDenyFiles)+1 {
		t.Fatalf("rules = %d, %d", d, f)
	}
	for _, c := range []struct {
		dirs, files []string
		want        string
	}{
		{[]string{".."}, nil, `".."`},
		{[]string{"a/b"}, nil, "path separator"},
		{nil, []string{`a\b`}, "path separator"},
		{nil, []string{""}, "empty"},
		{nil, []string{"[x"}, "pattern"},
	} {
		if _, err := NewAttachGuard(c.dirs, c.files, OwnPaths{}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q %q: %v", c.dirs, c.files, err)
		}
	}
}
