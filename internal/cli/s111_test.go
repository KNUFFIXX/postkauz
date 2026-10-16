package cli

import (
	"encoding/json"
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
