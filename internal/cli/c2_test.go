package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// C2 (25.10.2026): sync nennt Nachrichten, deren Inhalt geladen wurde,
// obwohl sie lokal schon da waren (nur Zahl und Bytes); bei 0 keine Zeile
// und kein JSON-Feld.
func TestRenderSyncAlreadyKnownDownloaded(t *testing.T) {
	var b bytes.Buffer
	if err := renderSync(&b, ops.SyncOutput{Account: "demo", Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "already known") {
		t.Fatalf("line printed without downloads: %q", b.String())
	}
	b.Reset()
	o := ops.SyncOutput{Account: "demo", Mode: "incremental", AlreadyKnownDownloaded: 12, AlreadyKnownDownloadedBytes: 3_400_000}
	if err := renderSync(&b, o); err != nil {
		t.Fatal(err)
	}
	if want := "already known, downloaded anyway: 12 messages (3.4 MB)\n0 messages mirrored\n0 errors\n"; !strings.HasSuffix(b.String(), want) {
		t.Fatalf("sync output = %q, want suffix %q", b.String(), want)
	}
	j, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(j, []byte(`"already_known_downloaded":12,"already_known_downloaded_bytes":3400000`)) {
		t.Fatalf("json = %s", j)
	}
	j, _ = json.Marshal(ops.SyncOutput{})
	if bytes.Contains(j, []byte("already_known")) {
		t.Fatalf("json without downloads = %s", j)
	}
}
