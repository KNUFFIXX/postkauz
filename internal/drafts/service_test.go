package drafts

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Kanarienwerte: Sie dürfen nie in Fehlermeldungen, Freigabe-Beschreibungen
// oder im Audit stehen.
const (
	canarySubject = "KANARIENBETREFF-S21"
	canaryBody    = "KANARIENTEXT-S21"
	canaryAddr    = "kanarie-s21@example.org"
)

type env struct {
	t   *testing.T
	st  *store.SQLite
	svc *Service
	now time.Time
	acc int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return setupEnv(t, st)
}

func setupEnv(t *testing.T, st *store.SQLite) *env {
	t.Helper()
	e := &env{t: t, st: st, now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	acc, err := st.CreateAccount(context.Background(), store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1",
		Port: 143, TLS: "none", Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	e.acc = acc
	e.svc = &Service{Store: st, Now: func() time.Time { return e.now }, Rand: zeroRand{}, PolicyVersion: 4}
	return e
}

// zeroRand liefert Zufall aus einem Zähler: jede Message-ID verschieden,
// aber reproduzierbar.
type zeroRand struct{}

var randCounter struct {
	sync.Mutex
	n byte
}

func (zeroRand) Read(p []byte) (int, error) {
	randCounter.Lock()
	defer randCounter.Unlock()
	randCounter.n++
	for i := range p {
		p[i] = randCounter.n
	}
	return len(p), nil
}

func content(subject string) Content {
	return Content{To: []store.Addr{{Name: "Anna", Address: "anna@example.org"}, {Address: canaryAddr}}, Subject: subject,
		BodyMarkdown: canaryBody, BodyText: canaryBody, BodyHTML: "<p>" + canaryBody + "</p>"}
}

func (e *env) create() store.Draft {
	e.t.Helper()
	r, err := e.svc.Create(context.Background(), NewDraft{AccountID: e.acc, Kind: KindNew, Content: content(canarySubject), CreatedBy: "key:agent"})
	if err != nil {
		e.t.Fatal(err)
	}
	return r.Draft
}

// reach bringt einen neuen Entwurf in den Zustand s (über die Dienste).
func (e *env) reach(s Status) store.Draft {
	e.t.Helper()
	ctx := context.Background()
	d := e.create()
	step := func(r Result, err error) {
		e.t.Helper()
		if err != nil {
			e.t.Fatalf("reach %s: %v", s, err)
		}
		d = r.Draft
	}
	switch s {
	case StatusDraft:
	case StatusPendingApproval:
		step(e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash}))
	case StatusReady:
		step(e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash}))
		step(e.svc.Approve(ctx, d.ApprovalID, "Demo Operator", testMessageHash))
	case StatusSentByHuman:
		// Über pending_approval, damit eine (danach ersetzte) Freigabe besteht.
		step(e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash}))
		step(e.svc.MarkSent(ctx, d.ID))
	case StatusAbandoned:
		step(e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash}))
		step(e.svc.Abandon(ctx, d.ID))
	}
	if d.Status != string(s) {
		e.t.Fatalf("reach %s: status %s", s, d.Status)
	}
	return d
}

// fire löst ein Ereignis am Entwurf aus, wie es die Operationen (S2.3) bzw.
// Sync (S2.6) tun werden.
func (e *env) fire(d store.Draft, ev Event) (Result, error) {
	ctx := context.Background()
	switch ev {
	case EvUpdate:
		return e.svc.Update(ctx, d.ID, content(canarySubject+" geändert"), "")
	case EvRequestReady:
		return e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash})
	case EvApprove:
		return e.svc.Approve(ctx, d.ApprovalID, "Demo Operator", testMessageHash)
	case EvReject:
		return e.svc.Reject(ctx, d.ApprovalID, "Demo Operator", "Ton zu scharf")
	case EvAbandon:
		return e.svc.Abandon(ctx, d.ID)
	case EvMarkSent:
		return e.svc.MarkSent(ctx, d.ID)
	case EvSentDetected:
		return e.svc.SentDetected(ctx, d.ID, 0, "message_id")
	case EvExpire:
		e.now = e.now.Add(DefaultApprovalTTL + time.Second)
		ids, err := e.svc.ExpireApprovals(ctx, e.acc)
		if err != nil {
			return Result{}, err
		}
		got, _ := e.st.Draft(ctx, d.ID)
		for _, id := range ids {
			if id == d.ID {
				return Result{Draft: got, From: Status(d.Status), To: Status(got.Status)}, nil
			}
		}
		// Nichts abgelaufen: dieselbe Ablehnung wie die Zustandsmaschine.
		_, err = Next(Status(got.Status), EvExpire)
		if err == nil {
			err = &DeniedError{Reason: ReasonInvalidTransition, Msg: "nothing expired"}
		}
		return Result{Draft: got}, err
	}
	e.t.Fatalf("unknown event %s", ev)
	return Result{}, nil
}

