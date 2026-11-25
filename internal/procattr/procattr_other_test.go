//go:build !windows

package procattr

import (
	"os/exec"
	"testing"
)

// Unter Unix bleibt der Start unverändert.
func TestPrepareUnchanged(t *testing.T) {
	cmd := exec.Command("true")
	Prepare(cmd)
	if cmd.SysProcAttr != nil {
		t.Fatalf("SysProcAttr = %+v", cmd.SysProcAttr)
	}
}
