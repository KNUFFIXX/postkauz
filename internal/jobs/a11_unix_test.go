//go:build unix

package jobs

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
)

// A11 Teil 3: vorübergehende Fehler des Textauszugs werden wiederholt
// (Wartezeit 10 Minuten, höchstens fünf Versuche); endgültige nicht.

// pdfFixture: n PDF-Anhänge; ext ist der Extraktor (PDFToText nach Wahl).
func pdfFixture(t *testing.T, n int) *fixture {
	t.Helper()
	f := newFixture(t, n)
	if _, err := f.st.DBForTest().Exec(`UPDATE attachments SET content_type = 'application/pdf', filename = 'geheim-name.pdf'`); err != nil {
		t.Fatal(err)
	}
	return f
}

type a11Clock struct{ now time.Time }

func (c *a11Clock) Now() time.Time { return c.now }

func (f *fixture) a11Runner(o extract.Options, clk *a11Clock, log *bytes.Buffer) *Runner {
	r := &Runner{Store: f.st, Blobs: f.blobs, Extractor: extract.New(o), Batch: 7, ExtractBudget: NoExtractBudget, Now: clk.Now}
	if log != nil {
		r.Log = slog.New(slog.NewTextHandler(log, nil))
	}
	return r
}

func (f *fixture) status(t *testing.T) (status, errText string) {
	t.Helper()
	var e *string
	if err := f.st.DBForTest().QueryRow(`SELECT extract_status, extract_error FROM attachments ORDER BY id LIMIT 1`).Scan(&status, &e); err != nil {
		t.Fatal(err)
	}
	if e != nil {
		errText = *e
	}
	return status, errText
}

// Start scheitert (Programm nach PDFTool nicht startbar): pending mit
// Zähler, Wartezeit eingehalten, nach fünf Versuchen failed mit Zahl.
func TestA11ExtractStartFailsRetried(t *testing.T) {
	f := pdfFixture(t, 1)
	ctx := context.Background()
	clk := &a11Clock{now: time.Date(2026, 11, 23, 8, 0, 0, 0, time.UTC)}
	var log bytes.Buffer
	r := f.a11Runner(extract.Options{PDFToText: "/nonexistent/a11/pdftotext"}, clk, &log)
	for attempt := 1; attempt <= 4; attempt++ {
		if _, err := r.RunAccount(ctx, f.acct); err != nil {
			t.Fatal(err)
		}
		st, _ := f.status(t)
		ret, ok, _ := f.st.ExtractRetryOf(ctx, 1)
		if st != "pending" || !ok || ret.N != attempt || ret.Kind != extract.ErrKindStart {
			t.Fatalf("attempt %d: %s %+v %v", attempt, st, ret, ok)
		}
		// Innerhalb der Wartezeit: kein neuer Versuch.
		clk.now = clk.now.Add(9 * time.Minute)
		if _, err := r.RunAccount(ctx, f.acct); err != nil {
			t.Fatal(err)
		}
		if ret2, _, _ := f.st.ExtractRetryOf(ctx, 1); ret2.N != attempt {
			t.Fatalf("retried within the wait: %d", ret2.N)
		}
		// Der Job bleibt fällig, ExtractPending zählt den Anhang.
		if n, _ := f.st.ExtractPending(ctx, f.acct); n != 1 {
			t.Fatalf("pending = %d", n)
		}
		clk.now = clk.now.Add(2 * time.Minute)
	}
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	st, e := f.status(t)
	if st != "failed" || !strings.Contains(e, "(5 attempts)") || !extract.IsTransientReason(e) {
		t.Fatalf("after five: %s %q", st, e)
	}
	if _, ok, _ := f.st.ExtractRetryOf(ctx, 1); ok {
		t.Fatal("retry note left after the final result")
	}
	l := log.String()
	if !strings.Contains(l, "text extraction of an attachment failed") || !strings.Contains(l, "transient=true") ||
		!strings.Contains(l, "attachment_id=1") || !strings.Contains(l, "message_id=1") || !strings.Contains(l, "attempt=5") {
		t.Fatalf("log:\n%s", l)
	}
	if strings.Contains(l, "geheim-name") || strings.Contains(l, "Wärmemengenzähler") {
		t.Fatalf("log names the file or its content:\n%s", l)
	}
}

