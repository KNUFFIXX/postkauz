//go:build linux

package procguard

import "syscall"

const supported = true

// Werte aus <linux/prctl.h>; das Paket syscall nennt sie nicht.
const (
	prGetDumpable = 3
	prSetDumpable = 4
)

func denyInspection() error {
	if _, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prSetDumpable, 0, 0); e != 0 {
		return e
	}
	return nil
}

func inspectable() (bool, error) {
	r, _, e := syscall.RawSyscall(syscall.SYS_PRCTL, prGetDumpable, 0, 0)
	if e != 0 {
		return true, e
	}
	return r != 0, nil
}
