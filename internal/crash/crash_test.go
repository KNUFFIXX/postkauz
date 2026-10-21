package crash

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"testing"
)

const secret = "KANARIE-MAILTEXT-7c1d"

func capture(f func()) (r Report) {
	defer func() {
		if v := recover(); v != nil {
			r = FromPanic(v, debug.Stack())
		}
	}()
	f()
	return
}

//go:noinline
func leak(s string) { panic(fmt.Sprintf("bad message %s", s)) }

//go:noinline
func index(xs []string, i int) string { return xs[i] }

func TestReportWithholdsValueAndArguments(t *testing.T) {
	r := capture(func() { leak(secret) })
	s := r.String()
	if strings.Contains(s, secret) || strings.Contains(s, "bad message") || r.Message != "panic value withheld (string)" {
		t.Fatalf("report leaks the value:\n%s", s)
	}
	if !strings.Contains(s, "internal/crash.leak (internal/crash/crash_test.go:") {
		t.Fatalf("frame missing:\n%s", s)
	}
	if strings.Contains(s, "0x") || strings.Contains(s, "goroutine ") || strings.Contains(s, "runtime/debug") {
		t.Fatalf("stack not cleaned:\n%s", s)
	}
	if r := capture(func() { panic(errors.New(secret)) }); strings.Contains(r.String(), secret) || !strings.Contains(r.Message, "*errors.errorString") {
		t.Fatalf("error value: %s", r.Message)
	}
}

func TestReportKeepsRuntimeMessages(t *testing.T) {
	r := capture(func() { _ = index([]string{secret}, 5) })
	if r.Message != "runtime error: index out of range [5] with length 1" {
		t.Fatalf("message = %q", r.Message)
	}
	var m map[string]int
	if r := capture(func() { m[secret] = 1 }); !strings.Contains(r.Message, "assignment to entry in nil map") {
		t.Fatalf("nil map: %q", r.Message)
	}
	var x any = secret
	if r := capture(func() { _ = x.(int) }); !strings.Contains(r.Message, "interface conversion") || strings.Contains(r.Message, secret) {
		t.Fatalf("type assertion: %q", r.Message)
	}
}

func TestSanitizeStackFormat(t *testing.T) {
	raw := "goroutine 1 [running]:\n" +
		"runtime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\n" +
		"panic({0x4b2c8e?, 0xc000012345?})\n\t/usr/local/go/src/runtime/panic.go:792 +0x132\n" +
		"github.com/KNUFFIXX/postkauz/internal/ops.(*Executor).Execute(0xc0001, {0x5, 0x6}, {{0x7, 0x8}})\n" +
		"\t/home/build/pk/internal/ops/executor.go:210 +0x45\n" +
		"main.main()\n\tgithub.com/KNUFFIXX/postkauz/cmd/postkauz/main.go:18 +0x1f\n" +
		"created by net/http.(*Server).Serve in goroutine 7\n\t/usr/local/go/src/net/http/server.go:3360 +0x485\n"
	got := strings.Join(SanitizeStack([]byte(raw)), "|")
	want := "github.com/KNUFFIXX/postkauz/internal/ops.(*Executor).Execute (internal/ops/executor.go:210)|" +
		"main.main (cmd/postkauz/main.go:18)|" +
		"created by net/http.(*Server).Serve (net/http/server.go:3360)"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// A02 A5 (Frage 15): Do und Go fangen einen Panic ab, geben einen
// *PanicError ohne Panic-Wert zurück und schreiben den bereinigten Bericht
// ins Log; Fehler und nil von fn kommen unverändert durch.
func TestDoAndGoRecoverPanics(t *testing.T) {
	var buf bytes.Buffer
	SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	defer SetLogger(nil)

	err := Do("unit task", func() error { leak(secret); return nil })
	var pe *PanicError
	if !errors.Is(err, ErrInternal) || !errors.As(err, &pe) || pe.Name != "unit task" ||
		err.Error() != "internal error in unit task: panic value withheld (string)" {
		t.Fatalf("Do = %v", err)
	}
	if !strings.Contains(strings.Join(pe.Report.Stack, "\n"), "internal/crash.leak (internal/crash/crash_test.go:") {
		t.Fatalf("stack: %v", pe.Report.Stack)
	}
	// In einer eigenen Goroutine: Der Prozess läuft weiter, der Fehler kommt
	// über den Kanal.
	if err := <-Go("background", func() error { _ = index([]string{secret}, 3); return nil }); !errors.Is(err, ErrInternal) ||
		err.Error() != "internal error in background: runtime error: index out of range [3] with length 1" {
		t.Fatalf("Go = %v", err)
	}
	log := buf.String()
	if strings.Contains(log, secret) || strings.Contains(log, "bad message") || strings.Count(log, `"msg":"panic in background task"`) != 2 ||
		!strings.Contains(log, `"task":"unit task"`) || !strings.Contains(log, "internal/crash.leak (internal/crash/crash_test.go:") {
		t.Fatalf("log:\n%s", log)
	}

	plain := errors.New("plain")
	if err := Do("x", func() error { return plain }); err != plain {
		t.Fatalf("error passes through: %v", err)
	}
	if err := <-Go("x", func() error { return nil }); err != nil {
		t.Fatalf("nil: %v", err)
	}
}
