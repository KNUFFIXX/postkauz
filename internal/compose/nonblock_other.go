//go:build !unix

package compose

// openNonblock: ohne Wirkung (Windows kennt keine FIFO im Dateisystem;
// Named Pipes liegen unter \\.\pipe\, außerhalb von attach_dir).
const openNonblock = 0
