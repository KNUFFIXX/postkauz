package importer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// rc6: Der Import erledigt die Textextraktion der Anhänge vollständig (nicht
// nur 60 s wie der Sync); --no-extract lässt es beim Budget; ein Abbruch
// lässt den Rest pending. Synthetisch: Nachrichten mit Textanhängen.

func attachmentMbox(t testing.TB, n int) string {
	t.Helper()
	var raws [][]byte
	for i := range n {
		raws = append(raws, []byte(fmt.Sprintf("From: absender@example.org\r\nTo: empfaenger@example.org\r\nSubject: Anhang %d\r\n"+
			"Message-ID: <anhang-%d@example.org>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"g%d\"\r\n\r\n"+
			"--g%d\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nText %d\r\n"+
			"--g%d\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Disposition: attachment; filename=\"notiz-%d.txt\"\r\n\r\n"+
			"Inhalt des Anhangs %d\r\n--g%d--\r\n", i, i, i, i, i, i, i, i, i)))
	}
	dir := t.TempDir()
	writeTB(t, filepath.Join(dir, "INBOX"), raws)
	return dir
}

func (e *env) importRunner(t testing.TB) *Runner {
	return &Runner{Store: e.st, Blobs: e.blobs, LockDir: t.TempDir(), Extractor: extract.New(extract.Options{})}
}

func (e *env) extractPending(t testing.TB) int {
	t.Helper()
	n, err := e.st.ExtractPending(context.Background(), e.acct)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestImportExtractsAllAttachments(t *testing.T) {
	const n = 45 // mehr als ein Stapel der Extraktion (20)
	dir := attachmentMbox(t, n)
	e := newEnv(t)
	var events int
	out, err := e.importRunner(t).Import(context.Background(), store.Account{ID: e.acct, Name: "demo"},
		ops.ImportOptions{Format: FormatMbox, Path: dir}, func(p ops.Progress) {
			if p.Event == "extract" {
				events++
				if p.Total != n {
					t.Errorf("extract progress total = %d, want %d", p.Total, n)
				}
			}
		})
	if err != nil {
		t.Fatal(err)
	}
	if out.New != n || out.AttachmentsExtracted != n || out.ExtractPending != 0 || e.extractPending(t) != 0 || events == 0 {
		t.Fatalf("new %d, extracted %d, pending %d (store %d), progress events %d", out.New, out.AttachmentsExtracted,
			out.ExtractPending, e.extractPending(t), events)
	}
	if k := e.count(t, `SELECT count(*) FROM attachments WHERE extract_status = 'ok' AND extracted_text LIKE 'Inhalt des Anhangs%'`); k != n {
		t.Fatalf("attachments with text = %d, want %d", k, n)
	}
}

// --no-extract: nur das Budget wie beim Sync (hier winzig, also ein Anhang).
func TestImportNoExtractKeepsBudget(t *testing.T) {
	dir := attachmentMbox(t, 5)
	e := newEnv(t)
	r := e.importRunner(t)
	r.BudgetWithoutExtract = time.Nanosecond
	out, err := r.Import(context.Background(), store.Account{ID: e.acct, Name: "demo"},
		ops.ImportOptions{Format: FormatMbox, Path: dir, NoExtract: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.AttachmentsExtracted != 1 || out.ExtractPending != 4 || e.extractPending(t) != 4 {
		t.Fatalf("extracted %d, pending %d (store %d); want 1/4", out.AttachmentsExtracted, out.ExtractPending, e.extractPending(t))
	}
}

// Abbruch (Strg+C) während der Extraktion: Der Rest bleibt pending (nicht
// failed), der Import endet mit dem Abbruch.
func TestImportExtractInterrupted(t *testing.T) {
	dir := attachmentMbox(t, 45)
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := e.importRunner(t).Import(ctx, store.Account{ID: e.acct, Name: "demo"},
		ops.ImportOptions{Format: FormatMbox, Path: dir}, func(p ops.Progress) {
			if p.Event == "extract" {
				cancel()
			}
		})
	if !errors.Is(err, context.Canceled) && (err == nil || !strings.Contains(err.Error(), "canceled")) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if p := e.extractPending(t); p == 0 || p == 45 {
		t.Fatalf("pending after interrupt = %d, want some but not all", p)
	}
	if k := e.count(t, `SELECT count(*) FROM attachments WHERE extract_status = 'failed'`); k != 0 {
		t.Fatalf("%d attachments failed after interrupt", k)
	}
	// Bericht trotz Abbruch (rc7, W3): Der Index lief vor der Extraktion,
	// die neuen Nachrichten sind suchbar; extract_pending stimmt.
	if out.New != 45 || out.MessagesIndexed < 45 || out.ExtractPending != e.extractPending(t) || out.ExtractPending == 0 {
		t.Fatalf("report on interrupt: new %d indexed %d extract_pending %d (store %d)", out.New, out.MessagesIndexed,
			out.ExtractPending, e.extractPending(t))
	}
}

// Budget 0 heißt Standardbudget (60 s), nicht „sofort aufhören": Mit
// --no-extract und ohne eigenes Budget werden 5 Anhänge alle fertig.
func TestImportNoExtractDefaultBudget(t *testing.T) {
	dir := attachmentMbox(t, 5)
	e := newEnv(t)
	out, err := e.importRunner(t).Import(context.Background(), store.Account{ID: e.acct, Name: "demo"},
		ops.ImportOptions{Format: FormatMbox, Path: dir, NoExtract: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.AttachmentsExtracted != 5 || out.ExtractPending != 0 {
		t.Fatalf("extracted %d, pending %d; want 5/0", out.AttachmentsExtracted, out.ExtractPending)
	}
}
