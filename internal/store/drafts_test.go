package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
)

// S2.1: Migration 0010 baut drafts, approvals und identities um, erweitert
// jobs um draft_push und ergänzt policies.sha256 und
// audit_events.policy_version. Ein Bestand mit Schema 9 (Stand S2.0)
// behält seine Zeilen; Spalten mit neuem Typ werden übersetzt.
func TestMigration0010FromSchema9(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 9); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at)
			VALUES (1,'demo','demo@example.org','127.0.0.1',143,'demo@example.org','account/0a1b2c3d/demo',1)`,
		`INSERT INTO api_keys(id,name,key_hash,role,scopes,created_at) VALUES (7,'agent','h','assistant','[]',1)`,
		`INSERT INTO messages(id,account_id,message_id_header,subject,created_at) VALUES (3,1,'m3@example.org','Betreff',1)`,
		`INSERT INTO identities(id,account_id,name,email,signature,reply_to,is_default) VALUES (4,1,'Demo','demo@example.org','-- Gruss','antwort@example.org',1)`,
		`INSERT INTO identities(id,account_id,name,email,is_default) VALUES (5,1,'Zweit','zweit@example.org',1)`,
		`INSERT INTO approvals(id,subject_type,subject_id,description,requested_by,requested_at,preview_hash)
			VALUES (2,'draft',6,'draft 6',7,10,'abc')`,
		`INSERT INTO drafts(id,account_id,identity_id,kind,in_reply_to_msg,message_id_header,subject,body_text,status,created_by,
			approval_id,created_at,updated_at) VALUES (6,1,4,'reply',3,'pk.1@example.org','Re: Betreff','Text','pending_approval',7,2,10,20)`,
		`INSERT INTO drafts(id,account_id,kind,message_id_header,status,created_at,updated_at,remote_uid)
			VALUES (8,1,'new','pk.2@example.org','abandoned',10,30,12)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() || KnownSchemaVersion() < 10 {
		t.Fatalf("schema %d, known %d", v, KnownSchemaVersion())
	}
	if s.LastSnapshot == "" || !strings.Contains(s.LastSnapshot, ".pre-0010-") {
		t.Fatalf("snapshot = %q", s.LastSnapshot)
	}

	ids, err := s.Identities(ctx, 1)
	if err != nil || len(ids) != 2 {
		t.Fatalf("identities = %+v, %v", ids, err)
	}
	if i := ids[0]; i.SignatureText != "-- Gruss" || i.ReplyTo != "antwort@example.org" || i.ReplyPrefix != "Re: " ||
		i.ForwardPrefix != "Fwd: " || !i.IsDefault || ids[1].IsDefault {
		t.Fatalf("identity after migration = %+v / %+v (only the first default stays)", i, ids[1])
	}
	a, err := s.Approval(ctx, 2)
	if err != nil || a.RequestedBy != "key:agent" || a.Decision != "pending" || !a.ExpiresAt.IsZero() {
		t.Fatalf("approval = %+v, %v", a, err)
	}
	d, err := s.Draft(ctx, 6)
	if err != nil || d.CreatedBy != "key:agent" || d.RemoteState != "pending" || !d.ClosedAt.IsZero() || d.IdentityID != 4 ||
		d.InReplyToMsg != 3 || d.ApprovalID != 2 || d.BodyText != "Text" || d.HeadersJSON != "{}" {
		t.Fatalf("draft 6 = %+v, %v", d, err)
	}
	d, err = s.Draft(ctx, 8)
	if err != nil || d.ClosedAt != unixMilliUTC(30) || d.RemoteState != "pushed" || d.RemoteUID != 12 {
		t.Fatalf("draft 8 (closed) = %+v, %v", d, err)
	}

	// Neue Regeln greifen, Fremdschlüssel sind wieder an.
	for q, want := range map[string]string{
		`INSERT INTO approvals(subject_type,subject_id,description,requested_at,preview_hash) VALUES ('draft',6,'x',1,'h')`:                    "UNIQUE",
		`INSERT INTO identities(account_id,email,is_default) VALUES (1,'d@example.org',1)`:                                                     "UNIQUE",
		`INSERT INTO drafts(account_id,kind,message_id_header,status,created_at,updated_at) VALUES (99,'new','x@example.org','draft',1,1)`:     "FOREIGN KEY",
		`INSERT INTO drafts(account_id,kind,message_id_header,status,created_at,updated_at) VALUES (1,'new','y@example.org','abandoned',1,1)`:  "CHECK",
		`INSERT INTO drafts(account_id,kind,message_id_header,remote_state,created_at,updated_at) VALUES (1,'new','z@example.org','gone',1,1)`: "CHECK",
		`INSERT INTO approvals(subject_type,subject_id,description,requested_at,preview_hash,decision) VALUES ('draft',8,'x',1,'h','maybe')`:   "CHECK",
		`INSERT INTO policies(document,created_at,sha256) VALUES ('{}',1,'kurz')`:                                                              "CHECK",
	} {
		if _, err := s.db.Exec(q); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %s", q, err, want)
		}
	}
	// jobs: draft_push ist erlaubt, ein offener Job je Art und Konto, und
	// die Trigger aus 0004 schreiben weiter in die neu gebaute Tabelle.
	if _, err := s.db.Exec(`INSERT INTO jobs(type,payload,run_at) VALUES ('draft_push','{"account_id":1}',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO jobs(type,payload,run_at) VALUES ('draft_push','{"account_id":1}',1)`); err == nil {
		t.Error("second open draft_push job for the same account must fail (jobs_open)")
	}
	if _, err := s.db.Exec(`DELETE FROM jobs WHERE type = 'index'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO messages(account_id,message_id_header,created_at) VALUES (1,'neu@example.org',2)`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'index' AND state = 'queued'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("index job from trigger after rebuild: %d, %v", n, err)
	}
	// policy_version im Audit.
	id, err := s.AuditWriter().(audit.IDWriter).AppendID(ctx, audit.Event{TS: time.Now(), ActorType: audit.ActorHuman,
		Operation: "drafts.get", Result: audit.ResultOK, PolicyVersion: 3})
	if err != nil {
		t.Fatal(err)
	}
	recs, err := s.AuditQuery(ctx, AuditFilter{AfterID: id - 1})
	if err != nil || len(recs) != 1 || recs[0].PolicyVersion != 3 {
		t.Fatalf("audit policy_version: %+v, %v", recs, err)
	}
}

