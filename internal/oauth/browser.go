package oauth

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

// OpenBrowser öffnet eine Adresse im Standardbrowser des Systems, ohne auf
// ihn zu warten. Nur https- und http-Adressen; kein Kommandointerpreter
// dazwischen (exec ohne Shell).
func OpenBrowser(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		return errors.New("oauth: refusing to open a non-http address")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
