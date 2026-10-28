package mailsync

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// listCounter zählt LIST-Abfragen (ListMailboxes) einer Verbindung.
type listCounter struct {
	imapRemote
	n *atomic.Int32
}

func (l listCounter) ListMailboxes(ctx context.Context) ([]provider.Mailbox, error) {
	l.n.Add(1)
	return l.imapRemote.ListMailboxes(ctx)
}

// Zusatz (c) der Prüfsitzung (T3): Wartet eine Aktion, deren Job im
// Backoff ist, schickt der Jobs-Takt von sync --watch kein LIST an den
// Server; der Ordnerabgleich vor den Aktionen läuft nur, wenn der Job
// wirklich läuft. Wird der Job fällig, gleicht er ab und führt aus.
func TestWatchSendsNoListWhileActionJobBacksOff(t *testing.T) {
	e := newPushEnv(t, "Archiv")
	raw := "Message-ID: <backoff@example.org>\r\nFrom: a@example.org\r\nSubject: Backoff\r\n\r\nText\r\n"
	if _, err := e.u.Append("INBOX", litBytes{bytes.NewReader([]byte(raw))}, &imap.AppendOptions{}); err != nil {
		t.Fatal(err)
	}
	e.runner.Actions = &actions.Runner{Store: e.st, Blobs: e.blobs}
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	svc := &actions.Service{Store: e.st}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "backoff@example.org")
	dest, _, _ := svc.FindMailbox(ctx, e.acct, "Archiv", "")
	plan, err := svc.PlanMove(ctx, ids, "", dest)
	if err != nil {
		t.Fatal(err)
	}
	a, err := svc.Create(ctx, actions.Request{AccountID: e.acct, Kind: store.ActionMove, IDs: ids, Plan: plan, RequestedBy: "cli",
		Params: store.ActionParams{TargetMailboxID: dest.MailboxID, TargetMailbox: dest.Path}})
	if err != nil {
		t.Fatal(err)
	}
	// Der Job ist nach einem Fehler im Backoff (nächster Versuch in einer Stunde).
	if err := e.st.ExecForTest(`UPDATE jobs SET attempts = 1, last_error = 'mailbox list: boom', run_at = ? WHERE type = 'mailbox_action'`,
		time.Now().Add(time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	var lists atomic.Int32
	e.runner.Connect = func(ctx context.Context, a store.Account, cred provider.Credential, idle time.Duration) (RemoteCloser, error) {
		return listCounter{imapRemote: imapRemote{connect(t, e.addr, idle)}, n: &lists}, nil
	}
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var rounds atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := e.runner.Sync(wctx, e.account(t), provider.PasswordCredential(testPass), ops.SyncOptions{Watch: true, Interval: time.Hour,
			JobsInterval: 20 * time.Millisecond}, func(p ops.Progress) {
			if p.Event == "round" {
				rounds.Add(1)
			}
		})
		done <- err
	}()
	waitFor(t, 5*time.Second, func() bool { return rounds.Load() == 1 })
	afterRound := lists.Load()
	time.Sleep(400 * time.Millisecond) // rund 20 Takte
	if n := lists.Load(); n != afterRound {
		t.Fatalf("LIST while the job backs off: %d after the round, %d later", afterRound, n)
	}
	if got, _ := e.st.Action(ctx, a.ID); got.State != store.ActionQueued {
		t.Fatalf("action ran during the backoff: %s", got.State)
	}
	// Fällig: ein Abgleich, dann die Aktion.
	if err := e.st.ExecForTest(`UPDATE jobs SET run_at = 0 WHERE type = 'mailbox_action' AND state = 'queued'`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, func() bool {
		got, _ := e.st.Action(ctx, a.ID)
		return got.State == store.ActionDone
	})
	// Mindestens der Abgleich des Jobs (danach kann eine IDLE-Meldung über
	// das Verschieben einen weiteren Lauf mit eigenem LIST auslösen).
	if n := lists.Load(); n <= afterRound {
		t.Fatalf("no LIST for the due job: %d (after round %d)", n, afterRound)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
