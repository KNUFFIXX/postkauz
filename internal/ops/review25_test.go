package ops

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/compose"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung S2.4/S2.5 (Befunde W1, W2, W3, K5): der fachliche Teil des
// Pushs ohne Server. Der Weg über IMAP steht in internal/mailsync
// (review25_test.go).

// newDraftFor legt einen Entwurf an (Agent, Konto demo).
func (e *pushEnv) newDraftFor(t *testing.T, subject string) int64 {
	t.Helper()
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": subject, "body_markdown": "Hallo " + subject}).(DraftsWriteOutput)
	return out.DraftID
}

// W1: Legt der Server ab, aber die Antwort geht verloren und der Lauf endet
// (Wiederaufbau gescheitert, Strg+C, Prozessende), übernimmt der nächste
// Lauf die Kopie aus dem Spiegel, statt eine zweite abzulegen. Das gilt
// auch, wenn der Entwurf vorher geändert wird (drafts.update hält die
// verwaiste Kopie nicht für eine Fassung des Mailclients).
func TestReviewW1NoDuplicateAcrossCalls(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	// Nur ein wartender Entwurf je Schritt: die beiden der Umgebung zuerst
	// ablegen.
	e.push(t)
	id := e.newDraftFor(t, "Verloren")
	d := e.draft(t, id)

	// Lauf 1: APPEND gelingt auf dem Server, die Bestätigung kommt nie an.
	e.app.lost = true
	if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("push = %v", err)
	}
	e.app.lost = false
	serverUID, raw := e.app.uid, e.app.raws[len(e.app.raws)-1]
	d = e.draft(t, id)
	if d.RemoteState != drafts.RemotePending || d.PushAttempt == nil || d.PushAttempt.ContentHash != e.app.ContentHash(raw) ||
		d.PushAttempt.MessageHash != compose.MessageHash(raw) || d.PushAttempt.MailboxID != e.mb {
		t.Fatalf("after the lost append = %+v", d)
	}
	if _, withCopy, err := e.pusher.Pending(ctx, e.acct, false); err != nil || withCopy != 1 {
		t.Fatalf("pending with attempt = %d, %v (the caller mirrors the folder first)", withCopy, err)
	}

	// Lauf 2: Der Sync hat den Ordner gespiegelt; dort liegt die Kopie.
	e.mirror(t, []store.NewMessage{{Link: store.LinkFlags{UID: serverUID, Seen: true, Draft: true}, MessageID: d.MessageIDHeader,
		ContentHash: e.app.ContentHash(raw), Size: int64(len(raw))}}, nil, serverUID+1)
	appends := len(e.app.raws)
	rep := e.push(t)
	if rep.Adopted != 1 || rep.Pushed != 0 || len(e.app.raws) != appends {
		t.Fatalf("second run = %+v, %d appends (want the copy adopted, no second append)", rep, len(e.app.raws)-appends)
	}
	d = e.draft(t, id)
	if d.RemoteState != drafts.RemotePushed || d.RemoteUID != int64(serverUID) || d.PushAttempt != nil ||
		d.PushedMessageHash != compose.MessageHash(raw) || d.PushedContentHash != e.app.ContentHash(raw) || d.RemoteUIDValidity != 77 {
		t.Fatalf("adopted = %+v", d)
	}
	_, all := e.auditJSON(t)
	if !strings.Contains(all, `"adopted":true`) {
		t.Fatalf("audit without adoption:\n%s", all)
	}

	// Dasselbe, aber der Entwurf wird vor dem nächsten Lauf geändert: Die
	// verwaiste Kopie ist keine Übernahme durch den Menschen.
	id2 := e.newDraftFor(t, "Zwei")
	e.app.lost = true
	if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("push = %v", err)
	}
	e.app.lost = false
	uid2, raw2 := e.app.uid, e.app.raws[len(e.app.raws)-1]
	d2 := e.draft(t, id2)
	e.mirror(t, []store.NewMessage{{Link: store.LinkFlags{UID: uid2, Seen: true, Draft: true}, MessageID: d2.MessageIDHeader,
		ContentHash: e.app.ContentHash(raw2), Size: int64(len(raw2))}}, nil, uid2+1)
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id2, "body_markdown": "Hallo neu"})
	d2 = e.draft(t, id2)
	if d2.RemoteState != drafts.RemotePending || d2.RemoteUID != int64(uid2) || d2.PushAttempt != nil {
		t.Fatalf("after update = %+v", d2)
	}
	if rep := e.push(t); rep.Pushed != 1 || rep.TakenOver != 0 {
		t.Fatalf("push after update = %+v", rep)
	}
	d2 = e.draft(t, id2)
	if d2.RemoteState != drafts.RemotePushed || len(d2.RemotePrevious) != 1 || d2.RemotePrevious[0].UID != int64(uid2) {
		t.Fatalf("after push = %+v", d2)
	}

	// Kam der Versuch nie an (keine Kopie im gespiegelten Ordner), legt der
	// nächste Lauf ab wie sonst.
	id3 := e.newDraftFor(t, "Nie angekommen")
	e.app.err = errors.Join(provider.ErrConnection, errors.New("test"))
	if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("push = %v", err)
	}
	e.app.err = nil
	if d := e.draft(t, id3); d.PushAttempt == nil {
		t.Fatal("no attempt recorded")
	}
	e.mirror(t, nil, nil, e.app.uid+1)
	if rep := e.push(t); rep.Pushed != 1 || rep.Adopted != 0 {
		t.Fatalf("push = %+v", rep)
	}
	if d := e.draft(t, id3); d.RemoteState != drafts.RemotePushed || d.PushAttempt != nil {
		t.Fatalf("draft = %+v", d)
	}
}

