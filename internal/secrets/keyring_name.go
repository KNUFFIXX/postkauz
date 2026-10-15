package secrets

import "runtime"

// keyringName nennt den Schlüsselbund des Betriebssystems für `doctor`.
func keyringName() string {
	switch runtime.GOOS {
	case "windows":
		return "Windows Credential Manager"
	case "darwin":
		return "macOS Keychain"
	default:
		return "Secret Service (D-Bus)"
	}
}
