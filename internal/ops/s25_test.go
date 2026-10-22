package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.5: der fachliche Teil des Jobs draft_push (DraftPusher) ohne Server:
// Ein Fake legt ab, der Spiegel des Entwürfe-Ordners wird von Hand gefüllt
// (wie ihn der nächste Sync sähe). Die Wege über IMAP prüfen
// internal/mailsync (Server im Prozess) und die Integrationstests.

type fakeAppender struct {
	uid   uint32
	uv    uint32
	err   error
	raws  [][]byte
	mids  []string
	dates []time.Time
	// reject: Message-IDs, deren Ablage der Server ablehnt (W3).
	reject map[string]bool
	// lost: Der Server legt ab, die Antwort geht verloren (Verbindung weg;
	// W1): Die Kopie ist da, AppendDraft meldet einen Verbindungsfehler.
	lost bool
	// before läuft vor dem Ablegen (Wettlauf mit einem anderen Prozess, K5).
	before func()
}

func (f *fakeAppender) ContentHash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f *fakeAppender) AppendDraft(_ context.Context, raw []byte, mid string, date time.Time) (AppendedCopy, error) {
	if f.before != nil {
		f.before()
	}
	if f.err != nil {
		return AppendedCopy{}, f.err
	}
	if f.reject[mid] {
		return AppendedCopy{}, ErrAppendRejected
	}
	f.uid++
	f.raws, f.mids, f.dates = append(f.raws, raw), append(f.mids, mid), append(f.dates, date)
	if f.lost {
		return AppendedCopy{}, fmt.Errorf("%w: connection lost after APPEND", provider.ErrConnection)
	}
	return AppendedCopy{UID: f.uid, UIDValidity: f.uv, ContentHash: f.ContentHash(raw)}, nil
}

// pushEnv: Entwurfsumgebung (newDraftEnv) mit Ordner Drafts im Spiegel.
type pushEnv struct {
	*draftEnv
	acc    store.Account
	mb     int64
	pusher *DraftPusher
	app    *fakeAppender
	now    time.Time
}

