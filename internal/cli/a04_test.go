package cli

import (
	"strings"
	"testing"
)

// A04 Teil 2: draft reply --no-derive-recipients legt die Antwort im Thread
// ohne Empfänger an (Warnung wie bei draft new), draft request lehnt ab,
// bis draft edit eine Adresse einträgt. Ohne die Option wie bisher.
// Teil 1: draft forward trägt In-Reply-To wie draft reply.
func TestA04ReplyWithoutDerivedRecipientsCLI(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	step := func(want int, args ...string) string {
		t.Helper()
		code, out, errs := run(t, args...)
		if code != want {
			t.Fatalf("%v: exit %d, want %d\nstdout: %s\nstderr: %s", args, code, want, out, errs)
		}
		return out + errs
	}
	out := step(ExitOK, "draft", "reply", ph["answer"], "--no-derive-recipients")
	for _, want := range []string{"draft 1 created", "in-reply-to: <antwort@example.org>", "warning: the draft has no recipient"} {
		if !strings.Contains(out, want) {
			t.Fatalf("draft reply --no-derive-recipients lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "to:          Bert") {
		t.Fatalf("recipient of the original taken over:\n%s", out)
	}
	if out := step(ExitDenied, "draft", "request", "1"); !strings.Contains(out, "no_recipients") {
		t.Fatalf("draft request: %s", out)
	}
	step(ExitOK, "draft", "edit", "1", "--to", "neu@example.org")
	if out := step(ExitOK, "draft", "show", "1"); !strings.Contains(out, "neu@example.org") || !strings.Contains(out, "in-reply-to: <antwort@example.org>") {
		t.Fatalf("draft show: %s", out)
	}
	// Ohne Option: abgeleitet wie bisher.
	if out := step(ExitOK, "draft", "reply", ph["answer"]); !strings.Contains(out, "to:          Bert <b@example.org>") || strings.Contains(out, "has no recipient") {
		t.Fatalf("draft reply: %s", out)
	}
	// Weiterleitung im Thread (Teil 1).
	if out := step(ExitOK, "draft", "forward", ph["answer"], "--to", "kollege@example.net"); !strings.Contains(out, "in-reply-to: <antwort@example.org>") {
		t.Fatalf("draft forward: %s", out)
	}
}
