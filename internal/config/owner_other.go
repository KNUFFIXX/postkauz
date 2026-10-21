//go:build !unix

package config

import "io/fs"

// fileOwner: ohne Unix-Rechte kein Besitzer (RestrictPermissions tut dort
// nichts).
func fileOwner(fs.FileInfo) (int, bool) { return 0, false }
