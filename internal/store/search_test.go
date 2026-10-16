package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/search"
)

// searchFixture: Konto mit INBOX und Archiv, sieben Nachrichten, Index
// aufgebaut.
type searchFixture struct {
	s      *SQLite
	acct   int64
	inbox  int64
	archiv int64
	ids    map[string]int64 // Message-ID → lokale ID
}

func indexAll(t *testing.T, s *SQLite, acct int64) int {
	t.Helper()
	st := search.NewStemmer()
	total := 0
	for {
		n, err := s.IndexBatch(context.Background(), acct, 3, st.Build)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return total
		}
		total += n
	}
}

func newSearchFixture(t *testing.T) *searchFixture {
	t.Helper()
	s := openMem(t)
	ctx := context.Background()
	f := &searchFixture{s: s, acct: newAccount(t, s), ids: map[string]int64{}}
	if _, _, err := s.SyncMailboxes(ctx, f.acct, []MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"},
		{Path: "Archiv/2024", Name: "2024", Role: "custom"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := s.Mailboxes(ctx, f.acct)
	for _, b := range boxes {
		if b.Path == "INBOX" {
			f.inbox = b.ID
		} else {
			f.archiv = b.ID
		}
	}
	day := func(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 10, 0, 0, 0, time.UTC) }
	msgs := []struct {
		mid, subject, from, body string
		date                     time.Time
		box                      int64
		att                      []NewAttachment
	}{
		{"r1", "Offene Rechnungen", "Anna Beispiel <anna@example.org>", "Bitte bis Freitag prüfen.", day(2026, 3, 1), 0, nil},
		{"r2", "Rechnung Gartenpflege", "Bodo Muster <bodo@example.net>", "Bitte pruefen Sie die Summe.", day(2026, 6, 15), 0,
			[]NewAttachment{{PartID: "2", Filename: "Ablesebeleg.txt", ContentType: "text/plain", Size: 10, ExtractStatus: "pending"}}},
		{"z1", "Zwangsvollstreckung aus dem Titel", "Anna Beispiel <anna@example.org>", "Der Gerichtsvollzieher kommt.", day(2024, 5, 2), 1, nil},
		{"e1", "Zählertausch", "Bodo Muster <bodo@example.net>", "Der Zähler im Erdgeschoß links wurde getauscht.", day(2026, 2, 1), 0, nil},
		{"e2", "Wasserhahn", "Anna Beispiel <anna@example.org>", "Links oben, im Erdgeschoss rechts tropft es.", day(2026, 2, 2), 0, nil},
		{"s1", "Grundsteuerbescheid 2026", "Bodo Muster <bodo@example.net>", "Die Kfz-Steuer liegt bei.", day(2026, 1, 10), 0, nil},
		{"s2", "Umsatzsteuer-Voranmeldung", "Anna Beispiel <anna@example.org>", "Abrechnung Wasser", day(2024, 7, 7), 1, nil},
	}
	uid := map[int64]uint32{}
	for _, m := range msgs {
		box := f.inbox
		if m.box == 1 {
			box = f.archiv
		}
		uid[box]++
		nm := NewMessage{Link: LinkFlags{UID: uid[box]}, MessageID: m.mid + "@example.org", Subject: m.subject, From: m.from,
			To: json.RawMessage(`[{"name":"Postfach Test","address":"postfach@example.org"}]`), Size: 100,
			DateHeader: m.date, InternalDate: m.date, ContentHash: "h-" + m.mid, TextPlain: m.body,
			HasAttachments: len(m.att) > 0, Attachments: m.att, Snippet: m.body}
		if _, err := s.ApplyBatch(ctx, MirrorBatch{AccountID: f.acct, MailboxID: box, New: []NewMessage{nm}}); err != nil {
			t.Fatal(err)
		}
		ids, _ := s.MessageIDsByHeader(ctx, f.acct, nm.MessageID)
		f.ids[m.mid] = ids[0]
	}
	return f
}

func (f *searchFixture) find(t *testing.T, q string) []string {
	t.Helper()
	hits, _ := f.page(t, q, 100, nil)
	return hits
}

func (f *searchFixture) page(t *testing.T, q string, limit int, after *search.Cursor) ([]string, SearchResult) {
	t.Helper()
	parsed, err := search.Parse(q, time.UTC)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	res, err := f.s.Search(context.Background(), SearchParams{Query: parsed, AccountID: f.acct, Limit: limit, After: after, Now: now})
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	byID := map[int64]string{}
	for k, v := range f.ids {
		byID[v] = k
	}
	var out []string
	for _, h := range res.Hits {
		out = append(out, byID[h.ID])
	}
	return out, res
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func TestIndexQueueAndSearch(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()
	if n, _ := f.s.IndexPending(ctx, f.acct); n != 7 {
		t.Fatalf("queued = %d, want 7", n)
	}
	jobs, _ := f.s.OpenJobs(ctx)
	if len(jobs) != 2 || jobs[0].Type != JobIndex || jobs[1].Type != JobExtract || jobs[0].AccountID != f.acct {
		t.Fatalf("jobs = %+v", jobs)
	}
	if n := indexAll(t, f.s, f.acct); n != 7 {
		t.Fatalf("indexed %d", n)
	}
	if n, _ := f.s.IndexPending(ctx, f.acct); n != 0 {
		t.Fatalf("still queued: %d", n)
	}
	cases := []struct {
		q    string
		want []string
	}{
		{"Rechnung", []string{"r1", "r2", "s2"}}, // Flexion: Rechnungen; Teilwort: Abrechnung
		{"Rechnungen", []string{"r1", "r2"}},     // Stamm: Rechnung
		{"prüfen", []string{"r1", "r2"}},
		{"pruefen", []string{"r1", "r2"}},
		{"Vollstreck", []string{"z1"}}, // Teilwort im Kompositum
		{"Erdgeschoss", []string{"e1", "e2"}},
		{"Erdgeschoß", []string{"e1", "e2"}},
		{`"Erdgeschoss links"`, []string{"e1"}}, // Phrase: e2 hat „Links … Erdgeschoss rechts"
		{"steuer", []string{"s1", "s2"}},        // Komposita
		{"steuer -grundsteuer", []string{"s2"}},
		{"from:bodo steuer", []string{"s1"}},
		{"to:postfach Zählertausch", []string{"e1"}},
		{"subject:Zähler", []string{"e1"}},
		{"has:attachment", []string{"r2"}},
		{"Ablesebeleg", []string{"r2"}}, // Dateiname des Anhangs
		{"-has:attachment Rechnung", []string{"r1", "s2"}},
		{"mailbox:Archiv", []string{"s2", "z1"}}, // Unterordner Archiv/2024
		{"in:inbox steuer", []string{"s1"}},      // Rolle
		{"after:2026-01-01 before:2026-03-01", []string{"e1", "e2", "s1"}},
		{"ab", []string{"r2", "s2"}}, // kurz: nur Präfix im Wortindex (Abrechnung, Ablesebeleg.txt)
		{"Abrechnung", []string{"s2"}},
		{"is:unread Gartenpflege", []string{"r2"}},
		{"is:read", nil},
		{"Vollstreckung -Titel", nil},
	}
	for _, c := range cases {
		if got := f.find(t, c.q); !slices.Equal(sorted(got), sorted(c.want)) {
			t.Errorf("search %q = %v, want %v", c.q, got, c.want)
		}
	}
	if got := f.find(t, "thread:1"); len(got) != 1 {
		t.Errorf("thread:1 = %v", got)
	}
	// Ranking: Treffer im Betreff vor Treffer nur im Text; ohne Text nach Datum.
	if got := f.find(t, "Zähler"); len(got) != 1 || got[0] != "e1" {
		t.Errorf("Zähler = %v", got)
	}
	// OR ist ein Wort, kein Operator: Alle drei Teile müssen passen (als
	// Operator kämen r2 und z1).
	if got := f.find(t, "Gartenpflege OR Zwangsvollstreckung"); len(got) != 0 {
		t.Errorf("OR is a word, not an operator: %v", got)
	}
	got, res := f.page(t, "mailbox:inbox", 100, nil)
	if !slices.Equal(got, []string{"r2", "r1", "e2", "e1", "s1"}) || res.Total != 5 || res.Hits[0].Account != "demo" ||
		res.Hits[0].Date.IsZero() || !res.Hits[0].HasAttachments {
		t.Errorf("date order = %v (total %d) %+v", got, res.Total, res.Hits[0])
	}
}

func TestSearchCursorPagination(t *testing.T) {
	f := newSearchFixture(t)
	indexAll(t, f.s, f.acct)
	for _, q := range []string{"Erdgeschoss OR steuer", "a", "-xyz", "mailbox:inbox"} {
		all, res := f.page(t, q, 100, nil)
		parsed, _ := search.Parse(q, time.UTC)
		fp := parsed.Fingerprint("")
		var paged []string
		var after *search.Cursor
		for i := 0; i < 20; i++ {
			got, r := f.page(t, q, 2, after)
			if len(got) == 0 {
				break
			}
			if r.Total != res.Total {
				t.Errorf("%q: total %d on page %d, want %d", q, r.Total, i, res.Total)
			}
			paged = append(paged, got...)
			last := r.Hits[len(r.Hits)-1]
			c := search.Cursor{Fingerprint: fp, Now: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).UnixMilli(), Score: last.Score, ID: last.ID}
			after = &c
		}
		if !slices.Equal(paged, all) {
			t.Errorf("%q: pages %v, all %v", q, paged, all)
		}
	}
}

func TestIndexFollowsChangesAndDeletes(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()
	indexAll(t, f.s, f.acct)
	db := f.s.db
	// Extraktion fertig: Text des Anhangs wird durchsuchbar.
	var attID int64
	_ = db.QueryRow(`SELECT id FROM attachments WHERE message_id = ?`, f.ids["r2"]).Scan(&attID)
	if err := f.s.SetExtraction(ctx, attID, "ok", "Wärmemengenzählerablesung Februar", "", false); err != nil {
		t.Fatal(err)
	}
	var reindex int
	if err := db.QueryRow(`SELECT reindex FROM index_queue WHERE message_id = ?`, f.ids["r2"]).Scan(&reindex); err != nil || reindex != 1 {
		t.Fatalf("extraction did not requeue: %v %d", err, reindex)
	}
	// Text geändert (Neuaufbereitung): alter Text verschwindet aus dem Index.
	if _, err := db.Exec(`UPDATE bodies SET text_plain = 'Nur noch Heizung' WHERE message_id = ?`, f.ids["e2"]); err != nil {
		t.Fatal(err)
	}
	if n := indexAll(t, f.s, f.acct); n != 2 {
		t.Fatalf("reindexed %d, want 2", n)
	}
	if got := f.find(t, "mengenzähler"); !slices.Equal(got, []string{"r2"}) {
		t.Errorf("attachment text = %v", got)
	}
	if got := f.find(t, "Erdgeschoss"); !slices.Equal(got, []string{"e1"}) {
		t.Errorf("after body change = %v", got)
	}
	// Gelöschte Nachricht verliert ihre Indexzeilen (Trigger).
	if _, err := db.Exec(`DELETE FROM messages WHERE id = ?`, f.ids["e1"]); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"fts_messages", "fts_trigram"} {
		var n int
		_ = db.QueryRow(`SELECT count(*) FROM ` + tbl + ` WHERE ` + tbl + ` MATCH '"erdgeschoss" OR "zahlertausch"'`).Scan(&n)
		if n != 0 {
			t.Errorf("%s still has %d rows of the deleted message", tbl, n)
		}
	}
	// Flags ändern reiht nicht ein (kein Textbezug).
	if _, err := db.Exec(`UPDATE messages SET flag_seen = 1`); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.s.IndexPending(ctx, 0); n != 0 {
		t.Errorf("flag change queued %d messages", n)
	}
}

