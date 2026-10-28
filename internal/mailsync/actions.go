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
//     sieht das Ergebnis im selben Lauf; vorher gleicht der Job die
//     Ordnerliste ab, wenn Aktionen warten und er wirklich läuft), und
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
	pending, err := a.Runner.Pending(ctx, e.AccountID)
	if err != nil {
		return nil, fmt.Errorf("actions: %w", err)
	}
	if pending && ensure && a.Ensure != nil {
		if err := a.Ensure(ctx, e.AccountID); err != nil {
			return nil, fmt.Errorf("actions: %w", err)
		}
	}
	remote, ok := e.Remote.(actions.Remote)
	if !ok {
		return nil, nil // Verbindung ohne Schreibkommandos (Tests mit Lese-Fakes)
	}
	var rep actions.Report
	_, err = a.Jobs.RunMailboxAction(ctx, e.AccountID, func(ctx context.Context) error {
		// Nachprüfung zu fd70f19, Punkt 2: Vor den Aktionen die Ordnerliste
		// abgleichen, damit die Prüfung des Ziels (Pfad und Rolle unter der
		// ID, V1) den Stand des Servers sieht und nicht den des letzten
		// Laufs (im Jobs-Takt von sync --watch gleicht sonst niemand die
		// Ordner ab). Zusatz (c) der Prüfsitzung (T3): nur, wenn Aktionen
		// warten und der Job wirklich läuft (fällig und übernommen); ein Job
		// im Backoff schickt also kein LIST alle 10 s. Scheitert der
		// Abgleich, scheitert der Job (Verbindung, Anmeldung: ohne Zählung
		// zurück; sonst mit Wartezeit), die Aktionen warten.
		if pending {
			if err := e.syncMailboxList(ctx); err != nil {
				return fmt.Errorf("mailbox list: %w", err)
			}
		}
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
		e.emit(Event{Kind: "actions", Done: rep.OK, Total: rep.OK + rep.Failed + rep.Stale, Message: actionsLine(rep)})
	}
	return out, err
}

// actionsLine fasst einen Lauf der Aktionen zusammen (Zahlen je Ziel; V6
// der Nachbesserung T2: die Watch-Zeile).
func actionsLine(rep actions.Report) string {
	return fmt.Sprintf("%d done, %d failed, %d stale (%d actions; see action list)", rep.OK, rep.Failed, rep.Stale, rep.Actions)
}