func newPushEnv(t *testing.T) *pushEnv {
	t.Helper()
	e := &pushEnv{draftEnv: newDraftEnv(t, false), app: &fakeAppender{uv: 77, uid: 100},
		now: time.Date(2026, 10, 22, 14, 3, 5, 0, time.FixedZone("CEST", 2*3600))}
	ctx := context.Background()
	if _, _, err := e.st.SyncMailboxes(ctx, e.acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Drafts", Name: "Drafts", Role: "drafts"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := e.st.Mailboxes(ctx, e.acct)
	for _, b := range boxes {
		if b.Role == "drafts" {
			e.mb = b.ID
		}
	}
	e.acc, _ = e.st.AccountByName(ctx, "demo")
	e.pusher = NewDraftPusher(e.deps)
	e.pusher.Now = func() time.Time { return e.now }
	return e
}

func (e *pushEnv) push(t *testing.T) DraftPushReport {
	t.Helper()
	rep, err := e.pusher.Push(context.Background(), e.acc, e.mb, e.app, true)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	return rep
}

func (e *pushEnv) draft(t *testing.T, id int64) store.Draft {
	t.Helper()
	d, err := e.st.Draft(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// mirror stellt den Spiegel des Entwürfe-Ordners so ein, wie ihn ein Sync
// danach sähe: neue Kopien (UID, Message-ID, Inhalts-Hash), entfernte UIDs,
// UIDNEXT.
func (e *pushEnv) mirror(t *testing.T, add []store.NewMessage, removed []uint32, uidNext uint32) {
	t.Helper()
	if _, err := e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: e.acct, MailboxID: e.mb, New: add, Removed: removed,
		State: &store.MailboxState{UIDValidity: 77, UIDNext: uidNext, Phase: store.PhaseReady, Total: len(add)}}); err != nil {
		t.Fatal(err)
	}
}

func mirrored(d store.Draft) store.NewMessage {
	return store.NewMessage{Link: store.LinkFlags{UID: uint32(d.RemoteUID), Seen: true, Draft: true}, MessageID: d.MessageIDHeader,
		ContentHash: d.PushedContentHash, Size: 100}
}

// Akzeptanz S2.5 (ohne Server): Ablage mit frischem Date und der beim
// Anlegen vergebenen Message-ID, Vermerk der Kopie, erneuter Push nach
// Änderung (alte Kopie bleibt, beide UIDs in drafts.get), Übernahme →
// draft_taken_over (taken_over bzw. missing), Audit ohne Inhalt.
func TestDraftPushAndTakeOver(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	const canaryBody = "KanarienText-S25"
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Termin", "body_markdown": canaryBody}).(DraftsWriteOutput)
	id := out.DraftID
	if out.Remote != "pending" || !strings.Contains(out.RemoteNote, "10 s") {
		t.Fatalf("create = %+v", out)
	}
	if n, withCopy, err := e.pusher.Pending(ctx, e.acct, false); err != nil || n != 3 || withCopy != 0 {
		t.Fatalf("pending = %d (%d with copy), %v", n, withCopy, err)
	}
	rep := e.push(t)
	// Entwurf 1 und 2 (pending_approval) aus der Umgebung werden abgelegt,
	// wie sie sind, dazu der neue.
	if rep.Pushed != 3 || rep.Failed+rep.Stale+rep.TakenOver+rep.Missing != 0 || len(e.app.raws) != 3 {
		t.Fatalf("report = %+v", rep)
	}
	raw := e.app.raws[2]
	if e.app.mids[2] != out.MessageID || !bytes.Contains(raw, []byte("Message-Id: <"+out.MessageID+">")) ||
		!bytes.Contains(raw, []byte("Date: Thu, 22 Oct 2026 14:03:05 +0200")) || !e.app.dates[2].Equal(e.now) {
		t.Fatalf("pushed copy: mid %s, date %v\n%s", e.app.mids[2], e.app.dates[2], raw)
	}
	d := e.draft(t, id)
	if d.RemoteState != drafts.RemotePushed || d.RemoteUID != 103 || d.RemoteUIDValidity != 77 || d.RemoteMailboxID != e.mb ||
		d.PushedDate != "Thu, 22 Oct 2026 14:03:05 +0200" || d.PushedContentHash == "" || d.PushNote != "" {
		t.Fatalf("draft after push = %+v", d)
	}
	// Nichts mehr zu tun.
	if rep := e.push(t); rep.Pushed != 0 {
		t.Fatalf("second push = %+v", rep)
	}
	// Mit dem gespeicherten Date entsteht dieselbe Kopie noch einmal (S2.7).
	date, err := time.Parse(time.RFC1123Z, d.PushedDate)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.pusher.o.rebuildAt(ctx, e.acc, d, date)
	if err != nil || !bytes.Equal(again.Raw, raw) {
		t.Fatalf("rebuild with the stored date differs: %v", err)
	}

	// Spiegel noch ohne die Kopie: drafts.update ist erlaubt (nichts belegt
	// eine Übernahme), die neue Fassung wartet.
	upd := e.must(t, assistant, "drafts.update", map[string]any{"draft_id": id, "body_markdown": canaryBody + " 2"}).(DraftsWriteOutput)
	if upd.Remote != "pending" || !strings.Contains(upd.RemoteNote, "uid 103") {
		t.Fatalf("update = %+v", upd)
	}
	d = e.draft(t, id)
	// Der nächste Sync spiegelt die erste Kopie; sie ist unverändert da.
	e.mirror(t, []store.NewMessage{mirrored(d)}, nil, 104)
	if rep := e.push(t); rep.Pushed != 1 {
		t.Fatalf("push after change = %+v", rep)
	}
	d = e.draft(t, id)
	if d.RemoteState != drafts.RemotePushed || d.RemoteUID != 104 || len(d.RemotePrevious) != 1 || d.RemotePrevious[0].UID != 103 {
		t.Fatalf("draft after second push = %+v", d)
	}
	got := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail)
	if got.RemoteUID != 104 || len(got.RemotePreviousUIDs) != 1 || got.RemotePreviousUIDs[0] != 103 || got.PushedDate == "" ||
		!strings.Contains(got.RemoteNote, "uid 104") || !strings.Contains(got.RemoteNote, "uid 103") {
		t.Fatalf("get = %+v", got)
	}

	// Übernahme: Der Mailclient hat die Kopie 104 durch eine bearbeitete
	// Fassung (gleiche Message-ID, UID 105) ersetzt.
	// Der Spiegel sieht 104 nicht mehr, dafür 105.
	e.mirror(t, []store.NewMessage{{Link: store.LinkFlags{UID: 105, Draft: true}, MessageID: d.MessageIDHeader,
		ContentHash: "anders", Size: 120}}, nil, 106)
	_, err = e.do(t, assistant, "drafts.update", map[string]any{"draft_id": id, "subject": "Neu"}, false)
	wantDenied(t, err, drafts.ReasonDraftTakenOver)
	if d := e.draft(t, id); d.RemoteState != drafts.RemoteTakenOver || d.Subject != "Termin" {
		t.Fatalf("after take-over = %s, subject %q", d.RemoteState, d.Subject)
	}
	// Auch ein Mensch ändert nicht mehr; Lesen geht weiter.
	_, err = e.do(t, humanCLI, "drafts.update", map[string]any{"draft_id": id, "subject": "Neu"}, false)
	wantDenied(t, err, drafts.ReasonDraftTakenOver)
	if g := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": id}).(DraftDetail); g.Remote != "taken_over" || g.Subject != "Termin" {
		t.Fatalf("get after take-over = %+v", g)
	}

	// Fehlt die Kopie ganz (gesendet oder gelöscht): missing.
	out2 := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Zwei", "body_markdown": "Text"}).(DraftsWriteOutput)
	if rep := e.push(t); rep.Pushed != 1 {
		t.Fatalf("push 2 = %+v", rep)
	}
	d2 := e.draft(t, out2.DraftID)
	e.mirror(t, []store.NewMessage{mirrored(d2)}, nil, uint32(d2.RemoteUID)+1)
	e.mirror(t, nil, []uint32{uint32(d2.RemoteUID)}, uint32(d2.RemoteUID)+1)
	_, err = e.do(t, assistant, "drafts.update", map[string]any{"draft_id": out2.DraftID, "subject": "Neu"}, false)
	wantDenied(t, err, drafts.ReasonDraftTakenOver)
	if !strings.Contains(err.Error(), "gone") {
		t.Fatalf("missing: %v", err)
	}
	if d := e.draft(t, out2.DraftID); d.RemoteState != drafts.RemoteMissing {
		t.Fatalf("remote = %s", d.RemoteState)
	}

	// Audit: Ablage und Befund als Systemereignisse, ohne Inhalt.
	evs, all := e.auditJSON(t)
	var pushes, checks int
	for _, ev := range evs {
		switch {
		case ev.Operation == drafts.OpPush && ev.Reason == drafts.ReasonAppended && ev.ActorType == "system":
			pushes++
		case ev.Operation == drafts.OpRemoteCheck && (ev.Reason == "taken_over" || ev.Reason == "missing"):
			checks++
		}
	}
	if pushes != 5 || checks != 2 {
		t.Fatalf("audit: %d pushes, %d checks\n%s", pushes, checks, all)
	}
	for _, w := range []string{canaryBody, "Termin", "b@example.org"} {
		if strings.Contains(all, w) {
			t.Fatalf("audit contains %q", w)
		}
	}
}

