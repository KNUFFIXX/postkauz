package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsPackageCachePath(t *testing.T) {
	for path, want := range map[string]bool{
		`C:\Users\x\AppData\Local\Packages\Claude_abc\LocalCache\Local\postkauz`:   true,
		`c:\users\x\appdata\local\packages\claude_abc\localcache\roaming\postkauz`: true,
		`C:/Users/x/AppData/Local/Packages/App_1/LocalCache/Local/p`:               true,
		`C:\Users\x\AppData\Local\postkauz`:                                        false,
		`C:\Packages\LocalCache\x`:                                                 false, // Paketname fehlt
		`D:\Packages\App\Cache\x`:                                                  false,
		``:                                                                         false,
	} {
		if got := isPackageCachePath(path); got != want {
			t.Errorf("isPackageCachePath(%q) = %v, want %v", path, got, want)
		}
	}
}

// fakeAppData baut %LOCALAPPDATA% und %APPDATA% in einem Testordner nach und
// schaltet die Prüfung ein. Eine Umleitung durch Windows (MSIX) stellt ein
// Symlink nach: Der Pfad unter %LOCALAPPDATA% ist dann dieselbe Datei wie
// sein Gegenstück im Paketordner, wie unter Windows gemessen (gleiche
// Datei-ID, 22.10.2026).
func fakeAppData(t *testing.T) (local, roaming string) {
	t.Helper()
	base := t.TempDir()
	local, roaming = filepath.Join(base, "Local"), filepath.Join(base, "Roaming")
	for _, d := range []string{local, roaming, filepath.Join(local, "Packages", "Other_x", "LocalCache", "Local", "postkauz")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	orig := redirectCheck
	redirectCheck = true
	t.Cleanup(func() { redirectCheck = orig })
	return local, roaming
}

// redirect legt target an und lässt path als Symlink darauf zeigen.
func redirect(t *testing.T, path, target string, file bool) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if file {
		if err := os.WriteFile(target, []byte("log_level = 'info'\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
}

func TestCheckRedirects(t *testing.T) {
	local, roaming := fakeAppData(t)
	cache := filepath.Join(local, "Packages", "Claude_abc", "LocalCache")
	p := Paths{DataDir: filepath.Join(local, "postkauz"), ConfigFile: filepath.Join(roaming, "postkauz", "config.toml")}

	// Datenverzeichnis vorhanden, nicht umgeleitet (das Gegenstück in
	// Other_x ist ein anderer Ordner): nichts.
	if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if r := CheckRedirects(p); r.Package != "" || r.DataDirReal != "" || len(r.Warnings) != 0 {
		t.Fatalf("no redirect expected: %+v", r)
	}
	if err := os.Remove(p.DataDir); err != nil {
		t.Fatal(err)
	}

	// Datenverzeichnis umgeleitet (D3.3c).
	redirect(t, p.DataDir, filepath.Join(cache, "Local", "postkauz"), false)
	r := CheckRedirects(p)
	if r.DataDirReal != filepath.Join(cache, "Local", "postkauz") || r.Package != "Claude_abc" || r.ConfigReal != "" || len(r.Warnings) != 1 {
		t.Fatalf("data dir redirect: %+v", r)
	}
	for _, want := range []string{"redirected by Windows", r.DataDirReal, "Claude_abc", "Task Scheduler", "--data-dir", EnvDataDir} {
		if !strings.Contains(r.Warnings[0], want) {
			t.Fatalf("warning without %q: %s", want, r.Warnings[0])
		}
	}

	// Konfigurationsdatei umgeleitet; geprüft nur, wenn sie existiert.
	redirect(t, p.ConfigFile, filepath.Join(cache, "Roaming", "postkauz", "config.toml"), true)
	if r := CheckRedirects(p); r.ConfigReal != "" {
		t.Fatalf("config without ConfigExists must not be checked: %+v", r)
	}
	p.ConfigExists = true
	r = CheckRedirects(p)
	if r.ConfigReal != filepath.Join(cache, "Roaming", "postkauz", "config.toml") || len(r.Warnings) != 2 || !strings.Contains(r.Warnings[1], "--config") {
		t.Fatalf("config redirect: %+v", r)
	}

	// Ausdrücklich im Paketordner oder außerhalb von %LOCALAPPDATA%: nichts.
	for _, dir := range []string{filepath.Join(cache, "Local", "postkauz"), filepath.Join(filepath.Dir(local), "elsewhere")} {
		if r := CheckRedirects(Paths{DataDir: dir}); len(r.Warnings) != 0 || r.DataDirReal != "" {
			t.Fatalf("%s: %+v", dir, r)
		}
	}

	// Unter Linux/macOS (Standard) prüft CheckRedirects nichts.
	redirectCheck = false
	if r := CheckRedirects(p); len(r.Warnings) != 0 || r.DataDirReal != "" || r.ConfigReal != "" {
		t.Fatalf("check off: %+v", r)
	}
}

// Sieht der Prozess die Umleitung eines Pakets und fehlt das
// Datenverzeichnis unter %LOCALAPPDATA% noch, warnt doctor vorab.
func TestCheckRedirectsPackageWithoutDataDir(t *testing.T) {
	local, _ := fakeAppData(t)
	p := Paths{DataDir: filepath.Join(local, "postkauz")}
	if r := CheckRedirects(p); len(r.Warnings) != 0 {
		t.Fatalf("no package view: %+v", r)
	}
	redirect(t, filepath.Join(local, "Claude"), filepath.Join(local, "Packages", "Claude_abc", "LocalCache", "Local", "Claude"), false)
	r := CheckRedirects(p)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "would be redirected") || r.Package != "Claude_abc" {
		t.Fatalf("package view, missing data dir: %+v", r)
	}
	// Außerhalb von %LOCALAPPDATA%/%APPDATA%: keine Warnung.
	p.DataDir = filepath.Join(filepath.Dir(local), "elsewhere", "postkauz")
	if r := CheckRedirects(p); len(r.Warnings) != 0 {
		t.Fatalf("outside app data: %+v", r)
	}
}

// LoadWith: --config und --data-dir gehen vor den Umgebungsvariablen und der
// Konfigurationsdatei; mit Profil gilt <data-dir>/profiles/<name>.
func TestLoadWithOverrides(t *testing.T) {
	dir := t.TempDir()
	envCfg := filepath.Join(dir, "env.toml")
	flagCfg := filepath.Join(dir, "flag.toml")
	if err := os.WriteFile(envCfg, []byte("log_level = \"warn\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flagCfg, []byte("log_level = \"debug\"\ndata_dir = "+tomlString(filepath.Join(dir, "file-data"))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvConfig, envCfg)
	t.Setenv(EnvDataDir, filepath.Join(dir, "env-data"))
	t.Setenv(EnvSecretsBackend, "")

	cfg, p, err := LoadWith(LoadOptions{ConfigFile: flagCfg})
	if err != nil {
		t.Fatal(err)
	}
	if p.ConfigFile != flagCfg || cfg.LogLevel != "debug" || p.DataDir != filepath.Join(dir, "env-data") {
		t.Fatalf("--config: cfg=%+v paths=%+v", cfg, p)
	}
	_, p, err = LoadWith(LoadOptions{ConfigFile: flagCfg, DataDir: filepath.Join(dir, "flag-data")})
	if err != nil || p.DataDir != filepath.Join(dir, "flag-data") || p.Database != filepath.Join(dir, "flag-data", "postkauz.db") {
		t.Fatalf("--data-dir: %v %+v", err, p)
	}
	_, p, err = LoadWith(LoadOptions{Profile: "test", DataDir: filepath.Join(dir, "flag-data")})
	if err != nil || p.DataDir != filepath.Join(dir, "flag-data", "profiles", "test") || p.ConfigFile != filepath.Join(dir, "profiles", "test", "config.toml") {
		t.Fatalf("--profile with --data-dir: %v %+v", err, p)
	}
	// Ohne Übersteuerung wie bisher: Umgebung.
	_, p, err = LoadWith(LoadOptions{})
	if err != nil || p.ConfigFile != envCfg || p.DataDir != filepath.Join(dir, "env-data") {
		t.Fatalf("defaults: %v %+v", err, p)
	}
	// Abschließender Trenner fällt weg (Quoting unter Windows).
	_, p, err = LoadWith(LoadOptions{ConfigFile: flagCfg + string(filepath.Separator), DataDir: filepath.Join(dir, "flag-data") + string(filepath.Separator)})
	if err != nil || p.ConfigFile != flagCfg || p.DataDir != filepath.Join(dir, "flag-data") {
		t.Fatalf("trailing separator: %v %+v", err, p)
	}
	// Relativer Pfad wird absolut.
	t.Chdir(dir)
	_, p, err = LoadWith(LoadOptions{ConfigFile: "flag.toml", DataDir: "rel-data"})
	if err != nil || p.ConfigFile != flagCfg || p.DataDir != filepath.Join(dir, "rel-data") {
		t.Fatalf("relative: %v %+v", err, p)
	}
}

// tomlString schreibt einen Pfad als TOML-Literal-String (Backslashes unter
// Windows bleiben so unverändert).
func tomlString(s string) string { return "'" + s + "'" }

// Außerhalb des Pakets (normales Fenster): Die Daten hat ein Programm im
// Paket angelegt; doctor zeigt die verborgene Kopie (Gegenprüfung A7).
func TestCheckRedirectsShadowCopy(t *testing.T) {
	local, roaming := fakeAppData(t)
	shadow := filepath.Join(local, "Packages", "Claude_abc", "LocalCache", "Local", "postkauz")
	if err := os.MkdirAll(shadow, 0o700); err != nil {
		t.Fatal(err)
	}
	p := Paths{DataDir: filepath.Join(local, "postkauz")}
	if r := CheckRedirects(p); r.DataDirShadow != "" || len(r.Warnings) != 0 {
		t.Fatalf("copy without database: %+v", r)
	}
	if err := os.WriteFile(filepath.Join(shadow, "postkauz.db"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, exists := range []bool{false, true} {
		if exists {
			if err := os.MkdirAll(p.DataDir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		r := CheckRedirects(p)
		if r.DataDirShadow != shadow || r.DataDirReal != "" || len(r.Warnings) == 0 || !strings.Contains(r.Warnings[0], "copy the folder") || !strings.Contains(r.Warnings[0], "Claude_abc") {
			t.Fatalf("shadow (data dir exists: %v): %+v", exists, r)
		}
	}
	// Unter %APPDATA% (Roaming) gilt dasselbe mit LocalCache\Roaming.
	q := Paths{DataDir: filepath.Join(roaming, "pk-data")}
	redirect(t, q.DataDir, filepath.Join(local, "Packages", "Claude_abc", "LocalCache", "Roaming", "pk-data"), false)
	if r := CheckRedirects(q); r.DataDirReal != filepath.Join(local, "Packages", "Claude_abc", "LocalCache", "Roaming", "pk-data") {
		t.Fatalf("roaming redirect: %+v", r)
	}
}

func TestUnderBaseAndRelTo(t *testing.T) {
	sep := string(filepath.Separator)
	base := filepath.Join(sep+"Users", "x", "AppData", "Local")
	for _, c := range []struct {
		path  string
		under bool
		rel   string
	}{
		{filepath.Join(base, "postkauz"), true, "postkauz"},
		{strings.ToUpper(filepath.Join(base, "postkauz")), true, "POSTKAUZ"},
		{filepath.Join(base, "a", "b") + sep, true, filepath.Join("a", "b")},
		{base, true, ""},
		{base + "2" + sep + "x", false, ""},
		{filepath.Join(sep+"Users", "x"), false, ""},
	} {
		if got := underBase(c.path, base); got != c.under {
			t.Errorf("underBase(%q) = %v", c.path, got)
		}
		if c.under {
			if got := relTo(base, c.path); got != c.rel {
				t.Errorf("relTo(%q) = %q, want %q", c.path, got, c.rel)
			}
		}
	}
	if underBase("", base) || underBase(base, "") {
		t.Error("empty paths must not match")
	}
}
