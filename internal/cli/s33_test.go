package cli

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/config"
	"github.com/KNUFFIXX/postkauz/internal/corpus"
	"github.com/KNUFFIXX/postkauz/internal/importer"
	"github.com/KNUFFIXX/postkauz/internal/mailsync"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// addTestAccount legt ein Konto ohne erreichbaren Server an.
func addTestAccount(t *testing.T, name string) {
	t.Helper()
	t.Setenv("POSTKAUZ_PASSWORD_"+strings.ToUpper(name), "x")
	if code, _, errs := run(t, "account", "add", name, "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none",
		"--insecure-test-server", "--password-from-env"); code != ExitOK {
		t.Fatalf("account add %s: %s", name, errs)
	}
}

// openTestData öffnet Datenbank und Blob-Store des Testverzeichnisses.
func openTestData(t *testing.T) (*store.SQLite, *blob.Store) {
	t.Helper()
	_, p, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(p.Database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	bs, err := blob.Open(p.Blobs, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return st, bs
}

// seedCorpus spiegelt den Korpus in das Konto, wie ein Sync es täte: je
// Korpus-Ordner ein Serverordner, Rohmail im Blob-Store, content_hash
// exakt. Liefert die Zahl der Nachrichten.
func seedCorpus(t *testing.T, st *store.SQLite, bs *blob.Store, account string, c *corpus.Corpus) int {
	t.Helper()
	ctx := context.Background()
	acc, err := st.AccountByName(ctx, account)
	if err != nil {
		t.Fatal(err)
	}
	var specs []store.MailboxSpec
	byFolder := map[string][]int{}
	for i, m := range c.Manifest.Messages {
		if _, ok := byFolder[m.Folder]; !ok {
			specs = append(specs, store.MailboxSpec{Path: m.Folder, Name: filepath.Base(m.Folder), Role: "custom"})
		}
		byFolder[m.Folder] = append(byFolder[m.Folder], i)
	}
	if _, _, err := st.SyncMailboxes(ctx, acc.ID, specs); err != nil {
		t.Fatal(err)
	}
	boxes, err := st.Mailboxes(ctx, acc.ID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, b := range boxes {
		var batch []store.NewMessage
		for k, i := range byFolder[b.Path] {
			raw, m := c.Raw[i], c.Manifest.Messages[i]
			info, err := bs.Put(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			hash, err := mailsync.ContentHash(bytes.NewReader(raw))
			if err != nil {
				t.Fatal(err)
			}
			idate, _ := time.Parse(time.RFC3339, m.InternalDate)
			batch = append(batch, store.NewMessage{Link: store.LinkFlags{UID: uint32(k + 1)}, MessageID: m.MessageID,
				From: m.FromAddr, Size: info.Size, BlobSHA256: info.SHA256, BlobSize: info.Size, BlobCompressed: info.Compressed,
				ContentHash: hash, InternalDate: idate})
		}
		if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acc.ID, MailboxID: b.ID, New: batch}); err != nil {
			t.Fatal(err)
		}
		n += len(batch)
	}
	return n
}

// contentHashes liefert die content_hash eines Kontos, sortiert.
func contentHashes(t *testing.T, st *store.SQLite, account string) []string {
	t.Helper()
	rows, err := st.DBForTest().Query(`SELECT m.content_hash FROM messages m JOIN accounts a ON a.id = m.account_id
		WHERE a.name = ? ORDER BY m.content_hash`, account)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		out = append(out, h)
	}
	return out
}

// Akzeptanz S3.3 Punkte 1 und 2: Der Korpus (1.000 Nachrichten) als mbox
// exportiert und in ein frisches Konto importiert ergibt jede Nachricht mit
// demselben content_hash (new: 1000), ein zweiter Import new: 0; als EML
// exportiert ist jede Datei Byte für Byte der Blob.
func TestExportCorpusRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full corpus (1,000 messages); skipped with -short")
	}
	setupEnv(t)
	addTestAccount(t, "quelle")
	addTestAccount(t, "ziel")
	c, err := corpus.Generate(corpus.Options{})
	if err != nil {
		t.Fatal(err)
	}
	st, bs := openTestData(t)
	if n := seedCorpus(t, st, bs, "quelle", c); n != 1000 {
		t.Fatalf("seeded %d messages", n)
	}
	src := contentHashes(t, st, "quelle")

	out := filepath.Join(t.TempDir(), "mbox")
	code, stdout, errs := run(t, "export", "mbox", "--to", out, "--account", "quelle", "--json")
	var ex ops.MessagesExportOutput
	if code != ExitOK || json.Unmarshal([]byte(stdout), &ex) != nil {
		t.Fatalf("export mbox: %d %s %s", code, stdout, errs)
	}
	if ex.Messages != 1000 || ex.Skipped != 0 || ex.NewlineAdded != 0 || len(ex.Folders) != 4 {
		t.Fatalf("export = %+v", ex)
	}
	// Thunderbird-Ablage: Archiv/2024 liegt in Archiv.sbd/2024.
	if _, err := os.Stat(filepath.Join(out, "Archiv.sbd", "2024")); err != nil {
		t.Fatal(err)
	}
	code, stdout, errs = run(t, "import", "mbox", out, "--account", "ziel", "--no-extract", "--json")
	var im ops.ImportOutput
	if code != ExitOK || json.Unmarshal([]byte(stdout), &im) != nil || im.New != 1000 || im.Errors != 0 || im.Skipped != 0 {
		t.Fatalf("import: %d %+v %s", code, im, errs)
	}
	if dst := contentHashes(t, st, "ziel"); !slices.Equal(src, dst) {
		t.Fatalf("content_hash differs after the round trip: %d vs %d messages", len(src), len(dst))
	}
	// Die Ordner kommen unter ihrem Namen zurück.
	folders, _, err := importer.Discover(out, importer.FormatMbox, "", store.ImportContent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range folders {
		names = append(names, f.Folder)
	}
	if !slices.Equal(names, []string{"Archiv/2024", "Archiv/2025", "Drafts", "INBOX"}) {
		t.Fatalf("import folders = %v", names)
	}
	code, stdout, errs = run(t, "import", "mbox", out, "--account", "ziel", "--rescan", "--no-extract", "--json")
	im = ops.ImportOutput{}
	if code != ExitOK || json.Unmarshal([]byte(stdout), &im) != nil || im.New != 0 || im.Existing != 1000 {
		t.Fatalf("second import: %d %+v %s", code, im, errs)
	}

	// EML: jede Datei ist Byte für Byte der Blob; index.csv nennt sie.
	emlDir := filepath.Join(t.TempDir(), "eml")
	if code, stdout, errs := run(t, "export", "eml", "--to", emlDir, "-a", "quelle"); code != ExitOK ||
		!strings.Contains(stdout, "messages: 1000, skipped: 0") || !strings.Contains(errs, "INBOX: ") {
		t.Fatalf("export eml: %d %s %s", code, stdout, errs)
	}
	f, err := os.Open(filepath.Join(emlDir, "index.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil || len(rows) != 1001 {
		t.Fatalf("index.csv: %d rows, %v", len(rows), err)
	}
	for _, r := range rows[1:] {
		got, err := os.ReadFile(filepath.Join(emlDir, filepath.FromSlash(r[6])))
		if err != nil {
			t.Fatal(err)
		}
		rc, err := bs.Get(r[5])
		if err != nil {
			t.Fatal(err)
		}
		want, _ := io.ReadAll(rc)
		_ = rc.Close()
		if !bytes.Equal(got, want) {
			t.Fatalf("%s differs from blob %s", r[6], r[5])
		}
	}
}

// CLI attach save und export: Ausgabe, Fehlercodes, Ablehnung mit
// Schlüssel (export nur für Menschen, attach save nur mit mail:export).
func TestAttachSaveAndExportCommands(t *testing.T) {
	dataDir, _ := setupEnv(t)
	addTestAccount(t, "demo")
	st, bs := openTestData(t)
	ctx := context.Background()
	acc, _ := st.AccountByName(ctx, "demo")
	if _, _, err := st.SyncMailboxes(ctx, acc.ID, []store.MailboxSpec{{Path: "INBOX", Name: "INBOX", Role: "inbox"}}); err != nil {
		t.Fatal(err)
	}
	boxes, _ := st.Mailboxes(ctx, acc.ID)
	raw := []byte("Subject: x\r\n\r\nx\r\n")
	msg, _ := bs.Put(bytes.NewReader(raw))
	att, _ := bs.Put(bytes.NewReader(append([]byte("MZ\x90\x00"), make([]byte, 64)...)))
	if _, err := st.ApplyBatch(ctx, store.MirrorBatch{AccountID: acc.ID, MailboxID: boxes[0].ID, New: []store.NewMessage{{
		Link: store.LinkFlags{UID: 1}, MessageID: "x@example.org", Size: msg.Size, BlobSHA256: msg.SHA256, BlobSize: msg.Size,
		Attachments: []store.NewAttachment{{PartID: "2", Filename: "Rechnung.pdf", ContentType: "application/x-msdownload",
			DeclaredType: "application/pdf", Size: att.Size, SHA256: att.SHA256, BlobSize: att.Size, Executable: true}}}}}); err != nil {
		t.Fatal(err)
	}
	atts, _ := st.AttachmentsByMessage(ctx, 1)
	id := strconv.FormatInt(atts[0].ID, 10)

	code, out, errs := run(t, "attach", "save", id, "--subdir", "belege")
	if code != ExitOK || !strings.Contains(out, "as belege/Rechnung.pdf.blocked below export_dir") || !strings.Contains(out, "warning: an executable type") {
		t.Fatalf("attach save: %d %s %s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "export", "belege", "Rechnung.pdf.blocked")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = run(t, "attach", "save", id, "--subdir", "belege", "--dry-run")
	if code != ExitOK || !strings.Contains(out, "dry run: would save attachment") || !strings.Contains(out, "Rechnung-2.pdf.blocked") {
		t.Fatalf("dry run: %d %s", code, out)
	}
	if code, _, errs := run(t, "attach", "save", id, "--subdir", "../raus"); code != ExitDenied || !strings.Contains(errs, "outside_export_dir") {
		t.Fatalf("outside: %d %s", code, errs)
	}
	if code, out, _ := run(t, "doctor"); code != ExitOK || !strings.Contains(out, "export:    "+filepath.Join(dataDir, "export")) {
		t.Fatalf("doctor: %s", out)
	}
	// Schlüssel: reader ohne mail:export, operator mit; export nie.
	for _, role := range []string{"reader", "operator", "admin"} {
		if code, _, _ := run(t, "key", "create", "--name", role+"-k", "--role", role); code != ExitOK {
			t.Fatal("key create")
		}
	}
	if code, _, _ := run(t, "attach", "save", id, "--key", "reader-k"); code != ExitDenied {
		t.Fatalf("reader: %d", code)
	}
	if code, out, errs := run(t, "attach", "save", id, "--key", "operator-k", "--dry-run=false"); code != ExitOK || !strings.Contains(out, "as Rechnung.pdf.blocked below") {
		t.Fatalf("operator: %d %s %s", code, out, errs)
	}
	target := filepath.Join(t.TempDir(), "x")
	if code, _, errs := run(t, "export", "eml", "--to", target, "--key", "admin-k"); code != ExitDenied || !strings.Contains(errs, "human_only") {
		t.Fatalf("export with a key: %d %s", code, errs)
	}
	// Eingabefehler und ein Ziel mit Inhalt.
	for _, args := range [][]string{{"export", "pst", "--to", target}, {"export", "mbox"}, {"export", "mbox", "--to", target, "--mailbox", ""}} {
		if code, _, errs := run(t, args...); code != ExitUsage {
			t.Errorf("%v: %d %s", args, code, errs)
		}
	}
	_ = os.MkdirAll(target, 0o700)
	_ = os.WriteFile(filepath.Join(target, "alt"), []byte("x"), 0o600)
	if code, _, errs := run(t, "export", "mbox", "--to", target); code != ExitDenied || !strings.Contains(errs, "target_not_fresh") {
		t.Fatalf("not fresh: %d %s", code, errs)
	}
	// Relativ zum aktuellen Ordner (nicht zu export_dir).
	wd := t.TempDir()
	t.Chdir(wd)
	if code, out, errs := run(t, "export", "mbox", "--to", "rel"); code != ExitOK || !strings.Contains(out, filepath.Join(wd, "rel")) {
		t.Fatalf("relative: %d %s %s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(wd, "rel", "INBOX")); err != nil {
		t.Fatal(err)
	}
}
