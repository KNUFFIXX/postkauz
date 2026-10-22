package drafts

import (
	"context"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Gegenprüfung S2.6–S2.8, Runde 1 (Versanderkennung).

// readyFallback legt einen Entwurf mit Rückfall-tauglichem Text an und gibt
// ihn frei (ready).
func (e *env) readyFallback(subject string) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	d := e.fallbackDraft(subject)
	r, err := e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash})
	if err != nil {
		e.t.Fatal(err)
	}
	e.now = e.now.Add(time.Hour)
	if r, err = e.svc.Approve(ctx, r.Draft.ApprovalID, "Demo Operator", testMessageHash); err != nil || r.Draft.Status != string(StatusReady) {
		e.t.Fatalf("approve: %v %s", err, r.Draft.Status)
	}
	return r.Draft
}

// webmailSent legt die vom Webmail neu gebaute Nachricht (neue Message-ID,
// gleicher Text) mit Datum at in Gesendet ab.
func (m *mirror) webmailSent(mid, subject string, at time.Time) int64 {
	return m.put("sent", sentMsg{messageID: mid, subject: "Re: " + subject, from: "demo@example.org",
		to: []store.Addr{{Address: "anna@example.org"}}, cc: []store.Addr{{Address: "bob@example.org"}}, date: at, text: fbBody})
}

// W1: ready, der Mensch sendet am Tag 6 aus dem Webmail (neue Message-ID),
// der Sync sieht die Nachricht erst am Tag 8 (nach approval_ttl). Der
// Entwurf ist gesendet (fallback), nicht abgelaufen; das Audit zeigt keinen
// Ablauf. Ebenso, wenn ein Lesen am Tag 7,5 den Entwurf schon auf draft
// zurückgesetzt hat: Das Zeitfenster hängt an der letzten Inhaltsänderung,
// nicht an updated_at.
func TestDetectSentBeforeExpiry(t *testing.T) {
	for _, read := range []bool{false, true} {
		e := newEnv(t)
		m := newMirror(e)
		start := e.now
		d := e.readyFallback("Wartung Aufzug")
		if !d.ContentChangedAt.Equal(start) || d.UpdatedAt.Equal(start) {
			t.Fatalf("content_changed_at %v, updated_at %v, created %v", d.ContentChangedAt, d.UpdatedAt, start)
		}
		m.webmailSent("web-1@example.org", "Wartung Aufzug", start.Add(6*24*time.Hour))
		if read {
			// Lesen am Tag 7,5 lässt die Freigabe ablaufen (E7).
			e.now = start.Add(7*24*time.Hour + 12*time.Hour)
			if got, err := e.svc.Get(context.Background(), d.ID); err != nil || got.Status != string(StatusDraft) {
				t.Fatalf("read on day 7.5: %v %s", err, got.Status)
			}
		}
		e.now = start.Add(8 * 24 * time.Hour)
		rep := e.check()
		if rep.SentByFallback != 1 || rep.Expired != 0 {
			t.Fatalf("read %v: report = %+v", read, rep)
		}
		if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) {
			t.Fatalf("read %v: draft = %s", read, got.Status)
		}
		n, err := e.st.QueryIntForTest(`SELECT count(*) FROM audit_events WHERE operation = 'drafts.expire'`)
		if want := map[bool]int{false: 0, true: 1}[read]; err != nil || n != want {
			t.Fatalf("read %v: expire audit %d, want %d (%v)", read, n, want, err)
		}
	}
}