// Akzeptanz S2.1 („Zustandstests"): Jeder erlaubte Übergang läuft genau
// einmal durch den Dienst (mit Datenbank) und landet im richtigen Zustand
// mit der richtigen Wirkung auf die Freigabe; jedes verbotene Paar ist
// denied und ändert nichts.
func TestServiceTransitions(t *testing.T) {
	ctx := context.Background()
	for _, s := range Statuses {
		for _, ev := range Events {
			w, allowed := wantTransitions[[2]string{string(s), string(ev)}]
			t.Run(string(s)+"/"+string(ev), func(t *testing.T) {
				e := newEnv(t)
				var d store.Draft
				if !allowed && (ev == EvApprove || ev == EvReject) && s == StatusDraft {
					// Ein Entwurf im Zustand draft mit Freigabe: vorgelegt und abgelehnt.
					d = e.reach(StatusPendingApproval)
					r, err := e.svc.Reject(ctx, d.ApprovalID, "Demo Operator", "")
					if err != nil {
						t.Fatal(err)
					}
					d = r.Draft
				} else {
					d = e.reach(s)
				}
				var before *store.Approval
				if d.ApprovalID > 0 {
					a, _ := e.st.Approval(ctx, d.ApprovalID)
					before = &a
				}
				r, err := e.fire(d, ev)
				after, gerr := e.st.Draft(ctx, d.ID)
				if gerr != nil {
					t.Fatal(gerr)
				}
				if !allowed {
					if !errors.Is(err, ErrDenied) {
						t.Fatalf("forbidden: err = %v, want denied", err)
					}
					if s.Closed() && ReasonOf(err) != ReasonDraftClosed {
						t.Fatalf("closed draft: reason %s, want draft_closed", ReasonOf(err))
					}
					if after.Status != d.Status || after.UpdatedAt != d.UpdatedAt || after.PreviewHash != d.PreviewHash {
						t.Fatalf("forbidden event changed the draft: %+v -> %+v", d, after)
					}
					return
				}
				if err != nil {
					t.Fatalf("allowed: %v", err)
				}
				if after.Status != w[0] || string(r.To) != w[0] {
					t.Fatalf("status %s (result %s), want %s", after.Status, r.To, w[0])
				}
				if closed := Status(after.Status).Closed(); closed != !after.ClosedAt.IsZero() {
					t.Fatalf("closed_at %v for status %s", after.ClosedAt, after.Status)
				}
				// Wirkung auf die Freigabe.
				var a store.Approval
				if after.ApprovalID > 0 {
					a, _ = e.st.Approval(ctx, after.ApprovalID)
				}
				wantDecision := map[string]string{"create": DecisionPending, "approve": DecisionApproved, "reject": DecisionRejected,
					"supersede": DecisionSuperseded, "expire": DecisionExpired}[w[1]]
				switch {
				case w[1] == "close_open" && before != nil && before.Decision == DecisionPending:
					wantDecision = DecisionSuperseded
				case w[1] == "close_open" && before != nil:
					wantDecision = before.Decision
				case w[1] == "" && before != nil:
					wantDecision = before.Decision
				}
				if a.Decision != wantDecision {
					t.Fatalf("approval decision %q, want %q (effect %q)", a.Decision, wantDecision, w[1])
				}
			})
		}
	}
}

// Akzeptanz S2 Punkt 5 und E7: Änderung nach der Freigabe → draft,
// Freigabe superseded, Freigeben der alten Freigabe approval_stale. Eine
// Änderung ohne Wirkung (nur andere Reihenfolge der Empfänger) lässt ready.
func TestUpdateAfterApprovalSupersedes(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.reach(StatusReady)
	hash := d.PreviewHash
	c := content(canarySubject)
	c.To = []store.Addr{c.To[1], c.To[0]}
	c.To[1].Address = strings.ToUpper(c.To[1].Address)
	r, err := e.svc.Update(ctx, d.ID, c, "")
	if err != nil || r.Changed || r.Draft.Status != string(StatusReady) || r.Draft.PreviewHash != hash {
		t.Fatalf("reordered recipients: %+v, %v", r, err)
	}
	r, err = e.svc.Update(ctx, d.ID, content(canarySubject+"!"), "")
	if err != nil || !r.Changed || r.To != StatusDraft || r.Draft.PreviewHash == hash || r.Draft.HookResult != "" {
		t.Fatalf("update after approval: %+v, %v", r, err)
	}
	if a, _ := e.st.Approval(ctx, d.ApprovalID); a.Decision != DecisionSuperseded {
		t.Fatalf("approval = %s, want superseded", a.Decision)
	}
	if _, err := e.svc.Approve(ctx, d.ApprovalID, "Demo Operator", testMessageHash); ReasonOf(err) != ReasonApprovalStale {
		t.Fatalf("approve old approval: %v, want approval_stale", err)
	}
	// Neu vorlegen und freigeben geht.
	r, err = e.svc.RequestReady(ctx, d.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash})
	if err != nil || r.Approval == nil || r.Approval.ID == d.ApprovalID || r.Approval.PreviewHash != r.Draft.PreviewHash {
		t.Fatalf("request again: %+v, %v", r, err)
	}
	if r, err := e.svc.Approve(ctx, r.Approval.ID, "Demo Operator", testMessageHash); err != nil || r.To != StatusReady ||
		r.Approval.DecidedBy != "Demo Operator" || !r.Approval.ExpiresAt.Equal(e.now.Add(DefaultApprovalTTL)) {
		t.Fatalf("approve new: %+v, %v", r, err)
	}
}

// Ein gescheiterter Prüf-Hook lässt den Entwurf in draft, ohne Freigabe.
func TestRequestReadyHookFailure(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.create()
	for _, h := range []string{HookFail, HookTimeout, HookError} {
		r, err := e.svc.RequestReady(ctx, d.ID, "key:agent", h, Checked{MessageHash: testMessageHash})
		if err != nil || r.To != StatusDraft || r.Approval != nil || r.Draft.HookResult != h || r.Draft.ApprovalID != 0 {
			t.Fatalf("hook %s: %+v, %v", h, r, err)
		}
	}
	if _, err := e.svc.RequestReady(ctx, d.ID, "key:agent", "maybe", Checked{MessageHash: testMessageHash}); err == nil {
		t.Fatal("unknown hook result must fail")
	}
}

// E7: Ablauf nach approval_ttl. Lesen setzt pending_approval und ready nach
// draft zurück (Freigabe expired), mit Audit-Eintrag (system,
// approval_expired, policy_version) ohne Inhalt; Freigeben nach Ablauf
// liefert approval_expired und speichert den Rückfall.
func TestApprovalExpiry(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.svc.ApprovalTTL = time.Hour
	ready := e.reach(StatusReady)
	pending := e.reach(StatusPendingApproval)
	other := e.reach(StatusPendingApproval)

	e.now = e.now.Add(59 * time.Minute)
	if d, err := e.svc.Get(ctx, ready.ID); err != nil || d.Status != string(StatusReady) {
		t.Fatalf("before expiry: %s, %v", d.Status, err)
	}
	e.now = e.now.Add(time.Minute)
	if d, err := e.svc.Get(ctx, ready.ID); err != nil || d.Status != string(StatusDraft) {
		t.Fatalf("ready after expiry: %s, %v", d.Status, err)
	}
	if a, _ := e.st.Approval(ctx, ready.ApprovalID); a.Decision != DecisionExpired {
		t.Fatalf("approval = %s", a.Decision)
	}
	// Get hat alle abgelaufenen des Kontos zurückgesetzt, List ebenso.
	list, err := e.svc.List(ctx, store.DraftFilter{AccountID: e.acc, Status: []string{string(StatusPendingApproval)}})
	if err != nil || len(list) != 0 {
		t.Fatalf("pending after expiry: %d, %v", len(list), err)
	}
	if _, err := e.svc.Approve(ctx, pending.ApprovalID, "Demo Operator", testMessageHash); ReasonOf(err) != ReasonApprovalStale {
		t.Fatalf("approve expired (already rolled back): %v", err)
	}

	// Freigeben genau beim Ablauf, ohne vorheriges Lesen.
	e2 := newEnv(t)
	e2.svc.ApprovalTTL = time.Hour
	p := e2.reach(StatusPendingApproval)
	e2.now = e2.now.Add(time.Hour)
	r, err := e2.svc.Approve(ctx, p.ApprovalID, "Demo Operator", testMessageHash)
	if ReasonOf(err) != ReasonApprovalExpired || r.Draft.Status != string(StatusDraft) {
		t.Fatalf("approve at expiry: %+v, %v", r.Draft.Status, err)
	}
	if d, _ := e2.st.Draft(ctx, p.ID); d.Status != string(StatusDraft) {
		t.Fatalf("rollback to draft not stored: %s", d.Status)
	}

	recs, err := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "drafts.expire"})
	if err != nil || len(recs) != 3 {
		t.Fatalf("expire audit events: %d, %v", len(recs), err)
	}
	ids := map[string]bool{}
	for _, r := range recs {
		if r.ActorType != "system" || r.Reason != ReasonApprovalExpired || r.PolicyVersion != 4 || r.KeyID != "system" {
			t.Errorf("audit event = %+v", r)
		}
		ids[r.Targets[0]] = true
	}
	for _, d := range []store.Draft{ready, pending, other} {
		if !ids[fmt.Sprintf("draft:%d", d.ID)] {
			t.Errorf("no expire event for draft %d", d.ID)
		}
	}
	e.noContentInAudit()
}

func (e *env) noContentInAudit() {
	e.t.Helper()
	recs, err := e.st.AuditQuery(context.Background(), store.AuditFilter{})
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range recs {
		s := fmt.Sprintf("%+v %s", r, r.ParamsRedacted)
		for _, c := range []string{canarySubject, canaryBody, canaryAddr} {
			if strings.Contains(s, c) {
				e.t.Errorf("audit contains draft content %q: %s", c, s)
			}
		}
	}
}

// Anlegen: Absender aus der Standardidentität (Kopie), sonst Kontoadresse
// mit Hinweis; fremde Identität identity_mismatch; Message-ID mit der
// Domain des Absenders; die Beschreibung der Freigabe ohne Inhalt.
func TestCreateIdentityAndMessageID(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	r, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: content(canarySubject)})
	if err != nil || r.Draft.FromEmail != "demo@example.org" || r.Draft.FromName != "" || r.Draft.IdentityID != 0 ||
		len(r.Notes) != 1 || r.Notes[0] != NoteNoIdentity {
		t.Fatalf("without identity: %+v, %v", r, err)
	}
	if !strings.HasPrefix(r.Draft.MessageIDHeader, "pk.") || !strings.HasSuffix(r.Draft.MessageIDHeader, "@example.org") {
		t.Fatalf("message id = %q", r.Draft.MessageIDHeader)
	}
	if r.Draft.RemoteState != RemotePending || r.Draft.Status != string(StatusDraft) || r.Draft.PreviewHash != PreviewHash(HashOf(r.Draft)) {
		t.Fatalf("new draft = %+v", r.Draft)
	}
	idID, err := e.st.AddIdentity(ctx, store.Identity{AccountID: e.acc, Name: "Demo Büro", Email: "buero@example.net"})
	if err != nil {
		t.Fatal(err)
	}
	r, err = e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindReply, Content: content(canarySubject)})
	if err != nil || r.Draft.IdentityID != idID || r.Draft.FromName != "Demo Büro" || r.Draft.FromEmail != "buero@example.net" ||
		!strings.HasSuffix(r.Draft.MessageIDHeader, "@example.net") || len(r.Notes) != 0 {
		t.Fatalf("default identity: %+v, %v", r.Draft, err)
	}
	other, err := e.st.CreateAccount(ctx, store.Account{Name: "zweit", Email: "zweit@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "z", AuthType: "password", CredentialRef: "account/0a1b2c3d/zweit"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Create(ctx, NewDraft{AccountID: other, IdentityID: idID, Kind: KindNew}); ReasonOf(err) != ReasonIdentityMismatch {
		t.Fatalf("foreign identity: %v", err)
	}
	if _, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: "draft"}); err == nil {
		t.Fatal("unknown kind must fail")
	}
	if _, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: Content{To: []store.Addr{{Address: "x"}}}}); ReasonOf(err) != ReasonInvalidAddress {
		t.Fatalf("invalid address: %v", err)
	}
	// Zwei Entwürfe, zwei Message-IDs.
	a, b := e.create(), e.create()
	if a.MessageIDHeader == b.MessageIDHeader {
		t.Fatal("message ids must differ")
	}
	// Beschreibung der Freigabe: Konto, Art, Zähler; kein Inhalt.
	rr, err := e.svc.RequestReady(ctx, a.ID, "key:agent", HookPass, Checked{MessageHash: testMessageHash})
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("draft %d (new) in account demo: 2 recipients, 0 attachments", a.ID)
	if rr.Approval.Description != want || rr.Approval.RequestedBy != "key:agent" {
		t.Fatalf("approval = %+v", rr.Approval)
	}
	own, err := e.svc.OwnAddresses(ctx, e.acc)
	if err != nil || strings.Join(own, ",") != "demo@example.org,buero@example.net" {
		t.Fatalf("own addresses = %v, %v", own, err)
	}
}

// E6: Hat der Mensch den Entwurf im Client übernommen, ist jede Änderung
// draft_taken_over; der Entwurf bleibt, wie er ist.
func TestUpdateTakenOver(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.create()
	if err := e.st.DraftTx(ctx, func(x *store.DraftTx) error {
		d.RemoteState = RemoteTakenOver
		return x.UpdateDraft(d)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Update(ctx, d.ID, content("neu"), ""); ReasonOf(err) != ReasonDraftTakenOver {
		t.Fatalf("update taken over: %v", err)
	}
	// Push nach Änderung: eine gepushte Kopie wird wieder pending.
	d.RemoteState = RemotePushed
	_ = e.st.DraftTx(ctx, func(x *store.DraftTx) error { return x.UpdateDraft(d) })
	r, err := e.svc.Update(ctx, d.ID, content("neu"), "")
	if err != nil || r.Draft.RemoteState != RemotePending {
		t.Fatalf("update pushed: %+v, %v", r.Draft, err)
	}
}

// Mehrere Prozesse: zwei Dienste auf derselben Datei ändern gleichzeitig;
// kurze Transaktionen mit BEGIN IMMEDIATE warten aufeinander statt mit
// SQLITE_BUSY zu scheitern, kein Übergang geht verloren.
func TestConcurrentWritersOnOneFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pk.db")
	open := func() *store.SQLite {
		st, err := store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		if err := st.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		return st
	}
	e := setupEnv(t, open())
	other := &Service{Store: open(), Now: e.svc.Now, Rand: zeroRand{}}
	d := e.create()
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i, svc := range []*Service{e.svc, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, err := svc.Update(ctx, d.ID, content(fmt.Sprintf("v%d-%d", i, j)), ""); err != nil {
					errs <- err
				}
				if _, err := svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew}); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	list, err := e.st.ListDrafts(ctx, store.DraftFilter{AccountID: e.acc})
	if err != nil || len(list) != 41 {
		t.Fatalf("drafts = %d, %v", len(list), err)
	}
}

// Fehlermeldungen nennen IDs und Gründe, nie Inhalt.
func TestErrorsWithoutContent(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	d := e.reach(StatusAbandoned)
	var msgs bytes.Buffer
	for _, ev := range Events {
		if _, err := e.fire(d, ev); err != nil {
			msgs.WriteString(err.Error() + "\n")
		}
	}
	_, err := e.svc.Create(ctx, NewDraft{AccountID: e.acc, Kind: KindNew, Content: Content{To: []store.Addr{{Address: canaryAddr + " extra"}},
		Subject: canarySubject}})
	msgs.WriteString(err.Error())
	for _, c := range []string{canarySubject, canaryBody, canaryAddr} {
		if strings.Contains(msgs.String(), c) {
			t.Errorf("errors contain %q:\n%s", c, msgs.String())
		}
	}
	e.noContentInAudit()
}
