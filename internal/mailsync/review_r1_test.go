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

func (failingCheck) AfterSync(context.Context, int64, drafts.CheckOptions) (drafts.CheckReport, error) {
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

// recordingCheck merkt sich die Optionen der Prüfung nach dem Spiegeln.
type recordingCheck struct {
	DraftPusher
	got []drafts.CheckOptions
}

func (r *recordingCheck) AfterSync(ctx context.Context, id int64, o drafts.CheckOptions) (drafts.CheckReport, error) {
	r.got = append(r.got, o)
	return r.DraftPusher.AfterSync(ctx, id, o)
}

// K3: Spiegelt ein Lauf den Entwürfe-Ordner nicht, entfällt die
// Versanderkennung (SkipSent); fehlt ein Ordner mit Rolle sent oder all,
// bleibt die Hochwassermarke stehen (KeepMark). Ein voller Lauf prüft
// vollständig.
func TestDraftCheckOptionsFollowMirroredMailboxes(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Sent")
	rec := &recordingCheck{DraftPusher: e.runner.Pusher}
	e.runner.Pusher = rec
	for _, x := range []struct {
		boxes []string
		want  drafts.CheckOptions
	}{
		{nil, drafts.CheckOptions{}},
		{[]string{"INBOX", "Sent"}, drafts.CheckOptions{SkipSent: true}},
		{[]string{"INBOX", "Drafts"}, drafts.CheckOptions{KeepMark: true}},
		{[]string{"INBOX"}, drafts.CheckOptions{SkipSent: true, KeepMark: true}},
	} {
		rec.got = nil
		if _, err := e.sync(t, ops.SyncOptions{Mailboxes: x.boxes}); err != nil {
			t.Fatal(err)
		}
		if len(rec.got) != 1 || rec.got[0] != x.want {
			t.Fatalf("mailboxes %v: options %+v, want %+v", x.boxes, rec.got, x.want)
		}
	}
}
