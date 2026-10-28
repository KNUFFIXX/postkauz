//go:build !unix

package ops

// openNonblock: ohne Wirkung (Windows kennt keine FIFO im Dateisystem).
const openNonblock = 0
