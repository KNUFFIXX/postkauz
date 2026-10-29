package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/blob"
)

// Nachbesserung 2, Punkt 8: Eine belegte Sperre des Blob-Store kommt beim
// Agenten als Code blob_busy mit der Bitte, es erneut zu versuchen, nicht
// als unbekannter Fehler; ohne den rohen Text (Pfade). docs/mcp.md nennt
// den Code.
func TestT3N2BlobBusyToolError(t *testing.T) {
	err := fmt.Errorf("%w (last error: %w)", blob.ErrBusy, fmt.Errorf("lock /data/KANARIE/gc.lock: is a directory"))
	te := classify(fmt.Errorf("drafts.create: %w", err))
	if te.Status != "error" || te.Code != "blob_busy" || !strings.Contains(te.Message, "try again") || strings.Contains(te.Message, "KANARIE") {
		t.Fatalf("classify(blob busy) = %+v", te)
	}
	b, rerr := os.ReadFile(filepath.Join("..", "..", "docs", "mcp.md"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(b), "`blob_busy`") {
		t.Fatal("docs/mcp.md does not list the code blob_busy")
	}
}
