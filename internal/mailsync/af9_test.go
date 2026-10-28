package mailsync

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T1b, A-F9 (a): Ein hart beendeter sync --watch (Aufgabe
// beendet, kein Strg+C) hinterlässt einen Start ohne Abschluss. Der nächste
// Sync des Kontos schließt ihn mit der Kontosperre ab (error/process_ended,
// ref_id, gleiche Herkunft), auch einen Start ohne Ziel (nur ein Konto);
// danach zählt doctor nichts mehr. Der eigene Start bleibt offen.
func TestAF9SyncClosesDeadStarts(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	old := time.Now().Add(-2 * time.Hour).UnixMilli()
	for _, targets := range []string{`["account:demo"]`, `[]`, `["account:anderes"]`} {
		if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result)
			VALUES (?, 'human', 'cli', 'sync.run', '{"watch":true}', ?, 'started')`, old, targets); err != nil {
			t.Fatal(err)
		}
	}
	// Der eigene Start dieses Aufrufs (wie vom Executor geschrieben).
	if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, targets, result)
		VALUES (?, 'human', 'cli', 'sync.run', '["account:demo"]', 'started')`, old); err != nil {
		t.Fatal(err)
	}
	starts, err := e.st.AuditQuery(ctx, store.AuditFilter{Result: "started"})
	if err != nil || len(starts) != 4 {
		t.Fatalf("starts = %d %v", len(starts), err)
	}
	own := starts[3].ID
	if n, err := e.st.AuditOpenStarts(ctx, time.Now()); err != nil || n != 4 {
		t.Fatalf("open starts before = %d %v", n, err)
	}
	if _, err := e.sync(t, ops.SyncOptions{AuditID: own}); err != nil {
		t.Fatal(err)
	}
	ends, err := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "sync.run", Result: "error"})
	if err != nil {
		t.Fatal(err)
	}
	refs := map[int64]bool{}
	for _, x := range ends {
		if x.Reason != "process_ended" || x.KeyID != "cli" || string(x.ParamsRedacted) != `{"watch":true}` {
			t.Fatalf("end entry = %+v", x)
		}
		refs[x.RefID] = true
	}
	if len(ends) != 2 || !refs[starts[0].ID] || !refs[starts[1].ID] {
		t.Fatalf("ends = %+v", ends)
	}
	// Offen: der Start des anderen Kontos und der eigene.
	if n, err := e.st.AuditOpenStarts(ctx, time.Now()); err != nil || n != 2 {
		t.Fatalf("open starts after = %d %v", n, err)
	}
	// Ein zweiter Sync schließt nichts doppelt (eindeutiger ref_id).
	if _, err := e.sync(t, ops.SyncOptions{AuditID: own}); err != nil {
		t.Fatal(err)
	}
	if again, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "sync.run", Result: "error"}); len(again) != 2 {
		t.Fatalf("ends after second sync = %d", len(again))
	}
}
