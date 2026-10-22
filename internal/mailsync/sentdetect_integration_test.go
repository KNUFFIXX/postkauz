package mailsync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"

	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/provider"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Integrationstest der Versanderkennung (S2.6, A02 E5) gegen Dovecot,
// Stalwart und GreenMail (POSTKAUZ_IT; `make integration`). Der zweite
// IMAP-Client spielt das Mailprogramm: Er „sendet", indem er die
// Nachricht in Gesendet ablegt (ohne \Draft) und die Kopie aus den
// Entwürfen entfernt. PostKauz selbst löscht nichts; aufgeräumt wird am
// Ende nur mit dem Testclient (gemeinsame Testkonten).

// sentPath liefert den Ordner mit Rolle sent; GreenMail legt keinen an,
// dort wird „Sent" angelegt (Rolle per Name).
func (e *itDraftEnv) sentPath() string {
	e.t.Helper()
	find := func() string {
		boxes, err := itProvider(e.t, e.srv, e.user, 0).ListMailboxes(context.Background())
		if err != nil {
			e.t.Fatal(err)
		}
		for _, m := range boxes {
			if m.Role == provider.RoleSent {
				return m.Path
			}
		}
		return ""
	}
	if p := find(); p != "" {
		return p
	}
	if err := e.client.Create("Sent", nil).Wait(); err == nil {
		// Den angelegten Ordner am Ende wieder entfernen (läuft nach dem
		// Aufräumen der Nachrichten; t.Cleanup in umgekehrter Reihenfolge).
		e.t.Cleanup(func() { _ = itClient(e.t, e.srv, e.user).Delete("Sent").Wait() })
	}
	p := find()
	if p == "" {
		e.t.Fatalf("%s: no mailbox with role sent", e.srv.name)
	}
	return p
}

// appendSent legt raw in Gesendet ab (\Seen, ohne \Draft) und merkt die
// Message-ID zum Aufräumen.
func (e *itDraftEnv) appendSent(sent string, raw []byte, mid string, cleanup *[]string) {
	e.t.Helper()
	app := e.client.Append(sent, int64(len(raw)), &imap.AppendOptions{Flags: []imap.Flag{imap.FlagSeen}})
	if _, err := app.Write(raw); err != nil {
		e.t.Fatal(err)
	}
	if err := app.Close(); err != nil {
		e.t.Fatal(err)
	}
	if _, err := app.Wait(); err != nil {
		e.t.Fatal(err)
	}
	*cleanup = append(*cleanup, mid)
}

// removeDraftCopy entfernt die Kopie im Entwürfe-Ordner, wie ein
// Mailprogramm nach dem Senden.
func (e *itDraftEnv) removeDraftCopy(d store.Draft) {
	e.t.Helper()
	c := e.client
	if _, err := c.Select(e.path, nil).Wait(); err != nil {
		e.t.Fatal(err)
	}
	set := imap.UIDSetNum(imap.UID(d.RemoteUID))
	if err := c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close(); err != nil {
		e.t.Fatal(err)
	}
	if err := c.UIDExpunge(set).Close(); err != nil {
		e.t.Fatal(err)
	}
}

// rawCopy holt die Kopie eines Entwurfs aus dem Entwürfe-Ordner.
func (e *itDraftEnv) rawCopy(d store.Draft) []byte {
	e.t.Helper()
	c := e.client
	if _, err := c.Select(e.path, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		e.t.Fatal(err)
	}
	sec := &imap.FetchItemBodySection{Peek: true}
	msgs, err := c.Fetch(imap.UIDSetNum(imap.UID(d.RemoteUID)), &imap.FetchOptions{UID: true,
		BodySection: []*imap.FetchItemBodySection{sec}}).Collect()
	if err != nil || len(msgs) != 1 {
		e.t.Fatalf("fetch copy: %v", err)
	}
	return msgs[0].FindBodySection(sec)
}

// readyPushed legt einen Entwurf an, lässt ihn ablegen und gibt ihn frei.
func (e *itDraftEnv) readyPushed(subject string, sync func()) store.Draft {
	e.t.Helper()
	o := e.create(subject)
	sync()
	req, err := e.do(itAssistant, "drafts.request_ready", map[string]any{"draft_id": o.DraftID})
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.do(itHuman, "drafts.approve", map[string]any{"approval_id": req.(ops.DraftsRequestOutput).ApprovalID}); err != nil {
		e.t.Fatal(err)
	}
	d := e.draft(o.DraftID)
	if d.Status != string(drafts.StatusReady) || d.RemoteState != drafts.RemotePushed {
		e.t.Fatalf("draft %d = %s/%s", d.ID, d.Status, d.RemoteState)
	}
	return d
}

