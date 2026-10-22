package hook

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// Die Tests starten als Hook die Testbinärdatei selbst (Muster aus
// os/exec): Argumente nach "--" wählen die Rolle. Eine Umgebungsvariable
// als Schalter ginge nicht, weil der Runner die Umgebung des Hooks leert.
// So laufen die Tests auf Linux, macOS und Windows ohne Shell.

// TestHookHelperProcess ist kein Test, sondern der Fake-Hook.
func TestHookHelperProcess(t *testing.T) {
	a := helperArgs()
	if a == nil {
		return
	}
	os.Exit(helperMain(a))
}

// helperArgs liefert die Argumente nach "--" (nil im normalen Testlauf).
func helperArgs() []string {
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			return os.Args[i+1:]
		}
	}
	return nil
}

func helperMain(a []string) int {
	switch a[0] {
	case "pass":
		os.Stdout.WriteString("all fine\n")
		return 0
	case "fail":
		os.Stdout.WriteString("subject contains TODO\n")
		os.Stderr.WriteString("greeting missing\n")
		return 3
	case "silent-fail":
		return 2
	case "flood": // flood <bytes> <exit>
		n, _ := strconv.Atoi(a[1])
		code, _ := strconv.Atoi(a[2])
		s := "x" + strings.Repeat("ä", n/2)
		os.Stdout.WriteString(s)
		return code
	case "sleep": // sleep <pidfile> <childpidfile>: startet ein Kind und wartet
		child := exec.Command(os.Args[0], "-test.run=^TestHookHelperProcess$", "--", "sleepchild", a[2])
		if err := child.Start(); err != nil {
			return 9
		}
		os.Stdout.WriteString("started\n")
		_ = os.WriteFile(a[1], []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(time.Hour)
	case "sleepchild":
		_ = os.WriteFile(a[1], []byte(strconv.Itoa(os.Getpid())), 0o600)
		time.Sleep(time.Hour)
	case "env": // env <outfile>
		_ = os.WriteFile(a[1], []byte(strings.Join(os.Environ(), "\n")), 0o600)
		return 0
	case "args": // args <outfile> <arg>…: Argumente, Dateiinhalte, Arbeitsverzeichnis
		wd, _ := os.Getwd()
		rep := map[string]any{"args": a[2:], "wd": wd, "env": map[string]string{
			"eml": os.Getenv(EnvEML), "html": os.Getenv(EnvBodyHTML), "text": os.Getenv(EnvBodyText), "meta": os.Getenv(EnvMetaJSON)}}
		contents := map[string]string{}
		for _, p := range a[2:] {
			if b, err := os.ReadFile(p); err == nil {
				contents[p] = string(b)
			}
		}
		rep["contents"] = contents
		b, _ := json.Marshal(rep)
		_ = os.WriteFile(a[1], b, 0o600)
		return 0
	}
	return 99
}

// helperCmd baut check_hook für eine Rolle des Fake-Hooks.
func helperCmd(t *testing.T, args ...string) []string {
	t.Helper()
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	return append([]string{self, "-test.run=^TestHookHelperProcess$", "--"}, args...)
}

func needChild(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
}

var sample = Input{EML: []byte("Subject: Hallo\r\n\r\nHallo Anna\r\n"), BodyHTML: "<p>Hallo Anna</p>", BodyText: "Hallo Anna",
	Meta: []byte(`{"subject":"Hallo"}`)}

// assertEmpty: Nach jedem Lauf ist das Temp-Verzeichnis wieder weg.
func assertEmpty(t *testing.T, base string) {
	t.Helper()
	ents, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 0 {
		t.Fatalf("temporary directory left behind: %v", ents)
	}
}

func run(t *testing.T, r Runner, in Input) Result {
	t.Helper()
	base := t.TempDir()
	r.TempDir = base
	res, err := r.Run(context.Background(), in)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	assertEmpty(t, base)
	return res
}

func TestEmptyCommandPasses(t *testing.T) {
	res := run(t, Runner{}, sample)
	if res.Status != Pass || res.Output != "" {
		t.Fatalf("result %+v", res)
	}
}

func TestPass(t *testing.T) {
	needChild(t)
	res := run(t, Runner{Command: helperCmd(t, "pass", "{eml}")}, sample)
	if res.Status != Pass || res.ExitCode != 0 {
		t.Fatalf("result %+v", res)
	}
}

// Gescheitert: Exit ungleich 0, stdout und stderr stehen im Ergebnis.
func TestFailWithReason(t *testing.T) {
	needChild(t)
	res := run(t, Runner{Command: helperCmd(t, "fail")}, sample)
	if res.Status != Fail || res.ExitCode != 3 || !strings.Contains(res.Output, "subject contains TODO") ||
		!strings.Contains(res.Output, "greeting missing") || res.Truncated {
		t.Fatalf("result %+v", res)
	}
	res = run(t, Runner{Command: helperCmd(t, "silent-fail")}, sample)
	if res.Status != Fail || res.Output != "check hook failed with exit code 2 and no output" {
		t.Fatalf("silent fail: %+v", res)
	}
}

// Programm fehlt oder ist unzulässig: error, nie pass; nichts bleibt liegen.
func TestProgramMissingIsError(t *testing.T) {
	dir := t.TempDir()
	bat := filepath.Join(dir, "lint.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, cmd := range map[string][]string{
		"not on PATH":   {"postkauz-no-such-hook-program-7f3a"},
		"absent file":   {filepath.Join(dir, "missing-hook")},
		"relative path": {filepath.Join("hooks", "lint")},
		"empty program": {" ", "{eml}"},
		"batch file":    {bat, "{eml}"},
	} {
		t.Run(name, func(t *testing.T) {
			res := run(t, Runner{Command: cmd}, sample)
			if res.Status != Error || !strings.HasPrefix(res.Output, "check hook error: ") {
				t.Fatalf("result %+v", res)
			}
			// Die Meldung nennt den Pfad aus der Policy-Datei nicht.
			if strings.Contains(res.Output, dir) || strings.Contains(res.Output, "no-such-hook") {
				t.Fatalf("output names the program: %q", res.Output)
			}
		})
	}
}

// Ausgabe über 4 KB: gekürzt an einer Zeichengrenze, der Hook bekommt
// keinen Schreibfehler (Exit 0 bleibt pass).
func TestOutputTruncated(t *testing.T) {
	needChild(t)
	for _, code := range []string{"0", "1"} {
		res := run(t, Runner{Command: helperCmd(t, "flood", "100000", code)}, sample)
		want := map[string]string{"0": Pass, "1": Fail}[code]
		if res.Status != want || !res.Truncated || len(res.Output) > MaxOutput || len(res.Output) < MaxOutput-utf8.UTFMax ||
			!utf8.ValidString(res.Output) || strings.ContainsRune(res.Output, utf8.RuneError) {
			t.Fatalf("exit %s: status %s, truncated %v, %d bytes", code, res.Status, res.Truncated, len(res.Output))
		}
	}
}

// Zeitüberschreitung: Hook und sein Kind werden beendet, Ergebnis timeout,
// Temp-Verzeichnis weg.
func TestTimeoutKillsHookAndChildren(t *testing.T) {
	needChild(t)
	dir := t.TempDir()
	pid, childPid := filepath.Join(dir, "pid"), filepath.Join(dir, "child")
	const timeout = 6 * time.Second
	start := time.Now()
	res := run(t, Runner{Command: helperCmd(t, "sleep", pid, childPid), Timeout: timeout}, sample)
	took := time.Since(start)
	if res.Status != Timeout || !strings.HasPrefix(res.Output, "check hook timed out after 6s") {
		t.Fatalf("result %+v", res)
	}
	if took < timeout || took > timeout+waitDelay+5*time.Second {
		t.Fatalf("returned after %s, want about %s", took, timeout)
	}
	if runtime.GOOS == "windows" {
		return // Prozessprüfung nur mit Unix-Mitteln (alive_unix_test.go)
	}
	for _, f := range []string{pid, childPid} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("hook did not start within %s: %v", timeout, err)
		}
		p, _ := strconv.Atoi(string(b))
		deadline := time.Now().Add(3 * time.Second)
		for processAlive(p) {
			if time.Now().After(deadline) {
				t.Fatalf("process %d (%s) still alive", p, filepath.Base(f))
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Abbruch durch den Aufrufer (Strg+C): Run liefert den Fehler des
// Kontexts, der Hook wird beendet, nichts bleibt liegen.
func TestCallerCancel(t *testing.T) {
	needChild(t)
	dir, base := t.TempDir(), t.TempDir()
	pid := filepath.Join(dir, "pid")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 1000; i++ {
			if _, err := os.Stat(pid); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		cancel()
	}()
	_, err := Runner{Command: helperCmd(t, "sleep", pid, filepath.Join(dir, "child")), TempDir: base, Timeout: time.Minute}.Run(ctx, sample)
	if err != context.Canceled {
		t.Fatalf("err = %v", err)
	}
	assertEmpty(t, base)
}

// Panic nach dem Schreiben der Dateien: Das Temp-Verzeichnis ist trotzdem
// weg (defer), der Panic geht an den Aufrufer (Executor: crash-Bericht).
func TestPanicRemovesTempDir(t *testing.T) {
	base := t.TempDir()
	var seen string
	testAfterWrite = func(dir string) { seen = dir; panic("boom") }
	t.Cleanup(func() { testAfterWrite = nil })
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		_, _ = Runner{Command: helperCmd(t), TempDir: base}.Run(context.Background(), sample)
	}()
	if seen == "" {
		t.Fatal("files were not written")
	}
	if _, err := os.Stat(seen); !os.IsNotExist(err) {
		t.Fatalf("temporary directory still there: %v", err)
	}
	assertEmpty(t, base)
}

// Kanarientest: Der Hook sieht weder POSTKAUZ_KEY noch ein Kontopasswort
// noch sonst etwas aus der Umgebung außer der erlaubten Liste.
func TestEnvironmentWithoutSecrets(t *testing.T) {
	needChild(t)
	const keyCanary, pwCanary, otherCanary = "CANARY-hook-key-5b1e", "CANARY-hook-pw-77c2", "CANARY-hook-other-0a9d"
	t.Setenv("POSTKAUZ_KEY", keyCanary)
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", pwCanary)
	t.Setenv("POSTKAUZ_SECRETS_BACKEND", otherCanary)
	t.Setenv("HOOK_TEST_OTHER", otherCanary)
	t.Setenv("LANG", "de_DE.UTF-8")
	out := filepath.Join(t.TempDir(), "env.txt")
	res := run(t, Runner{Command: helperCmd(t, "env", out)}, sample)
	if res.Status != Pass {
		t.Fatalf("result %+v", res)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	env := string(b)
	for _, bad := range []string{keyCanary, pwCanary, otherCanary, "POSTKAUZ_KEY=", "POSTKAUZ_PASSWORD_"} {
		if strings.Contains(env, bad) {
			t.Fatalf("hook environment contains %q:\n%s", bad, env)
		}
	}
	allowed := map[string]bool{"PATH": true, "HOME": true, "USERPROFILE": true, "TEMP": true, "TMP": true, "SYSTEMROOT": true, "LANG": true,
		EnvEML: true, EnvBodyHTML: true, EnvBodyText: true, EnvMetaJSON: true}
	for _, kv := range strings.Split(env, "\n") {
		k, _, _ := strings.Cut(kv, "=")
		if !allowed[strings.ToUpper(k)] {
			t.Errorf("unexpected variable %q in the hook environment", k)
		}
	}
	for _, want := range []string{"LANG=de_DE.UTF-8", EnvEML + "=", EnvMetaJSON + "="} {
		if !strings.Contains(env, want) {
			t.Errorf("hook environment lacks %q", want)
		}
	}
}

// Platzhalter: in jedem Argument ersetzt (auch mitten drin und doppelt),
// unbekannte bleiben stehen; die Dateien tragen genau die Eingabe, die
// Umgebung nennt dieselben Pfade, das Arbeitsverzeichnis ist das
// Temp-Verzeichnis, das danach weg ist.
func TestPlaceholders(t *testing.T) {
	needChild(t)
	out := filepath.Join(t.TempDir(), "args.json")
	res := run(t, Runner{Command: helperCmd(t, "args", out, "{eml}", "{body_html}", "{body_text}", "{meta_json}",
		"--file={eml}", "{eml}{eml}", "{unknown}", "plain")}, sample)
	if res.Status != Pass {
		t.Fatalf("result %+v", res)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var rep struct {
		Args     []string          `json:"args"`
		WD       string            `json:"wd"`
		Env      map[string]string `json:"env"`
		Contents map[string]string `json:"contents"`
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if len(rep.Args) != 8 {
		t.Fatalf("args %q", rep.Args)
	}
	eml, html, text, meta := rep.Args[0], rep.Args[1], rep.Args[2], rep.Args[3]
	dir := filepath.Dir(eml)
	if filepath.Base(eml) != FileEML || filepath.Base(html) != FileBodyHTML || filepath.Base(text) != FileBodyText || filepath.Base(meta) != FileMetaJSON ||
		filepath.Dir(meta) != dir || !strings.HasPrefix(filepath.Base(dir), "postkauz-hook-") {
		t.Fatalf("paths %q", rep.Args[:4])
	}
	if rep.Args[4] != "--file="+eml || rep.Args[5] != eml+eml || rep.Args[6] != "{unknown}" || rep.Args[7] != "plain" {
		t.Fatalf("replacement %q", rep.Args[4:])
	}
	if rep.Contents[eml] != string(sample.EML) || rep.Contents[html] != sample.BodyHTML || rep.Contents[text] != sample.BodyText ||
		rep.Contents[meta] != string(sample.Meta) {
		t.Fatalf("contents %q", rep.Contents)
	}
	if rep.Env["eml"] != eml || rep.Env["html"] != html || rep.Env["text"] != text || rep.Env["meta"] != meta {
		t.Fatalf("environment %q, args %q", rep.Env, rep.Args[:4])
	}
	if mustEval(t, rep.WD) != mustEval(t, dir) {
		t.Fatalf("working directory %q, want %q", rep.WD, dir)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	// Das Verzeichnis ist schon gelöscht; nur den Elternpfad auflösen.
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, filepath.Base(p))
}

// Rechte: Verzeichnis 0700, Dateien 0600 (Unix); geprüft während des Laufs.
func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits; Windows uses a protected ACL (proc_windows.go)")
	}
	var modes []os.FileMode
	testAfterWrite = func(dir string) {
		fi, _ := os.Stat(dir)
		modes = append(modes, fi.Mode().Perm())
		for _, n := range []string{FileEML, FileBodyHTML, FileBodyText, FileMetaJSON} {
			fi, _ := os.Stat(filepath.Join(dir, n))
			modes = append(modes, fi.Mode().Perm())
		}
		panic("stop")
	}
	t.Cleanup(func() { testAfterWrite = nil })
	func() {
		defer func() { _ = recover() }()
		_, _ = Runner{Command: helperCmd(t), TempDir: t.TempDir()}.Run(context.Background(), sample)
	}()
	if len(modes) != 5 || modes[0] != 0o700 {
		t.Fatalf("modes %v", modes)
	}
	for _, m := range modes[1:] {
		if m != 0o600 {
			t.Fatalf("file mode %v", m)
		}
	}
}

func TestWithNoteKeepsLimit(t *testing.T) {
	s, cut := withNote("note", strings.Repeat("ü", MaxOutput), true)
	if !strings.HasPrefix(s, "note\n") || len(s) > MaxOutput || !utf8.ValidString(s) || !cut {
		t.Fatalf("len %d cut %v", len(s), cut)
	}
}