// W3: Lehnt der Server einen Entwurf ab (zu groß, Kontingent), bekommt nur
// er den Hinweis push_rejected; die übrigen werden abgelegt, der Push ist
// kein Fehler. Der Watcher (retry=false) versucht ihn nicht alle 10 s
// erneut, der nächste Sync-Lauf schon; eine Änderung macht ihn sofort
// wieder fällig. draft show verspricht keine Ablage in 10 s mehr.
func TestReviewW3RejectedDraftDoesNotBlock(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	bad := e.newDraftFor(t, "Zu groß")
	good := e.newDraftFor(t, "Passt")
	e.app.reject = map[string]bool{e.draft(t, bad).MessageIDHeader: true}
	rep, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true)
	if err != nil || rep.Rejected != 1 || rep.Pushed != 3 {
		t.Fatalf("push = %+v, %v", rep, err)
	}
	if d := e.draft(t, good); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("valid draft = %s", d.RemoteState)
	}
	d := e.draft(t, bad)
	if d.RemoteState != drafts.RemotePending || d.PushNote != drafts.PushNotePushRejected || d.PushAttempt != nil {
		t.Fatalf("rejected draft = %+v", d)
	}
	g := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": bad}).(DraftDetail)
	if g.PushNote != "push_rejected" || !strings.Contains(g.RemoteNote, "refused") || strings.Contains(g.RemoteNote, "10 s") {
		t.Fatalf("get = %+v", g)
	}
	// Der Watcher lässt ihn liegen, der nächste Sync-Lauf versucht es.
	if n, _, err := e.pusher.Pending(ctx, e.acct, false); err != nil || n != 0 {
		t.Fatalf("due for the watcher = %d, %v", n, err)
	}
	if rep, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, false); err != nil || rep.Rejected != 0 {
		t.Fatalf("watcher push = %+v, %v", rep, err)
	}
	if n, _, err := e.pusher.Pending(ctx, e.acct, true); err != nil || n != 1 {
		t.Fatalf("pending at the start of a sync = %d, %v", n, err)
	}
	// Eine Änderung löscht den Hinweis; jetzt nimmt der Server sie an.
	e.must(t, assistant, "drafts.update", map[string]any{"draft_id": bad, "body_markdown": "kürzer"})
	if n, _, _ := e.pusher.Pending(ctx, e.acct, false); n != 1 {
		t.Fatalf("due after the change = %d", n)
	}
	e.app.reject = nil
	if rep, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, false); err != nil || rep.Pushed != 1 {
		t.Fatalf("push after the change = %+v, %v", rep, err)
	}
	if d := e.draft(t, bad); d.RemoteState != drafts.RemotePushed || d.PushNote != "" {
		t.Fatalf("after push = %+v", d)
	}
	// Audit und Hinweise ohne Inhalt; kein Text des Servers.
	_, all := e.auditJSON(t)
	for _, w := range []string{"Zu groß", "b@example.org"} {
		if strings.Contains(all, w) {
			t.Fatalf("audit contains %q", w)
		}
	}
}

