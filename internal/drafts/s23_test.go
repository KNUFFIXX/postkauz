package drafts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// S2.3: Mit As auditiert der Dienst jeden Übergang in derselben
// Transaktion (Akteur, Schlüssel, Draft-ID, Zustände, Freigabe, Hook; nie
// Inhalt) und reiht nach einer Änderung den Job draft_push ein. Im
// Trockenlauf läuft alles mit allen Prüfungen und wird zurückgerollt.
func TestAsAuditsTransitionsAndQueuesPush(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	who := Who{Actor: audit.ActorAgent, KeyID: "key:assistent", Scope: "mail:draft", Operation: "drafts.create"}

	// Trockenlauf: Ergebnis wie echt, aber nichts gespeichert.
	dry, err := e.svc.As(who, true).Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: content(canarySubject), CreatedBy: "key:assistent"})
	if err != nil || dry.Draft.ID == 0 || dry.To != StatusDraft || dry.PushJobID == 0 {
		t.Fatalf("dry create = %+v, %v", dry, err)
	}
	if l, _ := e.st.ListDrafts(ctx, store.DraftFilter{}); len(l) != 0 {
		t.Fatalf("dry run saved %d drafts", len(l))
	}
	if evs, _ := e.st.AuditEvents(ctx, 0, 100); len(evs) != 0 {
		t.Fatalf("dry run wrote audit: %+v", evs)
	}

	res, err := e.svc.As(who, false).Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: content(canarySubject), CreatedBy: "key:assistent"})
	if err != nil || res.PushJobID == 0 {
		t.Fatalf("create = %+v, %v", res, err)
	}
	id := res.Draft.ID
	who.Operation = "drafts.request_ready"
	if _, err := e.svc.As(who, false).RequestReady(ctx, id, "key:assistent", HookPass, Checked{MessageHash: testMessageHash}); err != nil {
		t.Fatal(err)
	}
	human := Who{Actor: audit.ActorHuman, KeyID: "cli", Scope: "mail:draft", Operation: "drafts.approve"}
	d, _ := e.st.Draft(ctx, id)
	if _, err := e.svc.As(human, false).Approve(ctx, d.ApprovalID, "Mensch", testMessageHash); err != nil {
		t.Fatal(err)
	}
	who.Operation = "drafts.update"
	up, err := e.svc.As(who, false).Update(ctx, id, content(canarySubject+" neu"), "")
	if err != nil || up.To != StatusDraft || up.PushJobID != res.PushJobID {
		t.Fatalf("update = %+v, %v (one open push job per account)", up, err)
	}

	evs, err := e.st.AuditEvents(ctx, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range evs {
		var p map[string]any
		_ = json.Unmarshal(ev.ParamsRedacted, &p)
		got = append(got, ev.Operation+" "+string(ev.ActorType)+" "+ev.KeyID+" "+p["from"].(string)+">"+p["to"].(string))
		if ev.Reason != ReasonTransition || p["draft_id"] != float64(id) || p["recipients"] != float64(2) {
			t.Errorf("event %+v params %v", ev, p)
		}
	}
	want := []string{"drafts.create agent key:assistent new>draft", "drafts.request_ready agent key:assistent draft>pending_approval",
		"drafts.approve human cli pending_approval>ready", "drafts.update agent key:assistent ready>draft"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("audit\n got %v\nwant %v", got, want)
	}
	var all strings.Builder
	for _, ev := range evs {
		// ParamsRedacted als Text (json.Marshal schriebe []byte als Base64).
		all.WriteString(ev.Operation + " " + string(ev.ParamsRedacted) + " " + strings.Join(ev.Targets, ",") + " " + ev.Reason + "\n")
	}
	for _, leak := range []string{canarySubject, canaryBody, canaryAddr, "Anna"} {
		if strings.Contains(all.String(), leak) {
			t.Fatalf("audit contains %q", leak)
		}
	}
	if !strings.Contains(all.String(), `"hook":"pass"`) || !strings.Contains(all.String(), `"approval_id"`) {
		t.Fatalf("audit lacks hook or approval: %s", all.String())
	}

	// Trockenlauf eines verbotenen Übergangs scheitert wie echt.
	if _, err := e.svc.As(human, true).Approve(ctx, d.ApprovalID, "Mensch", testMessageHash); ReasonOf(err) != ReasonApprovalStale {
		t.Fatalf("dry approve of a superseded approval: %v", err)
	}
	// Ohne As: kein Audit (wie in S2.1).
	n := len(evs)
	if _, err := e.svc.Abandon(ctx, id); err != nil {
		t.Fatal(err)
	}
	if evs, _ := e.st.AuditEvents(ctx, 0, 100); len(evs) != n {
		t.Fatalf("service without As wrote audit")
	}
}
