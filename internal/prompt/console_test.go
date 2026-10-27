package prompt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// fakeConsole ist ein Fake-Konsolenstrom: read liefert die Stücke der
// Reihe nach und blockiert danach, bis cancel kommt (wie ReadConsoleW ohne
// Eingabe).
type fakeConsole struct {
	mu        sync.Mutex
	m         uint32
	modes     []uint32 // jeder gesetzte Modus
	chunks    [][]uint16
	canceled  chan struct{}
	once      sync.Once
	modeErr   error
	setErr    error
	cancelled bool
}

func newFake(m uint32, chunks ...[]uint16) *fakeConsole {
	return &fakeConsole{m: m, chunks: chunks, canceled: make(chan struct{})}
}

func (f *fakeConsole) mode() (uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.m, f.modeErr
}

func (f *fakeConsole) setMode(m uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.setErr != nil {
		return f.setErr
	}
	f.m = m
	f.modes = append(f.modes, m)
	return nil
}

func (f *fakeConsole) read(buf []uint16) (int, error) {
	f.mu.Lock()
	if len(f.chunks) > 0 {
		c := f.chunks[0]
		n := copy(buf, c)
		if n < len(c) {
			f.chunks[0] = c[n:]
		} else {
			f.chunks = f.chunks[1:]
		}
		f.mu.Unlock()
		return n, nil
	}
	f.mu.Unlock()
	<-f.canceled
	return 0, errors.New("operation aborted")
}

func (f *fakeConsole) cancel() {
	f.mu.Lock()
	f.cancelled = true
	f.mu.Unlock()
	f.once.Do(func() { close(f.canceled) })
}

func u16(s string) []uint16 { return utf16.Encode([]rune(s)) }

// Modus der Windows-PowerShell-Konsole (typisch: verarbeitete Eingabe,
// Zeile, Echo, Einfügen, Schnellbearbeitung, erweiterte Flags) mit
// VT-Eingabe, wie PSReadLine sie hinterlassen kann.
const shellMode = modeProcessedInput | modeLineInput | modeEchoInput | modeInsertMode | modeQuickEditMode | modeExtendedFlags | modeVTInput

func TestReadConsoleLineASCIIAndModes(t *testing.T) {
	f := newFake(shellMode, u16("Test-123\r\n"))
	var rep Report
	b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
	if err != nil || string(b) != "Test-123" || rep.Terminator != "crlf" || rep.Reads != 1 || rep.Units != 10 {
		t.Fatalf("b=%q err=%v rep=%+v", b, err, rep)
	}
	during := f.modes[0]
	if during&modeEchoInput != 0 || during&modeVTInput != 0 || during&modeLineInput == 0 || during&modeProcessedInput == 0 ||
		during&modeQuickEditMode == 0 || during&modeInsertMode == 0 {
		t.Fatalf("mode during = %s", ModeNames(during))
	}
	if f.m != shellMode || rep.ModeAfter != ModeNames(shellMode) || rep.ModeBefore != rep.ModeAfter {
		t.Fatalf("mode not restored: %s (%+v)", ModeNames(f.m), rep)
	}
	if !strings.Contains(rep.ModeBefore, "ECHO_INPUT") || strings.Contains(rep.ModeDuring, "ECHO_INPUT") {
		t.Fatalf("mode names: %+v", rep)
	}
}

// Umlaut, Euro und ein Zeichen außerhalb der BMP (Surrogatpaar), über
// mehrere Lesevorgänge verteilt, das Paar mitten getrennt (Einfügen kommt
// in Stücken, Hypothesen c und d).
func TestReadConsoleLineUTF16AcrossReads(t *testing.T) {
	want := "Prüfung-ä€😀ß"
	all := u16(want + "\r\n")
	cut := len(u16("Prüfung-ä€")) + 1 // mitten im Surrogatpaar
	f := newFake(shellMode, all[:3], all[3:cut], all[cut:])
	var rep Report
	b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
	if err != nil || string(b) != want || rep.Reads != 3 || rep.Invalid != 0 {
		t.Fatalf("b=%q err=%v rep=%+v", b, err, rep)
	}
	describe(b, &rep)
	if rep.Runes != 12 || rep.NonASCII != 5 || rep.Bytes != len(want) {
		t.Fatalf("counts: %+v", rep)
	}
}

// Beachtet die Konsole die Zeilen-Eingabe nicht (Hypothese b), kommen die
// Zeichen einzeln, Rückschritt roh und Enter als CR.
func TestReadConsoleLineCharwiseWithBackspace(t *testing.T) {
	var chunks [][]uint16
	for _, r := range "abx\bc\x1a\r" {
		chunks = append(chunks, []uint16{uint16(r)})
	}
	f := newFake(shellMode, chunks...)
	var rep Report
	b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
	if err != nil || string(b) != "abc" || rep.Terminator != "cr" || rep.Reads != 7 {
		t.Fatalf("b=%q err=%v rep=%+v", b, err, rep)
	}
}

