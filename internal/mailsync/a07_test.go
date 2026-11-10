package mailsync

import (
	"context"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// orderCheck merkt sich die Reihenfolge von MissingDrafts und Push und
// setzt während des Pushs einen Entwurf auf missing (wie PushCheck, wenn
// die Kopie im selben Lauf verschwunden ist).
type orderCheck struct {
	recordingCheck
	st      *store.SQLite
	calls   []string
	markIDs []int64
}

func (o *orderCheck) MissingDrafts(ctx context.Context, id int64) (map[int64]bool, error) {
	o.calls = append(o.calls, "missing")
	return o.DraftPusher.MissingDrafts(ctx, id)
}

func (o *orderCheck) Push(ctx context.Context, acc store.Account, mailboxID int64, app ops.DraftAppender, retry bool) (ops.DraftPushReport, error) {
	o.calls = append(o.calls, "push")
	rep, err := o.DraftPusher.Push(ctx, acc, mailboxID, app, retry)
	for _, id := range o.markIDs {
		if e := o.st.ExecForTest(`UPDATE drafts SET remote_state = 'missing' WHERE id = ?`, id); e != nil {
			return rep, e
		}
	}
	return rep, err
}

// A07 Teil 6 (c), Sammelpunkt 0e: Der Sync liest MissingBefore vor dem
// Push. Ein Entwurf, den erst der Push dieses Laufs auf missing setzt,
// steht nicht in MissingBefore (sonst ersetzte „ersetzt“ ihn im selben
// Lauf, in dem die Kopie verschwindet).
func TestA07MissingBeforeIsReadBeforePush(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Sent")
	before := e.newDraft(t, "Vorher")
	during := e.newDraft(t, "Waehrend")
	if err := e.st.ExecForTest(`UPDATE drafts SET remote_state = 'missing' WHERE id = ?`, before.ID); err != nil {
		t.Fatal(err)
	}
	o := &orderCheck{recordingCheck: recordingCheck{DraftPusher: e.runner.Pusher}, st: e.st, markIDs: []int64{during.ID}}
	e.runner.Pusher = o
	if _, err := e.sync(t, ops.SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(o.calls) < 2 || o.calls[0] != "missing" || o.calls[1] != "push" {
		t.Fatalf("calls = %v, want missing before push", o.calls)
	}
	if len(o.got) != 1 || !o.got[0].MissingBefore[before.ID] || o.got[0].MissingBefore[during.ID] {
		t.Fatalf("missing before = %+v", o.got)
	}
}
