package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// import mbox/maildir (S1.10): Zählzeile wie in A01 D3.4, zweiter Lauf
// new: 0, Trockenlauf, Eingabefehler, Anzeige importierter Ordner.
func TestImportCommand(t *testing.T) {
	setupEnv(t)
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", "x")
	if code, _, errs := run(t, "account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none",
		"--insecure-test-server", "--password-from-env"); code != ExitOK {
		t.Fatalf("account add: %s", errs)
	}
	dir := t.TempDir()
	md := filepath.Join(dir, "Mail")
	for _, sub := range []string{"cur", "new", "tmp", ".Projekte/cur", ".Projekte/new"} {
		if err := os.MkdirAll(filepath.Join(md, sub), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"cur/1.M1P1.x:2,S":           "Subject: eins\r\nMessage-ID: <eins@example.org>\r\n\r\nWartungsvertrag Heizung\r\n",
		"new/2.M2P1.x":               "Subject: zwei\r\nMessage-ID: <zwei@example.org>\r\n\r\nzwei\r\n",
		"cur/3.M3P1.x:2,ST":          "Subject: weg\r\nMessage-ID: <weg@example.org>\r\n\r\nweg\r\n",
		".Projekte/cur/4.M4P1.x:2,F": "Subject: eins\r\nMessage-ID: <eins@example.org>\r\n\r\nWartungsvertrag Heizung\r\n",
	}
	for p, c := range files {
		if err := os.WriteFile(filepath.Join(md, mdName(p)), []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	code, out, errs := run(t, "import", "maildir", md, "--account", "demo", "--mailbox", "Archive", "--dry-run")
	if code != ExitOK || !strings.Contains(out, "dry run: nothing was written") || !strings.Contains(out, "new: 2, existing: 1, skipped: 1, errors: 0") {
		t.Fatalf("dry run: %d %s %s", code, out, errs)
	}
	code, out, errs = run(t, "import", "maildir", md, "--account", "demo", "--mailbox", "Archive")
	if code != ExitOK || !strings.HasSuffix(out, "new: 2, existing: 1, skipped: 1, errors: 0\n") || !strings.Contains(out, "Archive/Projekte") {
		t.Fatalf("import: %d %s %s", code, out, errs)
	}
	code, out, _ = run(t, "import", "maildir", md, "-a", "demo", "--mailbox", "Archive")
	if code != ExitOK || !strings.Contains(out, "new: 0, existing: 0") || !strings.Contains(out, "2 sources unchanged") {
		t.Fatalf("second run: %s", out)
	}
	code, out, _ = run(t, "import", "maildir", md, "-a", "demo", "--mailbox", "Archive", "--rescan", "--json")
	var o ops.ImportOutput
	if code != ExitOK || json.Unmarshal([]byte(out), &o) != nil || o.New != 0 || o.Existing != 3 || o.Skipped != 1 || len(o.Sources) != 2 {
		t.Fatalf("rescan --json: %d %s", code, out)
	}

	// --labels-from mit Komma im Pfad: nur Zuordnung, nichts Neues.
	lab := filepath.Join(dir, "Labels, alt")
	if err := os.MkdirAll(filepath.Join(lab, "cur"), 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(lab, "cur", mdName("9.M9P1.x:2,S")), []byte(files["cur/1.M1P1.x:2,S"]), 0o600)
	_ = os.WriteFile(filepath.Join(lab, "cur", mdName("8.M8P1.x:2,S")), []byte("Subject: fremd\r\nMessage-ID: <fremd@example.org>\r\n\r\nx\r\n"), 0o600)
	code, out, _ = run(t, "import", "maildir", md, "-a", "demo", "--mailbox", "Archive", "--labels-from", lab)
	if code != ExitOK || !strings.Contains(out, "1 folder assignments added") || !strings.HasSuffix(out, "new: 0, existing: 1, skipped: 1, errors: 0\n") {
		t.Fatalf("labels: %s", out)
	}

	// Anzeige und Suche: importierte Ordner mit Rolle import.
	code, out, _ = run(t, "show", "eins@example.org")
	if code != ExitOK || !strings.Contains(out, "Archive (imported), Archive/Projekte (imported), INBOX (imported)") ||
		!strings.Contains(out, "not on the server (imported only)") {
		t.Fatalf("show: %s", out)
	}
	code, out, _ = run(t, "search", "Wartungsvertrag in:Archive/Projekte")
	if code != ExitOK || !strings.Contains(out, "eins") {
		t.Fatalf("search: %s", out)
	}

	// Eingabefehler: Exit 2.
	for _, args := range [][]string{{"import", "pst", md}, {"import", "mbox", filepath.Join(dir, "fehlt")},
		{"import", "mbox", md}, {"import", "maildir"}, {"import", "maildir", md, "--mailbox", ""}} {
		if code, _, errs := run(t, append(args, "-a", "demo")...); code != ExitUsage {
			t.Errorf("%v: code %d (%s)", args, code, errs)
		}
	}
	// Ein Schlüssel ohne admin darf nicht importieren.
	if code, _, _ := run(t, "key", "create", "--name", "leser", "--role", "reader"); code != ExitOK {
		t.Fatal("key create")
	}
	if code, _, _ := run(t, "import", "maildir", md, "-a", "demo", "--key", "leser"); code != ExitDenied {
		t.Fatalf("import with a reader key: code %d", code)
	}
}
