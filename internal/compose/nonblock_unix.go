//go:build unix

package compose

import "syscall"

// openNonblock öffnet ohne zu warten: Eine FIFO, die nach der Prüfung per
// Lstat untergeschoben wurde, blockiert so nicht (A12.4). Für reguläre
// Dateien hat O_NONBLOCK keine Wirkung.
const openNonblock = syscall.O_NONBLOCK
