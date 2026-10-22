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
