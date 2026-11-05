package cli

import (
	"strings"
	"testing"
)

// A06 Teil 5: draft reply mit anderem Betreff zeigt die Warnung, der
// Entwurf entsteht trotzdem; mit dem abgeleiteten Betreff keine Warnung.
func TestA06SubjectWarningCLI(t *testing.T) {
	setupEnv(t)
	ph := seedS16(t)
	const warn = "warning: the subject differs from the original; Gmail may start a new conversation and drop In-Reply-To"
	code, out, errs := run(t, "draft", "reply", ph["answer"], "--subject", "Ganz anderer Betreff")
	if code != ExitOK || !strings.Contains(out+errs, warn) || !strings.Contains(out, "draft 1 created") {
		t.Fatalf("draft reply with another subject: %d\n%s%s", code, out, errs)
	}
	code, out, errs = run(t, "draft", "reply", ph["answer"])
	if code != ExitOK || strings.Contains(out+errs, "subject differs") {
		t.Fatalf("draft reply: %d\n%s%s", code, out, errs)
	}
}
