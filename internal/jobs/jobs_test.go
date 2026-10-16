package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/search"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

type fixture struct {
	st    *store.SQLite
	blobs *blob.Store
	acct  int64
}

// newFixture: ein Konto, n Nachrichten, jede mit einem Textanhang
// (wartet auf Extraktion).
func newFixture(t *testing.T, n int) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	b, err := blob.Open(t.TempDir(), blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	acct, err := st.CreateAccount(ctx, store.Account{Name: "demo", Email: "demo@example.org", Host: "127.0.0.1", Port: 143,
		TLS: "none", Username: "u", AuthType: "password", CredentialRef: "account/demo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.SyncMailboxes(ctx, acct, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acct)
	var batch []store.NewMessage
	for i := 1; i <= n; i++ {
		info, err := b.Put(bytes.NewReader([]byte(fmt.Sprintf("Wärmemengenzähler Ablesung Nummer %d", i))))
		if err != nil {
			t.Fatal(err)
		}
		batch = append(batch, store.NewMessage{Link: store.LinkFlags{UID: uint32(i)}, MessageID: fmt.Sprintf("m%d@example.org", i),
			Subject: fmt.Sprintf("Ablesung %d", i), From: "A <a@example.org>", To: json.RawMessage(`[]`), Size: 10,
			InternalDate: time.Date(2026, 1, i%28+1, 0, 0, 0, 0, time.UTC), ContentHash: fmt.Sprintf("h%d", i), TextPlain: "Anbei die Ablesung.",
			HasAttachments: true, Attachments: []store.NewAttachment{{PartID: "2", Filename: "ablesung.txt", ContentType: "text/plain",
				DeclaredType: "text/plain", Size: info.Size, SHA256: info.SHA256, BlobSize: info.Size, ExtractStatus: "pending"}}})
	}
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acct, MailboxID: boxes[0].ID, New: batch}); err != nil {
		t.Fatal(err)
	}
	return &fixture{st: st, blobs: b, acct: acct}
}

func (f *fixture) runner() *Runner {
	return &Runner{Store: f.st, Blobs: f.blobs, Extractor: extract.New(extract.Options{}), Batch: 7}
}

