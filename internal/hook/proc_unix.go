//go:build unix

package hook

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// tree hält den Hook samt Kindern in einer eigenen Prozessgruppe (Unix):
// Beenden heißt SIGKILL an die ganze Gruppe.
type tree struct{}

func newTree() (*tree, error) { return &tree{}, nil }

func (*tree) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setPdeathsig(cmd.SysProcAttr)
}

// attach: Die Gruppe entsteht schon beim Start (Setpgid).
func (*tree) attach(*os.Process) error { return nil }

// kill beendet die Prozessgruppe des Hooks (ihre ID ist seine PID). Eine
// Gruppe ohne Mitglieder ist kein Fehler.
func (*tree) kill(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

func (*tree) close() {}

// restrictDir: os.MkdirTemp legt das Verzeichnis schon mit 0700 an; hier
// noch einmal ausdrücklich (gegen eine ungewöhnliche umask, die nur Bits
// wegnimmt, schadet es nicht).
func restrictDir(dir string) error { return os.Chmod(dir, 0o700) }

// ownedByUser: Das Verzeichnis gehört dem Nutzer dieses Prozesses (nur
// eigene liegen gebliebene Temp-Verzeichnisse werden gelöscht).
func ownedByUser(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Getuid()
}
