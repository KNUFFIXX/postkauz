package store

import (
	"context"
	"testing"
	"time"
)

// V14 der Nachbesserung T2: Nach dem fünften Fehler ist der Job
// mailbox_action failed, sein run_at nennt den nächsten Versuch.
// EnsureActionJob (je Takt von sync --watch) legt bis dahin keinen neuen
// Job an; danach wieder genau einen.
func TestEnsureActionJobKeepsBackoffOfFailedJob(t *testing.T) {
	ctx := context.Background()
	s, acct, inbox, _, _ := actionEnv(t)
	yes := true
	if _, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionMark, State: ActionQueued, IDsHash: "h", RequestedBy: "cli",
		Params: ActionParams{Mark: &MarkParams{Seen: &yes}}, Targets: []ActionTarget{{MessageID: 1, MailboxID: inbox, UID: 1, UIDValidity: 7}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	// Fünf Fehler wie jobs.Runner.fail: der letzte mit 16 Minuten Wartezeit.
	for i := 0; i < 5; i++ {
		j, ok, err := s.ClaimJob(ctx, JobMailboxAction, acct, "w", time.Minute, true)
		if err != nil || !ok {
			t.Fatalf("claim %d: %v %v", i, ok, err)
		}
		if err := s.FailJob(ctx, j.ID, "w", "mailbox list: boom", 5, now.Add(-time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ExecForTest(`UPDATE jobs SET run_at = ? WHERE type = 'mailbox_action'`, now.Add(16*time.Minute).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var state string
	_ = s.db.QueryRow(`SELECT state FROM jobs WHERE type = 'mailbox_action'`).Scan(&state)
	if state != "failed" {
		t.Fatalf("job state = %s", state)
	}
	count := func() int {
		var n int
		_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action'`).Scan(&n)
		return n
	}
	for i := 0; i < 3; i++ {
		if made, err := s.EnsureActionJob(ctx, acct, now.Add(time.Duration(i)*10*time.Second)); err != nil || made {
			t.Fatalf("tick %d: new job %v %v", i, made, err)
		}
	}
	if n := count(); n != 1 {
		t.Fatalf("jobs = %d", n)
	}
	if made, err := s.EnsureActionJob(ctx, acct, now.Add(17*time.Minute)); err != nil || !made {
		t.Fatalf("after the backoff: %v %v", made, err)
	}
	if made, _ := s.EnsureActionJob(ctx, acct, now.Add(17*time.Minute)); made || count() != 2 {
		t.Fatalf("second job while one is open: %v, jobs %d", made, count())
	}
}
