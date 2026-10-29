package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/KNUFFIXX/postkauz/internal/ops"
)

// T3 (E8): purge, blob gc und audit rotate nur für den Menschen: mit --key
// (auch admin, auch im Trockenlauf) Exit 3 human_only; ohne Schlüssel und
// ohne Bestand zählen sie nichts. Ohne Frist verlangt purge --older-than.
func TestRetentionCommandsHumanOnly(t *testing.T) {
	setupEnv(t)
	if code, out, errs := run(t, "key", "create", "--name", "adm", "--role", "admin", "--json"); code != ExitOK {
		t.Fatalf("key create: %d %s %s", code, out, errs)
	}
	for _, args := range [][]string{
		{"purge", "--older-than", "1d"},
		{"blob", "gc"},
		{"audit", "rotate"},
	} {
		for _, dry := range []bool{false, true} {
			a := append(append([]string{}, args...), "--key", "adm")
			if dry {
				a = append(a, "--dry-run")
			}
			if code, _, errs := run(t, a...); code != ExitDenied || !strings.Contains(errs, "human_only") {
				t.Fatalf("%v: %d %q", a, code, errs)
			}
		}
		if code, out, errs := run(t, append(args, "--dry-run")...); code != ExitOK || out == "" {
			t.Fatalf("%v --dry-run: %d %q %q", args, code, out, errs)
		}
	}
	if code, _, errs := run(t, "purge"); code != ExitUsage || !strings.Contains(errs, "no period") {
		t.Fatalf("purge without period: %d %q", code, errs)
	}
	if code, out, _ := run(t, "audit", "rotate"); code != ExitOK || !strings.Contains(out, "nothing to rotate") {
		t.Fatalf("audit rotate: %d %q", code, out)
	}
}

// Die Ausgaben, wie sie docs/betrieb.md (Abschnitt 5) zeigt.
func TestRenderRetention(t *testing.T) {
	var b bytes.Buffer
	_ = renderPurge(&b, ops.RetentionPurgeOutput{OlderThan: "180d", Cutoff: "2026-05-01T10:00:00Z", GoneFromServer: 120, KeptImported: 100,
		KeptDrafts: 1, KeptTagged: 2, Eligible: 17, DryRun: true})
	want := "all accounts, deleted on the server before 2026-05-01T10:00:00Z (older than 180d):\n  gone from the server: 120\n" +
		"  kept (imported archive): 100\n  kept (a draft refers to them): 1\n  kept (tagged): 2\n" +
		"dry run: 17 messages would be deleted from the local archive; nothing changed\n"
	if b.String() != want {
		t.Fatalf("purge dry run:\n%s", b.String())
	}
	b.Reset()
	_ = renderPurge(&b, ops.RetentionPurgeOutput{OlderThan: "180d", Cutoff: "x", Purged: 17, Attachments: 7, ThreadsRemoved: 12})
	if !strings.HasSuffix(b.String(), "deleted: 17 messages, 7 attachment entries, 12 threads that became empty\nthe stored files stay until: blob gc\n") {
		t.Fatalf("purge:\n%s", b.String())
	}
	b.Reset()
	_ = renderBlobGC(&b, ops.BlobsGCOutput{MinAge: "1d", Files: 52000, Rows: 51990, Referenced: 51975, Unreferenced: 30, KeptYoung: 2,
		RemovedFiles: 28, RemovedRows: 26, Bytes: 12_300_000, DryRun: true})
	want = "blob store: 52000 files, 51990 rows in table blobs, 51975 referenced\n  without reference: 30, of these younger than 1d (kept): 2\n" +
		"would delete: 28 files (12.3 MB), 26 rows\ndry run: nothing changed\n"
	if b.String() != want {
		t.Fatalf("blob gc dry run:\n%s", b.String())
	}
	b.Reset()
	_ = renderAuditRotate(&b, ops.AuditRotateOutput{Status: "would_rotate", Days: 120, FromDay: "2025-06-01", ToDay: "2025-09-28", Events: 8400,
		Archive: "/data/audit-archive-2025-06-01-2025-09-28.db"})
	if b.String() != "dry run: would move 120 days (2025-06-01 to 2025-09-28, 8400 entries) into /data/audit-archive-2025-06-01-2025-09-28.db; nothing changed\n" {
		t.Fatalf("audit rotate dry run:\n%s", b.String())
	}
}
