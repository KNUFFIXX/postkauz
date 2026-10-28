package mailsync

import (
	"context"
	"fmt"

	"github.com/KNUFFIXX/postkauz/internal/actions"
	"github.com/KNUFFIXX/postkauz/internal/crash"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Job mailbox_action im Sync (T2, E1): Wer Zugangsdaten hat und die
// Kontosperre hält, wendet die wartenden Postfach-Aktionen (mark, move,
// trash, keywords) auf dem Server an, und zwar wie draft_push
//
//   - postkauz sync zu Beginn jedes Laufs, vor dem Spiegeln (der Spiegel
//     sieht das Ergebnis im selben Lauf), und
//   - postkauz sync --watch zusätzlich alle WatchOptions.JobsInterval
//     (Standard 10 s), auch zwischen zwei Läufen.
//
// Die fachlichen Regeln (Herkunft, Kopfprüfung, Ergebnis je Ziel) stehen in
// internal/actions.

// ActionRunner ist der fachliche Teil des Jobs (actions.Runner).
type ActionRunner interface {
	Pending(ctx context.Context, accountID int64) (bool, error)
	Run(ctx context.Context, accountID int64, remote actions.Remote) (actions.Report, error)
}

var _ ActionRunner = (*actions.Runner)(nil)

// actionJobs führt den Job mailbox_action aus (jobs.Runner).
type actionJobs interface {
	RunMailboxAction(ctx context.Context, accountID int64, run func(context.Context) error) (bool, error)
}

// Actions verbindet die Engine mit dem Job mailbox_action.
type Actions struct {
	Runner ActionRunner
	Jobs   actionJobs
	// Ensure reiht den Job ein, wenn Aktionen warten und keiner offen ist
	// (Store.EnsureActionJob).
	Ensure func(ctx context.Context, accountID int64) error
}

// runActions führt den fälligen Job mailbox_action aus. ensure reiht ihn
// vorher ein (Beginn eines Laufs). nil = nichts zu tun.
func (e *Engine) runActions(ctx context.Context, ensure bool) (*ops.SyncActions, error) {
	a := e.Actions
	if a == nil {
		return nil, nil
	}
	if ensure && a.Ensure != nil {
		if pending, err := a.Runner.Pending(ctx, e.AccountID); err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		} else if pending {
			if err := a.Ensure(ctx, e.AccountID); err != nil {
				return nil, fmt.Errorf("actions: %w", err)
			}
		}
	}
	remote, ok := e.Remote.(actions.Remote)
	if !ok {
		return nil, nil // Verbindung ohne Schreibkommandos (Tests mit Lese-Fakes)
	}
	var rep actions.Report
	_, err := a.Jobs.RunMailboxAction(ctx, e.AccountID, func(ctx context.Context) error {
		// crash.Do (A02 A5): Ein Panic beendet den Job mit Fehler.
		return crash.Do("mailbox actions", func() error {
			var err error
			rep, err = a.Runner.Run(ctx, e.AccountID, remote)
			return err
		})
	})
	if rep.Actions == 0 && err == nil {
		return nil, nil
	}
	out := &ops.SyncActions{Actions: rep.Actions, OK: rep.OK, Failed: rep.Failed, Stale: rep.Stale}
	if err != nil {
		out.Error = err.Error()
		e.emit(Event{Kind: "error", Mailbox: "actions", Message: err.Error()})
	}
	if rep.Actions > 0 {
		e.emit(Event{Kind: "actions", Done: rep.OK, Total: rep.OK + rep.Failed + rep.Stale})
	}
	return out, err
}
