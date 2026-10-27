package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

func doctorJSON(t *testing.T, args ...string) DoctorReport {
	t.Helper()
	code, out, errs := run(t, append([]string{"doctor", "--json"}, args...)...)
	var rep DoctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor --json %v: %d %v %s %s", args, code, err, out, errs)
	}
	return rep
}

// S1.11: doctor meldet den Stand des Audit-Exports und warnt, wenn ein
// Tag älter als zwei Tage nicht exportiert ist.
func TestDoctorReportsAuditExport(t *testing.T) {
	dataDir, _ := setupEnv(t)
	run(t, "accounts", "list")
	rep := doctorJSON(t)
	if rep.AuditLastExport != "" || rep.AuditDaysNotExported != 1 || rep.AuditExportDir != "" {
		t.Fatalf("fresh: %+v", rep)
	}
	for _, w := range rep.Warnings {
		if strings.Contains(w, "audit") {
			t.Fatalf("no warning for today: %v", rep.Warnings)
		}
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?, 'system', 'sync.run', 'ok')`,
		time.Now().Add(-5*24*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	rep = doctorJSON(t)
	if rep.AuditDaysNotExported != 2 || !strings.Contains(strings.Join(rep.Warnings, "\n"), "Daily audit export") {
		t.Fatalf("stale: %+v", rep)
	}
	dir := filepath.Join(t.TempDir(), "zweiter-ort")
	if code, out, errs := run(t, "audit", "export", "--to", dir, "--quiet"); code != ExitOK || !strings.Contains(out, "manifest: ") {
		t.Fatalf("export: %d %s %s", code, out, errs)
	}
	rep = doctorJSON(t)
	// Der Abschluss-Eintrag des Exports selbst entsteht nach dem Schreiben
	// der Dateien (A1, Migration 0018): Der heutige Tag bleibt offen.
	if rep.AuditLastExport == "" || rep.AuditExportLocation != dir || rep.AuditDaysNotExported != 1 {
		t.Fatalf("after export: %+v", rep)
	}
	if _, out, _ := run(t, "doctor"); !strings.Contains(out, "audit:     last export ") {
		t.Fatalf("doctor text:\n%s", out)
	}
}

// S1.11: Panic in der CLI (vor der Ausgabe) und im Handler: Exit 1, Code
// internal, bereinigter Stack auf stderr bzw. im Log, kein Mailtext.
func TestPanicReportWithoutContent(t *testing.T) {
	dataDir, _ := setupEnv(t)
	seedS16(t)
	const mailText = "Ihre Rechnung" // steht im Text der Mail html@example.org
	if _, out, _ := run(t, "show", "html@example.org"); !strings.Contains(out, mailText) {
		t.Fatalf("show without panic:\n%s", out)
	}
	t.Setenv("POSTKAUZ_INTERNAL_TEST_PANIC", "cli:messages.get")
	code, out, errs := run(t, "show", "html@example.org")
	if code != ExitError || out != "" || strings.Contains(errs, mailText) ||
		!strings.Contains(errs, "error: internal error: panic value withheld (string)") ||
		!strings.Contains(errs, "internal/cli.(*app).runBinding (internal/cli/bindings.go:") || strings.Contains(errs, "0x") {
		t.Fatalf("cli panic: %d out=%q\n%s", code, out, errs)
	}
	code, _, errs = run(t, "show", "html@example.org", "--json")
	if code != ExitError || !strings.Contains(errs, `"code":"internal"`) || strings.Contains(errs, mailText) {
		t.Fatalf("cli panic --json: %d %s", code, errs)
	}
	t.Setenv("POSTKAUZ_INTERNAL_TEST_PANIC", "handler:messages.get")
	code, _, errs = run(t, "show", "html@example.org")
	if code != ExitError || strings.Contains(errs, mailText) || !strings.Contains(errs, "internal error in messages.get: panic value withheld (string)") {
		t.Fatalf("handler panic: %d %s", code, errs)
	}
	t.Setenv("POSTKAUZ_INTERNAL_TEST_PANIC", "")
	if _, out, _ := run(t, "audit", "--json", "--operation", "messages.get", "--result", "error"); !strings.Contains(out, `"reason":"panic"`) {
		t.Fatalf("audit: %s", out)
	}
	logb, _ := os.ReadFile(filepath.Join(dataDir, "postkauz.log"))
	if strings.Contains(string(logb), mailText) || strings.Count(string(logb), `"msg":"panic`) != 3 || !strings.Contains(string(logb), `"stack":[`) {
		t.Fatalf("log:\n%s", logb)
	}
}

// S1.11: doctor --secrets prüft Dateirechte und sucht Passwörter und
// API-Schlüssel in Log, Audit, Tabellen und Exporten, ohne sie auszugeben.
func TestDoctorSecrets(t *testing.T) {
	dataDir, _ := setupEnv(t)
	seedS16(t) // Konto demo mit dem Kanarien-Passwort im (Test-)Schlüsselbund
	key := newKey(t, "--name", "agent", "--role", "reader")
	dir := filepath.Join(t.TempDir(), "export")
	run(t, "audit", "export", "--to", dir)

	code, out, errs := run(t, "doctor", "--secrets")
	if code != ExitOK || !strings.Contains(out, "result: ok") || !strings.Contains(out, "ok      secrets in database table audit_events") ||
		!strings.Contains(out, "ok      secrets in audit export "+dir) || strings.Contains(out, canary) {
		t.Fatalf("clean: %d\n%s\n%s", code, out, errs)
	}
	rep := doctorJSON(t, "--secrets")
	if rep.Secrets == nil || rep.Secrets.Problems != 0 || len(rep.Secrets.Findings) < 15 {
		t.Fatalf("json: %+v", rep.Secrets)
	}

	// Leck im Log (Passwort) und in der Exportdatei (Schlüssel).
	logPath := filepath.Join(dataDir, "postkauz.log")
	f, _ := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString(`{"msg":"oops","password":"` + canary + `"}` + "\n")
	_ = f.Close()
	files, _ := filepath.Glob(filepath.Join(dir, "postkauz-audit-*.jsonl"))
	g, _ := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = g.WriteString(`{"note":"` + key + `"}` + "\n")
	_ = g.Close()
	code, out, errs = run(t, "doctor", "--secrets")
	if code != ExitError || strings.Contains(out+errs, canary) || strings.Contains(out+errs, key) ||
		!regexp.MustCompile(`fail +secrets in log .*: password of account demo found \(line \d+\)`).MatchString(out) ||
		!strings.Contains(out, "an API key (pk_key_…) in "+filepath.Base(files[0])+" found (line ") ||
		!strings.Contains(errs, "secret check failed: 2 problems") {
		t.Fatalf("leaks: %d\n%s\n%s", code, out, errs)
	}

	// Dateirechte (Unix): Log für andere lesbar (die Datenbank setzt der
	// Store beim Öffnen selbst auf 0600).
	if runtime.GOOS != "windows" {
		_ = os.Chmod(logPath, 0o644)
		_, out, _ = run(t, "doctor", "--secrets")
		if !strings.Contains(out, "fail    permissions log: 0644, readable by others; run: chmod 600 ") {
			t.Fatalf("permissions:\n%s", out)
		}
	}
}
