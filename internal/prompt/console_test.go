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
	// K6: Bei zu langer Eingabe liest der Leser die Zeile zu Ende (der Rest
	// bliebe sonst für die Shell im Puffer), aber nicht weiter.
	for _, n := range []int{MaxLen + 1, MaxLen + 3, 3 * MaxLen} {
		rep = Report{}
		f = newFake(shellMode, u16(strings.Repeat("y", n)+"\r\n"), u16("next\r\n"))
		_, err := readConsoleLine(context.Background(), f, time.Second, &rep)
		if !errors.Is(err, ErrTooLong) || len(f.chunks) != 1 || string(utf16.Decode(f.chunks[0])) != "next\r\n" ||
			rep.Units != n+2 || rep.Terminator != "crlf" || f.m != shellMode {
			t.Fatalf("too long %d: %v rest=%d rep=%+v", n, err, len(f.chunks), rep)
		}
	}
	// Restpunkt der Nachprüfung: CR genau am Ende eines Lesepuffers (256
	// Einheiten), das LF folgt erst im nächsten Lesevorgang. Der Leser
	// liest es noch, sonst bekäme die Shell eine leere Zeile.
	for _, n := range []int{255, 256*17 - 1} {
		rep = Report{}
		f = newFake(shellMode, u16(strings.Repeat("w", n)+"\r\n"), u16("next\r\n"))
		b, err := readConsoleLine(context.Background(), f, time.Second, &rep)
		if n <= MaxLen && (err != nil || len(b) != n) || n > MaxLen && !errors.Is(err, ErrTooLong) {
			t.Fatalf("cr at buffer end %d: %d %v", n, len(b), err)
		}
		if len(f.chunks) != 1 || string(utf16.Decode(f.chunks[0])) != "next\r\n" ||
			rep.Units != n+2 || rep.Terminator != "crlf" || f.m != shellMode {
			t.Fatalf("cr at buffer end %d: rest=%d rep=%+v", n, len(f.chunks), rep)
		}
	}
	// Genau MaxLen Zeichen gehen noch.
	f = newFake(shellMode, u16(strings.Repeat("z", MaxLen)+"\r\n"), u16("next\r\n"))
	if b, err := readConsoleLine(context.Background(), f, time.Second, &rep); err != nil || len(b) != MaxLen || len(f.chunks) != 1 {
		t.Fatalf("max: %d %v", len(b), err)
	}
	// Strg+Z allein (Dateiende der Konsole) ergibt eine leere Eingabe.
	f = newFake(shellMode, []uint16{0x1a, '\r', '\n'})
	if b, err := readConsoleLine(context.Background(), f, time.Second, &rep); err != nil || len(b) != 0 {
		t.Fatalf("ctrl+z: %q %v", b, err)
	}
}

// Kanarientest: Der Report (wie prompt-probe ihn ausgibt) enthält die
// Eingabe nicht und auch keinen Hash (W2), nur Zähler; MatchesSHA256
// vergleicht mit einem vorgegebenen Hash.
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
	sum := sha256.Sum256([]byte(canary))
	want := hex.EncodeToString(sum[:])
	if bytes.Contains(out, []byte(want)) || bytes.Contains(out, []byte("sha256")) {
		t.Fatalf("report carries a hash of the input: %s", out)
	}
	if !MatchesSHA256(b, want) || !MatchesSHA256(b, strings.ToUpper(want)) || MatchesSHA256(b, want[:62]) ||
		MatchesSHA256(b, "zz"+want[2:]) || MatchesSHA256([]byte("anders"), want) || MatchesSHA256(b, "") {
		t.Fatal("MatchesSHA256")
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

// Gegenprüfung K8: Ein unbekannter Wert von POSTKAUZ_PASSWORD_READER fällt
// vor der Aufforderung auf (out bleibt leer), auch am Terminal.
func TestPasswordUnknownReaderBeforePrompt(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	t.Setenv(EnvReader, "conosle")
	var out bytes.Buffer
	_, rep, err := Password(context.Background(), f, &out, "Password: ", Options{})
	if !errors.Is(err, ErrUnknownReader) || !strings.Contains(err.Error(), `"conosle"`) || rep.Reader != "conosle" || out.Len() != 0 {
		t.Fatalf("err=%v rep=%+v out=%q", err, rep, out.String())
	}
	// Ausdrücklich gewählter Leser geht vor der Umgebung.
	if _, _, err := Password(context.Background(), f, &out, "Password: ", Options{Reader: ReaderTerm}); !errors.Is(err, ErrNotTerminal) {
		t.Fatalf("explicit reader: %v", err)
	}
}
