//go:build !windows

package export

// markFromInternet setzt die Herkunft „aus dem Internet“ nur unter Windows
// (zone_windows.go); andere Systeme kennen keinen Zone.Identifier.
func markFromInternet(string) error { return nil }
