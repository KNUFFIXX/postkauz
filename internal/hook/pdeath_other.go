//go:build unix && !linux

package hook

import "syscall"

// setPdeathsig: Pdeathsig gibt es nur unter Linux (K1); unter macOS und
// den BSDs bleibt ein Hook nach SIGKILL der CLI bis zu seinem eigenen Ende
// stehen, sein Temp-Verzeichnis löscht der nächste Lauf nach StaleAge.
func setPdeathsig(*syscall.SysProcAttr) {}
