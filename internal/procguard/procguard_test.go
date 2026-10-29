package procguard

import (
	"os"
	"os/exec"
	"testing"
)

const childEnv = "POSTKAUZ_PROCGUARD_CHILD"

// Nachbesserung T5, Punkt 6: Nach DenyInspection ist der Prozess unter
// Linux nicht mehr auslesbar (PR_GET_DUMPABLE = 0). Läuft im Kindprozess,
// damit der Testprozess selbst unverändert bleibt.
func TestT5NDenyInspection(t *testing.T) {
	if os.Getenv(childEnv) == "1" {
		if err := DenyInspection(); err != nil {
			t.Fatalf("DenyInspection: %v", err)
		}
		ok, err := Inspectable()
		if err != nil {
			t.Fatal(err)
		}
		if Supported && ok {
			t.Fatal("process is still inspectable after DenyInspection")
		}
		return
	}
	if testing.Short() {
		t.Skip("spawns a child process; skipped with -short")
	}
	if ok, err := Inspectable(); err != nil || !ok {
		t.Fatalf("test process before: inspectable %v, %v", ok, err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestT5NDenyInspection$", "-test.count=1")
	cmd.Env = append(os.Environ(), childEnv+"=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
}
