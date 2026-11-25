//go:build windows

package procattr

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// Unter Windows startet ein Hilfsprogramm ohne Konsolenfenster (in
// make cross nur übersetzt).
func TestPrepareNoWindow(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit")
	Prepare(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags&windows.CREATE_NO_WINDOW == 0 || !cmd.SysProcAttr.HideWindow {
		t.Fatalf("SysProcAttr = %+v", cmd.SysProcAttr)
	}
}
