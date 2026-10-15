package secrets

import (
	"fmt"
	"runtime"
)

func isWindows() bool { return runtime.GOOS == "windows" }

// Backend-Namen wie in config.Secrets.Backend.
const (
	BackendAuto         = "auto"
	BackendKeyring      = "keyring"
	BackendInsecureFile = "insecure-file"
)

// Open wählt den Speicher nach backend und legt immer die Umgebungsvariablen
// darüber (WithEnv). insecureDir ist das Verzeichnis des Dateispeichers.
//
//   - auto, keyring: Schlüsselbund; antwortet er nicht, ein Speicher, der
//     nur aus der Umgebung liest und beim Schreiben mit Hinweis scheitert.
//   - insecure-file: der unsichere Dateispeicher (nur Tests).
func Open(backend, insecureDir string) (Store, error) {
	return open(backend, insecureDir, NewKeyring())
}

func open(backend, insecureDir string, kr *Keyring) (Store, error) {
	switch backend {
	case "", BackendAuto, BackendKeyring:
		st := kr.Probe()
		if !st.Available {
			return WithEnv(Unavailable{Reason: "keyring unavailable: " + st.Detail}), nil
		}
		return WithEnv(kr), nil
	case BackendInsecureFile:
		return WithEnv(NewInsecureFile(insecureDir)), nil
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", backend)
	}
}
