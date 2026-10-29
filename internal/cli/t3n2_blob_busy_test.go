package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/KNUFFIXX/postkauz/internal/blob"
)

// Nachbesserung 2, Punkt 8: Die belegte Sperre des Blob-Store hat einen
// eigenen Code blob_busy („try again“), auch wenn zuletzt das Öffnen der
// Sperrdatei scheiterte und der Fehler eingewickelt ankommt; der Code steht
// in der Liste von docs/cli.md.
func TestT3N2BlobBusyErrorCode(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{LockWait: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, blob.LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	_, putErr := bs.Put(strings.NewReader("x"))
	for _, e := range []error{blob.ErrBusy, fmt.Errorf("drafts.create: %w", blob.ErrBusy), putErr} {
		exit, info := classify(e)
		if exit != ExitError || info.Code != "blob_busy" || !strings.Contains(info.Hint, "again") {
			t.Errorf("classify(%v) = %d %+v, want %d blob_busy with a hint to try again", e, exit, info, ExitError)
		}
	}
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	line := regexp.MustCompile("Codes: (`[a-z_]+`(?:, `[a-z_]+`)*)\\.").FindString(string(b))
	if !strings.Contains(line, "`blob_busy`") {
		t.Fatal("error code blob_busy is not listed in docs/cli.md")
	}
}