// Ein Bestand von 0.1.0-rc1 (Schema 7) läuft in einem Zug bis 10.
func TestMigration0010FromRC1Schema7(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "pk.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at)
		VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO jobs(type,payload,run_at) VALUES ('extract','{"account_id":1}',5)`); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.SchemaVersion(ctx); v != KnownSchemaVersion() {
		t.Fatalf("schema %d", v)
	}
	if _, err := s.DataDirID(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := s.AccountByName(ctx, "demo")
	if err != nil || a.CredentialRef != "account/demo" || len(a.Aliases) != 0 {
		t.Fatalf("account = %+v, %v", a, err)
	}
	var typ string
	var runAt int64
	if err := s.db.QueryRow(`SELECT type, run_at FROM jobs`).Scan(&typ, &runAt); err != nil || typ != "extract" || runAt != 5 {
		t.Fatalf("job kept: %s %d %v", typ, runAt, err)
	}
}

// Identitäten: die erste wird Standard, Standard wechselt, Entfernen der
// Standardidentität macht die älteste übrige zur Standardidentität; eigene
// Adressen umfassen Konto, Anmeldename, Aliasse, Identitäten und Reply-To.
func TestIdentitiesAndOwnAddresses(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "Demo@Example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "login@example.net", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAccountAliases(ctx, acc, []string{"alias@example.org"}); err != nil {
		t.Fatal(err)
	}
	first, err := s.AddIdentity(ctx, Identity{AccountID: acc, Name: "Demo", Email: "demo@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AddIdentity(ctx, Identity{AccountID: acc, Name: "Büro", Email: "Buero@Example.org", ReplyTo: "antwort@example.org"})
	if err != nil {
		t.Fatal(err)
	}
	defaults := func() []int64 {
		ids, err := s.Identities(ctx, acc)
		if err != nil {
			t.Fatal(err)
		}
		var out []int64
		for _, i := range ids {
			if i.IsDefault {
				out = append(out, i.ID)
			}
		}
		return out
	}
	if d := defaults(); len(d) != 1 || d[0] != first {
		t.Fatalf("default = %v, want first %d", d, first)
	}
	if err := s.SetDefaultIdentity(ctx, second); err != nil {
		t.Fatal(err)
	}
	if d := defaults(); len(d) != 1 || d[0] != second {
		t.Fatalf("default = %v, want second %d", d, second)
	}
	own, err := s.OwnAddresses(ctx, acc)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"demo@example.org", "login@example.net", "alias@example.org", "buero@example.org", "antwort@example.org"}
	for _, w := range want {
		if !strings.Contains(strings.Join(own, " "), w) {
			t.Errorf("own addresses %v lack %s", own, w)
		}
	}
	if len(own) != len(want) {
		t.Errorf("own addresses = %v, want %d entries (identity demo@example.org equals the account address)", own, len(want))
	}
	if err := s.RemoveIdentity(ctx, second); err != nil {
		t.Fatal(err)
	}
	if d := defaults(); len(d) != 1 || d[0] != first {
		t.Fatalf("after removing the default: %v, want %d", d, first)
	}
	if _, err := s.Identity(ctx, second); err == nil {
		t.Fatal("identity still there")
	}
}

// S2.3: Migration 0011 ergänzt drafts.compose_options (Standard {}); der
// Job draft_push wird je Konto höchstens einmal offen eingereiht, in der
// Transaktion des Entwurfs.
func TestComposeOptionsAndDraftPushJob(t *testing.T) {
	s := openMem(t)
	ctx := context.Background()
	acc, err := s.CreateAccount(ctx, Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143, TLS: "none",
		Username: "demo@example.org", AuthType: "password", CredentialRef: "account/0a1b2c3d/demo"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	var id, job1, job2 int64
	err = s.DraftTx(ctx, func(x *DraftTx) error {
		var err error
		if id, err = x.InsertDraft(Draft{AccountID: acc, Kind: "new", MessageIDHeader: "pk.1@example.org", Status: "draft",
			CreatedAt: now, UpdatedAt: now, ComposeOptions: `{"quote":false,"signature":true}`}); err != nil {
			return err
		}
		if job1, err = x.EnqueueDraftPush(acc, now, JobOrigin{KeyID: "cli"}); err != nil {
			return err
		}
		job2, err = x.EnqueueDraftPush(acc, now, JobOrigin{KeyID: "cli"})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if job1 == 0 || job1 != job2 {
		t.Fatalf("jobs %d, %d: want one open job", job1, job2)
	}
	d, err := s.Draft(ctx, id)
	if err != nil || d.ComposeOptions != `{"quote":false,"signature":true}` {
		t.Fatalf("draft = %+v, %v", d.ComposeOptions, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM jobs WHERE type = 'draft_push'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("draft_push jobs = %d, %v", n, err)
	}
	// Ohne Angabe: {} (Altbestand vor 0011 und leere Schalter).
	if _, err := s.db.Exec(`INSERT INTO drafts(account_id,kind,message_id_header,status,created_at,updated_at)
		VALUES (?,'new','pk.2@example.org','draft',1,1)`, acc); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListDrafts(ctx, DraftFilter{AccountID: acc})
	if err != nil || len(all) != 2 || all[1].ComposeOptions != "{}" {
		t.Fatalf("list = %+v, %v", all, err)
	}
	if _, err := s.db.Exec(`UPDATE drafts SET compose_options = '[]' WHERE id = ?`, id); err == nil {
		t.Fatal("compose_options accepts a JSON array")
	}
}
