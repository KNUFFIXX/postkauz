package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb, "test")
	return code, out.String(), errb.String()
}

func TestAccountsListJSON(t *testing.T) {
	code, out, errs := run(t, "accounts", "list", "--json")
	if code != ExitOK || errs != "" {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
	if strings.TrimSpace(out) != `{"accounts":[]}` {
		t.Errorf("stdout = %q", out)
	}
}

func TestAccountsListHuman(t *testing.T) {
	code, out, _ := run(t, "accounts", "list")
	if code != ExitOK || !strings.Contains(out, "No accounts") {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestVersionJSON(t *testing.T) {
	code, out, _ := run(t, "version", "--json")
	var v map[string]any
	if code != ExitOK || json.Unmarshal([]byte(out), &v) != nil || v["version"] != "test" || v["name"] != "mailwerk" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, ok := v["schema_version"]; !ok {
		t.Error("schema_version missing")
	}
}

func TestDoctorStubReportsPlaceholders(t *testing.T) {
	t.Setenv("MAILWERK_CONFIG", "/nonexistent/mailwerk-test.toml")
	code, out, _ := run(t, "doctor", "--json")
	var r DoctorReport
	if code != ExitOK || json.Unmarshal([]byte(out), &r) != nil {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if r.ConfigPath != "/nonexistent/mailwerk-test.toml" || r.ConfigExists || r.Keyring != "not_checked" {
		t.Errorf("report = %+v", r)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, _, errs := run(t, "bogus")
	if code != ExitUsage || !strings.Contains(errs, "error:") {
		t.Fatalf("code=%d stderr=%q", code, errs)
	}
}

func TestUnknownFlagJSONError(t *testing.T) {
	code, _, errs := run(t, "accounts", "list", "--nope", "--json")
	if code != ExitUsage {
		t.Fatalf("code=%d", code)
	}
	var e map[string]ErrorInfo
	if err := json.Unmarshal([]byte(errs), &e); err != nil || e["error"].Code != "usage" {
		t.Fatalf("stderr = %q (%v)", errs, err)
	}
}

func TestEveryBindingResolves(t *testing.T) {
	root, err := NewRoot("test", &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range bindings {
		cmd, _, err := root.Find(b.path)
		if err != nil || cmd.Name() != b.path[1] {
			t.Errorf("binding %s: command %v not found (%v)", b.op, b.path, err)
		}
	}
}