func (f *fixture) hits(t *testing.T, q string) int {
	t.Helper()
	parsed, err := search.Parse(q, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	res, err := f.st.Search(context.Background(), store.SearchParams{Query: parsed, AccountID: f.acct, Limit: 100, Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return len(res.Hits)
}

func TestRunAccountExtractsThenIndexes(t *testing.T) {
	f := newFixture(t, 30)
	ctx := context.Background()
	var events []Event
	r := f.runner()
	r.Progress = func(ev Event) { events = append(events, ev) }
	rep, err := r.RunAccount(ctx, f.acct)
	if err != nil || rep.Extracted != 30 || rep.Indexed != 30 || rep.IndexPending != 0 {
		t.Fatalf("report = %+v %v", rep, err)
	}
	// Text der Anhänge ist im selben Lauf durchsuchbar (Teilwort im Kompositum).
	if n := f.hits(t, "mengenzähler"); n != 30 {
		t.Fatalf("attachment text hits = %d", n)
	}
	if n := f.hits(t, `"Nummer 17"`); n != 1 {
		t.Fatalf("phrase in attachment = %d", n)
	}
	if len(events) == 0 || events[len(events)-1].Kind != "index" || events[len(events)-1].Done != 30 {
		t.Fatalf("events = %+v", events)
	}
	jobs, _ := f.st.OpenJobs(ctx)
	if len(jobs) != 0 {
		t.Fatalf("open jobs after the run = %+v", jobs)
	}
	// Nichts mehr zu tun: ein zweiter Lauf ist leer.
	if rep, err := f.runner().RunAccount(ctx, f.acct); err != nil || rep != (Report{}) {
		t.Fatalf("second run = %+v %v", rep, err)
	}
}

func TestExtractBudgetLeavesJobQueued(t *testing.T) {
	f := newFixture(t, 5)
	ctx := context.Background()
	r := f.runner()
	r.ExtractBudget = time.Nanosecond
	rep, err := r.RunAccount(ctx, f.acct)
	if err != nil || rep.Extracted != 1 {
		t.Fatalf("report = %+v %v", rep, err)
	}
	jobs, _ := f.st.OpenJobs(ctx)
	if len(jobs) != 1 || jobs[0].Type != store.JobExtract || jobs[0].State != "queued" {
		t.Fatalf("jobs = %+v", jobs)
	}
	// Der nächste Lauf macht weiter.
	if rep, err := f.runner().RunAccount(ctx, f.acct); err != nil || rep.Extracted != 4 {
		t.Fatalf("next run = %+v %v", rep, err)
	}
}

// Abbruch (kill -9 eines anderen Prozesses): Der Job bleibt „running" mit
// fremder Sperre. Ohne Kontosperre wartet ein Arbeiter die Sperre ab, mit
// Kontosperre (TakeOver) setzt er sofort fort; Doppeltes entsteht nicht.
func TestResumeAfterCrashedWorker(t *testing.T) {
	f := newFixture(t, 20)
	ctx := context.Background()
	r := f.runner()
	r.Extractor = nil
	// Ein „abgestürzter" Arbeiter hat den index-Job und 7 Nachrichten erledigt.
	j, ok, err := f.st.ClaimJob(ctx, store.JobIndex, f.acct, "crashed", time.Hour, false)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if n, err := f.st.IndexBatch(ctx, f.acct, 7, search.NewStemmer().Build); err != nil || n != 7 {
		t.Fatal(n, err)
	}
	_ = j
	if rep, _ := r.RunAccount(ctx, f.acct); rep.Indexed != 0 || rep.IndexPending != 13 {
		t.Fatalf("run without take-over = %+v", rep)
	}
	r.TakeOver = true
	if rep, err := r.RunAccount(ctx, f.acct); err != nil || rep.Indexed != 13 || rep.IndexPending != 0 {
		t.Fatalf("run with take-over = %+v %v", rep, err)
	}
	if n := f.hits(t, "Ablesung"); n != 20 {
		t.Fatalf("hits = %d", n)
	}
}

func TestCancelReleasesJob(t *testing.T) {
	f := newFixture(t, 3)
	ctx, cancel := context.WithCancel(context.Background())
	r := f.runner()
	r.Extractor = nil
	r.Progress = func(Event) { cancel() } // nach der ersten Transaktion abbrechen
	r.Batch = 1
	_, err := r.RunAccount(ctx, f.acct)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	jobs, _ := f.st.OpenJobs(context.Background())
	if len(jobs) != 2 || jobs[0].Type != store.JobIndex || jobs[0].State != "queued" {
		t.Fatalf("jobs after cancel = %+v", jobs)
	}
	r2 := f.runner()
	r2.Extractor = nil
	if rep, err := r2.RunAccount(context.Background(), f.acct); err != nil || rep.Indexed != 2 {
		t.Fatalf("resume = %+v %v", rep, err)
	}
}

// PDFs, die ohne pdftotext übersprungen wurden, kommen wieder dran, sobald
// das Werkzeug gefunden wird.
func TestSkippedPDFsAreRequeuedWhenToolAppears(t *testing.T) {
	f := newFixture(t, 2)
	ctx := context.Background()
	if _, err := f.runner().RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	db := f.st.DBForTest()
	if _, err := db.Exec(`UPDATE attachments SET extract_status = 'skipped', extract_error = ?, extracted_text = NULL,
		content_type = 'application/pdf' WHERE id = (SELECT min(id) FROM attachments)`, extract.ReasonNoPDFTool); err != nil {
		t.Fatal(err)
	}
	n, err := f.st.RequeueSkipped(ctx, f.acct, extract.ReasonNoPDFTool)
	if err != nil || n != 1 {
		t.Fatalf("requeue = %d %v", n, err)
	}
	jobs, _ := f.st.OpenJobs(ctx)
	if !slices.ContainsFunc(jobs, func(j store.JobInfo) bool { return j.Type == store.JobExtract && j.State == "queued" }) {
		t.Fatalf("no extract job after requeue: %+v", jobs)
	}
	if extract.New(extract.Options{}).PDFTool() == "" {
		t.Skip("pdftotext not installed; the run itself is not tested here")
	}
	if _, err := db.Exec(`UPDATE attachments SET extract_status = 'skipped', extract_error = ? WHERE extract_status = 'pending'`,
		extract.ReasonNoPDFTool); err != nil {
		t.Fatal(err)
	}
	rep, err := f.runner().RunAccount(ctx, f.acct)
	if err != nil || rep.Extracted != 1 {
		t.Fatalf("run = %+v %v", rep, err)
	}
	var status string
	_ = db.QueryRow(`SELECT extract_status FROM attachments WHERE id = (SELECT min(id) FROM attachments)`).Scan(&status)
	if status == "skipped" || status == "pending" {
		t.Fatalf("status = %s", status)
	}
}