func TestEnsureFTSVersionRebuilds(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()
	indexAll(t, f.s, f.acct)
	if v, _ := f.s.FTSVersion(ctx); v != search.FTSVersion {
		t.Fatalf("fts_version = %d", v)
	}
	if again, err := f.s.EnsureFTSVersion(ctx, search.FTSVersion); err != nil || again {
		t.Fatalf("same version rebuilt: %v %v", again, err)
	}
	rebuilt, err := f.s.EnsureFTSVersion(ctx, search.FTSVersion+1)
	if err != nil || !rebuilt {
		t.Fatalf("rebuild = %v %v", rebuilt, err)
	}
	if got := f.find(t, "Rechnung"); len(got) != 0 {
		t.Errorf("index not cleared: %v", got)
	}
	if n, _ := f.s.IndexPending(ctx, f.acct); n != 7 {
		t.Errorf("queued after rebuild = %d", n)
	}
	if n := indexAll(t, f.s, f.acct); n != 7 {
		t.Errorf("reindexed %d", n)
	}
	if got := f.find(t, "Rechnung"); len(got) != 3 {
		t.Errorf("after rebuild = %v", got)
	}
}

func TestJobsClaimLeaseAndComplete(t *testing.T) {
	f := newSearchFixture(t)
	ctx := context.Background()
	s := f.s
	j, ok, err := s.ClaimJob(ctx, JobIndex, f.acct, "w1", time.Minute, false)
	if err != nil || !ok || j.State != "running" {
		t.Fatalf("claim = %+v %v %v", j, ok, err)
	}
	if _, ok, _ := s.ClaimJob(ctx, JobIndex, f.acct, "w2", time.Minute, false); ok {
		t.Fatal("second worker claimed a locked job")
	}
	// Absturz von w1: mit Kontosperre sofort übernehmbar, sonst nach Ablauf.
	if _, ok, _ := s.ClaimJob(ctx, JobIndex, f.acct, "w2", 0, false); !ok {
		t.Fatal("expired lease not taken over")
	}
	if err := s.TouchJob(ctx, j.ID, "w1"); err != ErrJobLost {
		t.Fatalf("touch by old owner = %v", err)
	}
	j2, ok, _ := s.ClaimJob(ctx, JobIndex, f.acct, "w3", time.Hour, true)
	if !ok || j2.ID != j.ID {
		t.Fatal("take over with the account lock failed")
	}
	// Noch Arbeit in der Warteschlange: Complete lässt ihn fällig.
	if done, err := s.CompleteJob(ctx, j2, "w3"); err != nil || done {
		t.Fatalf("complete with queued work = %v %v", done, err)
	}
	indexAll(t, s, f.acct)
	j3, ok, _ := s.ClaimJob(ctx, JobIndex, f.acct, "w3", time.Minute, false)
	if !ok {
		t.Fatal("requeued job not claimable")
	}
	if done, err := s.CompleteJob(ctx, j3, "w3"); err != nil || !done {
		t.Fatalf("complete = %v %v", done, err)
	}
	if _, ok, _ := s.ClaimJob(ctx, JobIndex, f.acct, "w3", time.Minute, false); ok {
		t.Fatal("done job claimed again")
	}
	// Fehler: Wiederholung später, nach maxAttempts endgültig.
	x, ok, _ := s.ClaimJob(ctx, JobExtract, f.acct, "w1", time.Minute, false)
	if !ok {
		t.Fatal("extract job missing")
	}
	if err := s.FailJob(ctx, x.ID, "w1", "boom", 2, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ClaimJob(ctx, JobExtract, f.acct, "w1", time.Minute, false); ok {
		t.Fatal("job claimed before its retry time")
	}
	if _, err := s.db.Exec(`UPDATE jobs SET run_at = 0 WHERE id = ?`, x.ID); err != nil {
		t.Fatal(err)
	}
	x, _, _ = s.ClaimJob(ctx, JobExtract, f.acct, "w1", time.Minute, false)
	_ = s.FailJob(ctx, x.ID, "w1", "boom", 2, time.Now())
	jobs, _ := s.OpenJobs(ctx)
	if len(jobs) != 1 || jobs[0].State != "failed" || jobs[0].Attempts != 2 || jobs[0].LastError != "boom" {
		t.Fatalf("jobs = %+v", jobs)
	}
	// Ein neuer wartender Anhang legt nach dem endgültigen Fehler einen neuen Job an.
	if _, err := s.db.Exec(`UPDATE attachments SET extract_status = 'ok'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE attachments SET extract_status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.ClaimJob(ctx, JobExtract, f.acct, "w1", time.Minute, false); !ok {
		t.Fatal("no new extract job after the failed one")
	}
	if err := s.PruneJobs(ctx, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if jobs, _ := s.OpenJobs(ctx); len(jobs) != 1 || jobs[0].State != "running" {
		t.Fatalf("after prune = %+v", jobs)
	}
}

// Migration 0004 (S1.7) auf einem Bestand von S1.6: Snapshot, alle
// Nachrichten eingereiht, wartende Anhänge mit extract-Job, neue
// FTS-Tabellen.
func TestMigration0004QueuesExistingMessages(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "pk.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.migrateTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO accounts(id,name,email,host,port,username,credential_ref,created_at) VALUES (1,'demo','demo@example.org','127.0.0.1',143,'u','account/demo',1)`,
		`INSERT INTO messages(id,account_id,subject,internal_date,created_at) VALUES (5,1,'Rechnungen',1000,1)`,
		`INSERT INTO messages(id,account_id,subject,internal_date,created_at) VALUES (6,1,'Zwangsvollstreckung',2000,1)`,
		`INSERT INTO attachments(message_id,part_id,content_type,size) VALUES (5,'2','text/plain',3)`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(s.LastSnapshot), "pk.db.pre-0004-") {
		t.Fatalf("snapshot = %q", s.LastSnapshot)
	}
	if _, err := os.Stat(s.LastSnapshot); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.IndexPending(ctx, 1); n != 2 {
		t.Fatalf("queued = %d", n)
	}
	jobs, _ := s.OpenJobs(ctx)
	if len(jobs) != 2 || jobs[0].Type != JobIndex || jobs[1].Type != JobExtract {
		t.Fatalf("jobs = %+v", jobs)
	}
	if v, _ := s.FTSVersion(ctx); v != search.FTSVersion {
		t.Fatalf("fts_version = %d", v)
	}
	if n := indexAll(t, s, 1); n != 2 {
		t.Fatalf("indexed %d", n)
	}
	q, _ := search.Parse("vollstreck", time.UTC)
	res, err := s.Search(ctx, SearchParams{Query: q, Limit: 10, Now: time.Now()})
	if err != nil || len(res.Hits) != 1 || res.Hits[0].ID != 6 {
		t.Fatalf("search after migration = %+v %v", res, err)
	}
}
