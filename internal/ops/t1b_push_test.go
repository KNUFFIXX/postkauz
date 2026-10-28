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
		// Ein offener Versuch ist kein Absturz: doctor zählt ihn nicht.
		if n, err := e.st.AuditOpenStarts(ctx, time.Now().Add(time.Hour)); err != nil || n != 0 {
			t.Fatalf("open starts with an open attempt = %d %v", n, err)
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

// A8: Wird der Schlüssel zwischen Freigabe und Push widerrufen, legt der
// Push nichts ab: Freigabe abgelehnt (key_revoked), Entwurf verworfen,
// Systemereignis im Audit. Ein Entwurf des Menschen ist nicht betroffen.
// Der eingereihte Job trägt Schlüssel und Start-Eintrag des Aufrufs.
func TestA8RevokedKeyStopsPush(t *testing.T) {
	ctx := context.Background()
	e := newPushEnv(t)
	e.push(t)
	// Der Job der Umgebung ist erledigt (das macht sonst der Sync).
	if err := e.st.ExecForTest(`UPDATE jobs SET state = 'done'`); err != nil {
		t.Fatal(err)
	}
	id := e.newDraftFor(t, "Widerruf")
	jobs, err := e.st.OpenJobs(ctx)
	if err != nil || len(jobs) == 0 {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	var push store.JobInfo
	for _, j := range jobs {
		if j.Type == store.JobDraftPush {
			push = j
		}
	}
	if push.Origin.KeyID != assistant.ID || push.Origin.AuditID == 0 {
		t.Fatalf("job origin = %+v", push.Origin)
	}
	req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput)
	e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID})
	own := e.must(t, humanCLI, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Vom Menschen", "body_markdown": "Hallo"}).(DraftsWriteOutput).DraftID
	k, err := e.st.APIKeyByName(ctx, "assistent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	n := len(e.app.raws)
	e.settle(t)
	rep := e.push(t)
	if rep.KeyRevoked != 1 || rep.Pushed != 1 || len(e.app.raws) != n+1 || e.app.mids[len(e.app.mids)-1] != e.draft(t, own).MessageIDHeader {
		t.Fatalf("push after revoke = %+v, %d appends", rep, len(e.app.raws)-n)
	}
	d := e.draft(t, id)
	if d.Status != string(drafts.StatusAbandoned) || d.RemoteState != drafts.RemotePending {
		t.Fatalf("draft after revoke = %s/%s", d.Status, d.RemoteState)
	}
	a, err := e.st.Approval(ctx, req.ApprovalID)
	if err != nil || a.Decision != drafts.DecisionRejected || a.Reason != "key_revoked" || a.DecidedBy != "system" {
		t.Fatalf("approval = %+v %v", a, err)
	}
	evs, err := e.st.AuditQuery(ctx, store.AuditFilter{Operation: drafts.OpPush})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range evs {
		if ev.Reason == "key_revoked" && ev.ActorType == audit.ActorSystem {
			found = true
		}
	}
	if !found {
		t.Fatal("no system event key_revoked")
	}
	// Ein späterer Push versucht den Entwurf nicht noch einmal.
	if rep := e.push(t); rep.KeyRevoked != 0 || rep.Pushed != 0 {
		t.Fatalf("second push = %+v", rep)
	}
}

// A-F3: Der Mensch legt einen Entwurf an, ein Agent ändert ihn per
// drafts.update, dann wird der Schlüssel des Agenten widerrufen. Der Push
// legt nichts ab (KeyRevoked 1, kein APPEND): Der Entwurf bleibt beim
// Menschen (draft, zurückgehalten mit push_hold), Systemereignis im Audit.
// Erst eine Änderung durch den Menschen gibt ihn wieder frei. Ein Entwurf,
// den der Mensch selbst vorlegt, zählt den Inhalt als seinen.
func TestAF3RevokedLastWriterStopsPush(t *testing.T) {
	ctx := context.Background()
	for _, humanRequests := range []bool{false, true} {
		e := newPushEnv(t)
		e.push(t)
		own := e.must(t, humanCLI, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
			"subject": "Vom Menschen", "body_markdown": "Hallo"}).(DraftsWriteOutput).DraftID
		e.settle(t)
		e.push(t)
		e.must(t, assistant, "drafts.update", map[string]any{"draft_id": own, "body_markdown": "Hallo vom Agenten"})
		if d := e.draft(t, own); d.UpdatedBy != assistant.ID || d.CreatedBy != drafts.HumanKeyID {
			t.Fatalf("writers = %q/%q", d.CreatedBy, d.UpdatedBy)
		}
		k, err := e.st.APIKeyByName(ctx, "assistent")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.st.RevokeAPIKey(ctx, k.ID, time.Now()); err != nil {
			t.Fatal(err)
		}
		if humanRequests {
			// Der Mensch prüft und legt selbst vor: dann steht er für den
			// Inhalt, die Freigabe bindet ihn.
			req := e.must(t, humanCLI, "drafts.request_ready", map[string]any{"draft_id": own}).(DraftsRequestOutput)
			e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID})
			n := len(e.app.raws)
			e.settle(t)
			if rep := e.push(t); rep.KeyRevoked != 0 || rep.Pushed != 1 || len(e.app.raws) != n+1 {
				t.Fatalf("push after human request = %+v", rep)
			}
			continue
		}
		n := len(e.app.raws)
		e.settle(t)
		rep := e.push(t)
		if rep.KeyRevoked != 1 || rep.Pushed != 0 || len(e.app.raws) != n {
			t.Fatalf("push after revoke = %+v, %d appends", rep, len(e.app.raws)-n)
		}
		d := e.draft(t, own)
		if d.Status != string(drafts.StatusDraft) || d.PushHold != "key_revoked" || d.RemoteState != drafts.RemotePending {
			t.Fatalf("draft after revoke = %s/%s/%q", d.Status, d.RemoteState, d.PushHold)
		}
		evs, err := e.st.AuditQuery(ctx, store.AuditFilter{Operation: drafts.OpPush})
		if err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, ev := range evs {
			if ev.Reason == "key_revoked" && ev.ActorType == audit.ActorSystem {
				found++
			}
		}
		if found != 1 {
			t.Fatalf("system events key_revoked = %d", found)
		}
		show := e.must(t, humanCLI, "drafts.get", map[string]any{"draft_id": own}).(DraftDetail)
		if show.PushHold != "key_revoked" || show.RemoteNote != pushHoldNote("key_revoked") {
			t.Fatalf("draft show = %q / %q", show.PushHold, show.RemoteNote)
		}
		// Weitere Läufe versuchen ihn nicht und schreiben nichts mehr.
		if rep := e.push(t); rep.KeyRevoked != 0 || rep.Pushed != 0 || len(e.app.raws) != n {
			t.Fatalf("second push = %+v", rep)
		}
		// Der Mensch ändert: frei, abgelegt.
		e.must(t, humanCLI, "drafts.update", map[string]any{"draft_id": own, "body_markdown": "Hallo, geprüft"})
		e.settle(t)
		if rep := e.push(t); rep.Pushed != 1 || len(e.app.raws) != n+1 {
			t.Fatalf("push after human edit = %+v", rep)
		}
		if d := e.draft(t, own); d.PushHold != "" || d.UpdatedBy != drafts.HumanKeyID {
			t.Fatalf("draft after human edit = %+v", d)
		}
	}
}
