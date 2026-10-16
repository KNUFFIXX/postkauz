package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	if rep.AuditLastExport == "" || rep.AuditExportLocation != dir || rep.AuditDaysNotExported != 0 {
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
