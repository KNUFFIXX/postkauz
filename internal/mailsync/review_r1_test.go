package mailsync

import (
	"context"
	"errors"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// Gegenprüfung S2.6–S2.8, Runde 1.

// failingCheck ist ein Pusher, dessen Prüfung nach dem Spiegeln scheitert.
type failingCheck struct{ DraftPusher }

func (failingCheck) AfterSync(context.Context, int64) (drafts.CheckReport, error) {
	return drafts.CheckReport{}, errors.New("database is locked")
}

// K5: Scheitert die Prüfung der Entwürfe nach dem Spiegeln, zählt das als
// Fehler des Laufs; Nacharbeit, Jobs und Zählung laufen trotzdem, und der
// Lauf endet ohne Fehler (der nächste wiederholt die Prüfung).
func TestDraftCheckErrorDoesNotStopTheRun(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Sent")
	e.newDraft(t, "Termin")
	e.runner.Pusher = failingCheck{e.runner.Pusher}
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatalf("sync = %v", err)
	}
	if out.Errors != 1 || out.Drafts == nil || out.Drafts.Pushed != 1 || out.MessagesMirrored != 1 {
		t.Fatalf("out = errors %d, drafts %+v, mirrored %d", out.Errors, out.Drafts, out.MessagesMirrored)
	}
}
