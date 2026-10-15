//go:build !unix && !windows

package mailsync

import "os"

// Ohne Dateisperren des Betriebssystems (z. B. wasm): keine Sperre.
func lockFile(*os.File) error   { return nil }
func unlockFile(*os.File) error { return nil }
