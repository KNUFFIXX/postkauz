//go:build !windows

package prompt

import (
	"context"
	"errors"
	"os"
	"time"

	"golang.org/x/term"
)

// defaultReader: außerhalb von Windows bleibt x/term.
const defaultReader = ReaderTerm

func isTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

func consoleMode(*os.File) (uint32, bool) { return 0, false }

func readConsole(context.Context, *os.File, time.Duration, *Report) ([]byte, error) {
	return nil, errors.New("the console reader is only available on Windows; use --reader term")
}

func platformEnv(f *os.File) Env {
	e := Env{OS: "other"}
	if fi, err := f.Stat(); err == nil {
		switch m := fi.Mode(); {
		case m&os.ModeCharDevice != 0:
			e.StdinType = "char"
		case m&os.ModeNamedPipe != 0:
			e.StdinType = "pipe"
		case m.IsRegular():
			e.StdinType = "disk"
		default:
			e.StdinType = "unknown"
		}
	}
	return e
}
