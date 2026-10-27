package ops

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// pushCalls liefert die Aufrufe drafts.push eines Entwurfs (Start mit
// Endergebnis, A1-Ansicht).
func (e *pushEnv) pushCalls(t *testing.T) []store.AuditRecord {
	t.Helper()
	recs, err := e.st.AuditCalls(context.Background(), store.AuditFilter{Operation: drafts.OpPush})
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

// A7: Vor dem APPEND steht im Audit ein Eintrag drafts.push mit Ergebnis
// started. Bricht der Lauf zwischen APPEND und Vermerk ab (Fake-Provider:
// abgelegt, Antwort verloren) und verwirft der Mensch den Entwurf danach,
// übernimmt der nächste Sync die Kopie (pushed_after_close) bzw. schließt
// den Versuch ohne Kopie ab. Danach hat jeder Start einen Abschluss.
func TestA7AuditBeforeAppendAndOrphanAfterClose(t *testing.T) {
	ctx := context.Background()
	for _, landed := range []bool{true, false} {
		e := newPushEnv(t)
		e.push(t)
		id := e.newDraftFor(t, "Abbruch")
		e.app.lost = true
		if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
			t.Fatalf("push = %v", err)
		}
		e.app.lost = false
		uid, raw := e.app.uid, e.app.raws[len(e.app.raws)-1]
		d := e.draft(t, id)
		if d.PushAttempt == nil || d.PushAttempt.AuditID == 0 {
			t.Fatalf("attempt without audit id: %+v", d.PushAttempt)
		}
		calls := e.pushCalls(t)
		last := calls[len(calls)-1]
		if last.ID != d.PushAttempt.AuditID || last.Result != audit.ResultStarted || last.EndID != 0 {
			t.Fatalf("start entry before APPEND = %+v", last)
		}
		e.must(t, humanCLI, "drafts.abandon", map[string]any{"draft_id": id})
		if d := e.draft(t, id); d.PushAttempt == nil || d.Status != string(drafts.StatusAbandoned) {
			t.Fatalf("after abandon (folder not mirrored yet) = %+v", d)
		}
		var add []store.NewMessage
		if landed {
			add = []store.NewMessage{{Link: store.LinkFlags{UID: uid, Seen: true, Draft: true}, MessageID: d.MessageIDHeader,
				ContentHash: e.app.ContentHash(raw), Size: int64(len(raw))}}
		}
		e.mirror(t, add, nil, uid+1)
		rep, err := e.pusher.AfterSync(ctx, e.acct, drafts.CheckOptions{})
		if err != nil {
			t.Fatal(err)
		}
		d = e.draft(t, id)
		if d.PushAttempt != nil {
			t.Fatalf("attempt still open after the sync: %+v", d)
		}
		calls = e.pushCalls(t)
		var start store.AuditRecord
		for _, c := range calls {
			if c.ID == last.ID {
				start = c
			}
		}
		if landed {
			if rep.AdoptedClosed != 1 || d.PushNote != drafts.PushNotePushedAfterClose || d.RemoteUID != int64(uid) ||
				start.Result != audit.ResultOK || start.EndID == 0 {
				t.Fatalf("landed: report %+v, draft %+v, audit %+v", rep, d, start)
			}
		} else if rep.LostAttempts != 1 || start.Result != audit.ResultError || start.Reason != drafts.ReasonAttemptWithoutCopy {
			t.Fatalf("lost: report %+v, audit %+v", rep, start)
		}
		if n, err := e.st.AuditOpenStarts(ctx, time.Now().Add(time.Hour)); err != nil || n != 0 {
			t.Fatalf("open starts = %d %v", n, err)
		}
	}
}

// A7: Der gewöhnliche Push schließt seinen Start-Eintrag ab (appended),
// eine Ablehnung des Servers ebenso (push_rejected), und ein neuer Versuch
// schließt einen alten ohne Kopie ab.
func TestA7PushStartAndEnd(t *testing.T) {
	ctx := context.Background()
	e := newPushEnv(t)
	e.push(t)
	for _, c := range e.pushCalls(t) {
		if c.Result != audit.ResultOK || c.EndID == 0 || c.Reason != drafts.ReasonAppended {
			t.Fatalf("push call = %+v", c)
		}
	}
	id := e.newDraftFor(t, "Abgelehnt")
	e.app.reject = map[string]bool{e.draft(t, id).MessageIDHeader: true}
	e.push(t)
	e.app.reject = nil
	calls := e.pushCalls(t)
	if c := calls[len(calls)-1]; c.Result != audit.ResultError || c.Reason != drafts.PushNotePushRejected || c.EndID == 0 {
		t.Fatalf("rejected push = %+v", c)
	}
	// Abbruch ohne Kopie, dann ein neuer Versuch: der alte endet ohne Kopie.
	e.app.err = errors.Join(provider.ErrConnection, errors.New("test"))
	if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("push = %v", err)
	}
	e.app.err = nil
	e.mirror(t, nil, nil, e.app.uid+1)
	e.push(t)
	if d := e.draft(t, id); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("draft = %+v", d)
	}
	reasons := map[string]int{}
	for _, c := range e.pushCalls(t) {
		if c.EndID == 0 {
			t.Fatalf("start without end: %+v", c)
		}
		reasons[c.Reason]++
	}
	if reasons[drafts.ReasonAttemptWithoutCopy] != 1 {
		t.Fatalf("reasons = %v", reasons)
	}
}
