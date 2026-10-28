package mailsync

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.6: Versanderkennung (E5), Ablauf der Freigabe (E7) und Stand der
// Serverkopie (E6) im Sync, gegen den IMAP-Server im Prozess. Die
// Integrationstests (sentdetect_integration_test.go) prüfen dieselben
// Szenarien gegen Dovecot, Stalwart und GreenMail.

// sentRaw baut eine gesendete Nachricht, wie ein Mailprogramm sie in
// Gesendet ablegt (ohne \Draft).
func sentRaw(messageID, subject, to, text string) []byte {
	return []byte(strings.ReplaceAll(fmt.Sprintf("From: demo@example.org\nTo: %s\nSubject: %s\nDate: %s\nMessage-ID: <%s>\n"+
		"MIME-Version: 1.0\nContent-Type: text/plain; charset=utf-8\n\n%s\n", to, subject,
		time.Now().UTC().Format(time.RFC1123Z), messageID, text), "\n", "\r\n"))
}

// pushed legt einen Entwurf an und lässt ihn vom Sync ablegen.
func (e *pushEnv) pushed(t *testing.T, subject string) store.Draft {
	t.Helper()
	d := e.newDraft(t, subject)
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.Drafts == nil || out.Drafts.Pushed != 1 {
		t.Fatalf("push: %+v, %v", out.Drafts, err)
	}
	return e.draft(t, d.ID)
}

// ready gibt einen abgelegten Entwurf frei (wie draft request + approve
// mit der Nachricht, die im Ordner liegt).
func (e *pushEnv) ready(t *testing.T, d store.Draft) store.Draft {
	t.Helper()
	ctx := context.Background()
	r, err := e.svc.RequestReady(ctx, d.ID, "cli", drafts.HookPass, drafts.Checked{MessageHash: d.PushedMessageHash})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Approve(ctx, r.Draft.ApprovalID, "Operator", d.PushedMessageHash); err != nil {
		t.Fatal(err)
	}
	got := e.draft(t, d.ID)
	if got.Status != string(drafts.StatusReady) || got.RemoteState != drafts.RemotePushed {
		t.Fatalf("ready: %s/%s", got.Status, got.RemoteState)
	}
	return got
}

