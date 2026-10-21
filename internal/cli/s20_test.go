package cli

// Tests zu A02, Teil A (S2.0).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A02 A3 (Frage 6c): Ein vorhandenes Datenverzeichnis (und Log) mit zu
// weiten Rechten wird beim Öffnen verschärft; doctor --secrets meldet
// danach ok. Unix-Rechte; unter Windows gilt der Profilordner.
func TestOpenRestrictsDataDirPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permissions; on Windows the user profile's access rights apply")
	}
	dataDir, _ := setupEnv(t)
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %d %s", code, errs)
	}
	logPath := filepath.Join(dataDir, "postkauz.log")
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logPath, 0o644); err != nil {
		t.Fatal(err)
	}
	// doctor legt nichts an und ändert nichts: vorher fail.
	code, out, _ := run(t, "doctor", "--secrets")
	if code != ExitError || !strings.Contains(out, "fail    permissions data dir: 0755") || !strings.Contains(out, "fail    permissions log: 0644") {
		t.Fatalf("before: %d\n%s", code, out)
	}
	if code, _, errs := run(t, "accounts", "list"); code != ExitOK {
		t.Fatalf("accounts list: %d %s", code, errs)
	}
	code, out, errs := run(t, "doctor", "--secrets")
	if code != ExitOK || !strings.Contains(out, "ok      permissions data dir: 0700") || !strings.Contains(out, "ok      permissions log: 0600") ||
		!strings.Contains(out, "result: ok") {
		t.Fatalf("after: %d\n%s\n%s", code, out, errs)
	}
	b, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(b), `"msg":"restricted permissions"`) || !strings.Contains(string(b), `"from":"0755","to":"0700"`) {
		t.Fatalf("log: %v\n%s", err, b)
	}
}
