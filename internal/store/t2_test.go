package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Migration 0020 an Daten des Stands 19: Jobs mit Herkunft bleiben, Tags
// mit gültigen Namen bleiben, updated_by alter Entwürfe kommt aus dem
// Audit (letztes drafts.update), sonst bleibt NULL; der neue Job-Typ geht.
func TestMigration0020(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO jobs(type,payload,run_at,key_id,audit_id) VALUES ('draft_push','{"account_id":1}',5,'key:agent',7)`,
		`INSERT INTO messages(id,account_id,subject,created_at) VALUES (5,1,'Hallo',1)`,
		`INSERT INTO mailboxes(id,account_id,path,name,role) VALUES (1,1,'INBOX','INBOX','inbox'),
		        (2,1,'[Gmail]/Wichtig','Wichtig','custom'), (3,1,'[Google Mail]/Starred','Starred','custom'), (4,1,'Wichtig','Wichtig','custom')`,
		`INSERT INTO tags(id,account_id,name,source) VALUES (3,1,'rechnung','local')`,
		`INSERT INTO message_tags(message_id,tag_id,set_by,set_at) VALUES (5,3,'cli',9)`,
		`INSERT INTO drafts(id,account_id,kind,message_id_header,from_email,status,created_by,created_at,updated_at,content_changed_at)
		 VALUES (1,1,'new','pk.1@example.org','demo@example.org','draft','cli',1,1,1),
		        (2,1,'new','pk.2@example.org','demo@example.org','draft','cli',1,1,1)`,
		`INSERT INTO audit_events(ts,actor_type,api_key_id,operation,targets,result) VALUES
		 (1,'agent','key:alt','drafts.update','["draft:1"]','ok'),
		 (2,'agent','key:agent','drafts.update','["draft:1","account:demo"]','started'),
		 (3,'agent','key:dritt','drafts.update','["draft:1"]','denied'),
		 (4,'human','cli','drafts.update','["draft:12"]','ok')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var key, aud string
	if err := s.db.QueryRow(`SELECT key_id, audit_id FROM jobs WHERE type = 'draft_push'`).Scan(&key, &aud); err != nil || key != "key:agent" || aud != "7" {
		t.Fatalf("job origin = %q %q %v", key, aud, err)
	}
	var u1, u2 *string
	_ = s.db.QueryRow(`SELECT updated_by FROM drafts WHERE id = 1`).Scan(&u1)
	_ = s.db.QueryRow(`SELECT updated_by FROM drafts WHERE id = 2`).Scan(&u2)
	if u1 == nil || *u1 != "key:agent" || u2 != nil {
		t.Fatalf("updated_by = %v %v", u1, u2)
	}
	// V13: Bestand der Gmail-Sichten nach Namen gekennzeichnet, nur unter
	// [Gmail]/[Google Mail].
	var virt string
	if err := s.db.QueryRow(`SELECT group_concat(id) FROM (SELECT id FROM mailboxes WHERE virtual_view = 1 ORDER BY id)`).Scan(&virt); err != nil ||
		virt != "2,3" {
		t.Fatalf("virtual views = %q %v", virt, err)
	}
	if tags, err := s.MessageTags(ctx, []int64{5}); err != nil || len(tags[5]) != 1 || tags[5][0] != "rechnung" {
		t.Fatalf("tags = %v %v", tags, err)
	}
	if _, err := s.db.Exec(`INSERT INTO jobs(type,payload,run_at) VALUES ('mailbox_action','{"account_id":1}',1)`); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`INSERT INTO tags(account_id,name) VALUES (1,'Gross')`,
		`INSERT INTO tags(account_id,name) VALUES (1,'mit leer')`,
		`INSERT INTO tags(account_id,name) VALUES (1,'` + strings.Repeat("a", 65) + `')`,
		`INSERT INTO message_tags(message_id,tag_id,set_by,set_at) VALUES (5,3,'human',1)`,
		`INSERT INTO action_targets(action_id,message_id,mailbox_id,uid,uidvalidity) VALUES (1,5,1,0,1)`,
	} {
		if _, err := s.db.Exec(bad); err == nil {
			t.Fatalf("accepted: %s", bad)
		}
	}
}