// Exit-Code 1 mit Meldung: sofort failed; Zeitüberschreitung: failed;
// Signal: vorübergehend.
func TestA11ExtractPermanentAndSignal(t *testing.T) {
	for _, c := range []struct {
		name    string
		args    []string
		timeout time.Duration
		want    string
		kind    string
	}{
		{"exit 1", []string{"-c", "echo 'Syntax Error: kaputt' >&2; exit 1"}, 0, "failed", extract.ErrKindExit},
		{"timeout", []string{"-c", "sleep 5"}, 300 * time.Millisecond, "failed", extract.ErrKindTimeout},
		{"signal", []string{"-c", "kill -9 $$"}, 0, "pending", extract.ErrKindSignal},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := pdfFixture(t, 1)
			ctx := context.Background()
			clk := &a11Clock{now: time.Now()}
			var log bytes.Buffer
			r := f.a11Runner(extract.Options{PDFToText: "/bin/sh", PDFArgs: c.args, Timeout: c.timeout}, clk, &log)
			if _, err := r.RunAccount(ctx, f.acct); err != nil {
				t.Fatal(err)
			}
			st, e := f.status(t)
			if st != c.want {
				t.Fatalf("status = %s %q", st, e)
			}
			if !strings.Contains(log.String(), "error_kind="+c.kind) {
				t.Fatalf("log:\n%s", log.String())
			}
			if c.name == "exit 1" && (strings.Contains(e, "attempts") || extract.IsTransientReason(e)) {
				t.Fatalf("permanent error = %q", e)
			}
		})
	}
}

// 25 Anhänge in der Wartezeit: Der Lauf endet sofort, auch ohne Budget.
func TestA11ExtractWaitingDoesNotLoop(t *testing.T) {
	f := pdfFixture(t, 25)
	ctx := context.Background()
	clk := &a11Clock{now: time.Now()}
	r := f.a11Runner(extract.Options{PDFToText: "/nonexistent/a11/pdftotext"}, clk, nil)
	if _, err := r.RunAccount(ctx, f.acct); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.st.ExtractPending(ctx, f.acct); n != 25 {
		t.Fatalf("pending = %d", n)
	}
	done := make(chan error, 1)
	go func() {
		_, err := r.RunAccount(ctx, f.acct)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunAccount loops over attachments in their wait time")
	}
	if items, _ := f.st.PendingExtractionsAt(ctx, f.acct, 100, clk.now); len(items) != 0 {
		t.Fatalf("waiting attachments returned: %d", len(items))
	}
	if items, _ := f.st.PendingExtractionsAt(ctx, f.acct, 100, clk.now.Add(11*time.Minute)); len(items) != 25 {
		t.Fatalf("after the wait: %d", len(items))
	}
}

// Bestand: einmal je Konto werden nur failed mit vorübergehendem Grund
// wieder pending.
func TestA11RequeueTransientOnce(t *testing.T) {
	f := newFixture(t, 6)
	ctx := context.Background()
	db := f.st.DBForTest()
	for id, e := range map[int]string{
		1: "extractor failed: exit status 0xc0000142",
		2: "extractor failed: fork/exec /usr/bin/pdftotext: permission denied",
		3: `extractor failed: exec: "pdftotext": executable file not found in $PATH`,
		4: "extractor failed: signal: killed",
		5: "extractor failed: Syntax Error: Couldn't find trailer dictionary",
		6: "extract: timed out after 30s (process killed)",
	} {
		if _, err := db.Exec(`UPDATE attachments SET extract_status = 'failed', extract_error = ? WHERE id = ?`, e, id); err != nil {
			t.Fatal(err)
		}
	}
	n, ran, err := f.st.RequeueTransientFailed(ctx, f.acct)
	if err != nil || !ran || n != 4 {
		t.Fatalf("requeue = %d %v %v", n, ran, err)
	}
	var pending int
	_ = db.QueryRow(`SELECT count(*) FROM attachments WHERE extract_status = 'pending' AND id <= 4`).Scan(&pending)
	if pending != 4 {
		t.Fatalf("pending = %d", pending)
	}
	if _, err := db.Exec(`UPDATE attachments SET extract_status = 'failed', extract_error = 'extractor failed: signal: killed'`); err != nil {
		t.Fatal(err)
	}
	if n, ran, err := f.st.RequeueTransientFailed(ctx, f.acct); err != nil || ran || n != 0 {
		t.Fatalf("second requeue = %d %v %v", n, ran, err)
	}
}
