package cli

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// Kanarienwerte für doctor --blobs: Inhalt einer Rohmail und eines
// Dateianhangs; beide dürfen in keiner Ausgabe erscheinen.
const (
	blobCanaryMail   = "BLOBCANARY-mail-5c1e9a-never-shown"
	blobCanaryAttach = "BLOBCANARY-attach-77d02b-never-shown"
)

// blobFileWith sucht die Blob-Datei, deren Inhalt text enthält (Test
// liest unkomprimierte Blobs, wie der Sync sie ohne Option schreibt).
func blobFileWith(t *testing.T, root, text string) string {
	t.Helper()
	var found string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(text)) {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatalf("no blob with %q", text)
	}
	return found
}

func doctorBlobsJSON(t *testing.T, flag string) (int, DoctorBlobs, string) {
	t.Helper()
	code, out, errs := run(t, "doctor", "--json", flag)
	var rep DoctorReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil || rep.Blobs == nil {
		t.Fatalf("doctor --json %s: %d %v %s %s", flag, code, err, out, errs)
	}
	return code, *rep.Blobs, out + errs
}

// A03 A5.4 und A13.3: doctor --blobs prüft den Blob-Store gegen die
// Tabelle blobs und die Anhänge der Entwürfe, nennt nur Zahlen und
// Hashes und ändert nichts.
func TestDoctorBlobs(t *testing.T) {
	dataDir, _ := setupEnv(t)
	blobDir := filepath.Join(dataDir, "blobs")

	// Ohne Datenbank: nicht geprüft, nichts angelegt.
	if code, b, _ := doctorBlobsJSON(t, "--blobs"); code != ExitOK || b.Status != "not_checked" || b.Reason != "database missing" {
		t.Fatalf("no database: %d %+v", code, b)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Fatalf("doctor --blobs created the data directory: %v", err)
	}
	if code, out, errs := run(t, "doctor", "--blobs=foo"); code != ExitUsage || !strings.Contains(errs, "invalid value for --blobs") {
		t.Fatalf("--blobs=foo: %d %s %s", code, out, errs)
	}

	// Spiegel mit einer Nachricht und ein Entwurf mit Dateianhang (Blob
	// ohne Zeile in blobs).
	u := imapmemserver.NewUser("tester@example.invalid", canary)
	_ = u.Create("INBOX", nil)
	addr := serveMails(t, u, []string{"Message-ID: <blob@example.org>\r\nFrom: <a@example.org>\r\nTo: <b@example.org>\r\nSubject: Blob\r\n" +
		"Date: Mon, 02 Mar 2026 09:00:00 +0100\r\n\r\n" + blobCanaryMail + "\r\n"})
	if code, out, errs := runIn(t, canary+"\n", "account", "add", "demo", "--imap", addr, "--user", "tester@example.invalid",
		"--tls", "none", "--insecure-test-server", "--password-stdin"); code != ExitOK {
		t.Fatalf("account add: %d %s %s", code, out, errs)
	}
	if code, out, errs := run(t, "sync", "-a", "demo"); code != ExitOK {
		t.Fatalf("sync: %d %s %s", code, out, errs)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "attach"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "attach", "notiz.txt"), []byte(blobCanaryAttach), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, out, errs := run(t, "draft", "new", "-a", "demo", "--to", "b@example.org", "--subject", "Notiz", "--attach", "notiz.txt"); code != ExitOK {
		t.Fatalf("draft new: %d %s %s", code, out, errs)
	}
	mailFile := blobFileWith(t, blobDir, blobCanaryMail)
	attachFile := blobFileWith(t, blobDir, blobCanaryAttach)

	code, b, _ := doctorBlobsJSON(t, "--blobs=all")
	if code != ExitOK || b.Status != "ok" || b.Mode != "all" || b.Rows < 1 || b.Files != b.Rows+1 || b.Referenced != b.Files || b.Checked != b.Files || b.OK != b.Checked ||
		b.DraftAttachments != 1 || b.FilesWithoutRow != 0 || b.RowsWithoutFile != 0 || b.Problems != 0 || b.Examples != nil {
		t.Fatalf("clean, all: %d %+v", code, b)
	}
	rows := b.Rows
	code, out, errs := run(t, "doctor", "--blobs")
	if code != ExitOK || !strings.Contains(out, "blobs:\n  mode:    sample (1%), 1 of ") || !strings.Contains(out, "  result:  ok\n") {
		t.Fatalf("clean, sample: %d\n%s\n%s", code, out, errs)
	}

	// Verwaiste Datei, unbekannte Datei, Temporärdatei: nur Warnungen.
	bs, err := blob.Open(blobDir, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := bs.Put(strings.NewReader("verwaist"))
	if err != nil {
		t.Fatal(err)
	}
	unknown := filepath.Join(blobDir, "ab", "notes.txt")
	temp := filepath.Join(blobDir, "tmp", "put-123")
	for _, p := range []string{unknown, temp} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, b, _ = doctorBlobsJSON(t, "--blobs=all")
	if code != ExitOK || b.Status != "ok" || b.FilesWithoutRow != 1 || b.DraftAttachments != 1 || b.UnknownFiles != 1 || b.TempFiles != 1 ||
		b.Problems != 0 || len(b.Examples["files_without_row"]) != 1 || b.Examples["files_without_row"][0] != orphan.SHA256 {
		t.Fatalf("orphan: %d %+v", code, b)
	}

	// Beschädigter Blob (Rohmail überschrieben), Zeile ohne Datei,
	// Entwurfsanhang ohne Datei: Exit 1, je Art der Hash, nie Inhalt.
	if err := os.WriteFile(mailFile, []byte("kaputt "+blobCanaryMail), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(attachFile); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dataDir, "postkauz.db"))
	if err != nil {
		t.Fatal(err)
	}
	gone := strings.Repeat("0", 64)
	if err := st.ExecForTest(`INSERT INTO blobs(sha256, size, compressed, created_at) VALUES (?, 1, 0, 0)`, gone); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	mailSum := filepath.Base(mailFile)
	code, b, all := doctorBlobsJSON(t, "--blobs=all")
	if code != ExitError || b.Status != "problems" || b.Rows != rows+1 || b.Corrupt != 1 || b.RowsWithoutFile != 1 ||
		b.DraftAttachmentsWithoutFile != 1 || b.DraftAttachments != 0 || b.FilesWithoutRow != 1 || b.Problems != 3 ||
		b.Examples["corrupt"][0] != mailSum || b.Examples["rows_without_file"][0] != gone ||
		!strings.Contains(all, "blob check failed: 3 problems") {
		t.Fatalf("problems: %d %+v\n%s", code, b, all)
	}
	code, out, errs = run(t, "doctor", "--blobs=all")
	_, section, _ := strings.Cut(out, "blobs:")
	t.Logf("doctor --blobs=all:\nblobs:%s", section)
	if code != ExitError || !strings.Contains(out, "  corrupt: "+mailSum+"\n") || !strings.Contains(out, "  rows without file: "+gone+"\n") ||
		!strings.Contains(out, "  drafts:  1 draft attachment without file\n") || !strings.Contains(out, "  result:  3 problems\n") ||
		!strings.Contains(errs, "blob check failed: 3 problems") {
		t.Fatalf("problems, text: %d\n%s\n%s", code, out, errs)
	}
	for _, s := range []string{out + errs, all} {
		if strings.Contains(s, blobCanaryMail) || strings.Contains(s, blobCanaryAttach) || strings.Contains(s, "notiz.txt") || strings.Contains(s, "Subject") {
			t.Fatalf("content in output:\n%s", s)
		}
	}

	// doctor hat nichts geändert: Dateien und Zeilen bleiben.
	for _, p := range []string{unknown, temp, mailFile} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("doctor removed %s: %v", p, err)
		}
	}
	if !bs.Has(orphan.SHA256) {
		t.Fatal("doctor removed the orphan")
	}
}

func TestBlobSampleSize(t *testing.T) {
	for n, want := range map[int]int{0: 0, 1: 1, 99: 1, 100: 1, 250: 2, 50000: 500} {
		if got := blobSampleSize(n); got != want {
			t.Errorf("blobSampleSize(%d) = %d, want %d", n, got, want)
		}
	}
}
