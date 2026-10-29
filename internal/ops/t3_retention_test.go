package ops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/audit"
	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/drafts"
	"github.com/KNUFFIXX/postkauz/internal/flock"
	"github.com/KNUFFIXX/postkauz/internal/policy"
	"github.com/KNUFFIXX/postkauz/internal/retention"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Löschkonzept (T3, S3.4 Teil 2, E8, A11, A13.2, A13.3, A14): purge und
// blob gc über den Executor mit der echten Datenbank und einem echten
// Blob-Store.

// retCanary steht in Betreff, Absender und Text der Testnachrichten; es
// darf nie im Audit erscheinen (Kanarientest).
const retCanary = "CANARY-Zaunkoenig-7f3a"

// seedRetention legt für den Vertragstest an: eine Nachricht, die vor
// langer Zeit auf dem Server gelöscht wurde (retention.purge wirkt), und
// eine alte Zeile in blobs ohne Verweis und ohne Datei (blobs.gc wirkt).
// Dazu den Dienst in den Abhängigkeiten.
func seedRetention(t *testing.T, e *contractEnv) {
	t.Helper()
	ctx := context.Background()
	// Eine Transaktion für alle Zeilen: Die Umgebung entsteht je Teiltest
	// neu, und der Vertragstest liegt nahe an der Zeitgrenze von go test.
	// audit.rotate braucht einen alten Tag mit zwei Einträgen, exportiert
	// an einen eigenen Ort (audit.export an den Standardort wirkt dann
	// noch); Datei, Manifest und Nachweis entstehen unten wie beim Export.
	day := time.Date(2020, 1, 10, 0, 0, 0, 0, time.UTC)
	if err := e.st.ExecForTest(`BEGIN;
		INSERT INTO messages(account_id, subject, deleted_remote_at, created_at) VALUES (?1, 'Weg', 1, 1);
		INSERT INTO blobs(sha256, size, created_at) VALUES (?2, 3, 1);
		INSERT INTO audit_events(ts, actor_type, operation, result) VALUES (?3, 'human', 'accounts.list', 'ok'), (?4, 'human', 'accounts.list', 'ok');
		COMMIT;`, e.acct, strings.Repeat("ab", 32), day.Add(9*time.Hour).UnixMilli(), day.Add(10*time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.deps.Retention = &retention.Service{Store: e.st, Blobs: bs, LockDir: filepath.Dir(e.st.Path())}
	e.deps.RetentionConfig = RetentionConfig{BlobGCMinAge: 24 * time.Hour, AuditRetain: 365 * 24 * time.Hour}

	recs, err := e.st.AuditQuery(ctx, store.AuditFilter{Since: day, Until: day.Add(24 * time.Hour)})
	if err != nil || len(recs) != 2 {
		t.Fatalf("old audit day: %d %v", len(recs), err)
	}
	d, err := auditDayContent(recs, 0, AuditExportFormat, auditChainGenesis)
	if err != nil {
		t.Fatal(err)
	}
	dir, name := t.TempDir(), day.Format("2006-01-02")
	md := auditManifestDay{Day: name, File: auditFileName(name), Events: d.events, FirstEventID: d.first, LastEventID: d.last,
		SHA256: sha256Hex(d.content), ChainHead: d.head}
	if err := os.WriteFile(filepath.Join(dir, md.File), d.content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, auditManifestName()), buildManifest([]auditManifestDay{md}, ""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.st.SaveAuditExport(ctx, store.AuditExport{Day: name, FirstEventID: d.first, LastEventID: d.last, SHA256: md.SHA256,
		Location: dir, ExportedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
}

type retEnv struct {
	st    *store.SQLite
	x     *Executor
	svc   *retention.Service
	blobs *blob.Store
	acct  int64
	box   int64
	deps  Deps
}

func newRetEnv(t *testing.T) *retEnv {
	t.Helper()
	ctx := context.Background()
	st := openTestStore(t)
	acct, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "demo@example.org", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acct)
	bs, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &retEnv{st: st, blobs: bs, acct: acct, box: boxes[0].ID,
		svc: &retention.Service{Store: st, Blobs: bs, LockDir: filepath.Dir(st.Path())}}
	e.deps = Deps{Accounts: st, Audit: st, Retention: e.svc, Drafts: &drafts.Service{Store: st},
		RetentionConfig: RetentionConfig{BlobGCMinAge: 24 * time.Hour, AuditRetain: 365 * 24 * time.Hour}}
	reg := NewRegistry()
	for _, op := range []Operation{RetentionPurge(e.deps), BlobsGC(e.deps)} {
		if err := reg.Register(op); err != nil {
			t.Fatal(err)
		}
	}
	e.x = &Executor{Registry: reg, Audit: st.AuditWriter()}
	return e
}

func (e *retEnv) run(t *testing.T, op string, input string, dry bool) (any, error) {
	t.Helper()
	res, err := e.x.Execute(context.Background(), Request{Key: adminKey, Actor: audit.ActorHuman, Operation: op,
		Input: json.RawMessage(input), DryRun: &dry})
	return res.Output, err
}

// put legt Inhalt in den Blob-Store und liefert den Hash.
func (e *retEnv) put(t *testing.T, content string) string {
	t.Helper()
	info, err := e.blobs.Put(strings.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	return info.SHA256
}

// mirror spiegelt eine Nachricht mit Rohmail und einem Anhang (beide im
// Blob-Store) unter uid in INBOX und liefert ihre ID.
func (e *retEnv) mirror(t *testing.T, uid uint32, name string) int64 {
	t.Helper()
	ctx := context.Background()
	raw := e.put(t, "raw "+name)
	att := e.put(t, "attachment "+name)
	if _, err := e.st.ApplyBatch(ctx, store.MirrorBatch{AccountID: e.acct, MailboxID: e.box,
		New: []store.NewMessage{{Link: store.LinkFlags{UID: uid}, MessageID: name + "@example.org", Subject: retCanary + " " + name,
			From: retCanary + " <a@example.org>", Size: 10, BlobSHA256: raw, BlobSize: 10, TextPlain: retCanary + " Text " + name,
			Attachments: []store.NewAttachment{{PartID: "2", Filename: retCanary + ".txt", ContentType: "text/plain", Size: 5, SHA256: att, BlobSize: 5}},
			DateHeader:  time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC)}},
		State: &store.MailboxState{UIDValidity: 3, UIDNext: uid + 1, Phase: store.PhaseReady}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, name+"@example.org")
	return ids[0]
}

// goneFromServer löscht die Kopie unter uid auf dem „Server“ (Sync meldet
// sie als entfernt) und datiert die bemerkte Löschung zurück.
func (e *retEnv) goneFromServer(t *testing.T, id int64, uid uint32, ago time.Duration) {
	t.Helper()
	if _, err := e.st.ApplyBatch(context.Background(), store.MirrorBatch{AccountID: e.acct, MailboxID: e.box, Removed: []uint32{uid}}); err != nil {
		t.Fatal(err)
	}
	if err := e.st.ExecForTest(`UPDATE messages SET deleted_remote_at = ? WHERE id = ? AND deleted_remote_at IS NOT NULL`,
		time.Now().Add(-ago).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
}

func (e *retEnv) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	n, err := e.st.QueryIntForTest(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Akzeptanz 1: purge --dry-run zählt; echt löscht es nur, was die Regeln
// erlauben (auf dem Server gelöscht, älter als die Frist, nicht importiert,
// kein Entwurf verweist darauf, kein lokales Tag). Index, Zuordnungen,
// Anhangszeilen und der leere Thread folgen; das importierte Archiv, die
// Nachricht mit offenem Entwurf und die getaggte bleiben, der
// Entwurfsverweis bleibt als Zahl. Seit der Nachbesserung T3 (Punkt 9,
// E5) folgen Tags nicht mehr, sie halten die Nachricht.
func TestRetentionPurge(t *testing.T) {
	ctx := context.Background()
	e := newRetEnv(t)
	gone := e.mirror(t, 1, "gone")
	recent := e.mirror(t, 2, "recent")
	present := e.mirror(t, 3, "present")
	quoted := e.mirror(t, 4, "quoted")
	tagged := e.mirror(t, 5, "tagged")
	if _, err := e.st.ChangeTags(ctx, store.TagChange{AccountID: e.acct, MessageIDs: []int64{tagged, present}, Add: []string{"rechnung"},
		SetBy: "cli", Now: time.Now()}); err != nil {
		t.Fatal(err)
	}
	// Nur importiert (A13.2): deleted_remote_at = Importzeit, nie auf dem Server.
	if _, err := e.st.ApplyImport(ctx, store.ImportBatch{AccountID: e.acct, New: []store.ImportNew{{
		Msg:     store.NewMessage{MessageID: "archiv@example.org", Subject: retCanary + " archiv", Size: 10, ContentHash: "h-archiv"},
		Folders: []store.ImportFolderFlags{{Folder: "Archiv/2020"}}}}}); err != nil {
		t.Fatal(err)
	}
	ids, _ := e.st.MessageIDsByHeader(ctx, e.acct, "archiv@example.org")
	imported := ids[0]
	if err := e.st.ExecForTest(`UPDATE messages SET deleted_remote_at = ? WHERE id = ?`, time.Now().Add(-400*24*time.Hour).UnixMilli(), imported); err != nil {
		t.Fatal(err)
	}
	e.goneFromServer(t, gone, 1, 100*24*time.Hour)
	e.goneFromServer(t, recent, 2, 5*24*time.Hour)
	e.goneFromServer(t, quoted, 4, 100*24*time.Hour)
	e.goneFromServer(t, tagged, 5, 100*24*time.Hour)
	// Offener Entwurf, der auf „quoted“ antwortet (A14).
	dr, err := e.deps.Drafts.Create(ctx, drafts.NewDraft{AccountID: e.acct, Kind: drafts.KindReply, InReplyToMsg: quoted, CreatedBy: "cli",
		Content: drafts.Content{To: []store.Addr{{Address: "b@example.org"}}, Subject: "Re", BodyText: "Text"}})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := e.st.IndexBatch(ctx, 0, 100, search.NewStemmer().Build); err != nil || n == 0 {
		t.Fatalf("index: %d %v", n, err)
	}
	match := `SELECT (SELECT count(*) FROM fts_messages_docsize WHERE id = ?1) + (SELECT count(*) FROM fts_trigram_docsize WHERE id = ?1)`
	if e.count(t, match, gone) != 2 {
		t.Fatal("test setup: message not indexed")
	}
	thread := e.count(t, `SELECT thread_id FROM messages WHERE id = ?`, gone)
	before := e.count(t, `SELECT count(*) FROM messages`)

	// Trockenlauf: zählt, ändert nichts.
	out, err := e.run(t, "retention.purge", `{"older_than":"30d"}`, true)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(RetentionPurgeOutput)
	if o.GoneFromServer != 4 || o.KeptImported != 1 || o.KeptDrafts != 1 || o.KeptTagged != 1 || o.Eligible != 1 || o.Purged != 0 || !o.DryRun {
		t.Fatalf("dry run = %+v", o)
	}
	if e.count(t, `SELECT count(*) FROM messages`) != before {
		t.Fatal("dry run deleted messages")
	}

	out, err = e.run(t, "retention.purge", `{"older_than":"30d","account":"demo"}`, false)
	if err != nil {
		t.Fatal(err)
	}
	o = out.(RetentionPurgeOutput)
	if o.Purged != 1 || o.KeptTagged != 1 || o.Attachments != 1 || o.ThreadsRemoved != 1 || o.Account != "demo" || o.OlderThan != "30d" {
		t.Fatalf("purge = %+v", o)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM messages WHERE id = ?`:            0,
		`SELECT count(*) FROM bodies WHERE message_id = ?`:      0,
		`SELECT count(*) FROM attachments WHERE message_id = ?`: 0,
		`SELECT count(*) FROM index_queue WHERE message_id = ?`: 0,
	} {
		if n := e.count(t, q, gone); n != want {
			t.Errorf("%s: %d", q, n)
		}
	}
	if e.count(t, match, gone) != 0 {
		t.Error("purged message still in the full-text index")
	}
	if e.count(t, `SELECT count(*) FROM threads WHERE id = ?`, thread) != 0 {
		t.Error("empty thread left behind")
	}
	for _, id := range []int64{recent, present, quoted, imported, tagged} {
		if e.count(t, `SELECT count(*) FROM messages WHERE id = ?`, id) != 1 {
			t.Errorf("message %d was purged", id)
		}
	}
	for _, id := range []int64{present, tagged} {
		if e.count(t, `SELECT count(*) FROM message_tags WHERE message_id = ?`, id) != 1 {
			t.Errorf("tag of message %d lost", id)
		}
	}
	if got, _ := e.st.Draft(ctx, dr.Draft.ID); got.InReplyToMsg != quoted {
		t.Errorf("draft reference = %d, want %d", got.InReplyToMsg, quoted)
	}
	// Blobs bleiben (blob gc räumt sie ab).
	if e.count(t, `SELECT count(*) FROM blobs`) != 2*5 {
		t.Error("purge must not delete blob rows")
	}
	// Die ID wird nicht neu vergeben (0022).
	next := e.mirror(t, 9, "next")
	if next <= gone {
		t.Errorf("new message got id %d after purging %d", next, gone)
	}

	// Audit: Zähler im Abschluss, kein Inhalt.
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "retention.purge"})
	last := evs[len(evs)-1]
	if last.Result != audit.ResultOK || !strings.Contains(string(last.ParamsRedacted), `"purged":1`) ||
		!strings.Contains(string(last.ParamsRedacted), `"older_than":"30d"`) {
		t.Fatalf("audit end entry = %s", last.ParamsRedacted)
	}
	assertNoCanaryInAudit(t, e.st)
}

func assertNoCanaryInAudit(t *testing.T, st *store.SQLite) {
	t.Helper()
	all, err := st.AuditQuery(context.Background(), store.AuditFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		b, _ := json.Marshal(auditEventOut(r))
		if strings.Contains(string(b), retCanary) || strings.Contains(string(b), "example.org") {
			t.Fatalf("content in audit: %s", b)
		}
	}
}

// Ohne Frist (Konfiguration 0 = nie, keine Eingabe) löscht purge nichts und
// sagt, was fehlt; eine Frist aus der Konfiguration gilt ohne Eingabe.
func TestRetentionPurgePeriod(t *testing.T) {
	e := newRetEnv(t)
	if _, err := e.run(t, "retention.purge", `{}`, false); !strings.Contains(fmtErr(err), "no period") {
		t.Fatalf("without period: %v", err)
	}
	var ie *InputError
	if _, err := e.run(t, "retention.purge", `{"older_than":"bald"}`, false); !errors.As(err, &ie) {
		t.Fatalf("bad period: %v", err)
	}
	e.deps.RetentionConfig.DeletedRemoteAfter = 90 * 24 * time.Hour
	reg := NewRegistry()
	_ = reg.Register(RetentionPurge(e.deps))
	e.x.Registry = reg
	out, err := e.run(t, "retention.purge", `{}`, true)
	if err != nil || out.(RetentionPurgeOutput).OlderThan != "90d" {
		t.Fatalf("configured period: %+v %v", out, err)
	}
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// A11: Läuft ein Sync oder Import (Kontosperre), lehnen purge und blob gc
// ab (sync_running), ohne etwas zu löschen; der Trockenlauf geht.
func TestRetentionRefusedWhileSyncRuns(t *testing.T) {
	e := newRetEnv(t)
	gone := e.mirror(t, 1, "gone")
	e.goneFromServer(t, gone, 1, 100*24*time.Hour)
	l, err := flock.TryLock(filepath.Join(filepath.Dir(e.st.Path()), flock.AccountLockName(e.acct)))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	for _, op := range []string{"retention.purge", "blobs.gc"} {
		in := `{}`
		if op == "retention.purge" {
			in = `{"older_than":"1d"}`
		}
		if _, err := e.run(t, op, in, false); !errors.Is(err, ErrSyncRunning) {
			t.Fatalf("%s while a sync runs: %v", op, err)
		}
		if _, err := e.run(t, op, in, true); err != nil {
			t.Fatalf("%s dry run while a sync runs: %v", op, err)
		}
	}
	if e.count(t, `SELECT count(*) FROM messages WHERE id = ?`, gone) != 1 {
		t.Fatal("purge deleted while a sync was running")
	}
}

// Akzeptanz 2 (A11, A13.3, A14): blob gc entfernt nur Dateien ohne Verweis,
// die älter als das Mindestalter sind. Ein frischer Blob ohne Verweis
// bleibt, ein alter geht; Dateianhänge von Entwürfen (ohne Zeile in blobs)
// bleiben immer; ein wieder abgelegter Inhalt bekommt ein neues Datum und
// bleibt; die Rohmail einer gepurgten Nachricht geht samt Zeile.
func TestBlobsGC(t *testing.T) {
	ctx := context.Background()
	e := newRetEnv(t)
	gone := e.mirror(t, 1, "gone")
	keep := e.mirror(t, 2, "keep")
	e.goneFromServer(t, gone, 1, 100*24*time.Hour)
	if _, err := e.run(t, "retention.purge", `{"older_than":"30d"}`, false); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	age := func(h string) {
		t.Helper()
		p, _, err := e.blobs.Path(h)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	rawGone, attGone := sha("raw gone"), sha("attachment gone")
	// Dateianhang eines Entwurfs: nur Datei, keine Zeile (A13.3); alt.
	draftAtt := e.put(t, "datei vom rechner")
	if _, err := e.deps.Drafts.Create(ctx, drafts.NewDraft{AccountID: e.acct, Kind: drafts.KindNew, CreatedBy: "cli",
		Content: drafts.Content{To: []store.Addr{{Address: "b@example.org"}}, Subject: "Neu", BodyText: "Text",
			Attachments: []store.DraftAttachment{{SHA256: draftAtt, Filename: "a.txt", Size: 17}}}}); err != nil {
		t.Fatal(err)
	}
	orphanOld := e.put(t, "alt und verwaist")
	orphanFresh := e.put(t, "frisch und verwaist")
	reused := e.put(t, "wieder abgelegt")
	for _, h := range []string{rawGone, attGone, draftAtt, orphanOld, reused, sha("raw keep"), sha("attachment keep")} {
		age(h)
	}
	// Der Inhalt wird wieder abgelegt (Sync oder Entwurf, A14): neues Datum.
	if again := e.put(t, "wieder abgelegt"); again != reused {
		t.Fatal("hash changed")
	}

	out, err := e.run(t, "blobs.gc", `{}`, true)
	if err != nil {
		t.Fatal(err)
	}
	o := out.(BlobsGCOutput)
	// Unverwiesen: Rohmail und Anhang von „gone“ (Datei und Zeile), drei
	// Waisen; davon zwei frisch.
	if o.Unreferenced != 5 || o.KeptYoung != 2 || o.RemovedFiles != 3 || o.RemovedRows != 2 || !o.DryRun || o.MinAge != "1d" {
		t.Fatalf("dry run = %+v", o)
	}
	for _, h := range []string{rawGone, orphanOld} {
		if !e.blobs.Has(h) {
			t.Fatal("dry run removed a file")
		}
	}

	out, err = e.run(t, "blobs.gc", `{}`, false)
	if err != nil {
		t.Fatal(err)
	}
	o = out.(BlobsGCOutput)
	if o.RemovedFiles != 3 || o.RemovedRows != 2 || o.Skipped != 0 || o.Bytes == 0 {
		t.Fatalf("gc = %+v", o)
	}
	for h, want := range map[string]bool{rawGone: false, attGone: false, orphanOld: false,
		orphanFresh: true, reused: true, draftAtt: true, sha("raw keep"): true, sha("attachment keep"): true} {
		if e.blobs.Has(h) != want {
			t.Errorf("blob %s… present = %v, want %v", h[:8], !want, want)
		}
	}
	if e.count(t, `SELECT count(*) FROM blobs WHERE sha256 IN (?, ?)`, rawGone, attGone) != 0 {
		t.Error("rows of purged blobs left")
	}
	if e.count(t, `SELECT count(*) FROM blobs`) != 2 {
		t.Error("rows of referenced blobs removed")
	}
	_ = keep
	// Ein zweiter Lauf findet nichts Altes mehr.
	out, _ = e.run(t, "blobs.gc", `{}`, false)
	if o := out.(BlobsGCOutput); o.RemovedFiles != 0 || o.RemovedRows != 0 || o.KeptYoung != 2 {
		t.Fatalf("second gc = %+v", o)
	}
	// Mit vorgestellter Uhr gehen auch die frischen Waisen, nie der
	// Entwurfsanhang.
	e.svc.Now = func() time.Time { return time.Now().Add(48 * time.Hour) }
	out, _ = e.run(t, "blobs.gc", `{}`, false)
	if o := out.(BlobsGCOutput); o.RemovedFiles != 2 || !e.blobs.Has(draftAtt) {
		t.Fatalf("gc later = %+v, draft attachment present %v", o, e.blobs.Has(draftAtt))
	}
	evs, _ := e.st.AuditQuery(ctx, store.AuditFilter{Operation: "blobs.gc"})
	if last := evs[len(evs)-1]; !strings.Contains(string(last.ParamsRedacted), `"removed_files":2`) {
		t.Fatalf("audit end entry = %s", last.ParamsRedacted)
	}
	assertNoCanaryInAudit(t, e.st)
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// HumanOnly ohne MCP: Mit Schlüssel (jede Rolle) lehnt die Policy ab, auch
// im Trockenlauf (Vertragstest prüft das für jede Operation; hier direkt).
func TestRetentionOpsHumanOnly(t *testing.T) {
	e := newRetEnv(t)
	for _, op := range []string{"retention.purge", "blobs.gc"} {
		_, err := e.x.Execute(context.Background(), Request{Key: policy.Key{ID: "key:adm", Scopes: policy.Roles["admin"]},
			Actor: audit.ActorAgent, Operation: op, Input: json.RawMessage(`{}`)})
		var pe *PolicyError
		if !errors.As(err, &pe) || pe.Decision.Reason != "human_only" {
			t.Fatalf("%s with a key: %v", op, err)
		}
	}
}
