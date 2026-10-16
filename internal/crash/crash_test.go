package crash

import (
	"errors"
	"fmt"
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
