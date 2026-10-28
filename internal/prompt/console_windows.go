//go:build windows

package prompt

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// defaultReader: Windows liest über ReadConsoleW (A3).
const defaultReader = ReaderConsole

// winConsole ist der Konsolen-Eingabepuffer hinter stdin.
type winConsole struct{ h windows.Handle }

func (c winConsole) mode() (uint32, error) {
	var m uint32
	err := windows.GetConsoleMode(c.h, &m)
	return m, err
}

func (c winConsole) setMode(m uint32) error { return windows.SetConsoleMode(c.h, m) }

func (c winConsole) read(buf []uint16) (int, error) {
	var n uint32
	err := windows.ReadConsole(c.h, &buf[0], uint32(len(buf)), &n, nil)
	return int(n), err
}

func (c winConsole) cancel() { _ = windows.CancelIoEx(c.h, nil) }

func isTerminal(f *os.File) bool {
	_, ok := consoleMode(f)
	return ok
}

func consoleMode(f *os.File) (uint32, bool) {
	var m uint32
	if windows.GetConsoleMode(windows.Handle(f.Fd()), &m) != nil {
		return 0, false
	}
	return m, true
}

func readConsole(ctx context.Context, f *os.File, timeout time.Duration, rep *Report) ([]byte, error) {
	return readConsoleLine(ctx, winConsole{windows.Handle(f.Fd())}, timeout, rep)
}

// platformEnv beschreibt stdin und die Konsole für prompt-probe (ohne
// Werte von Umgebungsvariablen, nur ob sie gesetzt sind).
func platformEnv(f *os.File) Env {
	e := Env{OS: "windows"}
	switch t, err := windows.GetFileType(windows.Handle(f.Fd())); {
	case err != nil:
		e.StdinType = "error"
	case t == windows.FILE_TYPE_CHAR:
		e.StdinType = "char"
	case t == windows.FILE_TYPE_PIPE:
		e.StdinType = "pipe"
	case t == windows.FILE_TYPE_DISK:
		e.StdinType = "disk"
	default:
		e.StdinType = "unknown"
	}
	if m, ok := consoleMode(f); ok {
		e.ConsoleMode = ModeNames(m)
	}
	e.InputCodePage, _ = windows.GetConsoleCP()
	e.OutputCodePage, _ = windows.GetConsoleOutputCP()
	e.ANSICodePage = windows.GetACP()
	e.WindowsTerminal = os.Getenv("WT_SESSION") != ""
	return e
}
