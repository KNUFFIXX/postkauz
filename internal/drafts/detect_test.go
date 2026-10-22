package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.6: Prüfung nach dem Spiegeln mit Datenbank (Versanderkennung E5,
// Ablauf der Freigabe E7, Stand der Serverkopie E6).

// mirror ist ein gespiegeltes Postfach für die Tests: Ordner nach Rolle.
type mirror struct {
	e     *env
	boxes map[string]int64 // Rolle → Ordner-ID
	uid   map[int64]uint32
}

func newMirror(e *env) *mirror {
	e.t.Helper()
	ctx := context.Background()
	specs := []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}, {Path: "Sent", Name: "Sent", Role: "sent"},
		{Path: "Drafts", Name: "Drafts", Role: "drafts"}, {Path: "All", Name: "All", Role: "all"}}
	if _, _, err := e.st.SyncMailboxes(ctx, e.acc, specs); err != nil {
		e.t.Fatal(err)
	}
	mbs, err := e.st.Mailboxes(ctx, e.acc)
	if err != nil {
		e.t.Fatal(err)
	}
	m := &mirror{e: e, boxes: map[string]int64{}, uid: map[int64]uint32{}}
	for _, b := range mbs {
		m.boxes[b.Role] = b.ID
	}
	return m
}

// sentMsg beschreibt eine gespiegelte Nachricht.
type sentMsg struct {
	messageID, subject, from, text string
	to, cc                         []store.Addr
	date                           time.Time
	draftFlag                      bool
}

func addrs(l []store.Addr) json.RawMessage {
	if l == nil {
		return json.RawMessage("[]")
	}
	b, _ := json.Marshal(l)
	return b
}

// put legt die Nachricht im Ordner mit Rolle role ab und liefert ihre ID.
func (m *mirror) put(role string, s sentMsg) int64 {
	m.e.t.Helper()
	box := m.boxes[role]
	m.uid[box]++
	nm := store.NewMessage{Link: store.LinkFlags{UID: m.uid[box], Seen: true, Draft: s.draftFlag}, MessageID: s.messageID,
		Subject: s.subject, From: s.from, To: addrs(s.to), Cc: addrs(s.cc), DateHeader: s.date, InternalDate: s.date,
		Size: int64(100 + len(s.text) + int(m.uid[box])), TextPlain: s.text, TextUnquoted: s.text, ContentHash: s.messageID + s.text,
		CleanVersion: 1, Links: json.RawMessage("[]")}
	if _, err := m.e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: m.e.acc, MailboxID: box, New: []store.NewMessage{nm},
		State: &store.MailboxState{UIDValidity: 1, UIDNext: m.uid[box] + 1, Phase: store.PhaseReady}}); err != nil {
		m.e.t.Fatal(err)
	}
	n, err := m.e.st.QueryIntForTest(`SELECT max(id) FROM messages`)
	if err != nil {
		m.e.t.Fatal(err)
	}
	return int64(n)
}

// link ordnet eine vorhandene Nachricht zusätzlich dem Ordner mit Rolle role zu.
func (m *mirror) link(role string, id int64, draftFlag bool) {
	m.e.t.Helper()
	box := m.boxes[role]
	m.uid[box]++
	if _, err := m.e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: m.e.acc, MailboxID: box,
		Links: []store.Link{{MessageID: id, Flags: store.LinkFlags{UID: m.uid[box], Draft: draftFlag}}},
		State: &store.MailboxState{UIDValidity: 1, UIDNext: m.uid[box] + 1, Phase: store.PhaseReady}}); err != nil {
		m.e.t.Fatal(err)
	}
}

func (e *env) check() CheckReport {
	e.t.Helper()
	rep, err := e.svc.CheckAfterSync(context.Background(), e.acc)
	if err != nil {
		e.t.Fatal(err)
	}
	return rep
}

