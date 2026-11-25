//go:build !windows

package procattr

import "os/exec"

// Prepare ändert unter Unix nichts.
func Prepare(*exec.Cmd) {}
