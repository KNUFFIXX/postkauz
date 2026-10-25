package cli

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
	"github.com/KNUFFIXX/postkauz/internal/ops"
	"github.com/KNUFFIXX/postkauz/internal/store"
)

// interruptingImporter tut so, als drücke der Mensch während des Imports
// Strg+C: Er schickt sich selbst das Signal, wartet auf das Ende des
// Kontexts und liefert den Bericht bis dahin.
type interruptingImporter struct{}

func (interruptingImporter) Import(ctx context.Context, _ store.Account, _ ops.ImportOptions, _ func(ops.Progress)) (ops.ImportOutput, error) {
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(os.Interrupt)
	select {
	case <-ctx.Done():
	case <-time.After(10 * time.Second):
	}
	return ops.ImportOutput{New: 7, ExtractPending: 12, IndexPending: 3, Interrupted: true,
		Sources: []ops.ImportSource{{Path: "INBOX", Folder: "INBOX", Mode: "content", New: 7}}}, ctx.Err()
}

// Strg+C während import: Der Bericht bis dahin steht auf stdout (Text und
// --json), der Fehler auf stderr, Exit 1 mit Code interrupted (rc7/rc8).
func TestImportReportOnInterrupt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sends os.Interrupt to itself")
	}
	setupEnv(t)
	t.Setenv("POSTKAUZ_PASSWORD_DEMO", "x")
	if code, _, errs := run(t, "account", "add", "demo", "--imap", "127.0.0.1:1", "--user", "u", "--tls", "none",
		"--insecure-test-server", "--password-from-env"); code != ExitOK {
		t.Fatalf("account add: %s", errs)
	}
	old := newImporter
	newImporter = func(*store.SQLite, *blob.Store, *slog.Logger) ops.ImportRunner { return interruptingImporter{} }
	t.Cleanup(func() { newImporter = old })
	dir := t.TempDir()

	code, out, errs := run(t, "import", "mbox", dir, "-a", "demo")
	if code != ExitError || !strings.Contains(errs, "interrupted") || !strings.Contains(out, "new: 7,") ||
		!strings.Contains(out, "12 still pending") {
		t.Fatalf("text: code %d\nstdout %s\nstderr %s", code, out, errs)
	}
	code, out, errs = run(t, "import", "mbox", dir, "-a", "demo", "--json")
	var o ops.ImportOutput
	if code != ExitError || json.Unmarshal([]byte(out), &o) != nil || o.New != 7 || o.ExtractPending != 12 || !o.Interrupted ||
		!strings.Contains(errs, `"interrupted"`) {
		t.Fatalf("json: code %d\nstdout %s\nstderr %s", code, out, errs)
	}
}
