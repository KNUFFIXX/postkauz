package ops

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// Nachbesserung T5, Punkt 9 (g): Eine gescheiterte Anmeldung mit einem
// Schlüssel schreibt params {"via":{"transport":…}} wie OAuth und der
// Executor (Festlegung 2), für stdio und http. Ohne Writer kein Eintrag.
func TestT5NAuthenticateKeyAuditedWritesVia(t *testing.T) {
	e := newAdminEnv(t)
	ctx := context.Background()
	res, err := e.run(t, adminKey, "keys.create", map[string]any{"name": "agent-1", "role": "reader"})
	if err != nil {
		t.Fatal(err)
	}
	secret := res.Output.(KeysCreateOutput).Secret
	now := time.Now()
	for _, tr := range []string{"stdio", "http"} {
		au := &audit.Memory{}
		if _, err := AuthenticateKeyAudited(ctx, e.st, au, tr, secret+"x", now); err == nil {
			t.Fatalf("%s: wrong key accepted", tr)
		}
		ev := au.Events()
		if len(ev) != 1 {
			t.Fatalf("%s: %d events, want 1", tr, len(ev))
		}
		want := `{"via":{"transport":"` + tr + `"}}`
		if got := string(ev[0].ParamsRedacted); got != want || ev[0].Operation != OpAuthenticate || ev[0].Reason != "key_invalid" ||
			ev[0].Result != audit.ResultDenied {
			t.Errorf("%s: event %+v params %s, want %s", tr, ev[0], got, want)
		}
	}
	// Abgelaufen: Schlüsselname bekannt, gleiche Form.
	au := &audit.Memory{}
	if _, err := AuthenticateKeyAudited(ctx, e.st, au, "http", secret, now.Add(91*24*time.Hour)); err == nil {
		t.Fatal("expired key accepted")
	}
	if ev := au.Events(); len(ev) != 1 || ev[0].KeyID != "key:agent-1" || string(ev[0].ParamsRedacted) != `{"via":{"transport":"http"}}` {
		t.Errorf("expired: %+v", ev)
	}
	// Ohne Writer: Ablehnung ohne Eintrag (kein Panic).
	if _, err := AuthenticateKeyAudited(ctx, e.st, nil, "http", secret+"x", now); err == nil {
		t.Fatal("nil writer: wrong key accepted")
	}
}