// Ein Tag, der die Regeln aus E5 nicht erfüllt, hält die Migration auf,
// ohne etwas zu ändern.
func TestMigration0020RejectsInvalidTags(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 19); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO tags(id,account_id,name) VALUES (3,1,'Rechnung')`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	err = s.Migrate(ctx)
	if err == nil || !strings.Contains(err.Error(), "tag rules") {
		t.Fatalf("migrate = %v", err)
	}
	if v, _ := s.SchemaVersion(ctx); v != 19 {
		t.Fatalf("schema = %d", v)
	}
}

// actionEnv: Konto mit INBOX (UIDVALIDITY 7), Archiv (8), Papierkorb (9)
// und zwei Nachrichten in INBOX (UID 1 ungelesen, UID 2 gelesen).
func actionEnv(t *testing.T) (s *SQLite, acct, inbox, archiv, trash int64) {
	t.Helper()
	s = openMem(t)
	ctx := context.Background()
	acct = newAccount(t, s)
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv", Name: "Archiv", Role: "archive"}, {Path: "Trash", Name: "Trash", Role: "trash"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, acct)
	for _, b := range boxes {
		switch b.Path {
		case "INBOX":
			inbox = b.ID
		case "Archiv":
			archiv = b.ID
		case "Trash":
			trash = b.ID
		}
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, New: []NewMessage{newMsg(1, "a@example.org", false),
		newMsg(2, "b@example.org", true)}, State: &MailboxState{UIDValidity: 7, UIDNext: 3, Phase: PhaseReady, Total: 2}}); err != nil {
		t.Fatal(err)
	}
	for id, uv := range map[int64]int{archiv: 8, trash: 9} {
		if _, err := s.db.Exec(`UPDATE mailboxes SET uidvalidity = ? WHERE id = ?`, uv, id); err != nil {
			t.Fatal(err)
		}
	}
	return s, acct, inbox, archiv, trash
}

func flagSeen(t *testing.T, s *SQLite, mailbox int64, uid int) (seen int, pending bool) {
	t.Helper()
	var p *string
	if err := s.db.QueryRow(`SELECT flag_seen, pending_flags FROM message_mailboxes WHERE mailbox_id = ? AND uid = ?`, mailbox, uid).Scan(&seen, &p); err != nil {
		t.Fatal(err)
	}
	return seen, p != nil
}

// mark wirkt sofort im Spiegel (pending), ein Sync mit altem Serverstand
// überschreibt es nicht, solange der Job aussteht; scheitert der Job, gilt
// wieder der Wert davor; gelingt er, fällt die Markierung.
func TestActionMarkPendingFlags(t *testing.T) {
	ctx := context.Background()
	s, acct, inbox, _, _ := actionEnv(t)
	yes := true
	mark := func() Action {
		a, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionMark, State: ActionQueued, IDsHash: "h", RequestedBy: "key:agent",
			Params: ActionParams{Mark: &MarkParams{Seen: &yes}}, Targets: []ActionTarget{{MessageID: 1, MailboxID: inbox, UID: 1, UIDValidity: 7}}})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a := mark()
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 1 || !pending {
		t.Fatalf("after mark: seen=%d pending=%v", seen, pending)
	}
	var unread int
	_ = s.db.QueryRow(`SELECT unread FROM mailboxes WHERE id = ?`, inbox).Scan(&unread)
	if unread != 0 {
		t.Fatalf("unread = %d", unread)
	}
	var jobs int
	_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action' AND state = 'queued' AND key_id = 'key:agent'`).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d", jobs)
	}
	// Sync mit dem alten Stand des Servers: lokale Änderung bleibt.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, Flags: []LinkFlags{{UID: 1, Seen: false}}}); err != nil {
		t.Fatal(err)
	}
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 1 || !pending {
		t.Fatalf("after sync: seen=%d pending=%v", seen, pending)
	}
	if st, err := s.FinishAction(ctx, a.ID, []TargetResult{{MessageID: 1, MailboxID: inbox, Result: TargetFailed}}, "", time.Now()); err != nil || st != ActionFailed {
		t.Fatalf("finish = %s %v", st, err)
	}
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 0 || pending {
		t.Fatalf("after failure: seen=%d pending=%v", seen, pending)
	}
	a = mark()
	if st, err := s.FinishAction(ctx, a.ID, []TargetResult{{MessageID: 1, MailboxID: inbox, Result: TargetOK}}, "", time.Now()); err != nil || st != ActionDone {
		t.Fatalf("finish = %s %v", st, err)
	}
	if seen, pending := flagSeen(t, s, inbox, 1); seen != 1 || pending {
		t.Fatalf("after success: seen=%d pending=%v", seen, pending)
	}
	// Danach gewinnt wieder der Server.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: inbox, Flags: []LinkFlags{{UID: 1, Seen: false}}}); err != nil {
		t.Fatal(err)
	}
	if seen, _ := flagSeen(t, s, inbox, 1); seen != 0 {
		t.Fatalf("server lost: seen=%d", seen)
	}
}

