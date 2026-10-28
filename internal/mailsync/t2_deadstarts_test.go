package mailsync

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// insertStart schreibt einen offenen Start von sync.run für das Konto demo
// mit dem Zeitstempel ts (wie vom Executor eines anderen Prozesses).
func insertStart(t *testing.T, st *store.SQLite, ts time.Time) int64 {
	t.Helper()
	return insertStartDry(t, st, ts, false)
}

// insertStartDry wie insertStart, mit dry_run.
func insertStartDry(t *testing.T, st *store.SQLite, ts time.Time, dry bool) int64 {
	t.Helper()
	d := 0
	if dry {
		d = 1
	}
	if err := st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, api_key_id, operation, params_redacted, targets, result, dry_run)
		VALUES (?, 'human', 'cli', 'sync.run', '{}', '["account:demo"]', 'started', ?)`, ts.UnixMilli(), d); err != nil {
		t.Fatal(err)
	}
	rows, err := st.AuditQuery(context.Background(), store.AuditFilter{Result: "started"})
	if err != nil || len(rows) == 0 {
		t.Fatalf("starts: %v", err)
	}
	return rows[len(rows)-1].ID
}

// closedStarts liefert die Starts, die einen Abschluss process_ended haben.
func closedStarts(t *testing.T, st *store.SQLite) map[int64]bool {
	t.Helper()
	rows, err := st.AuditQuery(context.Background(), store.AuditFilter{Result: "error"})
	if err != nil {
		t.Fatal(err)
	}
	m := map[int64]bool{}
	for _, r := range rows {
		if r.Reason == "process_ended" {
			m[r.RefID] = true
		}
	}
	return m
}

// T2 Punkt 7 (i), Fall 1: Ein Lauf, der vor dem Holen der Kontosperre
// länger als eine Minute brauchte (hier: sein Start ist fünf Minuten alt),
// gilt einem anderen Lauf desselben Kontos nicht als tot. Bis T1b schloss
// der spätere Lauf ihn ab, und der echte Abschluss scheiterte am
// UNIQUE-Index audit_events_ref.
func TestSlowRunBeforeLockIsNotClosed(t *testing.T) {
	e := newPushEnv(t)
	slow := insertStart(t, e.st, time.Now().Add(-5*time.Minute))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if closedStarts(t, e.st)[slow] {
		t.Fatal("the start of a slow run (5 min) was closed as process_ended")
	}
}

// T2 Punkt 7 (i), Fall 2: Ein toter Start, der beim Start von sync --watch
// noch zu jung war, wird von einem späteren Lauf über alle Ordner
// abgeschlossen (heute prüfte das niemand mehr, solange der Dauerläufer
// die Sperre hielt); der eigene Start nie.
func TestWatchClosesStartsThatAgeLater(t *testing.T) {
	e := newPushEnv(t)
	dead := insertStart(t, e.st, time.Now().Add(-store.DeadStartAge+time.Second))
	own := insertStart(t, e.st, time.Now().Add(-2*store.DeadStartAge))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rounds := make(chan struct{}, 16)
	done := make(chan error, 1)
	go func() {
		_, err := e.runner.Sync(ctx, e.account(t), provider.PasswordCredential(testPass), ops.SyncOptions{Watch: true,
			Interval: 700 * time.Millisecond, JobsInterval: time.Hour, AuditID: own + 1}, func(p ops.Progress) {
			if p.Event == "round" {
				rounds <- struct{}{}
			}
		})
		done <- err
	}()
	<-rounds
	if closedStarts(t, e.st)[dead] {
		t.Fatal("start closed although younger than DeadStartAge")
	}
	waitFor(t, 10*time.Second, func() bool { return closedStarts(t, e.st)[dead] })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// own+1 ist der eigene Start (es gibt ihn nicht); own ist älter und
	// tot, also ebenfalls abgeschlossen; nichts doppelt.
	if c := closedStarts(t, e.st); !c[own] || len(c) != 2 {
		t.Fatalf("closed = %v", c)
	}
}

// Codex-Runde auf 589bb8d: Ein Trockenlauf (sync/import --dry-run) hält
// keine Kontosperre und läuft womöglich noch, wenn ein schreibender Lauf
// desselben Kontos die Sperre holt. Sein Start bleibt offen, auch wenn er
// älter als DeadStartAge ist, und sein echter Abschluss gelingt danach;
// ein gleich alter schreibender Start daneben wird abgeschlossen.
func TestDryRunStartIsNotClosedByParallelRun(t *testing.T) {
	e := newPushEnv(t)
	dry := insertStartDry(t, e.st, time.Now().Add(-2*store.DeadStartAge), true)
	dead := insertStart(t, e.st, time.Now().Add(-2*store.DeadStartAge))
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	c := closedStarts(t, e.st)
	if c[dry] || !c[dead] {
		t.Fatalf("closed = %v (dry run %d, dead %d)", c, dry, dead)
	}
	if err := e.st.ExecForTest(`INSERT INTO audit_events(ts, actor_type, operation, result, dry_run, ref_id) VALUES (?, 'human', 'sync.run', 'ok', 1, ?)`,
		time.Now().UnixMilli(), dry); err != nil {
		t.Fatalf("own end entry of the dry run: %v", err)
	}
	// doctor zählt einen offenen Trockenlauf-Start nicht.
	other := insertStartDry(t, e.st, time.Now().Add(-2*store.DeadStartAge), true)
	n, err := e.st.AuditOpenStarts(context.Background(), time.Now())
	if err != nil || n != 0 {
		t.Fatalf("open starts = %d %v (dry run %d)", n, err, other)
	}
}