// Festlegung S2.5: Ein Entwurf in ready wird nur abgelegt, wenn die jetzt
// gebaute Nachricht (ohne Date) die freigegebene ist; sonst kein Push,
// zurück auf draft, Freigabe superseded, Hinweis in drafts.get.
func TestDraftPushReadyNeedsApprovedMessage(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	ready := func(subject string) (int64, int64) {
		t.Helper()
		out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
			"subject": subject, "body_markdown": "Text"}).(DraftsWriteOutput)
		req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": out.DraftID}).(DraftsRequestOutput)
		e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID})
		return out.DraftID, req.ApprovalID
	}
	okID, _ := ready("Passt")
	staleID, staleApproval := ready("Passt nicht")
	// Die Identität ändert sich nach der Freigabe (andere Signatur): Die
	// gebaute Nachricht ist nicht mehr die freigegebene. Entfernen genügt
	// (der Entwurf behält den Absender, verliert aber die Signatur).
	d := e.draft(t, staleID)
	if err := e.st.RemoveIdentity(ctx, d.IdentityID); err != nil {
		t.Fatal(err)
	}
	// Entwurf okID nutzt dieselbe Identität: Er wird vorher angepasst, damit
	// nur staleID abweicht. Stattdessen: okID neu vorlegen und freigeben.
	e.must(t, humanCLI, "drafts.update", map[string]any{"draft_id": okID, "subject": "Passt jetzt"})
	req := e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": okID}).(DraftsRequestOutput)
	e.must(t, humanCLI, "drafts.approve", map[string]any{"approval_id": req.ApprovalID})

	rep := e.push(t)
	if rep.Stale != 1 {
		t.Fatalf("report = %+v", rep)
	}
	for _, mid := range e.app.mids {
		if mid == d.MessageIDHeader {
			t.Fatal("the stale ready draft was pushed")
		}
	}
	if ok := e.draft(t, okID); ok.Status != "ready" || ok.RemoteState != drafts.RemotePushed {
		t.Fatalf("approved draft = %s/%s", ok.Status, ok.RemoteState)
	}
	st := e.draft(t, staleID)
	if st.Status != "draft" || st.RemoteState != drafts.RemotePending || st.PushNote != drafts.PushNoteApprovalStale {
		t.Fatalf("stale draft = %+v", st)
	}
	if a, _ := e.st.Approval(ctx, staleApproval); a.Decision != drafts.DecisionSuperseded {
		t.Fatalf("approval = %s", a.Decision)
	}
	g := e.must(t, humanCLI, "drafts.get", map[string]any{"draft_id": staleID}).(DraftDetail)
	if g.PushNote != "approval_stale" || !strings.Contains(g.PushNoteText, "draft request") {
		t.Fatalf("get = %+v", g)
	}
	_, all := e.auditJSON(t)
	if !strings.Contains(all, "system system drafts.push") || !strings.Contains(all, "approval_stale") {
		t.Fatalf("audit:\n%s", all)
	}
	// Als gewöhnlicher Entwurf legt der nächste Push ihn ab (er ist ja ein
	// Entwurf); der Hinweis bleibt, bis er neu vorgelegt wird.
	if rep := e.push(t); rep.Pushed != 1 {
		t.Fatalf("next push = %+v", rep)
	}
	if st := e.draft(t, staleID); st.RemoteState != drafts.RemotePushed || st.PushNote != drafts.PushNoteApprovalStale {
		t.Fatalf("after next push = %+v", st)
	}
	e.must(t, assistant, "drafts.request_ready", map[string]any{"draft_id": staleID})
	if st := e.draft(t, staleID); st.PushNote != "" {
		t.Fatalf("note after request = %q", st.PushNote)
	}
}

