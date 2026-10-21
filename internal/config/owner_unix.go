//go:build unix

package config

import (
	"io/fs"
	"syscall"
)

// fileOwner liefert die Nutzer-ID des Besitzers (ok = false, wenn das
// System sie nicht nennt).
func fileOwner(fi fs.FileInfo) (uid int, ok bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}
