package drafts

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A05 Teil 1, Upgrade: Offene Entwürfe, die ein älterer Build schon
// vollständig geprüft hat (Hochwassermarke und fallback_checked_at
// gesetzt), prüft die nächste vollständige Prüfung einmal neu nach den
// geänderten Regeln (meta sent_check_rev/<Konto>); nicht bei KeepMark.
func TestFallbackRecheckAfterUpgrade(t *testing.T) {
	ctx := context.Background()
	quote := a05Body + "\n\n" + a05Forms[0].head + "\n\n" + a05History
	intro := "Am 21.10.2026 09:15 schrieb Anna <anna@example.org>:"
	// rc2State legt einen Antwort-Entwurf und seine in Gmail gesendete
	// Nachricht an, im Stand nach einem Lauf eines älteren Builds: alles
	// geprüft (Marke, fallback_checked_at), nichts gefunden.
	rc2State := func(e *env, m *mirror, subject string) (store.Draft, int64) {
		t.Helper()
		r, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindReply, CreatedBy: "key:agent",
			Content: Content{To: []store.Addr{{Name: "Anna", Address: "anna@example.org"}}, Subject: subject, BodyMarkdown: fbBody,
				BodyText: fbBody + "\n\n" + intro + "\n> " + strings.ReplaceAll(quote, "\n", "\n> ") + "\n"}})
		if err != nil {
			t.Fatal(err)
		}
		sent := m.put("sent", sentMsg{messageID: "gmail-" + subject + "@example.org", subject: subject, from: "demo@example.org",
			to: []store.Addr{{Address: "anna@example.org"}}, date: e.now.Add(5 * time.Minute),
			text: sentTextOf(fbBody + "\n\n" + intro + "\n\n" + quote + "\n")})
		if err := e.st.SetSentCheckMark(ctx, e.acc, sent); err != nil {
			t.Fatal(err)
		}
		if err := e.st.SetFallbackChecked(ctx, r.Draft.ID, e.draft(r.Draft.ID).ContentChangedAt); err != nil {
			t.Fatal(err)
		}
		return r.Draft, sent
	}
	t.Run("full run", func(t *testing.T) {
		e := newEnv(t)
		m := newMirror(e)
		d, sent := rc2State(e, m, "Re: Unterlagen")
		rep := e.check()
		if rep.SentByFallback != 1 || rep.Examined != 1 {
			t.Fatalf("report = %+v", rep)
		}
		if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
			t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
		}
		if rev, _ := e.st.SentCheckRev(ctx, e.acc); rev != SentCheckRev {
			t.Fatalf("rev = %d", rev)
		}
		// Einmal: Ein weiterer, schon geprüfter Entwurf wird danach nicht
		// mehr neu gelesen.
		other, _ := rc2State(e, m, "Re: Anderes")
		if rep := e.check(); rep.Examined != 0 || rep.SentByFallback != 0 {
			t.Fatalf("second run = %+v", rep)
		}
		if got := e.draft(other.ID); got.Status != string(StatusDraft) {
			t.Fatalf("other = %s", got.Status)
		}
	})
	t.Run("keep mark", func(t *testing.T) {
		e := newEnv(t)
		m := newMirror(e)
		rc2State(e, m, "Re: Unterlagen")
		rep, err := e.svc.CheckAfterSync(ctx, e.acc, CheckOptions{KeepMark: true})
		if err != nil || rep.SentByFallback != 1 {
			t.Fatalf("keep mark: %+v, %v", rep, err)
		}
		if rev, _ := e.st.SentCheckRev(ctx, e.acc); rev != 0 {
			t.Fatalf("rev after KeepMark = %d", rev)
		}
	})
}