// W2: Ändert sich nach dem Push etwas, das in den Nachrichtenbau eingeht,
// aber nicht im Entwurf steht (hier: Identität entfernt, die Signatur
// fällt weg), wird bei der Vorlage eine neue Kopie eingereiht; nach der
// Freigabe liegt im Ordner genau die freigegebene Nachricht.
func TestReviewW2ReadyMatchesTheCopy(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	e.push(t)
	id := e.newDraftFor(t, "Signatur")
	same := e.newDraftFor(t, "Unverändert")
	e.push(t)
	d := e.draft(t, id)
	if d.RemoteState != drafts.RemotePushed || d.PushedMessageHash == "" {
		t.Fatalf("pushed = %+v", d)
	}
	oldUID := d.RemoteUID
	// Ohne Änderung bleibt die Kopie, wie sie ist (keine zweite).
	e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": same})
	if s := e.draft(t, same); s.RemoteState != drafts.RemotePushed {
		t.Fatalf("unchanged draft = %s", s.RemoteState)
	}
	if err := e.st.RemoveIdentity(ctx, d.IdentityID); err != nil {
		t.Fatal(err)
	}
	req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id}).(DraftsRequestOutput)
	if d := e.draft(t, id); d.Status != "pending_approval" || d.RemoteState != drafts.RemotePending {
		t.Fatalf("after request = %s/%s (want a new copy queued)", d.Status, d.RemoteState)
	}
	e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID})
	if d := e.draft(t, id); d.Status != "ready" || d.RemoteState != drafts.RemotePending {
		t.Fatalf("after approve = %s/%s", d.Status, d.RemoteState)
	}
	n := len(e.app.raws)
	if rep := e.push(t); rep.Pushed != 1 || rep.Stale != 0 {
		t.Fatalf("push = %+v", rep)
	}
	raw := e.app.raws[n]
	a, err := e.st.Approval(ctx, req.ApprovalID)
	if err != nil {
		t.Fatal(err)
	}
	d = e.draft(t, id)
	if compose.MessageHash(raw) != a.MessageHash || d.PushedMessageHash != a.MessageHash || d.Status != "ready" ||
		d.RemoteState != drafts.RemotePushed || len(d.RemotePrevious) != 1 || d.RemotePrevious[0].UID != oldUID {
		t.Fatalf("copy after approve: hash %s, approval %s, draft %+v", compose.MessageHash(raw), a.MessageHash, d)
	}

	// Auch zwischen Vorlage und Freigabe: Der Push legt die vorgelegte
	// Fassung ab; ändert sich danach der Nachrichtenbau, scheitert approve
	// (approval_stale) wie bisher.
	id3 := e.newDraftFor(t, "Freigabe")
	e.push(t)
	req3 := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": id3}).(DraftsRequestOutput)
	if d := e.draft(t, id3); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("request without change = %s", d.RemoteState)
	}
	e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req3.ApprovalID})
	if d := e.draft(t, id3); d.RemoteState != drafts.RemotePushed {
		t.Fatalf("approve without change = %s", d.RemoteState)
	}
}

// K5: Wird ein Entwurf zwischen der Prüfung vor dem Push und dem APPEND
// abgeschlossen, bleibt die Kopie (kein Löschen) und wird vermerkt, aber
// nicht als aktuelle Kopie: push_note pushed_after_close, draft show sagt
// es, das Audit nennt closed.
func TestReviewK5PushedAfterClose(t *testing.T) {
	e := newPushEnv(t)
	e.push(t)
	id := e.newDraftFor(t, "Verworfen")
	e.app.before = func() {
		e.app.before = nil
		e.must(t, humanCLI, "drafts.abandon", map[string]any{"draft_id": id})
	}
	e.push(t)
	d := e.draft(t, id)
	if d.Status != "abandoned" || d.RemoteState == drafts.RemotePushed || d.RemoteUID == 0 ||
		d.PushNote != drafts.PushNotePushedAfterClose || d.PushAttempt != nil {
		t.Fatalf("closed draft = %+v", d)
	}
	g := e.must(t, humanCLI, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if !strings.Contains(g.RemoteNote, "after the draft was closed") || !strings.Contains(g.RemoteNote, "nothing is deleted") {
		t.Fatalf("remote note = %q", g.RemoteNote)
	}
	_, all := e.auditJSON(t)
	if !strings.Contains(all, `"closed":true`) || !strings.Contains(all, `"current":false`) {
		t.Fatalf("audit:\n%s", all)
	}
	n := len(e.app.raws)
	if rep := e.push(t); rep.Pushed != 0 || len(e.app.raws) != n {
		t.Fatalf("closed draft pushed again: %+v", rep)
	}
}
