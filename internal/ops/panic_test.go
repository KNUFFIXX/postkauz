package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/policy"
)

// S1.11: Ein Panic im Handler wird abgefangen. Der Aufruf scheitert mit
// ErrInternal, ohne den Panic-Wert (hier Mailtext) weiterzugeben; das Audit
// hat den Grund "panic"; der Prozess läuft weiter.
func TestExecutorRecoversHandlerPanic(t *testing.T) {
	const canary = "KANARIE-MAILTEXT-panic-41"
	reg := NewRegistry()
	for _, op := range []Operation{
		newTestOp("m.crash", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) {
			panic(fmt.Sprintf("body was %s", canary))
		}),
		newTestOp("m.fine", policy.ScopeMailRead, policy.R0, func(context.Context, Call) (any, error) {
			return map[string]string{"text": canary}, nil
		}),
	} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	au := &audit.Memory{}
	var reports []crash.Report
	x := &Executor{Registry: reg, Audit: au, OnPanic: func(op string, r crash.Report) { reports = append(reports, r) }}
	key := policy.Key{ID: "rd", Scopes: policy.Roles["reader"]}
	_, err := x.Execute(context.Background(), Request{Key: key, Actor: audit.ActorAgent, Operation: "m.crash"})
	var pe *PanicError
	if !errors.Is(err, ErrInternal) || !errors.As(err, &pe) || strings.Contains(err.Error(), canary) ||
		err.Error() != "internal error in m.crash: panic value withheld (string)" {
		t.Fatalf("err = %v", err)
	}
	if len(reports) != 1 || strings.Contains(reports[0].String(), canary) || len(reports[0].Stack) == 0 {
		t.Fatalf("reports = %+v", reports)
	}
	ev := au.Events()
	if len(ev) != 2 || ev[1].Result != audit.ResultError || ev[1].Reason != "panic" {
		t.Fatalf("audit = %+v", ev)
	}
	// Der Testschalter löst nach dem Handler einen Panic mit dessen Ausgabe aus.
	t.Setenv(EnvTestPanic, "handler:m.fine")
	if _, err := x.Execute(context.Background(), Request{Key: key, Operation: "m.fine"}); !errors.Is(err, ErrInternal) || strings.Contains(err.Error(), canary) {
		t.Fatalf("injected: %v", err)
	}
	t.Setenv(EnvTestPanic, "")
	if _, err := x.Execute(context.Background(), Request{Key: key, Operation: "m.fine"}); err != nil {
		t.Fatalf("after panic: %v", err)
	}
}