func (e *env) draft(id int64) store.Draft {
	e.t.Helper()
	d, err := e.st.Draft(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

// lastAudit liefert den letzten Audit-Eintrag mit Operation op.
func (e *env) lastAudit(op string) (store.AuditRecord, bool) {
	e.t.Helper()
	recs, err := e.st.AuditQuery(context.Background(), store.AuditFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	var out store.AuditRecord
	found := false
	for _, r := range recs {
		if r.Operation == op {
			out, found = r, true
		}
	}
	return out, found
}

// (A) Kopie mit derselben Message-ID in Gesendet → sent_by_human,
// sent_message_id, Audit system mit Grund message_id, ohne Inhalt; die
// erteilte Freigabe bleibt approved.
func TestDetectSentByMessageID(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	d := e.reach(StatusReady)
	e.now = e.now.Add(time.Hour)
	mid := m.put("sent", sentMsg{messageID: d.MessageIDHeader, subject: "anders", from: "demo@example.org", date: e.now, text: "x"})
	rep := e.check()
	if rep.SentByMessageID != 1 || rep.SentByFallback != 0 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != d.ID {
		t.Fatalf("report = %+v", rep)
	}
	got := e.draft(d.ID)
	if got.Status != string(StatusSentByHuman) || got.SentMessageID != mid || got.ClosedAt.IsZero() {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	if a, _ := e.st.Approval(context.Background(), got.ApprovalID); a.Decision != DecisionApproved {
		t.Fatalf("approval = %s", a.Decision)
	}
	r, ok := e.lastAudit(OpSentDetected)
	if !ok || r.ActorType != "system" || r.Reason != DetectMessageID {
		t.Fatalf("audit = %+v", r)
	}
	var p map[string]any
	_ = json.Unmarshal(r.ParamsRedacted, &p)
	if p["draft_id"] != float64(d.ID) || p["sent_message_id"] != float64(mid) || p["from"] != "ready" || p["to"] != "sent_by_human" {
		t.Fatalf("params = %v", p)
	}
	e.noContentInAudit()
	// Ein zweiter Lauf ändert nichts mehr.
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("second check = %+v", rep)
	}
}

// Befund K8 und E5: Kopien mit \Draft (Gmail „All Mail") und Nachrichten,
// die (auch) im Entwürfe-Ordner liegen, sind keine gesendeten Nachrichten;
// eine Nachricht nur im Posteingang ebenso wenig.
func TestDetectSentIgnoresDraftCopies(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	d := e.reach(StatusPendingApproval)
	msg := sentMsg{messageID: d.MessageIDHeader, subject: canarySubject, from: "demo@example.org", date: e.now, text: canaryBody,
		to: content("").To}
	// Gmail: die Entwurfskopie in Entwürfe und in „All Mail", beide mit \Draft.
	msg.draftFlag = true
	id := m.put("drafts", msg)
	m.link("all", id, true)
	// In „All Mail" ohne \Draft, aber zugleich im Entwürfe-Ordner.
	msg.draftFlag = false
	msg.text += " "
	id2 := m.put("drafts", msg)
	m.link("all", id2, false)
	// Eine Kopie nur in Gesendet, aber mit \Draft.
	msg.draftFlag, msg.text = true, msg.text+" "
	m.put("sent", msg)
	// Nur im Posteingang.
	msg.draftFlag, msg.text = false, msg.text+" "
	m.put("inbox", msg)
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("draft copies detected as sent: %+v", rep)
	}
	if got := e.draft(d.ID); got.Status != string(StatusPendingApproval) {
		t.Fatalf("status = %s", got.Status)
	}
	// Entfernt der Client die Kopie aus den Entwürfen (Senden), zählt die
	// Nachricht in „All Mail" ohne \Draft.
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE message_id = ? AND mailbox_id = ?`, id2, m.boxes["drafts"]); err != nil {
		t.Fatal(err)
	}
	if rep := e.check(); rep.SentByMessageID != 1 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != id2 {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
}

// fallbackDraft legt einen Entwurf mit längerem Text an (Rückfall-Tests).
func (e *env) fallbackDraft(subject string) store.Draft {
	e.t.Helper()
	r, err := e.svc.Create(context.Background(), NewDraft{AccountID: e.acc, Kind: KindReply, CreatedBy: "key:agent",
		Content: Content{To: []store.Addr{{Name: "Anna", Address: "anna@example.org"}}, Cc: []store.Addr{{Address: "bob@example.org"}},
			Subject: subject, BodyMarkdown: fbBody}})
	if err != nil {
		e.t.Fatal(err)
	}
	return r.Draft
}

// (B) neue Message-ID, gleicher Inhalt → sent_by_human (fallback); (C)
// stark geänderter Text → bleibt. Zeitfenster und Empfänger zählen.
func TestDetectSentFallback(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	if _, err := e.st.AddIdentity(context.Background(), store.Identity{AccountID: e.acc, Name: "Demo", Email: "demo@example.org",
		SignatureText: "Demo Muster\nBeispielweg 1\nTelefon 0000"}); err != nil {
		t.Fatal(err)
	}
	same := e.fallbackDraft("Re: Termin am Montag")
	changed := e.fallbackDraft("Re: Angebot")
	late := e.fallbackDraft("Re: Spät")
	sig := "\n\nDemo Muster\nBeispielweg 1\nTelefon 0000"
	to := []store.Addr{{Address: "BOB@example.org"}}
	cc := []store.Addr{{Name: "Anna M.", Address: "anna@example.org"}}
	at := e.now.Add(10 * time.Minute)
	sameID := m.put("sent", sentMsg{messageID: "neu-1@example.org", subject: "AW: Termin am Montag", from: "demo@example.org",
		to: to, cc: cc, date: at, text: fbBody + sig})
	m.put("sent", sentMsg{messageID: "neu-2@example.org", subject: "Re: Angebot", from: "demo@example.org",
		to: to, cc: cc, date: at, text: "Ganz anderer Text, das Angebot lehnen wir ab." + sig})
	m.put("sent", sentMsg{messageID: "neu-3@example.org", subject: "Re: Spät", from: "demo@example.org",
		to: to, cc: cc, date: e.now.Add(15 * 24 * time.Hour), text: fbBody + sig})
	rep := e.check()
	if rep.SentByFallback != 1 || rep.SentByMessageID != 0 || len(rep.SentDraftIDs) != 1 || rep.SentDraftIDs[0] != same.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(same.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sameID {
		t.Fatalf("same = %s, sent %d", got.Status, got.SentMessageID)
	}
	for _, d := range []store.Draft{changed, late} {
		if got := e.draft(d.ID); got.Status != string(StatusDraft) || got.SentMessageID != 0 {
			t.Fatalf("draft %d = %s", d.ID, got.Status)
		}
	}
	if r, ok := e.lastAudit(OpSentDetected); !ok || r.Reason != DetectFallback || r.ActorType != "system" ||
		strings.Contains(string(r.ParamsRedacted), "Termin") {
		t.Fatalf("audit = %+v", r)
	}
}

// Eine gesendete Nachricht schließt höchstens einen Entwurf; in „All
// Mail" zählt nur, was von einer eigenen Adresse kommt.
func TestDetectSentFallbackOnePerMessageAndOwnSender(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	a := e.fallbackDraft("Re: Doppelt")
	b := e.fallbackDraft("Re: Doppelt")
	to := []store.Addr{{Address: "anna@example.org"}, {Address: "bob@example.org"}}
	at := e.now.Add(time.Minute)
	// Nur in „All Mail", Absender fremd (eine empfangene Mail): zählt nicht.
	m.put("all", sentMsg{messageID: "fremd@example.org", subject: "Re: Doppelt", from: "Anna <anna@example.org>", to: to, date: at, text: fbBody})
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("received mail in All Mail detected: %+v", rep)
	}
	// Von der Kontoadresse, nur in „All Mail" (Gmail): zählt, aber nur für
	// einen der beiden gleichen Entwürfe (den älteren).
	m.put("all", sentMsg{messageID: "eigen@example.org", subject: "Re: Doppelt", from: "Demo <Demo@example.org>", to: to, date: at, text: fbBody})
	rep := e.check()
	if rep.SentByFallback != 1 || rep.SentDraftIDs[0] != a.ID {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(b.ID); got.Status != string(StatusDraft) {
		t.Fatalf("second draft = %s", got.Status)
	}
	// Ein Alias des Kontos zählt als eigene Adresse (Entscheid 26).
	if err := e.st.ExecForTest(`UPDATE accounts SET aliases = '["alias@example.net"]' WHERE id = ?`, e.acc); err != nil {
		t.Fatal(err)
	}
	m.put("all", sentMsg{messageID: "alias@example.net", subject: "Re: Doppelt", from: "alias@example.net", to: to, date: at, text: fbBody})
	if rep := e.check(); rep.SentByFallback != 1 || rep.SentDraftIDs[0] != b.ID {
		t.Fatalf("alias: report = %+v", rep)
	}
}

// E7, Entscheid 38: Beim Sync fallen abgelaufene Freigaben zurück (ready
// und pending_approval), mit Audit approval_expired.
func TestCheckAfterSyncExpiresApprovals(t *testing.T) {
	e := newEnv(t)
	ready := e.reach(StatusReady)
	pending := e.reach(StatusPendingApproval)
	fresh := e.reach(StatusDraft)
	e.now = e.now.Add(DefaultApprovalTTL + time.Second)
	rep := e.check()
	if rep.Expired != 2 {
		t.Fatalf("report = %+v", rep)
	}
	for _, d := range []store.Draft{ready, pending} {
		got := e.draft(d.ID)
		a, _ := e.st.Approval(context.Background(), got.ApprovalID)
		if got.Status != string(StatusDraft) || a.Decision != DecisionExpired {
			t.Fatalf("draft %d = %s, approval %s", d.ID, got.Status, a.Decision)
		}
	}
	if got := e.draft(fresh.ID); got.Status != string(StatusDraft) {
		t.Fatal(got.Status)
	}
	if r, ok := e.lastAudit("drafts.expire"); !ok || r.Reason != ReasonApprovalExpired || r.ActorType != "system" {
		t.Fatalf("audit = %+v", r)
	}
}

// E5/E6: Verschwindet die abgelegte Kopie aus dem Entwürfe-Ordner, ohne
// dass eine gesendete Nachricht passt, bleibt der Entwurf in seinem
// Zustand und remote_state wird nach dem Sync missing (draft list zeigt
// es); eine bearbeitete Fassung ergibt taken_over.
func TestCheckAfterSyncRefreshesRemoteState(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	push := func(d store.Draft) store.Draft {
		t.Helper()
		id := m.put("drafts", sentMsg{messageID: d.MessageIDHeader, subject: "x", draftFlag: true, date: e.now, text: "y"})
		var uid, hash string
		_ = e.st.DBForTest().QueryRow(`SELECT mm.uid, m.content_hash FROM message_mailboxes mm JOIN messages m ON m.id = mm.message_id
			WHERE m.id = ?`, id).Scan(&uid, &hash)
		u, _ := json.Number(uid).Int64()
		if _, _, err := e.svc.Pushed(ctx, d.ID, Fingerprint(e.draft(d.ID)), PushedCopy{MailboxID: m.boxes["drafts"], UIDValidity: 1,
			UID: u, ContentHash: hash}); err != nil {
			t.Fatal(err)
		}
		got := e.draft(d.ID)
		if got.RemoteState != RemotePushed {
			t.Fatalf("remote = %s", got.RemoteState)
		}
		return got
	}
	gone := push(e.reach(StatusReady))
	edited := push(e.reach(StatusDraft))
	kept := push(e.reach(StatusDraft))
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("unchanged copies: %+v", rep)
	}
	// gone: Kopie entfernt (der Spiegel hat den Ordner danach gesehen).
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, m.boxes["drafts"], gone.RemoteUID); err != nil {
		t.Fatal(err)
	}
	// edited: ein Mailprogramm hat eine neue Fassung gleicher Message-ID
	// gespeichert und die alte entfernt.
	if err := e.st.ExecForTest(`DELETE FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, m.boxes["drafts"], edited.RemoteUID); err != nil {
		t.Fatal(err)
	}
	m.put("drafts", sentMsg{messageID: edited.MessageIDHeader, subject: "bearbeitet", draftFlag: true, date: e.now, text: "z"})
	rep := e.check()
	if rep.Missing != 1 || rep.TakenOver != 1 || rep.SentByMessageID+rep.SentByFallback != 0 {
		t.Fatalf("report = %+v", rep)
	}
	if got := e.draft(gone.ID); got.RemoteState != RemoteMissing || got.Status != string(StatusReady) {
		t.Fatalf("gone = %s/%s", got.Status, got.RemoteState)
	}
	if got := e.draft(edited.ID); got.RemoteState != RemoteTakenOver {
		t.Fatalf("edited = %s", got.RemoteState)
	}
	if got := e.draft(kept.ID); got.RemoteState != RemotePushed {
		t.Fatalf("kept = %s", got.RemoteState)
	}
	if r, ok := e.lastAudit(OpRemoteCheck); !ok || r.ActorType != "system" {
		t.Fatalf("audit = %+v", r)
	}
	// Ein zweiter Lauf meldet nichts erneut.
	if rep := e.check(); !rep.Empty() {
		t.Fatalf("second check = %+v", rep)
	}
}