// Ein Verbindungsfehler beim Ablegen hält den Push an; die Entwürfe bleiben
// pending. Ein nicht baubarer Entwurf bekommt push_failed, die übrigen gehen
// weiter, und der Push ist kein Fehler (W3). Abgeschlossene Entwürfe werden
// nie abgelegt.
func TestDraftPushErrors(t *testing.T) {
	e := newPushEnv(t)
	ctx := context.Background()
	e.app.err = fmt.Errorf("%w: test", provider.ErrConnection)
	if _, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true); !errors.Is(err, provider.ErrConnection) {
		t.Fatalf("push = %v", err)
	}
	for _, id := range []int64{1, 2} {
		if d := e.draft(t, id); d.RemoteState != drafts.RemotePending {
			t.Fatalf("draft %d = %s", id, d.RemoteState)
		}
	}
	e.app.err = nil
	// Entwurf 1 verwerfen: wird nicht mehr abgelegt.
	e.must(t, humanCLI, "drafts.abandon", map[string]any{"draft_id": 1})
	// Ein Anhang, dessen Blob fehlt, macht Entwurf 3 unbaubar.
	out := e.must(t, assistant, "drafts.create", map[string]any{"kind": "new", "account": "demo", "to": []string{"b@example.org"},
		"subject": "Anhang", "body_markdown": "Text", "attachments": []map[string]any{{"attachment_id": 1}}}).(DraftsWriteOutput)
	d := e.draft(t, out.DraftID)
	delete(e.blobs.m, d.Attachments[0].SHA256)
	rep, err := e.pusher.Push(ctx, e.acc, e.mb, e.app, true)
	if err != nil || rep.Pushed != 1 || rep.Failed != 1 {
		t.Fatalf("push = %+v, %v", rep, err)
	}
	if d := e.draft(t, 1); d.RemoteState != drafts.RemotePending || d.RemoteUID != 0 {
		t.Fatalf("abandoned draft = %+v", d)
	}
	if d := e.draft(t, out.DraftID); d.PushNote != drafts.PushNotePushFailed || d.RemoteState != drafts.RemotePending {
		t.Fatalf("unbuildable draft = %+v", d)
	}
	g := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail)
	if !strings.Contains(g.RemoteNote, "could not be built") {
		t.Fatalf("remote note = %q", g.RemoteNote)
	}
	// Kein Entwürfe-Ordner: Hinweis an allen wartenden Entwürfen.
	if err := e.pusher.NoDraftsMailbox(ctx, e.acct); err != nil {
		t.Fatal(err)
	}
	if d := e.draft(t, out.DraftID); d.PushNote != drafts.PushNoteNoDraftsMailbox {
		t.Fatalf("note = %q", d.PushNote)
	}
	if g := e.must(t, assistant, "drafts.get", map[string]any{"draft_id": out.DraftID}).(DraftDetail); !strings.Contains(g.RemoteNote, "no Drafts folder") {
		t.Fatalf("remote note = %q", g.RemoteNote)
	}
}
