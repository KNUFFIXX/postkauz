package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/extract"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// A12 Teil 3 (a), (b), (d): doctor nennt das PDF-Programm aus [extract]
// (fehlende Datei: Warnung), die Zähler je Konto und notes; notes ändern
// den Exit-Code nicht.
func TestA12DoctorPDFAndNotes(t *testing.T) {
	extract.ResetProbesForTest()
	setupEnv(t)
	dir := t.TempDir()
	tool := filepath.Join(dir, "poppler", "pdftotext")
	cfg := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfg, []byte("[extract]\npdftotext = '"+tool+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("POSTKAUZ_CONFIG", cfg)
	addTestAccount(t, "demo")
	code, out, errs := run(t, "doctor", "--json")
	if code != ExitOK {
		t.Fatalf("doctor = %d %s", code, errs)
	}
	var rep struct {
		PDFToText struct {
			Path, Setting, Probe string
		} `json:"pdftotext"`
		Accounts []struct {
			Name               string  `json:"name"`
			ExtractFailed      *int    `json:"extract_failed"`
			ExtractExhausted   *int    `json:"extract_failed_exhausted"`
			ExtractWaiting     *int    `json:"extract_waiting"`
			ExtractSkippedTool *int    `json:"extract_skipped_tool"`
			GivenUp            []int64 `json:"draft_copies_given_up"`
		} `json:"accounts"`
		Warnings []string `json:"warnings"`
		Notes    []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.PDFToText.Path != tool || rep.PDFToText.Setting != "path" || rep.PDFToText.Probe != extract.ProbeNotRun {
		t.Fatalf("pdftotext = %+v", rep.PDFToText)
	}
	if !strings.Contains(strings.Join(rep.Warnings, "\n"), "pdftotext not found at the configured path") {
		t.Fatalf("warnings = %v", rep.Warnings)
	}
	a := rep.Accounts[0]
	if a.ExtractFailed == nil || a.ExtractExhausted == nil || a.ExtractWaiting == nil || a.ExtractSkippedTool == nil || a.GivenUp == nil {
		t.Fatalf("account fields missing: %s", out)
	}
	if rep.Notes == nil {
		t.Fatalf("notes missing: %s", out)
	}
	// Ein gescheiterter Anhang ist ein Hinweis (notes), kein Fehler.
	dataDir := os.Getenv("POSTKAUZ_DATA_DIR")
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	acc, _ := st.AccountByName(ctx, "demo")
	if _, _, err := st.SyncMailboxes(ctx, acc.ID, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acc.ID)
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acc.ID, MailboxID: boxes[0].ID, New: []store.NewMessage{{
		Link: store.LinkFlags{UID: 1}, MessageID: "a12@example.org", Size: 10, ContentHash: "h", CleanVersion: 1,
		Links: json.RawMessage("[]"), Attachments: []store.NewAttachment{{PartID: "2", Filename: "brief.pdf", ContentType: "application/pdf", Size: 5}}}},
		State: &store.MailboxState{UIDValidity: 1, UIDNext: 2, Phase: store.PhaseReady}}); err != nil {
		t.Fatal(err)
	}
	if err := st.ExecForTest(`UPDATE attachments SET extract_status = 'failed', extract_error = 'pdftotext version 4.06'`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	code, out, _ = run(t, "doctor")
	if code != ExitOK || !strings.Contains(out, "notes:") || !strings.Contains(out, "attach retry --account demo --all-failed") ||
		strings.Contains(out, "brief.pdf") {
		t.Fatalf("doctor (%d):\n%s", code, out)
	}
}

// attach retry ohne Auswahl: Fehler mit Hinweis.
func TestA12AttachRetryNeedsChoice(t *testing.T) {
	setupEnv(t)
	addTestAccount(t, "demo")
	code, _, errs := run(t, "attach", "retry", "-a", "demo")
	if code == ExitOK || !strings.Contains(errs, "--all-failed") {
		t.Fatalf("attach retry = %d %s", code, errs)
	}
}
