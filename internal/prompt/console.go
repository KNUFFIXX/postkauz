package prompt

import (
	"context"
	"fmt"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// console ist die Windows-Konsole aus Sicht des Lesers (console_windows.go);
// der Test ersetzt sie durch einen Fake-Konsolenstrom.
type console interface {
	mode() (uint32, error)
	setMode(m uint32) error
	// read liest wie ReadConsoleW höchstens len(buf) UTF-16-Einheiten.
	read(buf []uint16) (int, error)
	// cancel bricht ein laufendes read ab (CancelIoEx), soweit möglich.
	cancel()
}

// readConsoleLine liest eine Zeile verdeckt von der Konsole: Modus nach
// passwordMode setzen, lesen bis Zeilenende, Modus wiederherstellen. Das
// Lesen läuft in einer eigenen Goroutine, damit Zeitgrenze und ctx greifen,
// auch wenn ReadConsoleW blockiert; danach ist der Modus wiederhergestellt,
// eine noch blockierte Goroutine endet spätestens mit dem Prozess.
func readConsoleLine(ctx context.Context, c console, timeout time.Duration, rep *Report) ([]byte, error) {
	old, err := c.mode()
	if err != nil {
		return nil, ErrNotTerminal
	}
	rep.ModeBefore = ModeNames(old)
	if err := c.setMode(passwordMode(old)); err != nil {
		return nil, fmt.Errorf("set console mode: %w", err)
	}
	lineInput := false
	if m, err := c.mode(); err == nil {
		rep.ModeDuring = ModeNames(m)
		lineInput = m&modeLineInput != 0
	}
	defer func() {
		_ = c.setMode(old)
		if m, err := c.mode(); err == nil {
			rep.ModeAfter = ModeNames(m)
		}
	}()

	type result struct {
		units []uint16
		total int    // gelesene Einheiten, auch verworfene
		over  bool   // länger als MaxLen: Rest bis Zeilenende verworfen
		term  string // Zeilenende des letzten Stücks (nur bei over)
		reads int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var r result
		buf := make([]uint16, 256)
		for r.reads = 1; ; r.reads++ {
			n, err := c.read(buf)
			chunk := buf[:n]
			r.total += n
			if !r.over {
				r.units = append(r.units, chunk...)
				// Zu lang: nicht mehr sammeln, aber die Zeile zu Ende lesen.
				// Sonst bliebe der Rest im Eingabepuffer der Konsole, und
				// die Shell läse ihn danach als Befehl (Gegenprüfung K6).
				if len(r.units) > MaxLen+2 && !hasLineEnd(r.units) {
					clear(r.units)
					r.units, r.over = nil, true
				}
			}
			if hasLineEnd(chunk) || err != nil || n == 0 {
				// Füllt CR genau das Ende des Puffers, liegt bei Zeilen-
				// Eingabe das LF noch in der Konsole; ungelesen bekäme es
				// die Shell als leere Zeile. Deshalb ein Zeichen nachlesen
				// (nur bei vollem Puffer: sonst kam die Zeile ganz, und ein
				// CR ohne LF ließe den Leser blockieren).
				if err == nil && lineInput && n == len(buf) && chunk[n-1] == '\r' {
					var lf [1]uint16
					m, lerr := c.read(lf[:])
					r.reads++
					r.total += m
					if m == 1 && !r.over {
						r.units = append(r.units, lf[0])
					}
					chunk = append(chunk[n-1:n:n], lf[:m]...)
					err = lerr
				}
				if r.over {
					_, r.term = lineEnd(chunk)
				}
				r.err = err
				done <- r
				return
			}
		}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		c.cancel()
		return nil, ctx.Err()
	case <-timer.C:
		c.cancel()
		return nil, ErrTimeout
	}
	rep.Reads, rep.Units = r.reads, r.total
	if r.over {
		rep.Terminator = r.term
		return nil, ErrTooLong
	}
	b, err := decodeLine(r.units, rep)
	if err == nil && r.err != nil && rep.Terminator == "none" {
		err = r.err
	}
	return b, err
}

// lineEnd liefert die Stelle des ersten Zeilenendes in u und seine Art
// (crlf, cr, lf; none ohne Zeilenende, dann len(u)).
func lineEnd(u []uint16) (int, string) {
	for i, x := range u {
		switch {
		case x == '\r' && i+1 < len(u) && u[i+1] == '\n':
			return i, "crlf"
		case x == '\r':
			return i, "cr"
		case x == '\n':
			return i, "lf"
		}
	}
	return len(u), "none"
}

func hasLineEnd(u []uint16) bool {
	for _, x := range u {
		if x == '\n' || x == '\r' {
			return true
		}
	}
	return false
}

// decodeLine schneidet am ersten Zeilenende ab (CRLF bei Zeilen-Eingabe,
// CR, wenn die Konsole sie nicht beachtet, Hypothese b), wertet
// Rückschritt aus (falls die Konsole ihn durchreicht), lässt Strg+Z weg
// und wandelt UTF-16 in UTF-8; ein einzelnes Surrogat wird U+FFFD und
// zählt als ungültig.
func decodeLine(u []uint16, rep *Report) ([]byte, error) {
	var cut int
	cut, rep.Terminator = lineEnd(u)
	u = u[:cut]
	if rep.Terminator == "none" && len(u) == 0 {
		rep.Terminator = "eof"
	}
	var runes []rune
	for i := 0; i < len(u); i++ {
		x := rune(u[i])
		switch {
		case x == '\b' || x == 0x7f:
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
			continue
		case x == 0x1a: // Strg+Z
			continue
		case utf16.IsSurrogate(x):
			if i+1 < len(u) {
				if r := utf16.DecodeRune(x, rune(u[i+1])); r != utf8.RuneError {
					runes = append(runes, r)
					i++
					continue
				}
			}
			rep.Invalid++
			x = utf8.RuneError
		}
		runes = append(runes, x)
	}
	if len(runes) > MaxLen {
		return nil, ErrTooLong
	}
	return []byte(string(runes)), nil
}
