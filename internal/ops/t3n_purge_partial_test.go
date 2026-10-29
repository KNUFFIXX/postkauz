package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Nachbesserung T3 Punkt 7 (E8: Zähler im Audit): Bricht retention.purge
// nach schon festgeschriebenen 500er-Paketen ab (Fehler, Strg+C), stehen
// die bis dahin gelöschten Nachrichten als Zähler im Audit-Abschluss, und
// der Aufrufer bekommt den Bericht bis zum Abbruch (OutputOnError).
func TestT3NPurgeAbortKeepsPartialCountsInAudit(t *testing.T) {
	e := newRetEnv(t)
	ctx := context.Background()
	if err := e.st.ExecForTest(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 600)
		INSERT INTO messages(account_id, subject, deleted_remote_at, created_at) SELECT ?, 'Weg', 1, 1 FROM n`, e.acct); err != nil {
		t.Fatal(err)
	}
	// Das zweite Paket scheitert (Ersatz für einen Fehler oder Strg+C mitten im Lauf).
	if err := e.st.ExecForTest(`CREATE TRIGGER t3n_fail BEFORE DELETE ON messages
		WHEN old.id = (SELECT max(id) FROM messages) BEGIN SELECT RAISE(ABORT, 't3n boom'); END`); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "retention.purge", `{"older_than":"1d"}`, false)
	if err == nil {
		t.Fatal("purge should fail in the second batch")
	}
	if left := e.count(t, `SELECT count(*) FROM messages`); left != 100 {
		t.Fatalf("messages left = %d, want 100 (first batch of 500 committed)", left)
	}
	if o, ok := out.(RetentionPurgeOutput); !ok || o.Purged != 500 || o.Eligible != 600 {
		t.Errorf("partial output = %#v, want purged 500 of 600", out)
	}
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "retention.purge"})
	last := evs[len(evs)-1]
	if last.Result != audit.ResultError || !strings.Contains(string(last.ParamsRedacted), `"purged":500`) ||
		!strings.Contains(string(last.ParamsRedacted), `"eligible":600`) {
		t.Fatalf("audit end entry lacks the partial counts: result=%s params=%s", last.Result, last.ParamsRedacted)
	}
}

// Dasselbe für blobs.gc: Scheitert das Löschen der Zeilen, stehen die
// schon ermittelten Zähler im Audit-Abschluss.
func TestT3NGCAbortKeepsPartialCountsInAudit(t *testing.T) {
	e := newRetEnv(t)
	ctx := context.Background()
	if err := e.st.ExecForTest(`INSERT INTO blobs(sha256, size, created_at) VALUES (?, 3, 1)`, strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`CREATE TRIGGER t3n_fail BEFORE DELETE ON blobs BEGIN SELECT RAISE(ABORT, 't3n boom'); END`); err != nil {
		t.Fatal(err)
	}
	out, err := e.run(t, "blobs.gc", `{}`, false)
	if err == nil {
		t.Fatal("gc should fail while deleting rows")
	}
	if o, ok := out.(BlobsGCOutput); !ok || o.Rows != 1 || o.Unreferenced != 1 || o.RemovedRows != 0 {
		t.Errorf("partial output = %#v", out)
	}
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "blobs.gc"})
	last := evs[len(evs)-1]
	if last.Result != audit.ResultError || !strings.Contains(string(last.ParamsRedacted), `"unreferenced":1`) {
		t.Fatalf("audit end entry lacks the partial counts: result=%s params=%s", last.Result, last.ParamsRedacted)
	}
}
