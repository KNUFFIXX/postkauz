package cli

import (
	"context"
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
// Sperrdatei vorübergehend scheiterte und der Fehler eingewickelt ankommt;
// der Code steht in der Liste von docs/cli.md. Seit der dritten
// Nachbesserung (Punkt 8) ist ein dauerhafter Öffnungsfehler (hier gc.lock
// als Ordner) kein blob_busy mehr.
func TestT3N2BlobBusyErrorCode(t *testing.T) {
	root := t.TempDir()
	bs, err := blob.Open(root, blob.Options{LockWait: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	lock, err := bs.LockForGC(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_, putErr := bs.Put(strings.NewReader("x"))
	lock.Release()
	lastErr := fmt.Errorf("%w (last error: %w)", blob.ErrBusy, os.ErrPermission)
	for _, e := range []error{blob.ErrBusy, fmt.Errorf("drafts.create: %w", blob.ErrBusy), lastErr, putErr} {
		exit, info := classify(e)
		if exit != ExitError || info.Code != "blob_busy" || !strings.Contains(info.Hint, "again") {
			t.Errorf("classify(%v) = %d %+v, want %d blob_busy with a hint to try again", e, exit, info, ExitError)
		}
	}
	root2 := t.TempDir()
	bs2, err := blob.Open(root2, blob.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root2, blob.LockName), 0o700); err != nil {
		t.Fatal(err)
	}
	_, openErr := bs2.Put(strings.NewReader("x"))
	if _, info := classify(openErr); openErr == nil || info.Code == "blob_busy" {
		t.Errorf("classify(%v) = %+v, want no blob_busy for a permanent open error", openErr, info)
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
