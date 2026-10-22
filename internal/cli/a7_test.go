package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A7 (Befund D3.3c): --data-dir und --config wählen Datenverzeichnis und
// Konfigurationsdatei für einen Aufruf, mit Vorrang vor den
// Umgebungsvariablen; so kann eine geplante Aufgabe genau die Pfade
// benutzen, die doctor im Fenster gezeigt hat.
func TestDataDirAndConfigFlags(t *testing.T) {
	envData, _ := setupEnv(t)
	dir := t.TempDir()
	other := filepath.Join(dir, "other-data")
	cfgFile := filepath.Join(dir, "cfg.toml")
	exportDir := filepath.Join(dir, "export")
	if err := os.WriteFile(cfgFile, []byte("[audit]\nexport_dir = '"+exportDir+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	run(t, "key", "create", "--name", "env-key")
	if code, _, errs := run(t, "key", "create", "--name", "flag-key", "--data-dir", other); code != ExitOK {
		t.Fatalf("key create --data-dir: %d %s", code, errs)
	}
	_, envList, _ := run(t, "key", "list")
	_, flagList, _ := run(t, "key", "list", "--data-dir", other)
	if !strings.Contains(envList, "env-key") || strings.Contains(envList, "flag-key") || !strings.Contains(flagList, "flag-key") || strings.Contains(flagList, "env-key") {
		t.Fatalf("data dirs not separate:\nenv:\n%s\nflag:\n%s", envList, flagList)
	}
	if _, err := os.Stat(filepath.Join(other, "postkauz.db")); err != nil {
		t.Fatalf("database in --data-dir: %v", err)
	}

	code, out, errs := run(t, "doctor", "--json", "--data-dir", other, "--config", cfgFile)
	if code != ExitOK {
		t.Fatalf("doctor: %d %s", code, errs)
	}
	var rep DoctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("doctor json: %v\n%s", err, out)
	}
	if rep.DataDir != other || rep.ConfigPath != cfgFile || !rep.ConfigExists || rep.AuditExportDir != exportDir {
		t.Fatalf("doctor with flags: %+v", rep)
	}
	// Auf Linux/macOS (und unter Windows ohne App-Paket) keine Umleitung.
	if rep.DataDirReal != "" || rep.ConfigPathReal != "" || strings.Contains(out, "redirected") {
		t.Fatalf("unexpected redirect: %s", out)
	}
	_, out, _ = run(t, "doctor", "--json")
	if !strings.Contains(out, `"data_dir": "`+jsonEscape(envData)+`"`) && !strings.Contains(out, `"data_dir":"`+jsonEscape(envData)+`"`) {
		t.Fatalf("doctor without flags must use POSTKAUZ_DATA_DIR: %s", out)
	}

	for _, args := range [][]string{{"key", "list", "--data-dir", ""}, {"key", "list", "--config", " "}} {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Fatalf("%v: exit %d, want usage error", args, code)
		}
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return strings.Trim(string(b), `"`)
}
