package prompt

import (
	"context"
	"os"
	"time"

	"golang.org/x/term"
)

// readTerm liest mit golang.org/x/term.ReadPassword (Verhalten bis
// 0.2.0-rc8; unter Unix der Standard). Zeitgrenze und ctx stellen den
// gespeicherten Terminal-Zustand wieder her (sonst bliebe das Echo aus);
// die blockierte Goroutine endet spätestens mit dem Prozess.
func readTerm(ctx context.Context, in *os.File, timeout time.Duration, rep *Report) ([]byte, error) {
	fd := int(in.Fd())
	st, err := term.GetState(fd)
	if err != nil {
		return nil, ErrNotTerminal
	}
	if m, ok := consoleMode(in); ok {
		rep.ModeBefore = ModeNames(m)
	}
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := term.ReadPassword(fd)
		done <- result{b, err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	defer func() {
		if m, ok := consoleMode(in); ok {
			rep.ModeAfter = ModeNames(m)
		}
	}()
	rep.Terminator = "n/a"
	select {
	case r := <-done:
		rep.Reads = 1
		if len(r.b) > MaxLen {
			return nil, ErrTooLong
		}
		return r.b, r.err
	case <-ctx.Done():
		_ = term.Restore(fd, st)
		return nil, ctx.Err()
	case <-timer.C:
		_ = term.Restore(fd, st)
		return nil, ErrTimeout
	}
}
