package hook

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// K1 (nur Linux): Wird die CLI mit SIGKILL beendet, beendet der Kernel
// den Hook (Pdeathsig). Seine Kinder erreicht das nicht (Grenze,
// dokumentiert); das Temp-Verzeichnis löscht ein späterer Lauf.
func TestReviewK1HookDiesWithCaller(t *testing.T) {
	needChild(t)
	dir := t.TempDir()
	hookPid := filepath.Join(dir, "hook")
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	caller := exec.Command(self, "-test.run=^TestHookHelperProcess$", "--", "runner", hookPid, filepath.Join(dir, "child"), dir)
	if err := caller.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	for i := 0; i < 500 && pid == 0; i++ {
		if b, err := os.ReadFile(hookPid); err == nil {
			pid, _ = strconv.Atoi(string(b))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if pid == 0 {
		_ = caller.Process.Kill()
		t.Fatal("hook did not start")
	}
	if err := caller.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = caller.Wait()
	deadline := time.Now().Add(3 * time.Second)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("hook %d still alive after its caller was killed", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Das Kind des Hooks (eigene Gruppe des Hooks) aufräumen, falls es noch
	// läuft: Pdeathsig erreicht es nicht.
	if b, err := os.ReadFile(filepath.Join(dir, "child")); err == nil {
		if c, _ := strconv.Atoi(string(b)); c > 0 {
			_ = syscall.Kill(c, syscall.SIGKILL)
		}
	}
}
