package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorSecretsFindsJWT(t *testing.T) {
	dataDir, _ := setupEnv(t)
	run(t, "accounts", "list") // legt Datenbank und Log an
	tok := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJ0MWMtdGVzdCJ9.dDFjLW51ci1laW4tdGVzdC1zaWduYXR1cg"
	f, err := os.OpenFile(filepath.Join(dataDir, "postkauz.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"msg":"oops","auth":"Bearer ` + tok + `"}` + "\n")
	_ = f.Close()
	code, out, errs := run(t, "doctor", "--secrets")
	if code != ExitError || strings.Contains(out+errs, tok) || strings.Contains(out+errs, "eyJzdWIi") ||
		!strings.Contains(out, "postkauz.log: a JSON Web Token (eyJ….eyJ….…) found (line ") || !strings.Contains(errs, "secret check failed: 1 problems") {
		t.Fatalf("jwt: %d\n%s\n%s", code, out, errs)
	}
}