// Antrag auf Papierkorb: requested mit Freigabe, nichts im Spiegel, kein
// Job; nach der Freigabe queued mit Job; ein Ziel, dessen Kopie sich
// verändert hat, wird stale; Verschieben trägt die Kopie im Spiegel um.
func TestActionRequestDecideAndMove(t *testing.T) {
	ctx := context.Background()
	s, acct, inbox, _, trash := actionEnv(t)
	now := time.Now()
	a, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionTrash, State: ActionRequested, IDsHash: "h", RequestedBy: "key:agent",
		Params: ActionParams{TargetMailboxID: trash, TargetRole: "trash"}, Now: now,
		Targets:  []ActionTarget{{MessageID: 1, MailboxID: inbox, UID: 1, UIDValidity: 7}, {MessageID: 2, MailboxID: inbox, UID: 2, UIDValidity: 7}},
		Approval: &Approval{Description: "trash 2 messages", RequestedBy: "key:agent", RequestedAt: now, PreviewHash: "h", ExpiresAt: now.Add(time.Hour)}})
	if err != nil || a.State != ActionRequested || a.ApprovalID == 0 || len(a.Targets) != 2 || a.Targets[0].Mailbox != "INBOX" {
		t.Fatalf("create = %+v %v", a, err)
	}
	var jobs int
	_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action'`).Scan(&jobs)
	if jobs != 0 {
		t.Fatalf("job before approval")
	}
	// Nachricht 2 hat seither eine andere UID (der Server hat neu nummeriert).
	if _, err := s.db.Exec(`UPDATE message_mailboxes SET uid = 5 WHERE message_id = 2`); err != nil {
		t.Fatal(err)
	}
	a, err = s.DecideAction(ctx, ActionDecision{ActionID: a.ID, Approve: true, DecidedBy: "Testperson", Now: now, KeyID: "cli"})
	if err != nil || a.State != ActionQueued || a.Targets[1].Result != TargetStale || a.Targets[0].Result != "" {
		t.Fatalf("decide = %+v %v", a, err)
	}
	if _, err := s.DecideAction(ctx, ActionDecision{ActionID: a.ID, Approve: true, Now: now}); err == nil {
		t.Fatal("second decision accepted")
	}
	_ = s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'mailbox_action' AND key_id = 'cli'`).Scan(&jobs)
	if jobs != 1 {
		t.Fatalf("jobs = %d", jobs)
	}
	q, err := s.QueuedActions(ctx, acct)
	if err != nil || len(q) != 1 {
		t.Fatalf("queued = %+v %v", q, err)
	}
	st, err := s.FinishAction(ctx, a.ID, []TargetResult{{MessageID: 1, MailboxID: inbox, Result: TargetOK, DestMailboxID: trash, DestUIDValidity: 9, NewUID: 33}}, "", now)
	if err != nil || st != ActionPartial {
		t.Fatalf("finish = %s %v", st, err)
	}
	cs, err := s.ServerCopies(ctx, 1)
	if err != nil || len(cs) != 1 || cs[0].MailboxID != trash || cs[0].UID != 33 || !cs[0].ContentLoaded {
		t.Fatalf("copies = %+v %v", cs, err)
	}
}

// Ein Antrag verfällt nach seiner Frist, mit Systemereignis im Audit; ein
// verfallener Antrag lässt sich nicht mehr freigeben.
func TestActionExpire(t *testing.T) {
	ctx := context.Background()
	s, acct, inbox, _, trash := actionEnv(t)
	now := time.Now()
	a, err := s.CreateAction(ctx, NewAction{AccountID: acct, Kind: ActionTrash, State: ActionRequested, IDsHash: "h", RequestedBy: "cli",
		Params: ActionParams{TargetMailboxID: trash}, Now: now, Targets: []ActionTarget{{MessageID: 1, MailboxID: inbox, UID: 1, UIDValidity: 7}},
		Approval: &Approval{Description: "x", RequestedBy: "cli", RequestedAt: now, PreviewHash: "h", ExpiresAt: now.Add(time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ExpireActions(ctx, now); err != nil || n != 0 {
		t.Fatalf("early expire = %d %v", n, err)
	}
	if n, err := s.ExpireActions(ctx, now.Add(2*time.Minute)); err != nil || n != 1 {
		t.Fatalf("expire = %d %v", n, err)
	}
	b, _ := s.Action(ctx, a.ID)
	ap, _ := s.Approval(ctx, a.ApprovalID)
	if b.State != ActionExpired || ap.Decision != "expired" {
		t.Fatalf("state %s / %s", b.State, ap.Decision)
	}
	var ev int
	_ = s.db.QueryRow(`SELECT count(*) FROM audit_events WHERE operation = 'actions.expire' AND actor_type = 'system'`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("audit = %d", ev)
	}
	if _, err := s.DecideAction(ctx, ActionDecision{ActionID: a.ID, Approve: true, Now: now}); err == nil {
		t.Fatal("expired request approved")
	}
}

func TestTags(t *testing.T) {
	ctx := context.Background()
	s, acct, _, _, _ := actionEnv(t)
	r, err := s.ChangeTags(ctx, TagChange{AccountID: acct, MessageIDs: []int64{1, 2}, Add: []string{"rechnung", "a.b"}, SetBy: "key:nacht"})
	if err != nil || r.Added != 4 || len(r.Tags[1]) != 2 || r.Tags[1][0] != "a.b" {
		t.Fatalf("add = %+v %v", r, err)
	}
	r, err = s.ChangeTags(ctx, TagChange{AccountID: acct, MessageIDs: []int64{1}, Remove: []string{"rechnung", "fehlt"}, SetBy: "cli"})
	if err != nil || r.Removed != 1 || len(r.Tags[1]) != 1 {
		t.Fatalf("remove = %+v %v", r, err)
	}
	tl, err := s.ListTags(ctx, acct)
	if err != nil || len(tl) != 2 || tl[1].Name != "rechnung" || tl[1].Messages != 1 {
		t.Fatalf("list = %+v %v", tl, err)
	}
	if _, err := s.ChangeTags(ctx, TagChange{AccountID: acct, MessageIDs: []int64{1}, Add: []string{"Gross"}, SetBy: "cli"}); err == nil {
		t.Fatal("invalid tag name accepted")
	}
	// Tags überleben das Löschen auf dem Server.
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: 1, Removed: []uint32{2}}); err != nil {
		t.Fatal(err)
	}
	if tags, _ := s.MessageTags(ctx, []int64{2}); len(tags[2]) != 2 {
		t.Fatalf("tags after removal = %v", tags)
	}
}

// Laufzeit der Migration 0020 an der synthetischen Datenbank (Rahmen 4),
// ausgehend von Schema 19; nur mit POSTKAUZ_MEASURE_MIGRATION=1.
func TestMeasureMigration0020(t *testing.T) {
	if os.Getenv("POSTKAUZ_MEASURE_MIGRATION") == "" {
		t.Skip("set POSTKAUZ_MEASURE_MIGRATION=1 to measure migration 0020")
	}
	s, path := measureDB(t, 19)
	defer s.Close()
	took, links := measureFillAndMigrate(t, s, 20)
	fi, _ := os.Stat(path)
	t.Logf("migration 0020 (incl. snapshot): %s; messages 50000, links %d, audit 20000; db %d bytes; snapshot %s", took, links, fi.Size(), filepath.Base(s.LastSnapshot))
}

// V13: Der Ordnerabgleich setzt und löscht die Kennzeichnung; ServerCopies
// liefert sie.
func TestSyncMailboxesVirtualView(t *testing.T) {
	ctx := context.Background()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	acct, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "u", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	virtualOf := func() map[string]bool {
		out := map[string]bool{}
		rows, err := s.db.Query(`SELECT path, virtual_view FROM mailboxes WHERE account_id = ?`, acct)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var p string
			var v bool
			if err := rows.Scan(&p, &v); err != nil {
				t.Fatal(err)
			}
			out[p] = v
		}
		return out
	}
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Role: "inbox"}, {Path: "Flagged", Role: "custom", Virtual: true}}); err != nil {
		t.Fatal(err)
	}
	if v := virtualOf(); v["INBOX"] || !v["Flagged"] {
		t.Fatalf("after first sync: %v", v)
	}
	if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: acct, MailboxID: 2, New: []NewMessage{{Link: LinkFlags{UID: 3}, MessageID: "a@example.org",
		Subject: "x", Size: 1, ContentHash: "h"}}, State: &MailboxState{UIDValidity: 5, UIDNext: 4, Phase: PhaseReady, Total: 1}}); err != nil {
		t.Fatal(err)
	}
	if cs, err := s.ServerCopies(ctx, 1); err != nil || len(cs) != 1 || !cs[0].Virtual {
		t.Fatalf("copies = %+v %v", cs, err)
	}
	if _, _, err := s.SyncMailboxes(ctx, acct, []MailboxSpec{{Path: "INBOX", Role: "inbox"}, {Path: "Flagged", Role: "custom"}}); err != nil {
		t.Fatal(err)
	}
	if v := virtualOf(); v["Flagged"] {
		t.Fatalf("after second sync: %v", v)
	}
}