// Akzeptanz S2.6, drei Szenarien: (A) dieselbe Message-ID in Gesendet →
// sent_by_human (message_id); (B) neue Message-ID, gleicher Inhalt →
// sent_by_human (fallback); (C) neue Message-ID, stark geänderter Text →
// bleibt ready, remote missing. Jedes Mal hat der Client die Kopie aus den
// Entwürfen entfernt, wie beim Senden üblich.
func TestSyncDetectsSentDrafts(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Sent")
	client := imapSession(t, e.addr)

	a := e.ready(t, e.pushed(t, "Termin A"))
	// (A) Der Client sendet die Kopie mit ihrer Message-ID; in Gesendet
	// liegt sie mit einer Kopfzeile mehr und ohne \Draft.
	raw := append([]byte("X-Mailer-Sent: yes\r\n"), rawOf(t, client, "Drafts", imap.UID(a.RemoteUID))...)
	if _, err := e.u.Append("Sent", lit(raw), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	expunge(t, client, "Drafts", imap.UID(a.RemoteUID))
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.DraftCheck == nil || out.DraftCheck.SentByMessageID != 1 || out.DraftCheck.Missing != 0 {
		t.Fatalf("check = %+v", out.DraftCheck)
	}
	got := e.draft(t, a.ID)
	if got.Status != string(drafts.StatusSentByHuman) || got.SentMessageID == 0 {
		t.Fatalf("A: %s, sent %d", got.Status, got.SentMessageID)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE operation = ? AND reason = ? AND actor_type = 'system'`,
		drafts.OpSentDetected, drafts.DetectMessageID); n != 1 {
		t.Fatalf("audit message_id: %d", n)
	}

	// (B) Neue Message-ID, gleicher Inhalt (Webmail baut neu); (C) neue
	// Message-ID, stark geänderter Text. Beide zuerst ablegen, dann senden.
	b := e.ready(t, e.pushed(t, "Termin B"))
	c := e.ready(t, e.pushed(t, "Termin C"))
	if _, err := e.u.Append("Sent", lit(sentRaw("webmail-b@example.org", "Re: Termin B", "b@example.org", "Hallo Termin B")),
		&imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	expunge(t, client, "Drafts", imap.UID(b.RemoteUID))
	if _, err := e.u.Append("Sent", lit(sentRaw("webmail-c@example.org", "Termin C", "b@example.org",
		"Ganz neu geschrieben: leider klappt es diese Woche nicht mehr, wir melden uns.")),
		&imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	expunge(t, client, "Drafts", imap.UID(c.RemoteUID))
	out, err = e.sync(t, ops.SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if out.DraftCheck == nil || out.DraftCheck.SentByFallback != 1 || out.DraftCheck.Missing != 1 ||
		len(out.DraftCheck.SentDraftIDs) != 1 || out.DraftCheck.SentDraftIDs[0] != b.ID {
		t.Fatalf("check = %+v", out.DraftCheck)
	}
	if got := e.draft(t, b.ID); got.Status != string(drafts.StatusSentByHuman) {
		t.Fatalf("B: %s", got.Status)
	}
	if got := e.draft(t, c.ID); got.Status != string(drafts.StatusReady) || got.RemoteState != drafts.RemoteMissing {
		t.Fatalf("C: %s/%s", got.Status, got.RemoteState)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE operation = ? AND reason = ?`, drafts.OpSentDetected,
		drafts.DetectFallback); n != 1 {
		t.Fatalf("audit fallback: %d", n)
	}
	// Audit ohne Mailinhalt.
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE params_redacted LIKE '%Termin%' OR params_redacted LIKE '%Hallo%'`); n != 0 {
		t.Fatalf("audit with content: %d", n)
	}
	// Ein weiterer Lauf ändert nichts.
	if out, err := e.sync(t, ops.SyncOptions{}); err != nil || out.DraftCheck != nil {
		t.Fatalf("third sync: %+v, %v", out.DraftCheck, err)
	}
}

// Befund K8: Eine Entwurfskopie (\Draft) in Gesendet bzw. „All Mail" ist
// kein Versand.
func TestSyncIgnoresDraftFlaggedCopies(t *testing.T) {
	e := newPushEnv(t, "Drafts", "Sent")
	client := imapSession(t, e.addr)
	d := e.pushed(t, "Termin K8")
	raw := rawOf(t, client, "Drafts", imap.UID(d.RemoteUID))
	if _, err := e.u.Append("Sent", lit(append([]byte("X-Copy: 1\r\n"), raw...)),
		&imap.AppendOptions{Flags: []imap.Flag{imap.FlagDraft, imap.FlagSeen}}); err != nil {
		t.Fatal(err)
	}
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil || out.DraftCheck != nil {
		t.Fatalf("sync: %+v, %v", out.DraftCheck, err)
	}
	if got := e.draft(t, d.ID); got.Status != string(drafts.StatusDraft) || got.RemoteState != drafts.RemotePushed {
		t.Fatalf("draft = %s/%s", got.Status, got.RemoteState)
	}
}

// E7, Entscheid 38: Beim Sync fällt ein ready-Entwurf nach approval_ttl
// auf draft zurück, ohne dass jemand ihn liest.
func TestSyncExpiresApprovals(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	d := e.ready(t, e.pushed(t, "Termin TTL"))
	// approval_ttl (Standard 7 Tage) ist um.
	e.svc.Now = func() time.Time { return time.Now().Add(drafts.DefaultApprovalTTL + time.Minute) }
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil || out.DraftCheck == nil || out.DraftCheck.Expired != 1 {
		t.Fatalf("sync: %+v, %v", out.DraftCheck, err)
	}
	if got := e.draft(t, d.ID); got.Status != string(drafts.StatusDraft) {
		t.Fatalf("status = %s", got.Status)
	}
	if n := e.count(t, `SELECT count(*) FROM approvals WHERE decision = 'expired'`); n != 1 {
		t.Fatalf("expired approvals: %d", n)
	}
	if n := e.count(t, `SELECT count(*) FROM audit_events WHERE operation = 'drafts.expire' AND reason = 'approval_expired'`); n != 1 {
		t.Fatalf("audit: %d", n)
	}
}

// Entscheid 43: Ein einmaliger sync innerhalb von 30 s nach einer Änderung
// legt nicht ab und meldet das (drafts.waiting); der Dauerläufer holt die
// Ablage nach, sobald die Ruhezeit vorbei ist (Uhr des Pushs verstellt,
// statt 30 s zu warten).
func TestWatcherPushesChangedDraftAfterSettle(t *testing.T) {
	e := newPushEnv(t, "Drafts")
	var offset atomic.Int64
	pusher := e.runner.Pusher.(*ops.DraftPusher)
	pusher.Now = func() time.Time { return time.Now().Add(time.Duration(offset.Load())) }
	client := imapSession(t, e.addr)
	d := e.pushed(t, "Termin Ruhe") // erste Ablage sofort
	c := drafts.ContentOf(d)
	c.BodyMarkdown = "Hallo geändert"
	if _, err := e.svc.Update(context.Background(), d.ID, c, ""); err != nil {
		t.Fatal(err)
	}
	out, err := e.sync(t, ops.SyncOptions{})
	if err != nil || out.Drafts == nil || out.Drafts.Waiting != 1 || out.Drafts.Pushed != 0 {
		t.Fatalf("sync within 30 s: %+v, %v", out.Drafts, err)
	}
	if got := e.draft(t, d.ID); got.RemoteState != drafts.RemotePending {
		t.Fatalf("remote = %s", got.RemoteState)
	}
	if n := len(serverCopies(t, client, "Drafts", d.MessageIDHeader)); n != 1 {
		t.Fatalf("copies = %d", n)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var rounds atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, err := e.runner.Sync(ctx, e.account(t), provider.PasswordCredential(testPass), ops.SyncOptions{Watch: true, Interval: time.Hour,
			JobsInterval: 50 * time.Millisecond}, func(p ops.Progress) {
			if p.Event == "round" {
				rounds.Add(1)
			}
		})
		done <- err
	}()
	waitFor(t, 10*time.Second, func() bool { return rounds.Load() == 1 })
	// Mehrere Prüfungen des Watchers in der Ruhezeit: keine neue Kopie.
	time.Sleep(300 * time.Millisecond)
	if got := e.draft(t, d.ID); got.RemoteState != drafts.RemotePending {
		t.Fatalf("pushed within 30 s: %s", got.RemoteState)
	}
	// Ruhezeit vorbei: Der Watcher legt ab (der Job blieb fällig).
	offset.Store(int64(ops.DefaultChangeSettle + time.Second))
	waitFor(t, 10*time.Second, func() bool { return e.draft(t, d.ID).RemoteState == drafts.RemotePushed })
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := len(serverCopies(t, client, "Drafts", d.MessageIDHeader)); n != 2 {
		t.Fatalf("copies = %d", n)
	}
}
