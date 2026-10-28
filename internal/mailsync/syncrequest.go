package mailsync

import (
	"context"
	"errors"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Job sync im Dauerläufer (T2, sync.request bzw. MCP mail_sync_now;
// ADR-009 E1, Entscheid 23): Ein Agent oder Mensch reiht ihn ein, ohne
// Zugangsdaten. Wer sie hat und die Kontosperre hält, führt ihn aus:
//
//   - sync --watch prüft je WatchOptions.JobsInterval (10 s), ob einer
//     fällig ist, prüft seine Herkunft und läuft dann sofort (mit Ordnern
//     nur über diese, sonst über alle);
//   - jeder fehlerfreie Lauf (auch der einmalige postkauz sync und jeder
//     Lauf des Dauerläufers) erledigt die Jobs, die er abdeckt
//     (angefordert vor seinem Beginn, Ordner darin enthalten).

// SyncJobRunner ist der fachliche Teil des Jobs sync (ops.SyncJobs).
type SyncJobRunner interface {
	Claim(ctx context.Context, accountID int64, takeOver bool) (store.SyncJob, bool, error)
	Denied(ctx context.Context, j store.SyncJob) (string, error)
	Refuse(ctx context.Context, a store.Account, j store.SyncJob, reason string) error
	Complete(ctx context.Context, j store.SyncJob) error
	Release(ctx context.Context, j store.SyncJob, runAt time.Time) error
	Fail(ctx context.Context, j store.SyncJob, errText string) error
	Settle(ctx context.Context, accountID int64, mailboxes []string, since time.Time) (int, error)
}

var _ SyncJobRunner = (*ops.SyncJobs)(nil)

// SyncRequests verbindet die Engine mit dem Job sync.
type SyncRequests struct {
	Jobs    SyncJobRunner
	Account store.Account
	// TakeOver: Der Prozess hält die Kontosperre; ein laufender Job des
	// Kontos stammt von einem abgebrochenen Prozess.
	TakeOver bool
}

// settleRequests erledigt nach einem fehlerfreien Lauf ab since über
// mailboxes (leer = alle) die Jobs sync, die er abdeckt. Ein Fehler hält
// nichts auf (der Job bleibt fällig und läuft dann eben noch einmal).
func (e *Engine) settleRequests(ctx context.Context, mailboxes []string, since time.Time) {
	r := e.Requests
	if r == nil {
		return
	}
	n, err := r.Jobs.Settle(ctx, e.AccountID, mailboxes, since)
	switch {
	case err != nil:
		e.log().Warn("requested syncs not marked done", "error", ops.ErrorKinds(err))
	case n > 0:
		e.log().Info("requested syncs done by this run", "count", n)
	}
}

// runSyncRequest führt den fälligen Job sync des Kontos aus (nur im
// Dauerbetrieb): übernehmen, Herkunft prüfen, Lauf (round), abschließen.
// round liefert den Fehler, der Watch beendet (Anmeldung), und den des
// Laufs. Der Rückgabewert beendet Watch.
func (e *Engine) runSyncRequest(ctx context.Context, jobsInterval time.Duration, round func(only []string) (fatal, err error)) error {
	r := e.Requests
	j, ok, err := r.Jobs.Claim(ctx, e.AccountID, r.TakeOver)
	if err != nil {
		if ctx.Err() == nil {
			e.log().Warn("requested sync not checked; retrying at the next check", "error", ops.ErrorKinds(err))
		}
		return nil
	}
	if !ok {
		return nil
	}
	reason, err := r.Jobs.Denied(ctx, j)
	if err != nil {
		_ = r.Jobs.Release(ctx, j, time.Now().Add(jobsInterval))
		e.log().Warn("origin of a requested sync not checked; retrying later", "job_id", j.ID, "error", ops.ErrorKinds(err))
		return nil
	}
	if reason != "" {
		// Schlüssel widerrufen, abgelaufen oder ohne mail:sync: kein Lauf.
		if err := r.Jobs.Refuse(ctx, r.Account, j, reason); err != nil {
			e.log().Warn("refused sync request not fully recorded", "job_id", j.ID, "error", ops.ErrorKinds(err))
		}
		e.log().Warn("requested sync refused: the key that asked for it may no longer sync", "job_id", j.ID,
			"key", j.Origin.KeyID, "reason", reason)
		e.emit(Event{Kind: "sync_request", Done: int(j.ID), Message: "refused (" + reason + ")"})
		return nil
	}
	e.log().Info("requested sync: running it now", "job_id", j.ID, "key", j.Origin.KeyID, "mailboxes", len(j.Mailboxes))
	e.emit(Event{Kind: "sync_request", Done: int(j.ID), Message: "running"})
	var only []string
	if len(j.Mailboxes) > 0 {
		only = j.Mailboxes
	}
	fatal, err := round(only)
	switch {
	case ctx.Err() != nil:
		_ = r.Jobs.Release(ctx, j, time.Now())
	case err == nil:
		if err := r.Jobs.Complete(ctx, j); err != nil {
			e.log().Warn("requested sync not marked done", "job_id", j.ID, "error", ops.ErrorKinds(err))
		}
	case fatal != nil || errors.Is(err, provider.ErrConnection):
		// Verbindung weg oder Anmeldung abgelehnt: nicht als Fehler des
		// Jobs zählen; er bleibt fällig (nach einer Minute).
		_ = r.Jobs.Release(ctx, j, time.Now().Add(time.Minute))
	default:
		// Nur die Fehlerart: Der Text kann Ordnernamen und Serverantworten
		// enthalten.
		if err := r.Jobs.Fail(ctx, j, "sync failed: "+ops.ErrorKinds(err)); err != nil {
			e.log().Warn("failed sync request not recorded", "job_id", j.ID, "error", ops.ErrorKinds(err))
		}
	}
	return fatal
}
