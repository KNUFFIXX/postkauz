package hook

import "syscall"

// setPdeathsig: Stirbt die CLI (auch durch SIGKILL), schickt der Kernel dem
// Hook SIGKILL (Gegenprüfung S2.4/S2.5, K1). Das gilt nur für den Hook
// selbst, nicht für seine Kinder. Linux sendet das Signal, wenn der
// Thread endet, der den Hook gestartet hat; Go beendet Threads nur nach
// runtime.LockOSThread ohne Unlock, was dieses Paket nicht tut.
func setPdeathsig(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGKILL }
