package store

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// A10: Eine wiederhergestellte Datenbank ist eine eigene Instanz. Nichts
// schreibt danach still auf den Server (eingereihte Aktion abgeschlossen,
// Flags wie vorher, Antrag abgelehnt, Jobs failed, wartender Entwurf
// zurückgehalten, Ablageversuch gelöscht), nichts zeigt auf Schlüsselbund
// oder Exportort des Originals, offene Audit-Starts sind abgeschlossen, und
// das Systemereignis steht zuletzt im Audit. Lokale Jobs (Index) bleiben.
func TestPrepareRestored(t *testing.T) {
	ctx := context.Background()
	s, acct, inbox, _, trash := actionEnv(t)
	now := time.Now()
	yes := true
	mark, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionMark, State: ActionQueued, IDsHash: "h", RequestedBy: "key:agent",
		Params: ActionParams{Mark: &MarkParams{Seen: &yes}}, Targets: []ActionTarget{{MessageID: 1, MailboxID: inbox, UID: 1, UIDValidity: 7}}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionTrash, State: ActionRequested, IDsHash: "h", RequestedBy: "key:agent",
		Params: ActionParams{TargetMailboxID: trash, TargetRole: "trash"}, Now: now,
		Targets:  []ActionTarget{{MessageID: 2, MailboxID: inbox, UID: 2, UIDValidity: 7}},
		Approval: &Approval{Description: "trash 1 message", RequestedBy: "key:agent", RequestedAt: now, PreviewHash: "h", ExpiresAt: now.Add(time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 1 || !pending {
		t.Fatalf("seed: seen=%d pending=%v", seen, pending)
	}
	for _, q := range []string{
		`INSERT INTO drafts(id,account_id,kind,message_id_header,from_email,status,created_by,created_at,updated_at,content_changed_at,push_attempt,closed_at)
		 VALUES (1,1,'new','pk.1@example.org','demo@example.org','draft','cli',1,1,1,'{"audit_id":99}',NULL),
		        (2,1,'new','pk.2@example.org','demo@example.org','abandoned','cli',1,1,1,NULL,1)`,
		`INSERT INTO jobs(type,payload,run_at) VALUES ('draft_push','{"account_id":1}',1), ('sync','{"account_id":1}',1), ('index','{}',1)`,
		`INSERT INTO audit_exports(day,first_event_id,last_event_id,sha256,location,exported_at) VALUES ('2026-10-01',1,1,'x','/srv/export',1)`,
		`INSERT INTO audit_events(ts,actor_type,api_key_id,operation,result) VALUES (1,'human','cli','backup.create','started')`,
		`INSERT INTO meta(key,value) VALUES ('datadir_id','0123456789abcdef0123456789abcdef'), ('credential_refs_left','["account/demo"]')
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	newID := strings.Repeat("ab", 16)
	var got RestoreFixes
	fx, err := s.PrepareRestored(ctx, RestorePrep{DataDirID: newID, Now: now,
		CredentialRef: func(id, name string) string { return "account/" + id + "/" + name },
		Event: func(fx RestoreFixes) audit.Event {
			got = fx
			b, _ := json.Marshal(fx)
			return audit.Event{TS: now, ActorType: audit.ActorSystem, KeyID: "system", Operation: "backup.restore", Result: audit.ResultOK, ParamsRedacted: b}
		}})
	if err != nil {
		t.Fatal(err)
	}
	want := RestoreFixes{NewIdentity: true, CredentialRefs: 1, CredentialRefsLeft: 1, AuditExports: 1, Jobs: 3, Actions: 1, Requests: 1,
		DraftsHeld: 1, PushAttempts: 1, AuditStarts: 1}
	if fx != want || got != want {
		t.Fatalf("fixes = %+v (event %+v), want %+v", fx, got, want)
	}
	// Aktion abgeschlossen, Flag wie vor der Aktion, kein Job mehr offen.
	a, _ := s.Action(ctx, mark.ID)
	if a.State != ActionFailed || a.Error != RestoreReason {
		t.Fatalf("mark action = %s %s", a.State, a.Error)
	}
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 0 || pending {
		t.Fatalf("after restore: seen=%d pending=%v", seen, pending)
	}
	r, _ := s.Action(ctx, req.ID)
	ap, err := approvalDecision(s, r.ApprovalID)
	if r.State != ActionRejected || r.Error != RestoreReason || err != nil || ap != "rejected/system/restored" {
		t.Fatalf("request = %s %s, approval %s %v", r.State, r.Error, ap, err)
	}
	var open, index int
	_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE state IN ('queued','running') AND type != 'index'`).Scan(&open)
	_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE state = 'queued' AND type = 'index'`).Scan(&index)
	if open != 0 || index == 0 {
		t.Fatalf("open jobs = %d, index jobs = %d", open, index)
	}
	var hold, attempt *string
	_ = s.db.QueryRow(`SELECT push_hold, push_attempt FROM drafts WHERE id = 1`).Scan(&hold, &attempt)
	if hold == nil || *hold != RestoreReason || attempt != nil {
		t.Fatalf("draft 1: hold %v attempt %v", hold, attempt)
	}
	_ = s.db.QueryRow(`SELECT push_hold FROM drafts WHERE id = 2`).Scan(&hold)
	if hold != nil {
		t.Fatalf("closed draft held: %v", *hold)
	}
	if ids, _, err := s.DraftPushCandidates(ctx, acct, true); err != nil || len(ids) != 0 {
		t.Fatalf("push candidates = %v %v", ids, err)
	}
	// Kennung, Verweise, Merkliste, Exportort.
	if id, _ := s.DataDirIDFull(ctx); id != newID {
		t.Fatalf("datadir_id = %s", id)
	}
	if acc, _ := s.AccountByName(ctx, "demo"); acc.CredentialRef != "account/abababab/demo" {
		t.Fatalf("credential_ref = %s", acc.CredentialRef)
	}
	if left, _ := s.CredentialRefsLeft(ctx); len(left) != 0 {
		t.Fatalf("refs left = %v", left)
	}
	var loc string
	_ = s.db.QueryRow(`SELECT location FROM audit_exports`).Scan(&loc)
	if loc != "" {
		t.Fatalf("export location = %q", loc)
	}
	// Audit: offener Start abgeschlossen, Ereignis zuletzt.
	if n, _ := s.AuditOpenStarts(ctx, now.Add(time.Hour)); n != 0 {
		t.Fatalf("open starts = %d", n)
	}
	var lastOp, reason string
	_ = s.db.QueryRow(`SELECT operation FROM audit_events ORDER BY id DESC LIMIT 1`).Scan(&lastOp)
	_ = s.db.QueryRow(`SELECT reason FROM audit_events WHERE operation = 'backup.create' AND result = 'error'`).Scan(&reason)
	if lastOp != "backup.restore" || reason != RestoreReason {
		t.Fatalf("audit: last %s, closing reason %q", lastOp, reason)
	}

	// --keep-identity: Kennung und Verweise bleiben.
	fx, err = s.PrepareRestored(ctx, RestorePrep{Now: now})
	if err != nil || fx.NewIdentity || fx.CredentialRefs != 0 {
		t.Fatalf("keep identity: %+v %v", fx, err)
	}
	if id, _ := s.DataDirIDFull(ctx); id != newID {
		t.Fatalf("datadir_id changed: %s", id)
	}
	if _, err := s.PrepareRestored(ctx, RestorePrep{DataDirID: "zz"}); err == nil {
		t.Fatal("invalid id accepted")
	}
}

// approvalDecision liest Entscheidung, Entscheider und Grund einer Freigabe.
func approvalDecision(s *SQLite, id int64) (string, error) {
	var d, by, reason string
	err := s.db.QueryRow(`SELECT decision, COALESCE(decided_by,''), COALESCE(reason,'') FROM approvals WHERE id = ?`, id).Scan(&d, &by, &reason)
	return d + "/" + by + "/" + reason, err
}
