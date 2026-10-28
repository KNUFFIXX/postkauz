package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/spf13/cobra"

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
	// Toleranz bei der Größe (Restfall C2): eigene Zeile, eigenes Feld.
	b.Reset()
	o = ops.SyncOutput{Account: "demo", Mode: "incremental", LinkedDespiteSizeDifference: 3}
	if err := renderSync(&b, o); err != nil {
		t.Fatal(err)
	}
	if want := "\nlinked despite size difference: 3 messages\n0 messages mirrored\n0 errors\n"; !strings.HasSuffix(b.String(), want) {
		t.Fatalf("sync output = %q, want suffix %q", b.String(), want)
	}
	if j, _ := json.Marshal(o); !bytes.Contains(j, []byte(`"linked_despite_size_difference":3`)) {
		t.Fatalf("json = %s", j)
	}
	j, _ = json.Marshal(ops.SyncOutput{})
	if bytes.Contains(j, []byte("already_known")) || bytes.Contains(j, []byte("linked_despite")) {
		t.Fatalf("json without downloads = %s", j)
	}
}

// V6 der Nachbesserung T2: Postfach-Aktionen bekommen im Dauerbetrieb eine
// Zeile je Takt mit Ergebnis und im einmaligen Lauf eine in der
// Zusammenfassung.
func TestSyncShowsActionCounts(t *testing.T) {
	var b bytes.Buffer
	o := ops.SyncOutput{Account: "demo", Mode: "incremental", Actions: &ops.SyncActions{Actions: 2, OK: 3, Failed: 1, Stale: 1}}
	if err := renderSync(&b, o); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "actions: 3 done, 1 failed, 1 stale (2 actions; see action list)\n") {
		t.Fatalf("render: %s", b.String())
	}
	b.Reset()
	if err := renderSync(&b, ops.SyncOutput{Account: "demo", Mode: "incremental"}); err != nil || strings.Contains(b.String(), "actions:") {
		t.Fatalf("render without actions: %s %v", b.String(), err)
	}
	for _, watch := range []bool{true, false} {
		var out bytes.Buffer
		cmd := &cobra.Command{}
		cmd.Flags().Bool("watch", watch, "")
		cmd.Flags().Bool("json", false, "")
		p := syncProgress(cmd, &app{stdout: &out, stderr: io.Discard, log: slog.New(slog.NewTextHandler(io.Discard, nil))})
		p(ops.Progress{Event: "actions", Account: "privat", Done: 3, Total: 5, Message: "3 done, 1 failed, 1 stale (2 actions; see action list)"})
		if got := strings.Contains(out.String(), "  [privat] actions: 3 done, 1 failed, 1 stale (2 actions; see action list)\n"); got != watch {
			t.Fatalf("watch=%v: %q", watch, out.String())
		}
	}
}
