//go:build unix

package ops

import "syscall"

// openNonblock öffnet die Stildatei ohne zu warten: Eine FIFO, die nach der
// Prüfung per Lstat untergeschoben wurde, blockiert so nicht (Rest aus
// T1a, erledigt in T2; wie compose für attach_dir, A12.4). Für reguläre
// Dateien hat O_NONBLOCK keine Wirkung.
const openNonblock = syscall.O_NONBLOCK