func TestReadConsoleLineLoneSurrogateAndEscape(t *testing.T) {
	f := newFake(shellMode, []uint16{'a', 0xd83d, 'b', 0x1b, '[', 'A', '\r', '\n'})
	var rep Report
	b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
	if err != nil || string(b) != "a�b\x1b[A" || rep.Invalid != 1 {
		t.Fatalf("b=%q err=%v rep=%+v", b, err, rep)
	}
	describe(b, &rep)
	if rep.Escapes != 1 || rep.Invalid != 1 {
		t.Fatalf("counts: %+v", rep)
	}
}

func TestReadConsoleLineTimeoutAndCancel(t *testing.T) {
	f := newFake(shellMode, u16("halb"))
	var rep Report
	start := time.Now()
	_, err := readConsoleLine(context.Background(), f, 50*time.Millisecond, &rep)
	if !errors.Is(err, ErrTimeout) || !f.cancelled || f.m != shellMode || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout: err=%v cancelled=%v mode=%s", err, f.cancelled, ModeNames(f.m))
	}
	f = newFake(shellMode)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	if _, err := readConsoleLine(ctx, f, time.Minute, &rep); !errors.Is(err, context.Canceled) || !f.cancelled || f.m != shellMode {
		t.Fatalf("ctx: err=%v mode=%s", err, ModeNames(f.m))
	}
}

func TestReadConsoleLineErrors(t *testing.T) {
	var rep Report
	f := newFake(shellMode)
	f.modeErr = errors.New("invalid handle")
	if _, err := readConsoleLine(context.Background(), f, time.Second, &rep); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("no console: %v", err)
	}
	f = newFake(shellMode)
	f.setErr = errors.New("access denied")
	if _, err := readConsoleLine(context.Background(), f, time.Second, &rep); err == nil || !strings.Contains(err.Error(), "set console mode") {
		t.Fatalf("set mode: %v", err)
	}
	long := u16(strings.Repeat("x", MaxLen+10) + "\r\n")
	f = newFake(shellMode, long)
	if _, err := readConsoleLine(context.Background(), f, time.Second, &rep); !errors.Is(err, ErrTooLong) || f.m != shellMode {
		t.Fatalf("too long: %v", err)
	}
	// Strg+Z allein (Dateiende der Konsole) ergibt eine leere Eingabe.
	f = newFake(shellMode, []uint16{0x1a, '\r', '\n'})
	if b, err := readConsoleLine(context.Background(), f, time.Second, &rep); err != nil || len(b) != 0 {
		t.Fatalf("ctrl+z: %q %v", b, err)
	}
}

// Kanarientest: Der Report (wie prompt-probe ihn ausgibt) enthält die
// Eingabe nicht, nur Zähler und den Hash.
func TestReportHasNoSecret(t *testing.T) {
	const canary = "Kanarie-Prüfung-4711"
	f := newFake(shellMode, u16(canary+"\r\n"))
	var rep Report
	b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
	if err != nil {
		t.Fatal(err)
	}
	describe(b, &rep)
	out, _ := json.Marshal(rep)
	if bytes.Contains(out, []byte(canary)) || bytes.Contains(out, []byte("Kanarie")) || bytes.Contains(out, []byte("4711")) {
		t.Fatalf("report leaks the input: %s", out)
	}
	if sum := sha256.Sum256([]byte(canary)); rep.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %q", rep.SHA256)
	}
}

func TestModeNames(t *testing.T) {
	if got := ModeNames(0x1f7); got != "0x01f7 PROCESSED_INPUT|LINE_INPUT|ECHO_INPUT|MOUSE_INPUT|INSERT_MODE|QUICK_EDIT_MODE|EXTENDED_FLAGS|AUTO_POSITION" {
		t.Fatalf("got %q", got)
	}
	if got := ModeNames(0); got != "0x0000 none" {
		t.Fatalf("got %q", got)
	}
	if got := ModeNames(0x1200); got != "0x1200 VIRTUAL_TERMINAL_INPUT|0x1000" {
		t.Fatalf("got %q", got)
	}
}

// Kein Terminal (Datei statt Konsole): Fehler, bevor eine Aufforderung
// erscheint (Hypothese a: heute schon „standard input is not a terminal").
func TestPasswordNotTerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out bytes.Buffer
	for _, r := range []string{ReaderAuto, ReaderTerm, ReaderConsole} {
		_, rep, err := Password(context.Background(), f, &out, "Password: ", Options{Reader: r})
		if !errors.Is(err, ErrNotTerminal) || rep.IsTerminal || out.Len() != 0 {
			t.Fatalf("%q: err=%v rep=%+v out=%q", r, err, rep, out.String())
		}
	}
	if e := Environment(f); e.StdinType != "disk" {
		t.Fatalf("env = %+v", e)
	}
}
