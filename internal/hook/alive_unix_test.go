//go:build unix

package hook

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// processAlive: Der Prozess existiert und ist kein Zombie. Ein beendetes
// Enkelkind kann als Zombie stehen bleiben, wenn der Init-Prozess eines
// Containers nicht aufräumt; es läuft dann aber nicht mehr.
func processAlive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return true // kein /proc (macOS): Kill(0) entscheidet
	}
	// Feld 3 nach dem Programmnamen in Klammern ist der Zustand.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] != 'Z'
	}
	return true
}