// Akzeptanz S2.6 je Server: (A) Kopie mit derselben Message-ID in
// Gesendet → sent_by_human (message_id); (B) neue Message-ID, gleicher
// Inhalt → sent_by_human (fallback); (C) neue Message-ID, stark geänderter
// Text → bleibt ready, remote missing; ready älter als approval_ttl fällt
// auf draft; draft mark-sent und draft abandon nur ohne Schlüssel.
func TestIntegrationSentDetection(t *testing.T) {
	itRun(t, func(t *testing.T, srv itServer, user string) {
		e := newITDraftEnv(t, srv, user)
		sent := e.sentPath()
		var sentMids []string
		t.Cleanup(func() { e.cleanupMailbox(sent, sentMids) })
		tag := fmt.Sprintf("%s %d", srv.name, time.Now().UnixNano())
		sync := func() ops.SyncOutput {
			t.Helper()
			out, err := e.runner.Sync(context.Background(), e.acc, itPass, ops.SyncOptions{Mailboxes: []string{e.path, sent}}, nil)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
		pushSync := func() {
			t.Helper()
			if out := sync(); out.Drafts == nil || out.Drafts.Pushed != 1 {
				t.Fatalf("push: %+v", out.Drafts)
			}
		}
		a := e.readyPushed("IT A "+tag, pushSync)
		b := e.readyPushed("IT B "+tag, pushSync)
		c := e.readyPushed("IT C "+tag, pushSync)

		// (A) Das Mailprogramm sendet die Kopie mit ihrer Message-ID (eine
		// Kopfzeile mehr, ohne \Draft) und entfernt sie aus den Entwürfen.
		e.appendSent(sent, append([]byte("X-Sent-By-Client: yes\r\n"), e.rawCopy(a)...), a.MessageIDHeader, &sentMids)
		e.removeDraftCopy(a)
		// (B) Webmail baut neu: neue Message-ID, gleicher Text, Betreff mit Präfix.
		midB := fmt.Sprintf("webmail-b-%d@example.org", time.Now().UnixNano())
		e.appendSent(sent, sentRaw(midB, "Re: IT B "+tag, "b@example.org", "Text für IT B "+tag), midB, &sentMids)
		e.removeDraftCopy(b)
		// (C) Neue Message-ID und stark geänderter Text.
		midC := fmt.Sprintf("webmail-c-%d@example.org", time.Now().UnixNano())
		e.appendSent(sent, sentRaw(midC, "IT C "+tag, "b@example.org",
			"Ganz anders formuliert: Wir verschieben das Gespräch auf nächste Woche und melden uns."), midC, &sentMids)
		e.removeDraftCopy(c)

		out := sync()
		if out.DraftCheck == nil || out.DraftCheck.SentByMessageID != 1 || out.DraftCheck.SentByFallback != 1 || out.DraftCheck.Missing != 1 {
			t.Fatalf("check = %+v", out.DraftCheck)
		}
		if d := e.draft(a.ID); d.Status != string(drafts.StatusSentByHuman) || d.SentMessageID == 0 {
			t.Fatalf("A = %s, sent %d", d.Status, d.SentMessageID)
		}
		if d := e.draft(b.ID); d.Status != string(drafts.StatusSentByHuman) || d.SentMessageID == 0 {
			t.Fatalf("B = %s, sent %d", d.Status, d.SentMessageID)
		}
		if d := e.draft(c.ID); d.Status != string(drafts.StatusReady) || d.RemoteState != drafts.RemoteMissing {
			t.Fatalf("C = %s/%s", d.Status, d.RemoteState)
		}
		for reason, want := range map[string]int{drafts.DetectMessageID: 1, drafts.DetectFallback: 1} {
			n, err := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events WHERE operation = ? AND reason = ? AND actor_type = 'system'`,
				drafts.OpSentDetected, reason)
			if err != nil || n != want {
				t.Fatalf("audit %s: %d, %v", reason, n, err)
			}
		}
		if n, _ := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events WHERE params_redacted LIKE ? OR params_redacted LIKE '%Text für%'`,
			"%"+tag+"%"); n != 0 {
			t.Fatalf("audit with content: %d", n)
		}

		// draft mark-sent und draft abandon nur ohne Schlüssel (E2).
		for _, op := range []string{"drafts.mark_sent", "drafts.abandon"} {
			_, err := e.do(itAssistant, op, map[string]any{"draft_id": c.ID})
			var pe *ops.PolicyError
			if !errors.As(err, &pe) || pe.Decision.Reason != "human_only" {
				t.Fatalf("%s with a key = %v", op, err)
			}
		}
		if d := e.draft(c.ID); d.Status != string(drafts.StatusReady) {
			t.Fatalf("C after denied calls = %s", d.Status)
		}

		// Eine weitere Freigabe läuft ab: ready älter als approval_ttl → draft.
		ttl := e.readyPushed("IT TTL "+tag, pushSync)
		e.svc.Now = func() time.Time { return time.Now().Add(drafts.DefaultApprovalTTL + time.Minute) }
		out = sync()
		e.svc.Now = nil
		// C läuft dabei mit ab (auch ready); beide fallen auf draft.
		if out.DraftCheck == nil || out.DraftCheck.Expired != 2 {
			t.Fatalf("expiry check = %+v", out.DraftCheck)
		}
		if d := e.draft(ttl.ID); d.Status != string(drafts.StatusDraft) {
			t.Fatalf("TTL draft = %s", d.Status)
		}
		// Der Mensch schließt C selbst.
		if _, err := e.do(itHuman, "drafts.abandon", map[string]any{"draft_id": c.ID}); err != nil {
			t.Fatal(err)
		}
		if d := e.draft(c.ID); d.Status != string(drafts.StatusAbandoned) {
			t.Fatalf("C = %s", d.Status)
		}
	})
}

// cleanupMailbox entfernt die Nachrichten mit diesen Message-IDs aus einem
// Ordner (nur der Testclient; PostKauz löscht nie).
func (e *itDraftEnv) cleanupMailbox(path string, mids []string) {
	c := itClient(e.t, e.srv, e.user)
	if _, err := c.Select(path, nil).Wait(); err != nil {
		return
	}
	for _, mid := range mids {
		data, err := c.UIDSearch(&imap.SearchCriteria{Header: []imap.SearchCriteriaHeaderField{{Key: "Message-ID", Value: mid}}}, nil).Wait()
		if err != nil || len(data.AllUIDs()) == 0 {
			continue
		}
		set := imap.UIDSetNum(data.AllUIDs()...)
		_ = c.Store(set, &imap.StoreFlags{Op: imap.StoreFlagsAdd, Flags: []imap.Flag{imap.FlagDeleted}, Silent: true}, nil).Close()
		if err := c.UIDExpunge(set).Close(); err != nil {
			_ = c.Expunge().Close()
		}
	}
}