// K4: Der Rückfall-Abgleich liest die offenen Entwürfe vor der
// Transaktion. Hat ein anderer Schreiber den Inhalt seitdem geändert,
// schließt SentDetected den Entwurf nicht (draft_changed), und markSent
// zählt das nicht als Fehler; mit dem aktuellen Fingerprint schließt er.
func TestSentDetectedChecksFingerprint(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	d := e.fallbackDraft("Wartung Aufzug")
	seen := Fingerprint(d)
	mid := m.webmailSent("web-1@example.org", "Wartung Aufzug", e.now.Add(time.Minute))
	c := ContentOf(d)
	c.BodyMarkdown = "Ganz anderer Text, geändert nach dem Lesen."
	if _, err := e.svc.Update(ctx, d.ID, c, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.SentDetected(ctx, d.ID, mid, DetectFallback, seen); ReasonOf(err) != ReasonDraftChanged {
		t.Fatalf("stale fingerprint: %v", err)
	}
	if done, err := e.svc.markSent(ctx, d.ID, mid, DetectFallback, seen); done || err != nil {
		t.Fatalf("markSent with a stale fingerprint = %v, %v", done, err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) || got.SentMessageID != 0 {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
	if _, ok := e.lastAudit(OpSentDetected); ok {
		t.Fatal("sent_detected audited for a changed draft")
	}
	if _, err := e.svc.SentDetected(ctx, d.ID, mid, DetectFallback, Fingerprint(e.draft(d.ID))); err != nil {
		t.Fatalf("current fingerprint: %v", err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != mid {
		t.Fatalf("draft = %s, sent %d", got.Status, got.SentMessageID)
	}
}

// W2: Der Rückfall lädt nur neue Nachrichten und das Fenster neuer oder
// geänderter Entwürfe. Eine zweite Runde ohne neue Nachrichten prüft 0
// Kandidaten; eine neue Nachricht genau eine; eine Inhaltsänderung das
// Fenster dieses Entwurfs; ohne Marke (Bestand vor Migration 0016) die
// Fenster aller offenen Entwürfe. Erkannt wird weiterhin.
func TestFallbackExaminesOnlyNewMessages(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	start := e.now
	a := e.fallbackDraft("Wartung Aufzug")
	e.fallbackDraft("Wartung Heizung")
	for i := 0; i < 20; i++ {
		m.put("sent", sentMsg{messageID: "alt-" + string(rune('a'+i)) + "@example.org", subject: "Anderes Thema",
			from: "demo@example.org", to: []store.Addr{{Address: "x@example.org"}}, date: start.Add(time.Duration(i) * time.Minute), text: "egal"})
	}
	if rep := e.check(); rep.Examined != 20 || !rep.Empty() {
		t.Fatalf("first check = %+v", rep)
	}
	if rep := e.check(); rep.Examined != 0 {
		t.Fatalf("second check without new messages examined %d", rep.Examined)
	}
	m.put("sent", sentMsg{messageID: "neu@example.org", subject: "Noch ein Thema", from: "demo@example.org",
		to: []store.Addr{{Address: "x@example.org"}}, date: start.Add(time.Hour), text: "egal"})
	if rep := e.check(); rep.Examined != 1 {
		t.Fatalf("one new message: examined %d", rep.Examined)
	}
	// Geänderter Inhalt: das ganze Fenster dieses Entwurfs, einmal.
	e.now = start.Add(30 * time.Minute)
	c := ContentOf(a)
	c.BodyMarkdown = fbBody + "\n\nPS: bis Montag."
	if _, err := e.svc.Update(ctx, a.ID, c, ""); err != nil {
		t.Fatal(err)
	}
	if rep := e.check(); rep.Examined != 21 {
		t.Fatalf("changed draft: examined %d, want the 21 messages in its window", rep.Examined)
	}
	if rep := e.check(); rep.Examined != 0 {
		t.Fatalf("after the changed draft: examined %d", rep.Examined)
	}
	// Ohne Marke: alle Fenster (Bestand vor 0016).
	if err := e.st.ExecForTest(`DELETE FROM sent_check_marks`); err != nil {
		t.Fatal(err)
	}
	if rep := e.check(); rep.Examined != 21 {
		t.Fatalf("without a mark: examined %d", rep.Examined)
	}
	// Erkannt wird weiterhin, auch eine Nachricht, die erst nach der Marke kommt.
	m.webmailSent("web-1@example.org", "Wartung Heizung", start.Add(2*time.Hour))
	if rep := e.check(); rep.Examined != 1 || rep.SentByFallback != 1 {
		t.Fatalf("new matching message: %+v", rep)
	}
}

// K7: Ein Entwurf ohne Empfänger (der Rückfall kann ihn nie schließen)
// spannt kein Zeitfenster auf; früher begann das Fenster beim ersten
// offenen Entwurf, auch wenn er danach übersprungen wurde.
func TestFallbackWindowIgnoresDraftsWithoutRecipients(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	start := e.now
	if _, err := e.svc.Create(context.Background(), NewDraft{AccountID: e.acc, Kind: KindNew, CreatedBy: "key:agent",
		Content: Content{Subject: "Notiz", BodyMarkdown: fbBody}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		m.put("sent", sentMsg{messageID: "alt-" + string(rune('a'+i)) + "@example.org", subject: "Anderes Thema",
			from: "demo@example.org", to: []store.Addr{{Address: "x@example.org"}}, date: start.Add(time.Duration(i) * time.Hour), text: "egal"})
	}
	e.now = start.Add(30 * 24 * time.Hour)
	e.fallbackDraft("Wartung Aufzug")
	if err := e.st.ExecForTest(`DELETE FROM sent_check_marks`); err != nil {
		t.Fatal(err)
	}
	if rep := e.check(); rep.Examined != 0 {
		t.Fatalf("examined %d messages in the window of a draft without recipients", rep.Examined)
	}
}

// K3: Eine Nachricht mit dem Inhalts-Hash einer von PostKauz abgelegten
// Kopie (aktuelle, frühere) ist nie eine gesendete Nachricht, auch wenn der
// Spiegel sie nur in „All Mail" ohne \Draft sieht (etwa weil der
// Entwürfe-Ordner im Lauf fehlte): weder über die Message-ID noch im
// Rückfall für einen gleichlautenden anderen Entwurf. Eine wirklich
// gesendete Nachricht (andere Bytes) zählt weiter.
func TestDetectSentIgnoresOwnCopiesByHash(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	a := e.fallbackDraft("Wartung Aufzug")
	b := e.fallbackDraft("Wartung Aufzug")
	copyOf := func(d store.Draft, text string, uid int64) {
		t.Helper()
		if _, _, err := e.svc.Pushed(ctx, d.ID, Fingerprint(e.draft(d.ID)), PushedCopy{MailboxID: m.boxes["drafts"], UIDValidity: 1,
			UID: uid, ContentHash: d.MessageIDHeader + text}); err != nil {
			t.Fatal(err)
		}
		m.put("all", sentMsg{messageID: d.MessageIDHeader, subject: "Re: Wartung Aufzug", from: "demo@example.org",
			to: []store.Addr{{Address: "anna@example.org"}}, cc: []store.Addr{{Address: "bob@example.org"}}, date: e.now, text: text})
	}
	copyOf(a, fbBody, 50)
	// Frühere Kopie: Der Entwurf wurde geändert und erneut abgelegt.
	c := ContentOf(e.draft(a.ID))
	c.BodyMarkdown = fbBody + "\n\nPS: bis Montag."
	if _, err := e.svc.Update(ctx, a.ID, c, ""); err != nil {
		t.Fatal(err)
	}
	copyOf(a, fbBody+" PS: bis Montag.", 51)
	if got := e.draft(a.ID); len(got.RemotePrevious) != 1 || got.RemotePrevious[0].ContentHash != a.MessageIDHeader+fbBody {
		t.Fatalf("previous copies = %+v", got.RemotePrevious)
	}
	if rep := e.check(); rep.SentByMessageID+rep.SentByFallback != 0 {
		t.Fatalf("own copies detected as sent: %+v", rep)
	}
	for _, d := range []store.Draft{a, b} {
		if got := e.draft(d.ID); got.Status != string(StatusDraft) {
			t.Fatalf("draft %d = %s", d.ID, got.Status)
		}
	}
	// Der Mailclient sendet A mit seiner Message-ID (andere Bytes): zählt.
	sent := m.put("sent", sentMsg{messageID: a.MessageIDHeader, subject: "Re: Wartung Aufzug", from: "demo@example.org",
		to: []store.Addr{{Address: "anna@example.org"}}, date: e.now, text: "gesendet"})
	if rep := e.check(); rep.SentByMessageID != 1 {
		t.Fatalf("sent message: %+v", rep)
	}
	if got := e.draft(a.ID); got.Status != string(StatusSentByHuman) || got.SentMessageID != sent {
		t.Fatalf("a = %s, sent %d", got.Status, got.SentMessageID)
	}
}

// K3: Mit SkipSent (Entwürfe-Ordner nicht gespiegelt) erkennt die Prüfung
// nichts als gesendet und lässt den Stand der Kopie, nur der Ablauf läuft;
// mit KeepMark erkennt sie, vermerkt aber nichts (der nächste volle Lauf
// prüft dieselben Nachrichten erneut).
func TestCheckOptionsSkipSentAndKeepMark(t *testing.T) {
	e := newEnv(t)
	m := newMirror(e)
	ctx := context.Background()
	d := e.reach(StatusReady)
	stale := e.reach(StatusReady)
	m.put("sent", sentMsg{messageID: d.MessageIDHeader, subject: "x", from: "demo@example.org", date: e.now, text: "x"})
	e.now = e.now.Add(DefaultApprovalTTL + time.Hour)
	rep, err := e.svc.CheckAfterSync(ctx, e.acc, CheckOptions{SkipSent: true})
	if err != nil || rep.SentByMessageID != 0 || rep.Expired != 2 {
		t.Fatalf("SkipSent: %+v, %v", rep, err)
	}
	if got := e.draft(d.ID); got.Status != string(StatusDraft) {
		t.Fatalf("draft = %s", got.Status)
	}
	if _, ok, _ := e.st.SentCheckMark(ctx, e.acc); ok {
		t.Fatal("mark set without sent detection")
	}
	m.put("sent", sentMsg{messageID: "neu@example.org", subject: "y", from: "demo@example.org", date: e.now, text: "y"})
	for i := 0; i < 2; i++ {
		rep, err = e.svc.CheckAfterSync(ctx, e.acc, CheckOptions{KeepMark: true})
		if err != nil || rep.SentByMessageID != 1-i {
			t.Fatalf("KeepMark round %d: %+v, %v", i, rep, err)
		}
	}
	if _, ok, _ := e.st.SentCheckMark(ctx, e.acc); ok {
		t.Fatal("mark set with KeepMark")
	}
	if got := e.draft(stale.ID); !got.FallbackCheckedAt.IsZero() {
		t.Fatal("fallback_checked_at set with KeepMark")
	}
	if rep := e.check(); rep.Examined == 0 {
		t.Fatalf("full run after KeepMark examined nothing: %+v", rep)
	}
	if _, ok, _ := e.st.SentCheckMark(ctx, e.acc); !ok {
		t.Fatal("no mark after a full run")
	}
}
